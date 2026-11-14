package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	paymentmodel "gorixpay/internal/payment/model"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// maxPluginCallbackBytes bounds the raw notification body kept in memory.
const maxPluginCallbackBytes = 1 << 20

// PluginHandler completes orders from plugin-verified notifications. It is the
// generic counterpart of EpayHandler: the plugin owns the signature scheme, the
// handler owns the order bookkeeping.
type PluginHandler struct {
	db      *gorm.DB
	configs *channelconfig.Service
	service *Service
	plugins *pluginpayment.Registry
}

// RegisterPlugins mounts POST /api/v1/payment/callback/:plugin/:channel_id.
func RegisterPlugins(router *gin.Engine, db *gorm.DB, configs *channelconfig.Service, service *Service, plugins *pluginpayment.Registry) {
	handler := &PluginHandler{db: db, configs: configs, service: service, plugins: plugins}
	router.POST("/api/v1/payment/callback/:plugin/:channel_id", handler.callback)
}

func (handler *PluginHandler) callback(ctx *gin.Context) {
	pluginCode := strings.ToLower(strings.TrimSpace(ctx.Param("plugin")))
	channelID := strings.TrimSpace(ctx.Param("channel_id"))
	if pluginCode == "" || channelID == "" {
		ctx.String(http.StatusBadRequest, "fail")
		return
	}
	if handler.plugins == nil || handler.configs == nil || handler.service == nil {
		ctx.String(http.StatusInternalServerError, "fail")
		return
	}
	var channel channelrouter.Channel
	err := handler.db.WithContext(ctx.Request.Context()).
		Where("id = ? AND plugin_code = ? AND status = ?", channelID, pluginCode, channelrouter.StatusActive).
		First(&channel).Error
	if err != nil {
		ctx.String(http.StatusNotFound, "fail")
		return
	}
	plugin, exists := handler.plugins.Lookup(channel.PluginCode)
	if !exists {
		ctx.String(http.StatusNotFound, "fail")
		return
	}
	verifier, ok := plugin.(pluginpayment.CallbackVerifier)
	if !ok {
		// The plugin exists but cannot verify notifications.
		ctx.String(http.StatusNotFound, "fail")
		return
	}
	config, err := handler.configs.Decrypt(channel.ID, channel.ConfigEncrypted)
	if err != nil {
		ctx.String(http.StatusInternalServerError, "fail")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxPluginCallbackBytes+1))
	if err != nil || len(raw) > maxPluginCallbackBytes {
		ctx.String(http.StatusBadRequest, "fail")
		return
	}
	// The plugin needs the untouched body (form fields, JSON, raw signature
	// input), so it is handed the same bytes through a rewound reader.
	ctx.Request.Body = io.NopCloser(bytes.NewReader(raw))
	result, err := verifier.VerifyCallback(ctx.Request.Context(), ctx.Request, config.Values())
	if err != nil {
		if errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
			ctx.String(http.StatusInternalServerError, "fail")
			return
		}
		ctx.String(http.StatusUnauthorized, "fail")
		return
	}
	if !result.Paid {
		// A validly signed non-payment notification (closed order, refund, ...)
		// is acknowledged so the upstream stops retrying; nothing is booked.
		ctx.String(http.StatusOK, acknowledgement(result.Ack))
		return
	}
	tradeNo := strings.TrimSpace(result.TradeNo)
	channelTradeNo := strings.TrimSpace(result.ChannelTradeNo)
	if tradeNo == "" || channelTradeNo == "" || result.Amount <= 0 {
		ctx.String(http.StatusBadRequest, "fail")
		return
	}
	var order paymentmodel.Order
	if err := handler.db.WithContext(ctx.Request.Context()).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		ctx.String(http.StatusNotFound, "fail")
		return
	}
	// A callback may only settle an order that was actually routed to this
	// channel. Without this check the trade_no alone was enough, so any channel
	// (or anyone holding one channel's credentials) could confirm somebody
	// else's order and book the income.
	routed, err := handler.orderRoutedToChannel(ctx.Request.Context(), order.ID, channel.ID)
	if err != nil {
		ctx.String(http.StatusInternalServerError, "fail")
		return
	}
	if !routed {
		ctx.String(http.StatusBadRequest, "fail")
		return
	}
	if result.Amount != order.Amount {
		ctx.String(http.StatusBadRequest, "fail")
		return
	}
	_, _, err = handler.service.Complete(ctx.Request.Context(), Completion{
		OrderID: order.ID, ChannelID: channel.ID, ChannelTradeNo: channelTradeNo,
		Amount: result.Amount, Digest: notificationDigest(channel.PluginCode, channel.ID, raw),
	})
	if err != nil {
		ctx.String(completionStatus(err), "fail")
		return
	}
	ctx.String(http.StatusOK, acknowledgement(result.Ack))
}

// orderRoutedToChannel reports whether the order ever attempted payment through
// this channel. Any attempt counts, not just the latest one: a payer who retried
// on another channel may still complete an older attempt, and that payment is
// real money that must be booked.
func (handler *PluginHandler) orderRoutedToChannel(ctx context.Context, orderID, channelID string) (bool, error) {
	if orderID == "" || channelID == "" {
		return false, nil
	}
	var count int64
	if err := handler.db.WithContext(ctx).Model(&paymentmodel.Attempt{}).
		Where("order_id = ? AND channel_id = ?", orderID, channelID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// acknowledgement returns the plugin specific success body, or "success" when
// the gateway is happy with the generic one.
func acknowledgement(body string) string {
	if strings.TrimSpace(body) == "" {
		return "success"
	}
	return body
}

// notificationDigest keys the idempotency record: the same upstream callback
// (same plugin, channel and body) can never book the payment twice.
func notificationDigest(pluginCode, channelID string, raw []byte) string {
	digest := sha256.Sum256(append([]byte(pluginCode+"\n"+channelID+"\n"), raw...))
	return hex.EncodeToString(digest[:])
}

// completionStatus maps a completion failure onto the HTTP status the upstream
// should see: 400 for an amount mismatch, 409 for a conflicting replay, 404 for
// a missing order, 500 for anything unexpected.
func completionStatus(err error) int {
	switch {
	case errors.Is(err, ErrAmountMismatch):
		return http.StatusBadRequest
	case errors.Is(err, ErrOrderNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrChannelTradeConflict), errors.Is(err, ErrOrderNotPayable):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
