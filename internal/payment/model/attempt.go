package model

import "time"

type AttemptStatus string

const (
	AttemptCreating  AttemptStatus = "creating"
	AttemptSucceeded AttemptStatus = "succeeded"
	AttemptFailed    AttemptStatus = "failed"
)

type Attempt struct {
	ID             string        `gorm:"type:varchar(36);primaryKey"`
	OrderID        string        `gorm:"type:varchar(36);not null;uniqueIndex:idx_order_attempt"`
	ChannelID      *string       `gorm:"type:varchar(36);index"`
	AttemptNo      int           `gorm:"not null;uniqueIndex:idx_order_attempt"`
	Status         AttemptStatus `gorm:"type:varchar(20);not null"`
	ChannelTradeNo *string       `gorm:"type:varchar(128)"`
	ActionType     string        `gorm:"type:varchar(20)"`
	ActionPayload  string        `gorm:"type:text"`
	ErrorCode      *string       `gorm:"type:varchar(100)"`
	ErrorMessage   *string       `gorm:"type:varchar(500)"`
	CreatedAt      time.Time     `gorm:"not null"`
	UpdatedAt      time.Time     `gorm:"not null"`
}

func (Attempt) TableName() string {
	return "payment_attempts"
}
