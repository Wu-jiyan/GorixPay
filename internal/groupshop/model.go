// Package groupshop sells merchant groups: a merchant pays its balance for a
// group, the platform books the fee on the ledger and the merchant is moved into
// the group. A refund reverses the posting and detaches the merchant.
package groupshop

import (
	"errors"
	"time"
)

// PurchaseStatus is the lifecycle of one group purchase.
type PurchaseStatus string

const (
	// PurchaseActive means the group is bought and applied.
	PurchaseActive PurchaseStatus = "active"
	// PurchaseRefunded means the platform returned the money and detached the
	// merchant; the row stays for the audit trail.
	PurchaseRefunded PurchaseStatus = "refunded"
)

var (
	ErrInvalidRequest = errors.New("invalid group purchase request")
	ErrNotFound       = errors.New("group or purchase not found")
	// ErrGroupNotForSale covers a group that is disabled or priced at zero.
	ErrGroupNotForSale = errors.New("group is not for sale")
	// ErrInsufficientBalance means the merchant balance cannot cover the price.
	ErrInsufficientBalance = errors.New("merchant balance is insufficient")
	// ErrNotRefundable means the purchase is already refunded.
	ErrNotRefundable = errors.New("purchase is not refundable")
)

// Purchase is one paid group membership (group_purchases).
type Purchase struct {
	ID                  string         `gorm:"type:varchar(36);primaryKey" json:"purchase_id"`
	MerchantID          string         `gorm:"type:varchar(36);not null;uniqueIndex:idx_group_purchase_pair;index" json:"merchant_id"`
	GroupID             string         `gorm:"type:varchar(36);not null;uniqueIndex:idx_group_purchase_pair" json:"group_id"`
	PriceCents          int64          `gorm:"not null" json:"price_cents"`
	Status              PurchaseStatus `gorm:"type:varchar(20);not null;index" json:"status"`
	Attempts            int            `gorm:"not null;default:0" json:"attempts"`
	LedgerTransactionID *string        `gorm:"type:varchar(36)" json:"ledger_transaction_id"`
	ExpiresAt           *time.Time     `json:"expires_at"`
	CreatedAt           time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt           time.Time      `gorm:"not null" json:"updated_at"`
}

func (Purchase) TableName() string { return "group_purchases" }

// Plan is the read model of merchant_groups. The column set is a subset of the
// table owned by internal/merchantgroup, so this package never writes group
// fields other than price_cents.
type Plan struct {
	ID         string    `gorm:"column:id;type:varchar(36);primaryKey" json:"id"`
	Name       string    `gorm:"column:name;type:varchar(64);not null" json:"name"`
	RateBp     int       `gorm:"column:rate_bp;not null;default:0" json:"rate_bp"`
	SettleMode string    `gorm:"column:settle_mode;type:varchar(20)" json:"settle_mode"`
	Status     string    `gorm:"column:status;type:varchar(20)" json:"status"`
	PriceCents int64     `gorm:"column:price_cents;not null;default:0" json:"price_cents"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (Plan) TableName() string { return "merchant_groups" }

// Page is an offset pagination request.
type Page struct {
	Number int
	Size   int
}

// Offset translates the page into a GORM offset.
func (page Page) Offset() int {
	number := page.Number
	if number < 1 {
		number = 1
	}
	return (number - 1) * page.Limit()
}

// Limit clamps the page size to 200.
func (page Page) Limit() int {
	size := page.Size
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return size
}

// PurchaseFilter narrows the administrator listing.
type PurchaseFilter struct {
	MerchantID string
	Status     PurchaseStatus
}

func validPurchaseStatus(status PurchaseStatus) bool {
	return status == PurchaseActive || status == PurchaseRefunded
}
