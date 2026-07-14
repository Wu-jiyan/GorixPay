package auth

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"gorixpay/internal/compat/epay/signing"
	"gorixpay/internal/identity/model"
	"gorixpay/internal/merchant/credential"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const merchantContextKey = "epay_merchant"

func Middleware(db *gorm.DB, credentials *credential.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := ctx.Request.ParseForm(); err != nil {
			reject(ctx)
			return
		}
		uid, err := strconv.ParseInt(ctx.Request.Form.Get("pid"), 10, 64)
		if err != nil || uid <= 0 {
			reject(ctx)
			return
		}
		var merchant model.Merchant
		if err := db.WithContext(ctx.Request.Context()).
			Where("legacy_uid = ? AND status = ?", uid, model.StatusActive).
			First(&merchant).Error; err != nil {
			reject(ctx)
			return
		}
		resolved, err := credentials.ResolveActive(ctx.Request.Context(), merchant.ID, model.CredentialMD5)
		if err != nil {
			reject(ctx)
			return
		}
		params := make(map[string]any, len(ctx.Request.Form))
		for key, values := range ctx.Request.Form {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}
		if !strings.EqualFold(ctx.Request.Form.Get("sign_type"), "MD5") ||
			!signing.VerifyMD5(params, resolved.Secret, ctx.Request.Form.Get("sign")) {
			reject(ctx)
			return
		}
		ctx.Set(merchantContextKey, merchant)
		ctx.Next()
	}
}

func MustMerchant(ctx *gin.Context) model.Merchant {
	return ctx.MustGet(merchantContextKey).(model.Merchant)
}

func reject(ctx *gin.Context) {
	ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": -3, "msg": "商户签名校验失败"})
}

func IsAuthenticationError(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, credential.ErrCredentialNotFound)
}
