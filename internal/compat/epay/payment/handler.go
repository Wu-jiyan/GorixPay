package payment

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	channelrouter "gorixpay/internal/channel/router"
	epayauth "gorixpay/internal/compat/epay/auth"
	epayrequest "gorixpay/internal/compat/epay/request"
	epayv2 "gorixpay/internal/compat/epay/v2"
	"gorixpay/internal/merchant/credential"
	paymentmodel "gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentservice "gorixpay/internal/payment/service"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	amountPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	// outTradeNoPattern is the upstream order number charset of
	// Epay/includes/lib/api/Pay.php:58 and :319.
	outTradeNoPattern = regexp.MustCompile(`^[a-zA-Z0-9._\-|]+$`)
	// notifyURLPattern mirrors is_url() in Epay/includes/functions.php:1529.
	notifyURLPattern = regexp.MustCompile(`(?i)^https?://[^\s]+$`)
)

// requestMode selects which entrypoint the parameters belong to: mapi.php
// (Pay::create) or submit.php (Pay::submit). The two differ in exactly the
// parameters they demand, so the upstream checks are applied per mode.
type requestMode int

const (
	// modeJSON is Epay/includes/lib/api/Pay.php:263-331 (mapi.php).
	modeJSON requestMode = iota
	// modePage is Epay/includes/lib/api/Pay.php:8-62 (submit.php).
	modePage
)

type Handler struct {
	orders  *paymentservice.Service
	siteURL string
	starter PaymentStarter
	db      *gorm.DB
}

type PaymentStarter interface {
	Start(context.Context, string) (paymentorchestrator.Result, error)
}

// optionStarter is implemented by starters that accept the per-request payment
// hints (method/device/sub_openid/auth_code); the orchestrator does.
type optionStarter interface {
	StartWithOptions(context.Context, string, paymentorchestrator.StartOptions) (paymentorchestrator.Result, error)
}

func Register(router *gin.Engine, db *gorm.DB, credentials *credential.Service, orders *paymentservice.Service, siteURL string, starter PaymentStarter) {
	handler := &Handler{orders: orders, siteURL: strings.TrimRight(siteURL, "/"), starter: starter, db: db}
	authenticate := epayauth.Middleware(db, credentials)
	// submit.php renders failures as the upstream sysmsg page while every JSON
	// entrypoint answers {"code":-3,...}; both are HTTP 200 (Epay never answers
	// 4xx), so the two reply modes are chosen per route.
	authenticatePage := epayauth.HTMLMiddleware(db, credentials)
	router.POST("/mapi.php", authenticate, handler.mapi)
	router.GET("/submit.php", selectSubmitParameters, authenticatePage, handler.submit)
	router.POST("/submit.php", selectSubmitParameters, authenticatePage, handler.submit)
	router.POST("/api/pay/query", delegateV2, authenticate, handler.query)
	router.POST("/api/pay/close", delegateV2, authenticate, handler.close)
	router.GET("/api/v1/cashier/orders/:trade_no", handler.cashierOrder)
}

// delegateV2 hands RSA signed requests on the shared /api/pay/{query,close}
// paths to the V2 protocol dispatcher and keeps the legacy MD5 behaviour for
// everything else.
func delegateV2(ctx *gin.Context) {
	if !strings.EqualFold(ctx.Request.FormValue("sign_type"), "RSA") {
		ctx.Next()
		return
	}
	if epayv2.HandlePathRequest(ctx) {
		ctx.Abort()
		return
	}
	ctx.Next()
}

func (handler *Handler) mapi(ctx *gin.Context) {
	merchant := epayauth.MustMerchant(ctx)
	command, options, err := commandFromRequest(ctx, merchant.ID, modeJSON)
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": err.Error()})
		return
	}
	order, created, err := handler.orders.Create(ctx.Request.Context(), command)
	if err != nil {
		handleOrderError(ctx, command.OutTradeNo, err, false)
		return
	}
	if rejection := repeatOrderMessage(order, created); rejection != "" {
		ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": rejection})
		return
	}
	action, startErr := handler.startPayment(ctx, order, options)
	if startErr != nil {
		// Epay/includes/lib/Payment.php:283-291 answers code -2 when the channel
		// itself refused or failed; parameter problems stay at -1/-3.
		ctx.JSON(http.StatusOK, gin.H{"code": -2, "msg": "支付通道请求失败"})
		return
	}
	payload := handler.actionFields(order.TradeNo, action)
	// The flat upstream fields above are the contract. pay_type/pay_info stay as
	// additive aliases for the in-repo cashier; see deviation D1.
	payType, payInfo := epayAction(action)
	payload["code"] = 1
	payload["trade_no"] = order.TradeNo
	payload["msg"] = "订单创建成功"
	payload["pay_type"] = payType
	payload["pay_info"] = payInfo
	ctx.JSON(http.StatusOK, payload)
}

