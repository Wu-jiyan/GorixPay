package invite

import (
	"errors"
	"time"
)

var (
	ErrInvalidRequest = errors.New("invalid invite request")
	ErrNotFound       = errors.New("invite record not found")
)

// Rebate is one settled invite commission for a paid sale order.
type Rebate struct {
	ID           string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	InviterID    string    `gorm:"type:varchar(36);not null;index" json:"inviter_id"`
	InviteeID    string    `gorm:"type:varchar(36);not null;index" json:"invitee_id"`
	OrderID      string    `gorm:"type:varchar(36);not null;uniqueIndex" json:"order_id"`
	TradeNo      string    `gorm:"type:varchar(32);not null" json:"trade_no"`
	OrderAmount  int64     `gorm:"not null" json:"order_amount"`
	RebateAmount int64     `gorm:"not null" json:"rebate_amount"`
	RateBP       int       `gorm:"not null" json:"rate_bp"`
	CreatedAt    time.Time `gorm:"not null" json:"created_at"`
}

func (Rebate) TableName() string { return "invite_rebates" }

// Stats is the merchant invite dashboard payload.
type Stats struct {
	Enabled      bool
	RateBP       int64
	InviteCode   string
	InviteURL    string
	InvitedCount int64
	TodayIncome  int64
	TotalIncome  int64
}

// RecordView joins a rebate with the invitee display name.
type RecordView struct {
	Rebate      Rebate
	InviteeName string
}

// Page is a simple offset pagination request.
type Page struct {
	Number int
	Size   int
}
