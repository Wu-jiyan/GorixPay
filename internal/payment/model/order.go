package model

import (
	"errors"
	"time"
)

var (
	ErrInvalidTransition = errors.New("invalid order status transition")
	ErrInvalidAmount     = errors.New("amount must be positive")
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusPaid       Status = "paid"
	StatusClosed     Status = "closed"
	StatusFailed     Status = "failed"
)

// Kind distinguishes merchant sales from self-service account funding orders.
type Kind string

const (
	KindSale     Kind = "sale"
	KindRecharge Kind = "recharge"
	KindDeposit  Kind = "deposit"
)

type Order struct {
	ID          string `gorm:"type:varchar(36);primaryKey" json:"id"`
	TradeNo     string `gorm:"type:varchar(32);uniqueIndex;not null" json:"trade_no"`
	MerchantID  string `gorm:"type:varchar(36);not null;uniqueIndex:idx_merchant_out_trade" json:"merchant_id"`
	OutTradeNo  string `gorm:"type:varchar(64);not null;uniqueIndex:idx_merchant_out_trade" json:"out_trade_no"`
	PaymentType string `gorm:"type:varchar(32);not null;index" json:"payment_type"`
	Name        string `gorm:"type:varchar(255);not null" json:"name"`
	Amount      int64  `gorm:"not null" json:"amount"`
	Currency    string `gorm:"type:char(3);not null" json:"currency"`
	Status      Status `gorm:"type:varchar(20);not null;index" json:"status"`
	Kind        Kind   `gorm:"type:varchar(20);not null;default:'sale';index" json:"kind"`
	// Version is the protocol generation that created the order: 0 for the
	// legacy V1 endpoints, 1 for the V2 API. It selects the notification format
	// exactly like upstream's order.version column.
	Version   int     `gorm:"not null;default:0" json:"version"`
	NotifyURL string  `gorm:"type:text;not null" json:"notify_url"`
	ReturnURL *string `gorm:"type:text" json:"return_url"`
	ClientIP  *string `gorm:"type:varchar(45)" json:"client_ip"`
	// Buyer carries the payer identity a channel needs to start the payment
	// (sub_openid for JSAPI). Upstream keeps the same value in pre_order.buyer.
	Buyer          *string    `gorm:"type:varchar(128);index" json:"buyer"`
	Attach         *string    `gorm:"type:text" json:"attach"`
	ChannelTradeNo *string    `gorm:"type:varchar(128);index" json:"channel_trade_no"`
	PaidAt         *time.Time `gorm:"index" json:"paid_at"`
	ExpiresAt      time.Time  `gorm:"not null;index" json:"expires_at"`
	CreatedAt      time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"not null" json:"updated_at"`
}

func (Order) TableName() string {
	return "payment_orders"
}

// IsSale reports whether the order is a merchant sale (the only kind that
// triggers merchant notifications and invite rebates).
func (order Order) IsSale() bool {
	return order.Kind == "" || order.Kind == KindSale
}

func (order *Order) Transition(target Status, at time.Time) error {
	allowed := false
	switch order.Status {
	case StatusPending:
		allowed = target == StatusProcessing || target == StatusPaid || target == StatusClosed || target == StatusFailed
	case StatusProcessing:
		allowed = target == StatusPaid || target == StatusClosed || target == StatusFailed
	}
	if !allowed {
		return ErrInvalidTransition
	}
	order.Status = target
	order.UpdatedAt = at.UTC()
	if target == StatusPaid {
		paidAt := at.UTC()
		order.PaidAt = &paidAt
	}
	return nil
}

func (order Order) Terminal() bool {
	return order.Status == StatusPaid || order.Status == StatusClosed || order.Status == StatusFailed
}

// MaxAmount caps a single order at 100000.00 CNY, the same ceiling the V1 and V2
// protocol layers apply. Without it a machine caller could create a recharge for
// an amount that overflows every downstream fee calculation.
const MaxAmount int64 = 100000 * 100

func ValidateAmount(amount int64) error {
	if amount <= 0 || amount > MaxAmount {
		return ErrInvalidAmount
	}
	return nil
}
