// Package profitsharing splits a successful payment between the merchant and
// its configured receivers: each receiver declares a basis-point rate, the
// platform fee is taken first, and the remainder is posted to the ledger.
package profitsharing

import (
	"errors"
	"time"
)

// ReceiverType decides how a split is delivered.
type ReceiverType string

const (
	// ReceiverInternal is paid by a ledger transfer between two platform
	// liability accounts, atomic with the payment itself.
	ReceiverInternal ReceiverType = "internal"
	// ReceiverChannel needs upstream profit sharing (the acquirer splits the
	// transaction). No channel plugin implements it yet, so those splits stay
	// pending and are recorded with the reason.
	ReceiverChannel ReceiverType = "channel"
)

// ReceiverStatus controls whether a receiver takes part in new splits.
type ReceiverStatus string

const (
	ReceiverActive   ReceiverStatus = "active"
	ReceiverDisabled ReceiverStatus = "disabled"
)

// OrderStatus is the lifecycle of one split row.
type OrderStatus string

const (
	// OrderPending waits for a channel side split.
	OrderPending OrderStatus = "pending"
	// OrderDone means the ledger entry exists and the receiver was credited.
	OrderDone OrderStatus = "done"
	// OrderFailed recorded an error while posting; retry can revive it.
	OrderFailed OrderStatus = "failed"
)

var (
	ErrInvalidRequest = errors.New("invalid profit sharing request")
	ErrNotFound       = errors.New("profit sharing record not found")
	ErrRateExceeded   = errors.New("receiver rates exceed the available order amount")
	// ErrReceiverInUse blocks deleting a receiver that already produced splits.
	ErrReceiverInUse = errors.New("receiver already has profit sharing records")
	ErrOrderNotPaid  = errors.New("payment order is not paid")
)

// Receiver is one profit sharing destination of a merchant.
type Receiver struct {
	ID           string         `gorm:"type:varchar(36);primaryKey" json:"id"`
	MerchantID   string         `gorm:"type:varchar(36);not null;uniqueIndex:idx_ps_receiver_account;index" json:"merchant_id"`
	ReceiverType ReceiverType   `gorm:"type:varchar(20);not null" json:"receiver_type"`
	Account      string         `gorm:"type:varchar(191);not null;uniqueIndex:idx_ps_receiver_account" json:"account"`
	Name         string         `gorm:"type:varchar(128);not null" json:"name"`
	RateBp       int            `gorm:"not null" json:"rate_bp"`
	Status       ReceiverStatus `gorm:"type:varchar(20);not null;index" json:"status"`
	CreatedAt    time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"not null" json:"updated_at"`
}

func (Receiver) TableName() string { return "profit_sharing_receivers" }

// Order is one split produced from a paid payment order.
type Order struct {
	ID                  string      `gorm:"type:varchar(36);primaryKey" json:"id"`
	OrderID             string      `gorm:"type:varchar(36);not null;uniqueIndex:idx_ps_order_receiver;index" json:"order_id"`
	MerchantID          string      `gorm:"type:varchar(36);not null;index" json:"merchant_id"`
	ReceiverID          string      `gorm:"type:varchar(36);not null;uniqueIndex:idx_ps_order_receiver" json:"receiver_id"`
	Amount              int64       `gorm:"not null" json:"amount"`
	Status              OrderStatus `gorm:"type:varchar(20);not null;index" json:"status"`
	LedgerTransactionID *string     `gorm:"type:varchar(36)" json:"ledger_transaction_id"`
	ChannelBatchNo      *string     `gorm:"type:varchar(64)" json:"channel_batch_no"`
	Error               *string     `gorm:"type:varchar(255)" json:"error"`
	CreatedAt           time.Time   `gorm:"not null" json:"created_at"`
	UpdatedAt           time.Time   `gorm:"not null" json:"updated_at"`
}

func (Order) TableName() string { return "profit_sharing_orders" }

// Page is an offset pagination request.
type Page struct {
	Number int
	Size   int
}

// ReceiverFilter narrows a receiver listing.
type ReceiverFilter struct {
	MerchantID string
	Status     ReceiverStatus
}

// OrderFilter narrows a split listing.
type OrderFilter struct {
	MerchantID string
	OrderID    string
	Status     OrderStatus
}

func validReceiverType(value ReceiverType) bool {
	return value == ReceiverInternal || value == ReceiverChannel
}

func validReceiverStatus(value ReceiverStatus) bool {
	return value == ReceiverActive || value == ReceiverDisabled
}

func validOrderStatus(value OrderStatus) bool {
	return value == OrderPending || value == OrderDone || value == OrderFailed
}
