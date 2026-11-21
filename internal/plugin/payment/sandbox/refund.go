package sandbox

import (
	"context"
	"errors"
	"strings"

	pluginpayment "gorixpay/internal/plugin/payment"
)

// Refund settles a sandbox payment locally: there is no acquirer to ask, so the
// refund always succeeds and reports the refund number as its own reference. It
// exists so the whole refund path (order → finance record → channel call → ledger)
// can be exercised end to end without real money.
func (*Plugin) Refund(_ context.Context, request pluginpayment.RefundRequest) (pluginpayment.RefundResult, error) {
	refundNo := strings.TrimSpace(request.RefundNo)
	if refundNo == "" {
		return pluginpayment.RefundResult{}, errors.New("sandbox refund requires a refund number")
	}
	if request.Amount <= 0 {
		return pluginpayment.RefundResult{}, errors.New("sandbox refund requires a positive amount")
	}
	return pluginpayment.RefundResult{ChannelRefundNo: "SANDBOX-" + refundNo, Amount: request.Amount}, nil
}

var _ pluginpayment.Refunder = (*Plugin)(nil)
