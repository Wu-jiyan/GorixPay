package payment

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math/big"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	channelrouter "gorixpay/internal/channel/router"
	epayauth "gorixpay/internal/compat/epay/auth"
	"gorixpay/internal/merchant/credential"
	paymentmodel "gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentservice "gorixpay/internal/payment/service"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var amountPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

type Handler struct {
	orders  *paymentservice.Service
	siteURL string
	starter PaymentStarter
}

type PaymentStarter interface {
	Start(context.Context, string) (paymentorchestrator.Result, error)
}

func Register(router *gin.Engine, db *gorm.DB, credentials *credential.Service, orders *paymentservice.Service, siteURL string, starter PaymentStarter) {
	handler := &Handler{orders: orders, siteURL: strings.TrimRight(siteURL, "/"), starter: starter}
	authenticate := epayauth.Middleware(db, credentials)
	router.POST("/mapi.php", authenticate, handler.mapi)
	router.GET("/submit.php", authenticate, handler.submit)
	router.POST("/submit.php", authenticate, handler.submit)
	router.POST("/api/pay/query", authenticate, handler.query)
	router.POST("/api/pay/close", authenticate, handler.close)
	router.GET("/api/v1/cashier/orders/:trade_no", handler.cashierOrder)
}

func (handler *Handler) mapi(ctx *gin.Context) {
	merchant := epayauth.MustMerchant(ctx)
	command, err := commandFromRequest(ctx, merchant.ID, true)
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": err.Error()})
		return
	}
	order, _, err := handler.orders.Create(ctx.Request.Context(), command)
	if err != nil {
		handleOrderError(ctx, command.OutTradeNo, err, false)
		return
	}
	action := pluginpayment.Action{Type: pluginpayment.ActionJump, URL: handler.cashierURL(order.TradeNo)}
	if handler.starter != nil {
		result, startErr := handler.starter.Start(ctx.Request.Context(), order.TradeNo)
		if startErr == nil {
			action = result.Action
		} else if !errors.Is(startErr, channelrouter.ErrNoAvailableChannel) {
			ctx.JSON(http.StatusOK, gin.H{"code": -1, "msg": "支付通道请求失败"})
			return
		}
	}
	payType, payInfo := epayAction(action)
	ctx.JSON(http.StatusOK, gin.H{
		"code":     1,
		"msg":      "订单创建成功",
		"trade_no": order.TradeNo,
		"pay_type": payType,
		"pay_info": payInfo,
	})
}

func (handler *Handler) submit(ctx *gin.Context) {
	merchant := epayauth.MustMerchant(ctx)
	command, err := commandFromRequest(ctx, merchant.ID, false)
	if err != nil {
		ctx.Header("Content-Type", "text/html; charset=UTF-8")
		ctx.String(http.StatusOK, "<!doctype html><html><body>%s</body></html>", html.EscapeString(err.Error()))
		return
	}
	order, _, err := handler.orders.Create(ctx.Request.Context(), command)
	if err != nil {
		handleOrderError(ctx, command.OutTradeNo, err, true)
		return
	}
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	ctx.String(http.StatusOK, `<!doctype html><html><head><meta charset="UTF-8"><title>正在跳转</title></head><body><script>window.location.replace(%q);</script></body></html>`, handler.cashierURL(order.TradeNo))
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
		"expires_at":   order.ExpiresAt,
	})
}

func commandFromRequest(ctx *gin.Context, merchantID string, requireClientIP bool) (paymentservice.CreateCommand, error) {
	amount, err := ParseAmount(ctx.Request.Form.Get("money"))
	if err != nil {
		return paymentservice.CreateCommand{}, errors.New("金额不合法")
	}
	clientIP := ctx.Request.Form.Get("clientip")
	if requireClientIP && net.ParseIP(clientIP) == nil {
		return paymentservice.CreateCommand{}, errors.New("用户IP地址(clientip)不合法")
	}
	command := paymentservice.CreateCommand{
		MerchantID:  merchantID,
		OutTradeNo:  ctx.Request.Form.Get("out_trade_no"),
		PaymentType: ctx.Request.Form.Get("type"),
		Name:        ctx.Request.Form.Get("name"),
		Amount:      amount,
		NotifyURL:   ctx.Request.Form.Get("notify_url"),
		ReturnURL:   ctx.Request.Form.Get("return_url"),
		ClientIP:    clientIP,
		Attach:      ctx.Request.Form.Get("param"),
	}
	if !requireClientIP && command.PaymentType == "" {
		command.PaymentType = "cashier"
	}
	return command, nil
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
	if errors.Is(err, paymentservice.ErrIdempotencyConflict) {
		message = fmt.Sprintf("该订单(%s)支付参数有变化，请更换订单号重新发起支付", outTradeNo)
	} else if errors.Is(err, paymentservice.ErrInvalidCommand) {
		message = "提交参数不完整或格式不正确"
	}
	if htmlResponse {
		ctx.Header("Content-Type", "text/html; charset=UTF-8")
		ctx.String(http.StatusOK, "<!doctype html><html><body>%s</body></html>", html.EscapeString(message))
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
