package merchanttools

import (
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"

	channelrouter "gorixpay/internal/channel/router"
	identityhttp "gorixpay/internal/identity/httpapi"
	paymentservice "gorixpay/internal/payment/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Deps are the optional dependencies of the merchant tools. Orders and Starter
// may be nil, in which case the endpoints that need them answer 503.
type Deps struct {
	DB        *gorm.DB
	Sessions  gin.HandlerFunc
	PublicURL string
	Orders    *paymentservice.Service
	Starter   PaymentStarter
}

// Handler exposes the merchant tools.
type Handler struct {
	service *Service
}

// Register mounts the authenticated merchant endpoints and the public pay code
// page. It takes the engine because the payer page lives outside /api/v1:
// /api/v1/merchant/pay-code and /api/v1/merchant/test-order for the console,
// /pay/code/{payment_type} for the payer.
func Register(router *gin.Engine, deps Deps) *Service {
	service := New(deps.DB, deps.PublicURL, nil).WithPayments(deps.Orders, deps.Starter)
	if router == nil || deps.Sessions == nil {
		return service
	}
	handler := &Handler{service: service}
	merchant := router.Group("/api/v1/merchant", deps.Sessions)
	merchant.GET("/pay-code", handler.merchantPayCode)
	merchant.POST("/test-order", handler.merchantTestOrder)
	router.GET("/pay/code/ack", handler.payCodeACK)
	router.POST("/pay/code/ack", handler.payCodeACK)
	router.GET("/pay/code/:payment_type", handler.payCodePage)
	router.POST("/pay/code/:payment_type", handler.payCodeSubmit)
	return service
}

type testOrderRequest struct {
	PaymentType string `json:"payment_type"`
	AmountCents int64  `json:"amount_cents"`
}

func (handler *Handler) merchantPayCode(ctx *gin.Context) {
	principal := identityhttp.MustSession(ctx)
	amount := int64(0)
	if raw := strings.TrimSpace(ctx.Query("amount_cents")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			invalid(ctx)
			return
		}
		amount = parsed
	}
	code, err := handler.service.PayCode(ctx.Request.Context(), principal.PrincipalID, ctx.Query("payment_type"), amount)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, code)
}

func (handler *Handler) merchantTestOrder(ctx *gin.Context) {
	principal := identityhttp.MustSession(ctx)
	var request testOrderRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalid(ctx)
		return
	}
	order, err := handler.service.CreateTestOrder(ctx.Request.Context(), principal.PrincipalID, request.PaymentType, request.AmountCents)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, order)
}

// payCodePage renders the amount form behind a scanned pay code.
func (handler *Handler) payCodePage(ctx *gin.Context) {
	paymentType := strings.ToLower(strings.TrimSpace(ctx.Param("payment_type")))
	pid := strings.TrimSpace(ctx.Query("pid"))
	amount := strings.TrimSpace(ctx.Query("amount"))
	if !paymentTypePattern.MatchString(paymentType) || pid == "" {
		ctx.Header("Content-Type", "text/html; charset=utf-8")
		ctx.String(http.StatusNotFound, `<p>收款码无效</p>`)
		return
	}
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.String(http.StatusOK, payCodePageHTML(paymentType, pid, amount))
}

// payCodeSubmit creates the order for a scanned code and hands the payer to the
// cashier.
func (handler *Handler) payCodeSubmit(ctx *gin.Context) {
	paymentType := strings.ToLower(strings.TrimSpace(ctx.Param("payment_type")))
	pid, err := strconv.ParseInt(strings.TrimSpace(ctx.PostForm("pid")), 10, 64)
	if err != nil || pid <= 0 {
		handler.payCodeFailure(ctx, http.StatusBadRequest, "收款码参数无效")
		return
	}
	amount, err := strconv.ParseInt(strings.TrimSpace(ctx.PostForm("amount")), 10, 64)
	if err != nil || amount <= 0 {
		handler.payCodeFailure(ctx, http.StatusBadRequest, "请输入支付金额（分）")
		return
	}
	if amount > maxPayCodeAmount {
		handler.payCodeFailure(ctx, http.StatusBadRequest, "支付金额超出上限")
		return
	}
	order, err := handler.service.CreatePayCodeOrder(ctx.Request.Context(), pid, amount, paymentType)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			handler.payCodeFailure(ctx, http.StatusNotFound, "商户不存在")
		case errors.Is(err, ErrUnavailable):
			handler.payCodeFailure(ctx, http.StatusServiceUnavailable, "支付通道未就绪")
		case errors.Is(err, ErrPaymentTypeUnavailable):
			handler.payCodeFailure(ctx, http.StatusBadRequest, "该支付方式不可用")
		default:
			handler.payCodeFailure(ctx, http.StatusBadRequest, "创建订单失败")
		}
		return
	}
	if _, err := handler.service.StartPayCodeOrder(ctx.Request.Context(), order.TradeNo); err != nil {
		handler.payCodeFailure(ctx, http.StatusServiceUnavailable, "拉起支付失败，请稍后重试")
		return
	}
	ctx.Redirect(http.StatusFound, handler.service.CashierURL(order.TradeNo))
}

// payCodeACK is the placeholder notify URL of a pay code order: the money lands
// on the merchant balance through the payment callback, so the platform answers
// the notification itself and no merchant endpoint is involved.
func (handler *Handler) payCodeACK(ctx *gin.Context) {
	ctx.String(http.StatusOK, "success")
}

func (handler *Handler) payCodeFailure(ctx *gin.Context, status int, message string) {
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.String(status, `<p>`+html.EscapeString(message)+`</p>`)
}

func payCodePageHTML(paymentType, pid, amount string) string {
	var builder strings.Builder
	builder.WriteString(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8">`)
	builder.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	builder.WriteString(`<title>收款码支付</title></head><body>`)
	builder.WriteString(`<h3>收款码支付</h3>`)
	builder.WriteString(`<form method="post" action="/pay/code/` + html.EscapeString(paymentType) + `">`)
	builder.WriteString(`<input type="hidden" name="pid" value="` + html.EscapeString(pid) + `">`)
	builder.WriteString(`<p>支付方式：` + html.EscapeString(paymentType) + `</p>`)
	value := ""
	if amount != "" {
		value = ` value="` + html.EscapeString(amount) + `"`
	}
	builder.WriteString(`<p><label>金额（分）<input type="number" min="1" name="amount"` + value + ` required></label></p>`)
	builder.WriteString(`<p><button type="submit">去支付</button></p>`)
	builder.WriteString(`</form></body></html>`)
	return builder.String()
}

func invalid(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}

func writeError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		invalid(ctx)
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "商户不存在"})
	case errors.Is(err, ErrNotConfigured):
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": "site_url_missing", "message": "站点公共地址未配置，无法生成收款码"})
	case errors.Is(err, ErrUnavailable):
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": "service_unavailable", "message": "自助测试支付未接线"})
	case errors.Is(err, ErrPaymentTypeUnavailable):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "payment_type_unavailable", "message": "该支付方式不可用"})
	case errors.Is(err, channelrouter.ErrNoAvailableChannel):
		ctx.JSON(http.StatusConflict, gin.H{"code": "no_available_channel", "message": "该支付方式暂无可用通道"})
	default:
		var rejection paymentservice.Rejection
		if errors.As(err, &rejection) {
			ctx.JSON(http.StatusBadRequest, gin.H{"code": "rejected", "message": rejection.Error()})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}
