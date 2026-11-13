// Package cashier exposes the public cashier endpoints: starting a payment,
// polling its status, and (in sandbox mode) completing it locally.
package cashier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	channelrouter "gorixpay/internal/channel/router"
	paymentcallback "gorixpay/internal/payment/callback"
	paymentmodel "gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentservice "gorixpay/internal/payment/service"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	startLimit   = 10
	startWindow  = time.Minute
	statusLimit  = 60
	statusWindow = time.Minute

	sandboxPluginCode           = "sandbox"
	sandboxChannelTradeNoPrefix = "SANDBOX"
	sandboxDigestPrefix         = "sandbox:"
)

const (
	startIPKeyPrefix    = "cashier:start:ip:"
	startOrderKeyPrefix = "cashier:start:order:"
	statusIPKeyPrefix   = "cashier:status:ip:"
)

// Starter starts (or resumes) a payment for a trade number.
type Starter interface {
	Start(context.Context, string) (paymentorchestrator.Result, error)
}

// Limiter is a fixed-window rate limiter; the Redis client satisfies it, and a
// nil Limiter disables limiting.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

type Deps struct {
	DB *gorm.DB
	// Orders stays part of the wiring contract; the cashier reads orders through
	// DB so that start, status and complete all observe one consistent row.
	Orders         *paymentservice.Service
	Starter        Starter
	Callbacks      *paymentcallback.Service
	Limiter        Limiter
	SandboxEnabled bool
}

type Handler struct {
	deps Deps
	now  func() time.Time
}

type channelView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PluginCode string `json:"plugin_code"`
}

type startResponse struct {
	PayType string      `json:"pay_type"`
	PayInfo any         `json:"pay_info"`
	Channel channelView `json:"channel"`
	Status  string      `json:"status"`
}

// Register mounts the cashier routes. The sandbox completion route only exists
// when the sandbox is enabled, so a disabled sandbox cannot be reached at all.
func Register(router *gin.Engine, deps Deps) {
	handler := &Handler{deps: deps, now: time.Now}
	router.POST("/api/v1/cashier/orders/:trade_no/start", handler.start)
	router.GET("/api/v1/cashier/orders/:trade_no/status", handler.status)
	if deps.SandboxEnabled {
		router.POST("/api/v1/sandbox/orders/:trade_no/complete", handler.complete)
	}
}

func (handler *Handler) start(ctx *gin.Context) {
	tradeNo := strings.TrimSpace(ctx.Param("trade_no"))
	if !handler.allow(ctx, []string{startIPKeyPrefix + ctx.ClientIP(), startOrderKeyPrefix + tradeNo}, startLimit, startWindow) {
		ctx.JSON(http.StatusTooManyRequests, gin.H{"code": "too_many_requests", "message": "请求过于频繁，请稍后再试"})
		return
	}
	order, err := handler.order(ctx.Request.Context(), tradeNo)
	if err != nil {
		handler.orderError(ctx, err)
		return
	}
	switch order.Status {
	case paymentmodel.StatusPaid:
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_paid", "message": "订单已支付"})
		return
	case paymentmodel.StatusClosed, paymentmodel.StatusFailed:
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_closed", "message": "订单已关闭"})
		return
	}
	if !order.ExpiresAt.After(handler.now().UTC()) {
		ctx.JSON(http.StatusGone, gin.H{"code": "order_expired", "message": "订单已过期"})
		return
	}
	if order.Status == paymentmodel.StatusProcessing {
		if action, channel, ok := handler.storedAction(ctx.Request.Context(), order.ID); ok {
			ctx.JSON(http.StatusOK, startResponse{PayType: string(action.Type), PayInfo: payInfo(action), Channel: channel, Status: string(paymentmodel.StatusProcessing)})
			return
		}
	}
	if handler.deps.Starter == nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	result, startErr := handler.deps.Starter.Start(ctx.Request.Context(), order.TradeNo)
	if startErr != nil {
		handler.startError(ctx, startErr)
		return
	}
	ctx.JSON(http.StatusOK, startResponse{
		PayType: string(result.Action.Type),
		PayInfo: payInfo(result.Action),
		Channel: channelView{ID: result.Channel.ID, Name: result.Channel.Name, PluginCode: result.Channel.PluginCode},
		Status:  string(paymentmodel.StatusProcessing),
	})
}

func (handler *Handler) status(ctx *gin.Context) {
	if !handler.allow(ctx, []string{statusIPKeyPrefix + ctx.ClientIP()}, statusLimit, statusWindow) {
		ctx.JSON(http.StatusTooManyRequests, gin.H{"code": "too_many_requests", "message": "请求过于频繁，请稍后再试"})
		return
	}
	order, err := handler.order(ctx.Request.Context(), strings.TrimSpace(ctx.Param("trade_no")))
	if err != nil {
		handler.orderError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": order.Status, "paid_at": order.PaidAt})
}

func (handler *Handler) complete(ctx *gin.Context) {
	if !handler.deps.SandboxEnabled {
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "resource not found"})
		return
	}
	if handler.deps.Callbacks == nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	order, err := handler.order(ctx.Request.Context(), strings.TrimSpace(ctx.Param("trade_no")))
	if err != nil {
		handler.orderError(ctx, err)
		return
	}
	if order.Status == paymentmodel.StatusPaid {
		ctx.JSON(http.StatusOK, gin.H{"status": string(paymentmodel.StatusPaid)})
		return
	}
	if order.Status != paymentmodel.StatusPending && order.Status != paymentmodel.StatusProcessing {
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_not_payable", "message": "当前订单状态不可支付"})
		return
	}
	channelID := handler.latestChannelID(ctx.Request.Context(), order.ID)
	if channelID == "" {
		ctx.JSON(http.StatusConflict, gin.H{"code": "no_channel", "message": "订单尚未发起支付"})
		return
	}
	// A sandbox completion must never settle an order that was routed to a real
	// channel: only orders whose latest attempt went to the sandbox plugin may
	// be completed locally.
	if !handler.sandboxChannel(ctx.Request.Context(), channelID) {
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_not_payable", "message": "当前订单状态不可支付"})
		return
	}
	digest := sha256.Sum256([]byte(sandboxDigestPrefix + order.ID))
	if _, _, err := handler.deps.Callbacks.Complete(ctx.Request.Context(), paymentcallback.Completion{
		OrderID:        order.ID,
		ChannelID:      channelID,
		ChannelTradeNo: sandboxChannelTradeNoPrefix + order.TradeNo,
		Amount:         order.Amount,
		Digest:         hex.EncodeToString(digest[:]),
	}); err != nil {
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_not_payable", "message": "当前订单状态不可支付"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": string(paymentmodel.StatusPaid)})
}

