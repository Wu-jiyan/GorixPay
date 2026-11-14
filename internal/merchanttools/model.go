// Package merchanttools implements the two merchant self-service tools that
// upstream Epay serves from user/onecode.php and user/test.php: a printable
// aggregated pay code and a self-service test order.
package merchanttools

import (
	"context"
	"errors"
	"time"

	paymentorchestrator "gorixpay/internal/payment/orchestrator"
)

var (
	ErrInvalidRequest = errors.New("invalid merchant tool request")
	ErrNotFound       = errors.New("merchant not found")
	// ErrNotConfigured means the deployment has no public URL, so no scan target
	// can be produced.
	ErrNotConfigured = errors.New("site public url is not configured")
	// ErrUnavailable means the order starter was not wired in.
	ErrUnavailable = errors.New("test order service unavailable")
	// ErrPaymentTypeUnavailable rejects a payment method that the catalogue
	// marks as disabled.
	ErrPaymentTypeUnavailable = errors.New("payment type is not available")
)

// PaymentStarter is the orchestrator entry the test order and the pay code page
// need. The orchestrator service satisfies it; a nil starter makes both
// endpoints answer 503.
type PaymentStarter interface {
	Start(ctx context.Context, tradeNo string) (paymentorchestrator.Result, error)
}

// PayCode is the rendered aggregated receiving code of one merchant.
type PayCode struct {
	CodeSVG   string    `json:"code_svg"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// TestOrder is the result of a merchant self-service test payment.
type TestOrder struct {
	TradeNo    string `json:"trade_no"`
	PayType    string `json:"pay_type"`
	PayInfo    any    `json:"pay_info"`
	CashierURL string `json:"cashier_url"`
}

const (
	// maxTestOrderAmount caps a self-service test order at 1.00 CNY so the
	// endpoint cannot be used as a payout channel.
	maxTestOrderAmount int64 = 100
	// maxPayCodeAmount caps a pay code at the V2 single-payment limit.
	maxPayCodeAmount int64 = 100000 * 100
	// payCodeTTL is how long the returned code is advertised as valid.
	payCodeTTL = 24 * time.Hour
)
