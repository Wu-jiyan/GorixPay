// Package page serves the browser facing payment entry points that upstream Epay
// exposes next to the API: /pay/submit/{trade_no}/, /pay/checkpay/{trade_no}/,
// /pay/return/{trade_no}/ and /pay/ok/{trade_no}/.
//
// The protocol surfaces (submit.php, mapi.php, api.php, /api/*) never render
// these URLs themselves except as a payurl fallback, but every merchant SDK that
// completes a payment in a browser ends up on /pay/return/{trade_no}/ and
// verifies $_GET with the merchant key (EpayCore::verifyReturn). Dropping the
// signed query string there makes verifyReturn() fail, so the redirect is part of
// the compatibility contract, not a convenience.
package page

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Signer builds the signed parameter set handed to the merchant. The epay
// notification sender implements it, so the browser return and the asynchronous
// notification always carry the same signature format.
type Signer interface {
	NotificationValues(ctx context.Context, order model.Order) (url.Values, error)
}

// Starter begins (or resumes) a payment. The payment orchestrator implements it;
// a nil starter makes the per-channel pages fall back to the cashier.
type Starter interface {
	StartWithOptions(ctx context.Context, tradeNo string, options paymentorchestrator.StartOptions) (paymentorchestrator.Result, error)
}

type Handler struct {
	db      *gorm.DB
	signer  Signer
	starter Starter
	siteURL string
	captcha CaptchaVerifier
}

// CaptchaVerifier checks a captcha answer. internal/security implements it; a
// nil verifier means captcha verification is not wired, and the compatibility
// endpoint answers with code -1 instead of pretending the code was right.
type CaptchaVerifier func(ctx context.Context, id, code string) error

// Option customises the page handler without changing the four positional
// parameters every caller already passes.
type Option func(*Handler)

// WithCaptchaVerifier wires the captcha check used by getshop.php?act=captcha_verify.
func WithCaptchaVerifier(verifier CaptchaVerifier) Option {
	return func(handler *Handler) {
		handler.captcha = verifier
	}
}

// WithStarter wires the payment starter used by the per-channel pages. Without
// it those pages keep redirecting to the cashier instead of opening the channel.
func WithStarter(starter Starter) Option {
	return func(handler *Handler) {
		handler.starter = starter
	}
}

// Register mounts the browser payment pages. siteURL is the public base URL used
// to build the cashier link; it is trimmed of a trailing slash.
func Register(router *gin.Engine, db *gorm.DB, siteURL string, signer Signer, options ...Option) {
	handler := &Handler{db: db, signer: signer, siteURL: strings.TrimRight(siteURL, "/")}
	for _, option := range options {
		option(handler)
	}
	router.GET("/pay/submit/:trade_no/", handler.submit)
	router.GET("/pay/checkpay/:trade_no/", handler.checkpay)
	router.GET("/pay/return/:trade_no/", handler.returnToMerchant)
	router.GET("/pay/ok/:trade_no/", handler.ok)
	router.GET("/submit2.php", handler.submit2)
	// Upstream serves one private payment page per channel entry point:
	// /pay/wxpay/{trade_no}/ shows the scan page, /pay/wxjspay/ the in-WeChat
	// JSAPI page, /pay/wxwappay/ the mobile one. Merchants cache these URLs, so
	// the names are part of the compatibility surface and each one carries the
	// method it was written for.
	for name := range channelPageFuncs {
		pageName := name
		router.GET("/pay/"+pageName+"/:trade_no/", func(ctx *gin.Context) {
			handler.channelPage(ctx, pageName)
		})
	}
	router.GET("/getshop.php", handler.getshop)
}

// channelPageFuncs maps the upstream page name onto the payment method it starts.
// An empty value means "let the channel decide" (scan on PC, redirect on mobile).
var channelPageFuncs = map[string]string{
	"alipay":     "",
	"alipayjs":   "jsapi",
	"wxpay":      "",
	"wxjspay":    "jsapi",
	"wxwappay":   "wap",
	"wxappay":    "app",
	"bank":       "",
	"quickpay":   "quickpay",
	"unionpay":   "",
	"unionpayjs": "jsapi",
	"scanpay":    "scan",
	"ecny":       "",
	// Legacy names kept so an old cached link still resolves.
	"scan":   "scan",
	"qrcode": "",
	"jsapi":  "jsapi",
	"mini":   "app",
	"h5":     "wap",
	"red":    "",
	"wap":    "wap",
}

