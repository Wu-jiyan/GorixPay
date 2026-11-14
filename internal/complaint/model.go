package complaint

import (
	"context"
	"errors"
	"time"
)

type Status string
type Direction string

const (
	StatusPending Status = "pending"
	StatusReplied Status = "replied"
	StatusClosed  Status = "closed"
	StatusOverdue Status = "overdue" // derived at read time, never stored

	DirectionIn  Direction = "in"
	DirectionOut Direction = "out"
)

var (
	ErrInvalidRequest = errors.New("invalid complaint request")
	ErrNotFound       = errors.New("complaint not found")
	ErrDuplicate      = errors.New("complaint already registered")
	ErrClosed         = errors.New("complaint is closed")
	ErrMerchant       = errors.New("merchant not found")
)

// Complaint is one channel complaint. Status stores pending/replied/closed; the
// overdue state is computed from DeadlineAt when it is still open.
type Complaint struct {
	ID          string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	MerchantID  string     `gorm:"type:varchar(36);not null;index" json:"merchant_id"`
	OrderID     *string    `gorm:"type:varchar(36);index" json:"order_id"`
	Channel     string     `gorm:"type:varchar(20);not null;index" json:"channel"`
	ComplaintNo string     `gorm:"type:varchar(64);not null" json:"complaint_no"`
	Status      Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	Reason      string     `gorm:"type:varchar(255);not null" json:"reason"`
	AmountCents int64      `gorm:"not null;default:0" json:"amount_cents"`
	DeadlineAt  *time.Time `json:"deadline_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	CreatedAt   time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"not null" json:"updated_at"`
}

func (Complaint) TableName() string { return "complaints" }

// Message is one conversation entry. Direction "in" comes from the channel,
// "out" is the platform reply.
type Message struct {
	ID          string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	ComplaintID string    `gorm:"type:varchar(36);not null;index" json:"complaint_id"`
	Direction   Direction `gorm:"type:varchar(10);not null" json:"direction"`
	Content     string    `gorm:"type:text;not null" json:"content"`
	Operator    *string   `gorm:"type:varchar(64)" json:"operator"`
	CreatedAt   time.Time `gorm:"not null" json:"created_at"`
}

func (Message) TableName() string { return "complaint_messages" }

// Puller is the channel-side complaint puller hook. No payment plugin exposes a
// complaint listing endpoint yet ("插件侧未提供拉取接口"), so nothing implements
// this interface in the repository. A future scheduler can call Pull for each
// channel and feed the rows into Service.Create.
type Puller interface {
	Pull(ctx context.Context, since time.Time) ([]Complaint, error)
}

// ViewStatus derives the display status: a pending complaint past its deadline
// is shown as overdue without rewriting the stored row. Once replied or closed
// the stored status wins.
func ViewStatus(complaint Complaint, now time.Time) Status {
	if complaint.Status != StatusPending {
		return complaint.Status
	}
	if complaint.DeadlineAt != nil && now.After(complaint.DeadlineAt.UTC()) {
		return StatusOverdue
	}
	return StatusPending
}

func ValidStatus(status Status) bool {
	return status == StatusPending || status == StatusReplied || status == StatusClosed
}
