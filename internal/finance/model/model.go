package model

import (
	"errors"
	"time"
)

var ErrInvalidTransition = errors.New("invalid finance order status transition")

type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

type Refund struct {
	ID                  string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	RefundNo            string     `gorm:"type:varchar(32);not null;uniqueIndex" json:"refund_no"`
	MerchantID          string     `gorm:"type:varchar(36);not null;uniqueIndex:idx_refund_merchant_out" json:"merchant_id"`
	PaymentOrderID      string     `gorm:"type:varchar(36);not null;index" json:"payment_order_id"`
	OutRefundNo         string     `gorm:"type:varchar(64);not null;uniqueIndex:idx_refund_merchant_out" json:"out_refund_no"`
	Amount              int64      `gorm:"not null" json:"amount"`
	Currency            string     `gorm:"type:char(3);not null" json:"currency"`
	Reason              string     `gorm:"type:varchar(255);not null" json:"reason"`
	Status              Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	LedgerTransactionID *string    `gorm:"type:varchar(36)" json:"ledger_transaction_id"`
	RejectionReason     *string    `gorm:"type:varchar(255)" json:"rejection_reason"`
	ProcessedBy         *string    `gorm:"type:varchar(36)" json:"processed_by"`
	ProcessedAt         *time.Time `json:"processed_at"`
	CreatedAt           time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"not null" json:"updated_at"`
}

func (Refund) TableName() string { return "refund_orders" }

type Settlement struct {
	ID                  string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	SettlementNo        string     `gorm:"type:varchar(32);not null;uniqueIndex" json:"settlement_no"`
	MerchantID          string     `gorm:"type:varchar(36);not null;uniqueIndex:idx_settlement_merchant_out" json:"merchant_id"`
	OutSettlementNo     string     `gorm:"type:varchar(64);not null;uniqueIndex:idx_settlement_merchant_out" json:"out_settlement_no"`
	Amount              int64      `gorm:"not null" json:"amount"`
	Currency            string     `gorm:"type:char(3);not null" json:"currency"`
	Destination         string     `gorm:"type:varchar(255);not null" json:"destination"`
	Status              Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	LedgerTransactionID *string    `gorm:"type:varchar(36)" json:"ledger_transaction_id"`
	RejectionReason     *string    `gorm:"type:varchar(255)" json:"rejection_reason"`
	ProcessedBy         *string    `gorm:"type:varchar(36)" json:"processed_by"`
	ProcessedAt         *time.Time `json:"processed_at"`
	CreatedAt           time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"not null" json:"updated_at"`
}

func (Settlement) TableName() string { return "settlement_orders" }

func ValidStatus(status Status) bool {
	return status == StatusPending || status == StatusApproved || status == StatusRejected
}