// PagePathFor reports the site-local payment page for an order, or "" when the
// payment type has none (a custom pay-type code the compatibility layer never
// knew). The cashier uses it to hand a phone over to the server-rendered page
// instead of opening the channel url in a second window.
func PagePathFor(tradeNo, paymentType string) string {
	name := strings.ToLower(strings.TrimSpace(paymentType))
	if name == "" {
		return ""
	}
	if _, ok := channelPageFuncs[name]; !ok {
		return ""
	}
	return "/pay/" + name + "/" + url.PathEscape(tradeNo) + "/"
}

// submit2 is the retry page upstream links from a failed channel attempt
// (Epay/submit2.php). It re-opens an unpaid order: a missing order and a paid
// order both answer with the upstream sysmsg page, anything else goes back to the
// cashier, which re-runs the channel selection.
func (handler *Handler) submit2(ctx *gin.Context) {
	tradeNo := strings.TrimSpace(ctx.Query("trade_no"))
	if tradeNo == "" {
		handler.sysMessage(ctx, "该订单号不存在，请返回来源地重新发起请求！")
		return
	}
	var order model.Order
	if err := handler.db.WithContext(ctx.Request.Context()).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		handler.sysMessage(ctx, "该订单号不存在，请返回来源地重新发起请求！")
		return
	}
	if order.Status != model.StatusPending && order.Status != model.StatusProcessing {
		// Epay/submit2.php:9-10 answers a finished order with this exact text.
		handler.sysMessage(ctx, "该订单("+order.OutTradeNo+")已完成支付，请勿重复发起支付")
		return
	}
	ctx.Redirect(http.StatusFound, handler.cashierPath(order.TradeNo))
}

// sysMessage renders the sysmsg page of Epay/includes/functions.php:382-401.
func (handler *Handler) sysMessage(ctx *gin.Context, message string) {
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	ctx.String(http.StatusOK, `<!DOCTYPE html><html lang="zh-CN"><head><meta http-equiv="Content-Type" content="text/html; charset=utf-8" /><title>站点提示信息</title></head><body id="error-page"><h3>站点提示信息</h3>`+html.EscapeString(message)+`</body></html>`)
}

// submit mirrors upstream Pay.php: with a channel chosen the browser goes to that
// channel's own page (/pay/{func}/{trade_no}/), which starts the payment and
// renders it. Without a usable page name it falls back to the cashier, where the
// buyer picks a method.
func (handler *Handler) submit(ctx *gin.Context) {
	order, err := handler.load(ctx)
	if err != nil {
		handler.notFound(ctx)
		return
	}
	if order.Status == model.StatusPaid {
		ctx.Redirect(http.StatusFound, handler.returnPath(order.TradeNo))
		return
	}
	if name := strings.ToLower(strings.TrimSpace(order.PaymentType)); name != "" {
		if _, ok := channelPageFuncs[name]; ok {
			ctx.Redirect(http.StatusFound, handler.siteURL+"/pay/"+name+"/"+url.PathEscape(order.TradeNo)+"/")
			return
		}
	}
	ctx.Redirect(http.StatusFound, handler.cashierPath(order.TradeNo))
}

