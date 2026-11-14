// Package channelops owns the operational catalogue around payment channels:
// the payment method list shown to merchants, the per-day channel rotation
// table, and the WeChat official account (公众号) credentials.
package channelops

import (
	"context"
	"errors"
	"time"
)

// Status is the shared active/disabled switch of every table in this package.
type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

var (
	ErrInvalidRequest = errors.New("invalid channel operations request")
	ErrNotFound       = errors.New("channel operations record not found")
	// ErrDuplicate reports a code or (channel_id, roll_date) that already exists.
	ErrDuplicate = errors.New("record already exists")
	// ErrInUse reports a payment method that channels still reference.
	ErrInUse = errors.New("record is still in use")
)

// RollResolver is the read-only view the channel router needs: the weight a
// channel should use on a given day, and whether a roll row exists at all.
type RollResolver interface {
	RollWeight(ctx context.Context, channelID string, day time.Time) (int, bool)
}

// PayType is one entry of the payment method catalogue (pay_types).
type PayType struct {
	ID        string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	Code      string    `gorm:"type:varchar(32);not null;uniqueIndex" json:"code"`
	Name      string    `gorm:"type:varchar(64);not null" json:"name"`
	Icon      *string   `gorm:"type:varchar(255)" json:"icon"`
	Sort      int       `gorm:"not null;default:0" json:"sort"`
	Status    Status    `gorm:"type:varchar(20);not null;index" json:"status"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

func (PayType) TableName() string { return "pay_types" }

// PayRoll is one day of channel rotation (pay_rolls). A missing row means the
// channel keeps its configured weight.
type PayRoll struct {
	ID        string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	ChannelID string    `gorm:"type:varchar(36);not null;uniqueIndex:idx_pay_roll_channel_date" json:"channel_id"`
	RollDate  time.Time `gorm:"type:date;not null;uniqueIndex:idx_pay_roll_channel_date" json:"roll_date"`
	Weight    int       `gorm:"not null;default:1" json:"weight"`
	Status    Status    `gorm:"type:varchar(20);not null;index" json:"status"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

func (PayRoll) TableName() string { return "pay_rolls" }

// WeChatMPConfig is one 公众号 credential set. Secrets are stored encrypted and
// never leave the service in plaintext.
type WeChatMPConfig struct {
	ID                 string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	MerchantID         *string   `gorm:"type:varchar(36);index" json:"merchant_id"`
	AppID              string    `gorm:"type:varchar(64);not null" json:"app_id"`
	AppSecretEncrypted string    `gorm:"type:text;not null" json:"-"`
	Token              *string   `gorm:"type:varchar(64)" json:"token"`
	AESKeyEncrypted    *string   `gorm:"type:text" json:"-"`
	Status             Status    `gorm:"type:varchar(20);not null;index" json:"status"`
	CreatedAt          time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt          time.Time `gorm:"not null" json:"updated_at"`
}

func (WeChatMPConfig) TableName() string { return "wechat_mp_configs" }

// WeChatMPView is the API shape of a config: no ciphertext, only flags telling
// the operator which secrets are set.
type WeChatMPView struct {
	ID               string    `json:"id"`
	MerchantID       *string   `json:"merchant_id"`
	AppID            string    `json:"app_id"`
	Token            *string   `json:"token"`
	Status           Status    `json:"status"`
	SecretConfigured bool      `json:"secret_configured"`
	AESKeyConfigured bool      `json:"aes_key_configured"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// View projects the stored config onto its API shape.
func (config WeChatMPConfig) View() WeChatMPView {
	return WeChatMPView{
		ID: config.ID, MerchantID: config.MerchantID, AppID: config.AppID, Token: config.Token,
		Status: config.Status, SecretConfigured: config.AppSecretEncrypted != "",
		AESKeyConfigured: config.AESKeyEncrypted != nil && *config.AESKeyEncrypted != "",
		CreatedAt:        config.CreatedAt, UpdatedAt: config.UpdatedAt,
	}
}

// Page is an offset pagination request.
type Page struct {
	Number int
	Size   int
}

// Offset translates the page into a GORM offset.
func (page Page) Offset() int {
	number := page.Number
	if number < 1 {
		number = 1
	}
	return (number - 1) * page.Limit()
}

// Limit clamps the page size to 200.
func (page Page) Limit() int {
	size := page.Size
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return size
}

func validStatus(status Status) bool {
	return status == StatusActive || status == StatusDisabled
}

// DayStart normalizes a timestamp to the UTC midnight that identifies a roll day.
func DayStart(day time.Time) time.Time {
	year, month, date := day.UTC().Date()
	return time.Date(year, month, date, 0, 0, 0, 0, time.UTC)
}
