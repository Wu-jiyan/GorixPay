package auth

import (
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"

	epaysigning "gorixpay/internal/compat/epay/signing"
	"gorixpay/internal/identity/model"
	"gorixpay/internal/merchant/credential"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const merchantContextKey = "epay_merchant"

// ReplyMode selects how an authentication failure is written. Upstream never
// answers 4xx: mapi.php and api.php go through echojsonmsg (HTTP 200 with a JSON
// envelope), submit.php goes through sysmsg/exit (HTTP 200 HTML). SDKs in
// several languages abort on 4xx before reading the body, so the status has to
// stay 200.
type ReplyMode int

const (
	// ReplyJSON writes HTTP 200 with the epay JSON envelope.
	ReplyJSON ReplyMode = iota
	// ReplyHTML writes the sysmsg page used by submit.php.
	ReplyHTML
)

// Middleware verifies the V1 merchant signature for the JSON entrypoints
// (mapi.php, api.php, /api/pay/{query,close}). sign_type is optional and
// defaults to MD5; RSA uses the merchant's active RSA public key. The legacy
// entrypoints never require a timestamp, matching ApiHelper::api_verify.
func Middleware(db *gorm.DB, credentials *credential.Service) gin.HandlerFunc {
	return middlewareWith(db, credentials, ReplyJSON)
}

// HTMLMiddleware is the submit.php variant: failures render the upstream sysmsg
// page instead of JSON.
func HTMLMiddleware(db *gorm.DB, credentials *credential.Service) gin.HandlerFunc {
	return middlewareWith(db, credentials, ReplyHTML)
}

func middlewareWith(db *gorm.DB, credentials *credential.Service, mode ReplyMode) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := ctx.Request.ParseForm(); err != nil {
			reject(ctx, mode, -1, "参数解析失败")
			return
		}
		form := ctx.Request.Form
		if _, present := form["pid"]; !present {
			// Epay/mapi.php:2-5 answers -4 "未传入任何参数"; Epay/submit.php:2-6
			// exits with the "not configured" page before any other check.
			if mode == ReplyHTML {
				ctx.Abort()
				ctx.Header("Content-Type", "text/html; charset=UTF-8")
				ctx.String(http.StatusOK, "你还未配置支付接口商户！")
				return
			}
			reject(ctx, mode, -4, "未传入任何参数")
			return
		}
		uid, err := strconv.ParseInt(strings.TrimSpace(form.Get("pid")), 10, 64)
		if err != nil || uid <= 0 {
			// Pay.php:280-281: empty pid is a business error, not an auth error.
			reject(ctx, mode, -1, "商户ID不能为空")
			return
		}
		var merchant model.Merchant
		if err := db.WithContext(ctx.Request.Context()).
			Where("legacy_uid = ? AND status = ?", uid, model.StatusActive).
			First(&merchant).Error; err != nil {
			reject(ctx, mode, -3, "商户ID不存在")
			return
		}
		params := make(map[string]any, len(form))
		for key, values := range form {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}
		signType := strings.ToUpper(strings.TrimSpace(form.Get("sign_type")))
		signature := form.Get("sign")
		switch signType {
		case "", "MD5":
			resolved, resolveErr := credentials.ResolveActive(ctx.Request.Context(), merchant.ID, model.CredentialMD5)
			if resolveErr != nil {
				reject(ctx, mode, -3, "该商户只能使用RSA签名类型")
				return
			}
			if !epaysigning.VerifyMD5(params, resolved.Secret, signature) {
				reject(ctx, mode, -3, "MD5签名校验失败")
				return
			}
		case "RSA":
			resolved, resolveErr := credentials.ResolveRSA(ctx.Request.Context(), merchant.ID)
			if resolveErr != nil {
				reject(ctx, mode, -3, "该商户未配置RSA公钥")
				return
			}
			publicKey, parseErr := epaysigning.ParseRSAPublicKeyPEM(resolved.PublicKey)
			if parseErr != nil || !epaysigning.VerifyRSA(params, publicKey, signature) {
				reject(ctx, mode, -3, "RSA签名校验失败")
				return
			}
		default:
			reject(ctx, mode, -3, signType+"签名校验失败")
			return
		}
		ctx.Set(merchantContextKey, merchant)
		ctx.Next()
	}
}

func MustMerchant(ctx *gin.Context) model.Merchant {
	return ctx.MustGet(merchantContextKey).(model.Merchant)
}

// reject writes the failure the way the matching upstream entrypoint does: a
// sysmsg HTML page for submit.php, an echojsonmsg envelope everywhere else.
func reject(ctx *gin.Context, mode ReplyMode, code int, message string) {
	ctx.Abort()
	if mode == ReplyHTML {
		// Epay/includes/functions.php:382-401 sysmsg(): HTTP 200 with an HTML page.
		ctx.Header("Content-Type", "text/html; charset=UTF-8")
		ctx.String(http.StatusOK, `<!DOCTYPE html><html lang="zh-CN"><head><meta http-equiv="Content-Type" content="text/html; charset=utf-8" />`+
			`<title>站点提示信息</title></head><body id="error-page"><h3>站点提示信息</h3>`+
			html.EscapeString(message)+`</body></html>`)
		return
	}
	// Epay/includes/functions.php:1430 echojsonmsg(): HTTP 200 JSON.
	ctx.JSON(http.StatusOK, gin.H{"code": code, "msg": message})
}

func IsAuthenticationError(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, credential.ErrCredentialNotFound)
}