// channelPage answers the per-channel private payment pages. Upstream starts the
// channel inside the page and then renders a template per result type
// (lib\Plugin::loadForPay -> Payment::echoDefault): jump redirects the browser,
// qrcode paints the code. GorixPay does the same here instead of bouncing to the
// cashier SPA, so a merchant that links straight to /pay/wxpay/{trade_no}/ gets
// the payment without a second hop.
func (handler *Handler) channelPage(ctx *gin.Context, name string) {
	order, err := handler.load(ctx)
	if err != nil {
		handler.notFound(ctx)
		return
	}
	if order.Status == model.StatusPaid {
		ctx.Redirect(http.StatusFound, handler.returnPath(order.TradeNo))
		return
	}
	device := deviceFromRequest(ctx.Request)
	action, ok := handler.channelAction(ctx, order, channelPageFuncs[name], device)
	if !ok {
		ctx.Redirect(http.StatusFound, handler.cashierPath(order.TradeNo))
		return
	}
	// Upstream never answers a payment with a bare HTTP redirect except for the one
	// combination that can pay in place: a 微信 order opened inside the WeChat
	// browser (Epay/includes/lib/api/Pay.php:131-133 sends it straight to the JSAPI
	// trade, Epay/includes/lib/Payment.php:60-72 puts the jump inside a page).
	// Everything else renders a page, because a 微信 H5 url is dead outside WeChat
	// and an 支付宝 url dead outside 支付宝:
	//   - 手机 + 微信支付 → wxpay_wap: code, 「打开微信」, and the copy-the-link recipe
	//   - 手机 + 支付宝 → alipay_h5: the amount, an 「打开支付宝」 button and the link
	//   - PC → the scan page
	inAppWeChat := device == "wechat" && isWeChatPayment(order.PaymentType)
	switch {
	case inAppWeChat && action.Type == pluginpayment.ActionJump:
		ctx.Redirect(http.StatusFound, action.URL)
	case inAppWeChat && action.Type == pluginpayment.ActionQRCode && isWebURL(action.URL):
		ctx.Redirect(http.StatusFound, action.URL)
	// A jump is rendered as a page rather than a 302 so the address bar keeps a
	// history entry and the buyer can come back to the cashier (upstream does the
	// same with <script>window.location.replace</script>). A 微信 or 支付宝 order
	// gets the code page even for a jump: the link only pays inside that app, so
	// the buyer needs something to carry over by hand.
	case action.Type == pluginpayment.ActionJump:
		if device != "pc" && !isWeChatPayment(order.PaymentType) && !isAlipayPayment(order.PaymentType) {
			handler.renderJumpPage(ctx, order, action.URL)
			return
		}
		handler.renderScanPage(ctx, order, action.URL, device)
	case action.Type == pluginpayment.ActionQRCode:
		handler.renderScanPage(ctx, order, action.URL, device)
	case action.Type == pluginpayment.ActionURLScheme:
		ctx.Header("Content-Type", "text/html; charset=UTF-8")
		ctx.String(http.StatusOK, `<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>正在打开支付</title><script>location.href=%q;</script></head><body><p>正在打开支付…</p></body></html>`, action.URL)
	default:
		ctx.Redirect(http.StatusFound, handler.cashierPath(order.TradeNo))
	}
}

// channelAction starts (or resumes) the order for this page. It answers false
// when nothing can be started, which sends the browser to the cashier.
//
// An order that is already `processing` was started by the API that created it
// (mapi.php, submit.php), and the orchestrator refuses to start it twice. The
// channel URL it produced is still what the buyer needs, so replay the stored
// attempt — the same way upstream reopens an order without re-creating it.
func (handler *Handler) channelAction(ctx *gin.Context, order model.Order, method, device string) (pluginpayment.Action, bool) {
	if order.Status == model.StatusProcessing {
		if action, ok := handler.storedAction(ctx, order.ID); ok {
			return action, true
		}
	}
	if handler.starter == nil {
		return pluginpayment.Action{}, false
	}
	result, err := handler.starter.StartWithOptions(ctx.Request.Context(), order.TradeNo, paymentorchestrator.StartOptions{
		Method: method,
		Device: device,
	})
	if err != nil {
		return pluginpayment.Action{}, false
	}
	if strings.TrimSpace(result.Action.URL) == "" {
		return pluginpayment.Action{}, false
	}
	return result.Action, true
}

