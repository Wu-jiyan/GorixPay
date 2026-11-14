package transfer

import (
	"errors"
	"time"
)

// Status is the payout order lifecycle.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

// Kind separates ordinary merchant withdrawals from red packet payouts. Both
// share the review lifecycle, only the listing and reporting split them.
type Kind string

const (
	KindNormal Kind = "normal"
	KindRed    Kind = "red"
)

var (
	ErrInvalidRequest      = errors.New("invalid transfer request")
	ErrNotFound            = errors.New("transfer order not found")
	ErrIdempotencyConflict = errors.New("transfer idempotency conflict")
	ErrInsufficientBalance = errors.New("insufficient transfer balance")
	ErrStateConflict       = errors.New("transfer state conflict")
)

// Order is the payout request row from transfer_orders (migration 000011).
type Order struct {
	ID                  string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	TransferNo          string     `gorm:"type:varchar(32);not null;uniqueIndex" json:"transfer_no"`
	MerchantID          string     `gorm:"type:varchar(36);not null;uniqueIndex:idx_transfer_merchant_out;index" json:"merchant_id"`
	OutTransferNo       string     `gorm:"type:varchar(64);not null;uniqueIndex:idx_transfer_merchant_out" json:"out_transfer_no"`
	Amount              int64      `gorm:"not null" json:"amount"`
	Currency            string     `gorm:"type:char(3);not null" json:"currency"`
	AccountName         string     `gorm:"type:varchar(128);not null" json:"account_name"`
	AccountNo           string     `gorm:"type:varchar(128);not null" json:"account_no"`
	BankName            string     `gorm:"type:varchar(128)" json:"bank_name"`
	BranchName          string     `gorm:"type:varchar(191)" json:"branch_name"`
	Status              Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	Kind                Kind       `gorm:"type:varchar(20);not null;default:'normal';index" json:"kind"`
	BatchNo             *string    `gorm:"type:varchar(64);index" json:"batch_no"`
	Remark              *string    `gorm:"type:varchar(191)" json:"remark"`
	LedgerTransactionID *string    `gorm:"type:varchar(36)" json:"ledger_transaction_id"`
	RejectionReason     *string    `gorm:"type:varchar(255)" json:"rejection_reason"`
	ProcessedBy         *string    `gorm:"type:varchar(36)" json:"processed_by"`
	ProcessedAt         *time.Time `json:"processed_at"`
	CreatedAt           time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"not null" json:"updated_at"`
}

func (Order) TableName() string { return "transfer_orders" }

func ValidStatus(status Status) bool {
	return status == StatusPending || status == StatusApproved || status == StatusRejected
}

// ValidKind reports whether a kind is understood. The empty value means normal.
func ValidKind(kind Kind) bool {
	return kind == "" || kind == KindNormal || kind == KindRed
}

// NormalizeKind maps the empty value onto the ordinary withdrawal kind.
func NormalizeKind(kind Kind) Kind {
	if kind == "" {
		return KindNormal
	}
	return kind
}
