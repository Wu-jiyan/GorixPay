package device

import (
	"errors"
	"time"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"

	BroadcastPending   Status = "pending"
	BroadcastDelivered Status = "delivered"

	JobPending Status = "pending"
	JobSent    Status = "sent"
	JobPrinted Status = "printed"
)

var (
	ErrInvalidRequest = errors.New("invalid device request")
	ErrUnauthorized   = errors.New("device authentication failed")
	ErrNotFound       = errors.New("device not found")
	ErrDuplicate      = errors.New("device already registered")
	ErrUnavailable    = errors.New("test order service unavailable")
)

// VoiceDevice is one speaker terminal. The secret is a plain random string
// because the device firmware cannot do anything stronger.
type VoiceDevice struct {
	ID         string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	MerchantID *string    `gorm:"type:varchar(36);index" json:"merchant_id"`
	DeviceNo   string     `gorm:"type:varchar(64);not null;uniqueIndex" json:"device_no"`
	Name       string     `gorm:"type:varchar(64);not null" json:"name"`
	Secret     string     `gorm:"type:varchar(64);not null" json:"-"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	Status     Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	CreatedAt  time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"not null" json:"updated_at"`
}

func (VoiceDevice) TableName() string { return "voice_devices" }

// Broadcast is one spoken notification queued for a device.
type Broadcast struct {
	ID          string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	DeviceID    string     `gorm:"type:varchar(36);not null;index" json:"device_id"`
	OrderID     *string    `gorm:"type:varchar(36);index" json:"order_id"`
	Text        string     `gorm:"type:varchar(255);not null" json:"text"`
	AmountCents int64      `gorm:"not null;default:0" json:"amount_cents"`
	Status      Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	CreatedAt   time.Time  `gorm:"not null" json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at"`
}

func (Broadcast) TableName() string { return "voice_broadcasts" }

// Printer is one cloud receipt printer.
type Printer struct {
	ID         string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	MerchantID string    `gorm:"type:varchar(36);not null;index" json:"merchant_id"`
	Name       string    `gorm:"type:varchar(64);not null" json:"name"`
	Secret     string    `gorm:"type:varchar(64);not null" json:"-"`
	Status     Status    `gorm:"type:varchar(20);not null;index" json:"status"`
	CreatedAt  time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt  time.Time `gorm:"not null" json:"updated_at"`
}

func (Printer) TableName() string { return "printers" }

// PrintJob is one receipt queued for a printer.
type PrintJob struct {
	ID        string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	PrinterID string     `gorm:"type:varchar(36);not null;index" json:"printer_id"`
	OrderID   *string    `gorm:"type:varchar(36);index" json:"order_id"`
	Content   string     `gorm:"type:text;not null" json:"content"`
	Status    Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	Attempts  int        `gorm:"not null;default:0" json:"attempts"`
	CreatedAt time.Time  `gorm:"not null" json:"created_at"`
	PrintedAt *time.Time `json:"printed_at"`
}

func (PrintJob) TableName() string { return "print_jobs" }

func ValidStatus(status Status) bool {
	return status == StatusActive || status == StatusDisabled
}
