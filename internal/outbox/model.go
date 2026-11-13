package outbox

import "time"

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusDead       Status = "dead"
)

type Message struct {
	ID             string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	Topic          string     `gorm:"type:varchar(100);not null;index" json:"topic"`
	AggregateID    string     `gorm:"type:varchar(36);not null;index" json:"aggregate_id"`
	IdempotencyKey string     `gorm:"type:varchar(191);not null;uniqueIndex" json:"idempotency_key"`
	Payload        string     `gorm:"type:text;not null" json:"payload"`
	Status         Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	Attempts       int        `gorm:"not null" json:"attempts"`
	AvailableAt    time.Time  `gorm:"not null;index" json:"available_at"`
	LeaseUntil     *time.Time `gorm:"index" json:"lease_until,omitempty"`
	LastError      *string    `gorm:"type:varchar(500)" json:"last_error,omitempty"`
	CreatedAt      time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"not null" json:"updated_at"`
}

func (Message) TableName() string {
	return "outbox_messages"
}
