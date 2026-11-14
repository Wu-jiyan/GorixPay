package settlement

import (
	"errors"
	"time"
)

type RuleStatus string
type Period string
type TransferStatus string
type SettleSource string

const (
	RuleActive RuleStatus = "active"
	RulePaused RuleStatus = "paused"

	PeriodDaily Period = "daily"

	TransferNone    TransferStatus = "none"
	TransferPending TransferStatus = "pending"
	TransferDone    TransferStatus = "done"
	TransferFailed  TransferStatus = "failed"

	SourceManual SettleSource = "manual"
	SourceAuto   SettleSource = "auto"
)

var (
	ErrInvalidRequest      = errors.New("invalid settlement request")
	ErrRuleNotFound        = errors.New("settle rule not found")
	ErrSettlementNotFound  = errors.New("settlement not found")
	ErrSettlementNotReady  = errors.New("settlement is not approved")
	ErrNoSettlementProfile = errors.New("merchant has no settlement profile")
)

// Rule is one scheduled settlement rule. A nil MerchantID means the rule applies
// to every merchant with a settlement profile.
type Rule struct {
	ID             string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	Name           string     `gorm:"type:varchar(100);not null" json:"name"`
	MerchantID     *string    `gorm:"type:varchar(36);index" json:"merchant_id"`
	MinAmountCents int64      `gorm:"not null;default:0" json:"min_amount_cents"`
	FeeBP          int        `gorm:"not null;default:0" json:"fee_bp"`
	Period         Period     `gorm:"type:varchar(20);not null;default:'daily'" json:"period"`
	AutoTransfer   bool       `gorm:"not null;default:false" json:"auto_transfer"`
	Status         RuleStatus `gorm:"type:varchar(20);not null;default:'active';index" json:"status"`
	LastRunAt      *time.Time `json:"last_run_at"`
	CreatedAt      time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"not null" json:"updated_at"`
}

func (Rule) TableName() string { return "settle_rules" }

func ValidRuleStatus(status RuleStatus) bool {
	return status == RuleActive || status == RulePaused
}

func ValidPeriod(period Period) bool {
	return period == PeriodDaily
}

// settlementRow reads the finance settlement row together with the automation
// columns added by migration 000018.
type settlementRow struct {
	ID                  string         `gorm:"column:id;primaryKey"`
	SettlementNo        string         `gorm:"column:settlement_no"`
	MerchantID          string         `gorm:"column:merchant_id"`
	OutSettlementNo     string         `gorm:"column:out_settlement_no"`
	Amount              int64          `gorm:"column:amount"`
	Currency            string         `gorm:"column:currency"`
	Destination         string         `gorm:"column:destination"`
	Status              string         `gorm:"column:status"`
	Source              SettleSource   `gorm:"column:source;not null;default:'manual'"`
	FeeCents            int64          `gorm:"column:fee_cents;not null;default:0"`
	TransferStatus      TransferStatus `gorm:"column:transfer_status;not null;default:'none'"`
	TransferAt          *time.Time     `gorm:"column:transfer_at"`
	LedgerTransactionID *string        `gorm:"column:ledger_transaction_id"`
	RejectionReason     *string        `gorm:"column:rejection_reason"`
	ProcessedBy         *string        `gorm:"column:processed_by"`
	ProcessedAt         *time.Time     `gorm:"column:processed_at"`
	CreatedAt           time.Time      `gorm:"column:created_at"`
	UpdatedAt           time.Time      `gorm:"column:updated_at"`
}

func (settlementRow) TableName() string { return "settlement_orders" }
