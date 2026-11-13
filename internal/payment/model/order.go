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
	ID          string `gorm:"type:varchar(36);primaryKey"`
	TradeNo     string `gorm:"type:varchar(32);uniqueIndex;not null"`
	MerchantID  string `gorm:"type:varchar(36);not null;uniqueIndex:idx_merchant_out_trade"`
	OutTradeNo  string `gorm:"type:varchar(64);not null;uniqueIndex:idx_merchant_out_trade"`
	PaymentType string `gorm:"type:varchar(32);not null;index"`
	Name        string `gorm:"type:varchar(255);not null"`
	Amount      int64  `gorm:"not null"`
	Currency    string `gorm:"type:char(3);not null"`
	Status      Status `gorm:"type:varchar(20);not null;index"`
	Kind        Kind   `gorm:"type:varchar(20);not null;default:'sale';index"`
	// Version is the protocol generation that created the order: 0 for the
	// legacy V1 endpoints, 1 for the V2 API. It selects the notification format
	// exactly like upstream's order.version column.
	Version   int     `gorm:"not null;default:0"`
	NotifyURL string  `gorm:"type:text;not null"`
	ReturnURL *string `gorm:"type:text"`
	ClientIP  *string `gorm:"type:varchar(45)"`
	// Buyer carries the payer identity a channel needs to start the payment
	// (sub_openid for JSAPI). Upstream keeps the same value in pre_order.buyer.
	Buyer          *string    `gorm:"type:varchar(128);index"`
	Attach         *string    `gorm:"type:text"`
	ChannelTradeNo *string    `gorm:"type:varchar(128);index"`
	PaidAt         *time.Time `gorm:"index"`
	ExpiresAt      time.Time  `gorm:"not null;index"`
	CreatedAt      time.Time  `gorm:"not null"`
	UpdatedAt      time.Time  `gorm:"not null"`
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

func ValidateAmount(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