// actionFields maps a plugin action onto the flat V1 response of
// Epay/includes/lib/Payment.php:246-296: jump publishes payurl, qrcode
// publishes qrcode, scheme publishes urlscheme, html publishes html. Anything
// else degrades to a payurl pointing at the payment page (Payment.php:293-295).
func (handler *Handler) actionFields(tradeNo string, action pluginpayment.Action) gin.H {
	fields := gin.H{}
	switch action.Type {
	case pluginpayment.ActionJump:
		fields["payurl"] = action.URL
	case pluginpayment.ActionQRCode:
		fields["qrcode"] = action.URL
	case pluginpayment.ActionURLScheme:
		fields["urlscheme"] = action.URL
	case pluginpayment.ActionHTML:
		fields["html"] = action.Data
	default:
		fields["payurl"] = handler.cashierURL(tradeNo)
	}
	return fields
}

// startPayment resolves the action the merchant has to follow. Two upstream
// situations are not merchant errors: no usable channel sends the buyer to the
// cashier (Pay.php:168-171), and an order that was already started replays the
// stored payment instead of calling the channel again (Pay.php:150-152).
func (handler *Handler) startPayment(ctx *gin.Context, order paymentmodel.Order, options paymentorchestrator.StartOptions) (pluginpayment.Action, error) {
	fallback := pluginpayment.Action{Type: pluginpayment.ActionJump, URL: handler.cashierURL(order.TradeNo)}
	if handler.starter == nil {
		return fallback, nil
	}
	var result paymentorchestrator.Result
	var err error
	if optionStarter, ok := handler.starter.(optionStarter); ok {
		result, err = optionStarter.StartWithOptions(ctx.Request.Context(), order.TradeNo, options)
	} else {
		result, err = handler.starter.Start(ctx.Request.Context(), order.TradeNo)
	}
	switch {
	case err == nil:
		return result.Action, nil
	case errors.Is(err, channelrouter.ErrNoAvailableChannel):
		return fallback, nil
	case errors.Is(err, paymentorchestrator.ErrOrderNotStartable):
		if stored, ok := handler.storedAction(ctx.Request.Context(), order.ID); ok {
			return stored, nil
		}
		return fallback, nil
	default:
		return pluginpayment.Action{}, err
	}
}

// storedAction replays the action saved by the last successful attempt so a
// repeated mapi call answers with the same payurl the merchant already has.
func (handler *Handler) storedAction(ctx context.Context, orderID string) (pluginpayment.Action, bool) {
	if handler.db == nil {
		return pluginpayment.Action{}, false
	}
	var attempt paymentmodel.Attempt
	if err := handler.db.WithContext(ctx).
		Where("order_id = ? AND status = ?", orderID, paymentmodel.AttemptSucceeded).
		Order("attempt_no DESC").First(&attempt).Error; err != nil {
		return pluginpayment.Action{}, false
	}
	if strings.TrimSpace(attempt.ActionPayload) == "" {
		return pluginpayment.Action{}, false
	}
	var action pluginpayment.Action
	if err := json.Unmarshal([]byte(attempt.ActionPayload), &action); err != nil || action.Type == "" {
		return pluginpayment.Action{}, false
	}
	return action, true
}

// repeatOrderMessage reproduces the duplicate handling of
// Epay/includes/lib/api/Pay.php:407-413 (and :141-147 for submit.php): reusing
// the out_trade_no of a finished order is refused, while a still pending order
// keeps its trade_no.
func repeatOrderMessage(order paymentmodel.Order, created bool) string {
	if created {
		return ""
	}
	switch order.Status {
	case paymentmodel.StatusPaid, paymentmodel.StatusClosed, paymentmodel.StatusFailed:
		return fmt.Sprintf("该订单(%s)已完成支付，请勿重复发起支付", order.OutTradeNo)
	default:
		return ""
	}
}

func (handler *Handler) submit(ctx *gin.Context) {
	merchant := epayauth.MustMerchant(ctx)
	command, _, err := commandFromRequest(ctx, merchant.ID, modePage)
	if err != nil {
		writeSysMessage(ctx, err.Error())
		return
	}
	order, created, err := handler.orders.Create(ctx.Request.Context(), command)
	if err != nil {
		handleOrderError(ctx, command.OutTradeNo, err, true)
		return
	}
	if rejection := repeatOrderMessage(order, created); rejection != "" {
		writeSysMessage(ctx, rejection)
		return
	}
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	ctx.String(http.StatusOK, cashierRedirectScript(handler.cashierURL(order.TradeNo), ctx.Request.Form.Get("sitename")))
}

