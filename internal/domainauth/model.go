package domainauth

import (
	"errors"
	"time"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

var (
	ErrNotFound       = errors.New("domain not found")
	ErrInvalidRequest = errors.New("invalid domain request")
	ErrInvalidDomain  = errors.New("invalid domain format")
	ErrDuplicate      = errors.New("domain already submitted")
	ErrStateConflict  = errors.New("domain state conflict")
)

// Domain is one approved or pending payment domain of a merchant.
type Domain struct {
	ID              string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	MerchantID      string     `gorm:"type:varchar(36);not null;uniqueIndex:idx_merchant_domain" json:"merchant_id"`
	Domain          string     `gorm:"type:varchar(191);not null;uniqueIndex:idx_merchant_domain" json:"domain"`
	Status          Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	RejectionReason *string    `gorm:"type:varchar(255)" json:"rejection_reason"`
	ReviewerID      *string    `gorm:"type:varchar(36)" json:"reviewer_id"`
	CreatedAt       time.Time  `gorm:"not null" json:"created_at"`
	ReviewedAt      *time.Time `json:"reviewed_at"`
}

func (Domain) TableName() string { return "merchant_domains" }

func ValidStatus(status Status) bool {
	return status == StatusPending || status == StatusApproved || status == StatusRejected
}
