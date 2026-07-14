package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrImmutable = errors.New("ledger records are immutable")

type AccountStatus string
type AccountCategory string
type Direction string
type Bucket string
type FreezeStatus string
type OperationType string
type OperationStatus string

const (
	AccountActive   AccountStatus = "active"
	AccountDisabled AccountStatus = "disabled"

	CategoryAsset     AccountCategory = "asset"
	CategoryLiability AccountCategory = "liability"
	CategoryEquity    AccountCategory = "equity"
	CategoryRevenue   AccountCategory = "revenue"
	CategoryExpense   AccountCategory = "expense"

	DirectionDebit  Direction = "debit"
	DirectionCredit Direction = "credit"

	BucketAvailable Bucket = "available"
	BucketFrozen    Bucket = "frozen"

	FreezeActive   FreezeStatus = "active"
	FreezeReleased FreezeStatus = "released"

	OperationCredit   OperationType = "credit"
	OperationDebit    OperationType = "debit"
	OperationTransfer OperationType = "transfer"

	OperationPending  OperationStatus = "pending"
	OperationPosted   OperationStatus = "posted"
	OperationRejected OperationStatus = "rejected"
)

type Account struct {
	ID        string          `gorm:"type:varchar(36);primaryKey" json:"id"`
	OwnerType string          `gorm:"type:varchar(32);not null;uniqueIndex:idx_ledger_account_owner" json:"owner_type"`
	OwnerID   string          `gorm:"type:varchar(36);not null;uniqueIndex:idx_ledger_account_owner" json:"owner_id"`
	Purpose   string          `gorm:"type:varchar(32);not null;uniqueIndex:idx_ledger_account_owner" json:"purpose"`
	Category  AccountCategory `gorm:"type:varchar(20);not null" json:"category"`
	Currency  string          `gorm:"type:char(3);not null;uniqueIndex:idx_ledger_account_owner" json:"currency"`
	Status    AccountStatus   `gorm:"type:varchar(20);not null;index" json:"status"`
	CreatedAt time.Time       `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time       `gorm:"not null" json:"updated_at"`
}

func (Account) TableName() string { return "ledger_accounts" }

type Transaction struct {
	ID             string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	IdempotencyKey string    `gorm:"type:varchar(191);not null;uniqueIndex" json:"idempotency_key"`
	RequestHash    string    `gorm:"type:varchar(64);not null" json:"-"`
	ReferenceType  string    `gorm:"type:varchar(64);not null;index:idx_ledger_reference" json:"reference_type"`
	ReferenceID    string    `gorm:"type:varchar(64);not null;index:idx_ledger_reference" json:"reference_id"`
	Description    string    `gorm:"type:varchar(255);not null" json:"description"`
	Metadata       string    `gorm:"type:text;not null" json:"metadata"`
	CreatedAt      time.Time `gorm:"not null;index" json:"created_at"`
}

func (Transaction) TableName() string                        { return "ledger_transactions" }
func (transaction *Transaction) BeforeUpdate(*gorm.DB) error { return ErrImmutable }
func (transaction *Transaction) BeforeDelete(*gorm.DB) error { return ErrImmutable }

type Entry struct {
	ID            string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	TransactionID string    `gorm:"type:varchar(36);not null;uniqueIndex:idx_ledger_entry_sequence" json:"transaction_id"`
	AccountID     string    `gorm:"type:varchar(36);not null;index" json:"account_id"`
	Sequence      int       `gorm:"not null;uniqueIndex:idx_ledger_entry_sequence" json:"sequence"`
	Direction     Direction `gorm:"type:varchar(10);not null" json:"direction"`
	Bucket        Bucket    `gorm:"type:varchar(20);not null" json:"bucket"`
	Amount        int64     `gorm:"not null" json:"amount"`
	BalanceAfter  int64     `gorm:"not null" json:"balance_after"`
	CreatedAt     time.Time `gorm:"not null;index" json:"created_at"`
}

func (Entry) TableName() string                  { return "ledger_entries" }
func (entry *Entry) BeforeUpdate(*gorm.DB) error { return ErrImmutable }
func (entry *Entry) BeforeDelete(*gorm.DB) error { return ErrImmutable }

type Balance struct {
	AccountID string    `gorm:"type:varchar(36);primaryKey" json:"account_id"`
	Available int64     `gorm:"not null" json:"available"`
	Frozen    int64     `gorm:"not null" json:"frozen"`
	Version   int64     `gorm:"not null" json:"version"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

func (Balance) TableName() string { return "ledger_balances" }

type Freeze struct {
	ID                   string       `gorm:"type:varchar(36);primaryKey" json:"id"`
	AccountID            string       `gorm:"type:varchar(36);not null;index" json:"account_id"`
	BusinessKey          string       `gorm:"type:varchar(191);not null;uniqueIndex" json:"business_key"`
	Amount               int64        `gorm:"not null" json:"amount"`
	Status               FreezeStatus `gorm:"type:varchar(20);not null;index" json:"status"`
	FreezeTransactionID  string       `gorm:"type:varchar(36);not null" json:"freeze_transaction_id"`
	ReleaseTransactionID *string      `gorm:"type:varchar(36)" json:"release_transaction_id"`
	CreatedAt            time.Time    `gorm:"not null" json:"created_at"`
	ReleasedAt           *time.Time   `json:"released_at"`
}

func (Freeze) TableName() string { return "ledger_freezes" }

type Operation struct {
	ID                    string          `gorm:"type:varchar(36);primaryKey" json:"id"`
	IdempotencyKey        string          `gorm:"type:varchar(191);not null;uniqueIndex" json:"idempotency_key"`
	RequestHash           string          `gorm:"type:varchar(64);not null" json:"-"`
	OperationType         OperationType   `gorm:"type:varchar(20);not null;index" json:"operation_type"`
	Status                OperationStatus `gorm:"type:varchar(20);not null;index" json:"status"`
	AccountID             string          `gorm:"type:varchar(36);not null;index" json:"account_id"`
	CounterpartyAccountID string          `gorm:"type:varchar(36);not null;index" json:"counterparty_account_id"`
	Amount                int64           `gorm:"not null" json:"amount"`
	ReferenceType         string          `gorm:"type:varchar(64);not null;index" json:"reference_type"`
	ReferenceID           string          `gorm:"type:varchar(64);not null;index" json:"reference_id"`
	Description           string          `gorm:"type:varchar(255);not null" json:"description"`
	Metadata              string          `gorm:"type:text;not null" json:"metadata"`
	CreatedBy             string          `gorm:"type:varchar(36);not null;index" json:"created_by"`
	ProcessedBy           *string         `gorm:"type:varchar(36)" json:"processed_by"`
	TransactionID         *string         `gorm:"type:varchar(36)" json:"transaction_id"`
	RejectionReason       *string         `gorm:"type:varchar(255)" json:"rejection_reason"`
	CreatedAt             time.Time       `gorm:"not null;index" json:"created_at"`
	ProcessedAt           *time.Time      `json:"processed_at"`
}

func (Operation) TableName() string { return "ledger_operations" }