// selectSubmitParameters implements the parameter group rule of
// Epay/includes/lib/api/Pay.php:12-18: submit.php reads the query string when
// the query carries pid and only otherwise reads the body. Merging both groups
// would let an unsigned parameter from the ignored group override a signed
// field, so the ignored group is dropped before the signature is verified.
func selectSubmitParameters(ctx *gin.Context) {
	request := ctx.Request
	if request.URL == nil {
		return
	}
	if _, present := request.URL.Query()["pid"]; present {
		request.Body = http.NoBody
		request.ContentLength = 0
		request.Header.Del("Content-Type")
		return
	}
	request.URL.RawQuery = ""
	request.URL.ForceQuery = false
}

// cashierRedirectScript is the inline script of
// Epay/includes/lib/api/Pay.php:161, including the urlencoded base64 sitename
// parameter the cashier page expects.
func cashierRedirectScript(cashierURL, sitename string) string {
	encoded := url.QueryEscape(base64.StdEncoding.EncodeToString([]byte(html.EscapeString(sitename))))
	target := cashierURL
	if strings.Contains(target, "?") {
		target += "&sitename=" + encoded
	} else {
		target += "?sitename=" + encoded
	}
	return "<script>window.location.replace('" + target + "');</script>"
}

// writeSysMessage renders the upstream sysmsg() page
// (Epay/includes/functions.php:382-401) with HTTP 200, which is what submit.php
// answers for every parameter error.
func writeSysMessage(ctx *gin.Context, message string) {
	const title = "站点提示信息"
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	ctx.String(http.StatusOK,
		`<!DOCTYPE html><html lang="zh-CN"><head><meta http-equiv="Content-Type" content="text/html; charset=utf-8" />`+
			`<title>`+title+`</title></head><body id="error-page"><h3>`+title+`</h3>`+html.EscapeString(message)+`</body></html>`)
}

func (handler *Handler) query(ctx *gin.Context) {
	merchant := epayauth.MustMerchant(ctx)
	order, err := handler.orders.Find(ctx.Request.Context(), merchant.ID, ctx.Request.Form.Get("trade_no"), ctx.Request.Form.Get("out_trade_no"))
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": "订单号不存在"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"code":         0,
		"msg":          "succ",
		"trade_no":     order.TradeNo,
		"out_trade_no": order.OutTradeNo,
		"type":         order.PaymentType,
		"name":         order.Name,
		"money":        formatAmount(order.Amount),
		"status":       epayStatus(order.Status),
		"addtime":      order.CreatedAt.Format("2006-01-02 15:04:05"),
		"endtime":      formatOptionalTime(order.PaidAt),
	})
}

func (handler *Handler) close(ctx *gin.Context) {
	merchant := epayauth.MustMerchant(ctx)
	order, _, err := handler.orders.Close(ctx.Request.Context(), merchant.ID, ctx.Request.Form.Get("trade_no"), ctx.Request.Form.Get("out_trade_no"))
	if err != nil {
		message := "订单号不存在"
		if errors.Is(err, paymentservice.ErrOrderNotClosable) {
			message = "当前订单状态不允许关闭"
		}
		ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": message})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "msg": "订单关闭成功", "trade_no": order.TradeNo})
}

func (handler *Handler) cashierOrder(ctx *gin.Context) {
	order, err := handler.orders.FindPublic(ctx.Request.Context(), ctx.Param("trade_no"))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"code": "order_not_found", "message": "order not found"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"trade_no":     order.TradeNo,
		"name":         order.Name,
		"amount":       order.Amount,
		"currency":     order.Currency,
		"payment_type": order.PaymentType,
		"status":       order.Status,
		"kind":         order.Kind,
		"return_url":   order.ReturnURL,
		"created_at":   order.CreatedAt,
		"expires_at":   order.ExpiresAt,
	})
}