func (handler *Handler) allow(ctx *gin.Context, keys []string, limit int, window time.Duration) bool {
	if handler.deps.Limiter == nil {
		return true
	}
	for _, key := range keys {
		allowed, err := handler.deps.Limiter.Allow(ctx.Request.Context(), key, limit, window)
		if err != nil {
			continue
		}
		if !allowed {
			return false
		}
	}
	return true
}

func (handler *Handler) order(ctx context.Context, tradeNo string) (paymentmodel.Order, error) {
	if tradeNo == "" {
		return paymentmodel.Order{}, paymentservice.ErrInvalidQuery
	}
	var order paymentmodel.Order
	if err := handler.deps.DB.WithContext(ctx).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return paymentmodel.Order{}, paymentservice.ErrOrderNotFound
		}
		return paymentmodel.Order{}, err
	}
	return order, nil
}

// storedAction replays the action saved by a previous orchestrator.Start so a
// refreshing cashier page never re-runs the upstream channel call.
func (handler *Handler) storedAction(ctx context.Context, orderID string) (pluginpayment.Action, channelView, bool) {
	var attempt paymentmodel.Attempt
	if err := handler.deps.DB.WithContext(ctx).
		Where("order_id = ? AND status = ?", orderID, paymentmodel.AttemptSucceeded).
		Order("attempt_no DESC").First(&attempt).Error; err != nil {
		return pluginpayment.Action{}, channelView{}, false
	}
	if attempt.ActionType == "" || attempt.ActionPayload == "" {
		return pluginpayment.Action{}, channelView{}, false
	}
	var action pluginpayment.Action
	if err := json.Unmarshal([]byte(attempt.ActionPayload), &action); err != nil {
		return pluginpayment.Action{}, channelView{}, false
	}
	if action.Type == "" {
		action.Type = pluginpayment.ActionType(attempt.ActionType)
	}
	return action, handler.channel(ctx, attempt.ChannelID), true
}

func (handler *Handler) latestChannelID(ctx context.Context, orderID string) string {
	var attempt paymentmodel.Attempt
	if err := handler.deps.DB.WithContext(ctx).
		Where("order_id = ?", orderID).
		Order("attempt_no DESC").First(&attempt).Error; err != nil {
		return ""
	}
	if attempt.ChannelID == nil {
		return ""
	}
	return strings.TrimSpace(*attempt.ChannelID)
}

func (handler *Handler) channel(ctx context.Context, channelID *string) channelView {
	if channelID == nil || strings.TrimSpace(*channelID) == "" {
		return channelView{}
	}
	var channel channelrouter.Channel
	if err := handler.deps.DB.WithContext(ctx).Where("id = ?", *channelID).First(&channel).Error; err != nil {
		return channelView{ID: *channelID}
	}
	return channelView{ID: channel.ID, Name: channel.Name, PluginCode: channel.PluginCode}
}

// sandboxChannel reports whether the attempt's channel is served by the sandbox
// plugin. Unknown channels are rejected, which keeps the local completion route
// from settling anything that was not created by the sandbox itself.
func (handler *Handler) sandboxChannel(ctx context.Context, channelID string) bool {
	var channel channelrouter.Channel
	if err := handler.deps.DB.WithContext(ctx).Where("id = ?", channelID).First(&channel).Error; err != nil {
		return false
	}
	return channel.PluginCode == sandboxPluginCode
}

func (handler *Handler) startError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, channelrouter.ErrNoAvailableChannel):
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": "no_channel", "message": "没有可用的支付通道"})
	case errors.Is(err, paymentorchestrator.ErrOrderNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "order_not_found", "message": "order not found"})
	case errors.Is(err, paymentorchestrator.ErrOrderNotStartable):
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_not_startable", "message": "订单正在支付中，请稍后重试"})
	default:
		ctx.JSON(http.StatusBadGateway, gin.H{"code": "channel_error", "message": "支付通道请求失败"})
	}
}

func (handler *Handler) orderError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, paymentservice.ErrOrderNotFound), errors.Is(err, paymentservice.ErrInvalidQuery):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "order_not_found", "message": "order not found"})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}

func payInfo(action pluginpayment.Action) any {
	switch action.Type {
	case pluginpayment.ActionJump, pluginpayment.ActionQRCode, pluginpayment.ActionURLScheme:
		return action.URL
	default:
		return action.Data
	}
}
