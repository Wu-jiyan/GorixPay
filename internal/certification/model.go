package certification

import (
	"errors"
	"time"
)

type CertType string
type Status string

const (
	CertTypePerson CertType = "person"
	CertTypeCorp   CertType = "corp"

	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

var (
	ErrNotFound            = errors.New("certification not found")
	ErrInvalidRequest      = errors.New("invalid certification request")
	ErrInvalidIDNo         = errors.New("invalid id number")
	ErrAlreadyApproved     = errors.New("certification already approved")
	ErrPendingReview       = errors.New("certification already pending review")
	ErrInsufficientBalance = errors.New("insufficient balance for certification fee")
	ErrStateConflict       = errors.New("certification state conflict")
)

// Certification is the merchant real-name certification row.
type Certification struct {
	ID                string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	MerchantID        string     `gorm:"type:varchar(36);not null;uniqueIndex" json:"merchant_id"`
	CertType          CertType   `gorm:"type:varchar(10);not null" json:"cert_type"`
	RealNameEncrypted string     `gorm:"type:text;not null" json:"-"`
	IDNoEncrypted     string     `gorm:"type:text;not null" json:"-"`
	CorpName          *string    `gorm:"type:varchar(128)" json:"corp_name"`
	CorpNo            *string    `gorm:"type:varchar(64)" json:"corp_no"`
	Status            Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	FeeCents          int64      `gorm:"not null;default:0" json:"fee_cents"`
	RejectionReason   *string    `gorm:"type:varchar(255)" json:"rejection_reason"`
	ReviewerID        *string    `gorm:"type:varchar(36)" json:"reviewer_id"`
	SubmittedAt       time.Time  `gorm:"not null" json:"submitted_at"`
	ReviewedAt        *time.Time `json:"reviewed_at"`
	CreatedAt         time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"not null" json:"updated_at"`
}

func (Certification) TableName() string { return "merchant_certifications" }

func ValidStatus(status Status) bool {
	return status == StatusPending || status == StatusApproved || status == StatusRejected
}