// commandFromRequest applies the upstream parameter checks in the upstream
// order and reports their exact messages. Epay/includes/lib/api/Pay.php:53-62
// covers submit.php, :313-322 covers mapi.php; the only differences are the
// return_url requirement (submit only) and the type/clientip requirements
// (mapi only).
func commandFromRequest(ctx *gin.Context, merchantID string, mode requestMode) (paymentservice.CreateCommand, paymentorchestrator.StartOptions, error) {
	form := ctx.Request.Form
	outTradeNo := form.Get("out_trade_no")
	if strings.TrimSpace(outTradeNo) == "" {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("订单号(out_trade_no)不能为空")
	}
	notifyURL := form.Get("notify_url")
	if strings.TrimSpace(notifyURL) == "" {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("通知地址(notify_url)不能为空")
	}
	returnURL := form.Get("return_url")
	if mode == modePage && strings.TrimSpace(returnURL) == "" {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("回调地址(return_url)不能为空")
	}
	name := form.Get("name")
	if strings.TrimSpace(name) == "" {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("商品名称(name)不能为空")
	}
	money := form.Get("money")
	if strings.TrimSpace(money) == "" {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("金额(money)不能为空")
	}
	options := paymentorchestrator.StartOptions{}
	if mode == modeJSON {
		// submit.php never accepted method/device; only mapi.php and pay/create do.
		options = epayrequest.Options(form.Get)
	}
	paymentType := strings.TrimSpace(form.Get("type"))
	if mode == modeJSON && paymentType == "" && options.Method == "scan" {
		// Pay.php:307-310 fills an empty type from the auth_code prefix.
		paymentType = epayrequest.ScanPayType(options.AuthCode)
		if paymentType == "" {
			return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("未知的付款码类型")
		}
	}
	if mode == modeJSON && paymentType == "" {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("支付方式(type)不能为空")
	}
	if mode == modeJSON && options.Method == "jsapi" && options.SubOpenID == "" {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("jsapi支付时参数(sub_openid)不能为空")
	}
	if mode == modeJSON && options.Method == "scan" && options.AuthCode == "" {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("付款码支付时授权码(auth_code)不能为空")
	}
	clientIP := strings.TrimSpace(form.Get("clientip"))
	if mode == modeJSON && clientIP == "" {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("用户IP地址(clientip)不能为空")
	}
	if !outTradeNoPattern.MatchString(outTradeNo) {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("订单号(out_trade_no)格式不正确")
	}
	amount, err := ParseAmount(money)
	if err != nil {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("金额不合法")
	}
	if mode == modeJSON && net.ParseIP(clientIP) == nil {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("用户IP地址不合法")
	}
	if !notifyURLPattern.MatchString(notifyURL) {
		return paymentservice.CreateCommand{}, paymentorchestrator.StartOptions{}, errors.New("通知地址(notify_url)格式不正确")
	}
	if mode == modePage && paymentType == "" {
		// submit.php without a type goes to the cashier page, which owns the
		// channel choice; the platform books it on the generic cashier channel.
		paymentType = "cashier"
	}
	return paymentservice.CreateCommand{
		MerchantID:  merchantID,
		OutTradeNo:  outTradeNo,
		PaymentType: paymentType,
		Name:        name,
		Amount:      amount,
		NotifyURL:   notifyURL,
		ReturnURL:   returnURL,
		ClientIP:    clientIP,
		Buyer:       options.SubOpenID,
		Attach:      form.Get("param"),
	}, options, nil
}

func ParseAmount(value string) (int64, error) {
	if !amountPattern.MatchString(value) {
		return 0, errors.New("invalid amount")
	}
	rational, ok := new(big.Rat).SetString(value)
	if !ok || rational.Sign() <= 0 {
		return 0, errors.New("invalid amount")
	}
	scaled := new(big.Rat).Mul(rational, big.NewRat(100, 1))
	numerator := scaled.Num()
	denominator := scaled.Denom()
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	if new(big.Int).Lsh(remainder, 1).Cmp(denominator) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() || quotient.Sign() <= 0 {
		return 0, errors.New("invalid amount")
	}
	return quotient.Int64(), nil
}

func (handler *Handler) cashierURL(tradeNo string) string {
	return handler.siteURL + "/cashier.php?trade_no=" + tradeNo
}

func handleOrderError(ctx *gin.Context, outTradeNo string, err error, htmlResponse bool) {
	message := "创建订单失败，请返回重试！"
	var rejection paymentservice.Rejection
	if errors.As(err, &rejection) {
		message = rejection.Reason
	} else if errors.Is(err, paymentservice.ErrIdempotencyConflict) {
		message = fmt.Sprintf("该订单(%s)支付参数有变化，请更换订单号重新发起支付", outTradeNo)
	} else if errors.Is(err, paymentservice.ErrInvalidCommand) {
		message = "提交参数不完整或格式不正确"
	}
	if htmlResponse {
		// submit.php reports every order level failure as a sysmsg page
		// (Epay/includes/lib/api/Pay.php:143, :156).
		writeSysMessage(ctx, message)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": message})
}

func epayStatus(status paymentmodel.Status) int {
	if status == paymentmodel.StatusPaid {
		return 1
	}
	if status == paymentmodel.StatusClosed || status == paymentmodel.StatusFailed {
		return 2
	}
	return 0
}

func formatAmount(amount int64) string {
	return fmt.Sprintf("%d.%02d", amount/100, amount%100)
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format("2006-01-02 15:04:05")
	return &formatted
}

func epayAction(action pluginpayment.Action) (string, any) {
	if action.Type == pluginpayment.ActionJump || action.Type == pluginpayment.ActionQRCode || action.Type == pluginpayment.ActionURLScheme {
		return string(action.Type), action.URL
	}
	return string(action.Type), action.Data
}
