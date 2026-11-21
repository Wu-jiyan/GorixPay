package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	channelrouter "gorixpay/internal/channel/router"
	paymentmodel "gorixpay/internal/payment/model"
	pluginpayment "gorixpay/internal/plugin/payment"

	"gorm.io/gorm"
)

// ErrRefundUnsupported means the channel never captured a payment we can return,
// or its plugin has no refund implementation. The finance layer can still record
// a bookkeeping refund; the money simply has to be moved by hand.
var ErrRefundUnsupported = errors.New("channel does not support refunds")

// Refund sends money back through the channel that captured the order.
//
// The channel is taken from the order's successful attempt, not from a fresh
// selection: a refund has to travel the same route as the payment did, including
// the merchant's account and key pair.
func (service *Service) Refund(ctx context.Context, order paymentmodel.Order, refundNo string, amount int64, description string) (string, error) {
	if amount <= 0 {
		return "", fmt.Errorf("%w: refund amount", pluginpayment.ErrInvalidConfiguration)
	}
	channelID, err := service.attemptChannelID(ctx, order.ID)
	if err != nil {
		return "", err
	}
	channel, plugin, err := service.router.ByID(ctx, channelID, pluginpayment.CapabilityRefund)
	if err != nil {
		if errors.Is(err, channelrouter.ErrNoAvailableChannel) {
			return "", ErrRefundUnsupported
		}
		return "", err
	}
	refunder, ok := plugin.(pluginpayment.Refunder)
	if !ok {
		return "", ErrRefundUnsupported
	}
	configuration := make(map[string]string)
	if service.configs != nil {
		decoded, err := service.configs.Decrypt(channel.ID, channel.ConfigEncrypted)
		if err != nil {
			return "", err
		}
		configuration = decoded.Values()
	}
	result, err := refunder.Refund(ctx, pluginpayment.RefundRequest{
		TradeNo:       order.TradeNo,
		OrgTradeNo:    channelTradeNo(order),
		RefundNo:      refundNo,
		Amount:        amount,
		OrgAmount:     order.Amount,
		Currency:      order.Currency,
		Description:   description,
		Configuration: configuration,
	})
	if err != nil {
		return "", err
	}
	return result.ChannelRefundNo, nil
}

// attemptChannelID is the channel that produced the paid attempt.
func (service *Service) attemptChannelID(ctx context.Context, orderID string) (string, error) {
	var attempt paymentmodel.Attempt
	err := service.db.WithContext(ctx).
		Where("order_id = ? AND status = ? AND channel_id IS NOT NULL", orderID, paymentmodel.AttemptSucceeded).
		Order("attempt_no DESC").First(&attempt).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrRefundUnsupported
		}
		return "", err
	}
	if attempt.ChannelID == nil || strings.TrimSpace(*attempt.ChannelID) == "" {
		return "", ErrRefundUnsupported
	}
	return *attempt.ChannelID, nil
}

// channelTradeNo is the reference the channel gave the payment, when it gave one.
// Acquirers generally accept their own order number for a refund request.
func channelTradeNo(order paymentmodel.Order) string {
	if order.ChannelTradeNo != nil {
		return strings.TrimSpace(*order.ChannelTradeNo)
	}
	return ""
}