// storedAction reads the action the last successful attempt produced.
func (handler *Handler) storedAction(ctx *gin.Context, orderID string) (pluginpayment.Action, bool) {
	var attempt model.Attempt
	if err := handler.db.WithContext(ctx.Request.Context()).
		Where("order_id = ? AND status = ?", orderID, model.AttemptSucceeded).
		Order("attempt_no DESC").First(&attempt).Error; err != nil {
		return pluginpayment.Action{}, false
	}
	if attempt.ActionPayload == "" {
		return pluginpayment.Action{}, false
	}
	var action pluginpayment.Action
	if err := json.Unmarshal([]byte(attempt.ActionPayload), &action); err != nil {
		return pluginpayment.Action{}, false
	}
	if action.Type == "" {
		action.Type = pluginpayment.ActionType(attempt.ActionType)
	}
	if strings.TrimSpace(action.URL) == "" {
		return pluginpayment.Action{}, false
	}
	return action, true
}

// renderScanPage paints the code page for a channel URL: the layout of upstream's
// wxpay_qrcode / wxpay_wap templates, including the copy-the-link route for the
// cases where a code cannot be scanned.
func (handler *Handler) renderScanPage(ctx *gin.Context, order model.Order, content, device string) {
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	if err := scanPageTemplate.Execute(ctx.Writer, handler.scanPageData(ctx.Request.Context(), order, content, device)); err != nil {
		handler.serverError(ctx)
	}
}

// renderJumpPage paints the page upstream emits for a jump result
// (Epay/includes/lib/Payment.php:60-72): a script that replaces the location, plus
// a manual link so a blocked redirect still leaves the buyer a way in.
func (handler *Handler) renderJumpPage(ctx *gin.Context, order model.Order, target string) {
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	data := jumpPage{
		Brand:     scanBrand(order.PaymentType),
		IsWeChat:  isWeChatPayment(order.PaymentType),
		Amount:    formatAmount(order.Amount),
		Name:      strings.TrimSpace(order.Name),
		TradeNo:   order.TradeNo,
		Target:    target,
		BackURL:   handler.cashierPath(order.TradeNo),
		DoneURL:   handler.siteURL + "/pay/checkpay/" + url.PathEscape(order.TradeNo) + "/",
		ExpiresAt: 0,
		Theme:     scanTheme(order.PaymentType),
	}
	if err := jumpPageTemplate.Execute(ctx.Writer, data); err != nil {
		handler.serverError(ctx)
	}
}

// isWebURL reports whether the action URL is a page a browser can open, as
// opposed to a scannable scheme such as weixin://.
func isWebURL(target string) bool {
	return strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")
}

// deviceFromRequest classifies the browser the same way the protocol layer does:
// the in-app browsers keep their identity because only they can run a JSAPI trade
// type.
func deviceFromRequest(request *http.Request) string {
	agent := strings.ToLower(request.UserAgent())
	switch {
	case strings.Contains(agent, "micromessenger"):
		return "wechat"
	case strings.Contains(agent, "alipayclient"):
		return "alipay"
	case strings.Contains(agent, "mobile"), strings.Contains(agent, "android"),
		strings.Contains(agent, "iphone"), strings.Contains(agent, "ipad"),
		strings.Contains(agent, "ipod"), strings.Contains(agent, "harmony"):
		return "mobile"
	default:
		return "pc"
	}
}

// getshop covers Epay/getshop.php: the cashier polls act=poll for the payment
// state, and act=captcha_verify trades a solved captcha for a short-lived key.
// Both answer HTTP 200 with a code field, the way upstream does.
func (handler *Handler) getshop(ctx *gin.Context) {
	act := strings.TrimSpace(ctx.Query("act"))
	switch act {
	case "captcha_verify":
		if handler.captcha == nil {
			ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证码未启用"})
			return
		}
		if err := handler.captcha(ctx.Request.Context(), ctx.Query("captcha_id"), ctx.Query("captcha_code")); err != nil {
			ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证码错误"})
			return
		}
		key, err := randomKey()
		if err != nil {
			ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": "系统繁忙"})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"code": 1, "key": key})
	case "", "poll":
		tradeNo := strings.TrimSpace(ctx.Query("trade_no"))
		var order model.Order
		if err := handler.db.WithContext(ctx.Request.Context()).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": "订单不存在"})
			return
		}
		paid := order.Status == model.StatusPaid
		status := "pending"
		if paid {
			status = "paid"
		}
		ctx.JSON(http.StatusOK, gin.H{"code": 1, "trade_no": order.TradeNo, "status": status, "paid": paid})
	default:
		ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": "不支持的操作"})
	}
}

