package announcement

import (
	"errors"
	"time"
)

var (
	ErrNotFound       = errors.New("announcement not found")
	ErrInvalidRequest = errors.New("invalid announcement request")
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
)

const (
	maxTitleLength   = 191
	maxContentLength = 65535
	publishedListCap = 100
)

type Announcement struct {
	ID          string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	Title       string     `gorm:"type:varchar(191);not null" json:"title"`
	Content     string     `gorm:"type:text;not null" json:"content"`
	Status      Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (Announcement) TableName() string {
	return "announcements"
}

func ValidStatus(status Status) bool {
	return status == StatusDraft || status == StatusPublished
}

type CreateInput struct {
	Title   string
	Content string
	Status  Status
}

type UpdateInput struct {
	Title   string
	Content string
	Status  Status
}
