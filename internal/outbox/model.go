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
	ID             string     `gorm:"type:varchar(36);primaryKey"`
	Topic          string     `gorm:"type:varchar(100);not null;index"`
	AggregateID    string     `gorm:"type:varchar(36);not null;index"`
	IdempotencyKey string     `gorm:"type:varchar(191);not null;uniqueIndex"`
	Payload        string     `gorm:"type:text;not null"`
	Status         Status     `gorm:"type:varchar(20);not null;index"`
	Attempts       int        `gorm:"not null"`
	AvailableAt    time.Time  `gorm:"not null;index"`
	LeaseUntil     *time.Time `gorm:"index"`
	LastError      *string    `gorm:"type:varchar(500)"`
	CreatedAt      time.Time  `gorm:"not null"`
	UpdatedAt      time.Time  `gorm:"not null"`
}

func (Message) TableName() string {
	return "outbox_messages"
}