func randomKey() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// checkpay mirrors upstream's polling page: a paid order moves on to the return
// redirect, anything else stays on the cashier.
func (handler *Handler) checkpay(ctx *gin.Context) {
	order, err := handler.load(ctx)
	if err != nil {
		handler.notFound(ctx)
		return
	}
	if order.Status == model.StatusPaid {
		ctx.Redirect(http.StatusFound, handler.returnPath(order.TradeNo))
		return
	}
	ctx.Redirect(http.StatusFound, handler.cashierPath(order.TradeNo))
}

// returnToMerchant sends the browser to the merchant return_url with the same
// signed parameter set the asynchronous notification carries. Upstream builds it
// with creat_callback and appends http_build_query(...) to return_url
// (Epay/includes/functions.php:519-531).
func (handler *Handler) returnToMerchant(ctx *gin.Context) {
	order, err := handler.load(ctx)
	if err != nil {
		handler.notFound(ctx)
		return
	}
	if order.Status == model.StatusPending || order.Status == model.StatusProcessing {
		ctx.Redirect(http.StatusFound, handler.cashierPath(order.TradeNo))
		return
	}
	if order.ReturnURL == nil || strings.TrimSpace(*order.ReturnURL) == "" {
		ctx.Redirect(http.StatusFound, handler.siteURL+"/pay/ok/"+url.PathEscape(order.TradeNo)+"/")
		return
	}
	target := *order.ReturnURL
	if handler.signer != nil && order.Status == model.StatusPaid {
		values, signErr := handler.signer.NotificationValues(ctx.Request.Context(), order)
		if signErr != nil {
			handler.serverError(ctx)
			return
		}
		target = appendQuery(target, values)
	}
	ctx.Redirect(http.StatusFound, target)
}

func (handler *Handler) ok(ctx *gin.Context) {
	order, err := handler.load(ctx)
	if err != nil {
		handler.notFound(ctx)
		return
	}
	status := "支付成功"
	if order.Status != model.StatusPaid {
		status = "订单状态：" + string(order.Status)
	}
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	ctx.String(http.StatusOK, `<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title></head><body style="font-family:system-ui;text-align:center;padding:15vh 1rem"><h1>%s</h1><p>订单号：%s</p><p>金额：%s 元</p></body></html>`,
		html.EscapeString(status), html.EscapeString(status), html.EscapeString(order.TradeNo), formatAmount(order.Amount))
}

func (handler *Handler) load(ctx *gin.Context) (model.Order, error) {
	tradeNo := strings.TrimSpace(ctx.Param("trade_no"))
	if tradeNo == "" {
		return model.Order{}, errors.New("missing trade number")
	}
	var order model.Order
	err := handler.db.WithContext(ctx.Request.Context()).Where("trade_no = ?", tradeNo).First(&order).Error
	return order, err
}

func (handler *Handler) cashierPath(tradeNo string) string {
	return handler.siteURL + "/cashier.php?trade_no=" + url.QueryEscape(tradeNo)
}

func (handler *Handler) returnPath(tradeNo string) string {
	return "/pay/return/" + url.PathEscape(tradeNo) + "/"
}

func (handler *Handler) notFound(ctx *gin.Context) {
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	ctx.String(http.StatusNotFound, `<!doctype html><html><body>订单不存在</body></html>`)
}

func (handler *Handler) serverError(ctx *gin.Context) {
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	ctx.String(http.StatusInternalServerError, `<!doctype html><html><body>签名生成失败</body></html>`)
}

func appendQuery(target string, values url.Values) string {
	if len(values) == 0 {
		return target
	}
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	return target + separator + values.Encode()
}

func formatAmount(amount int64) string {
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	return fmt.Sprintf("%s%d.%02d", sign, amount/100, amount%100)
}
