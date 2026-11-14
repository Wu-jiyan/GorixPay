package notify

import (
	"errors"
	"strings"
	"time"
)

// ChannelKind is what a notification channel can deliver.
type ChannelKind string

const (
	KindSMS  ChannelKind = "sms"
	KindMail ChannelKind = "mail"
)

// ChannelStatus lets an operator stage a channel before switching it on.
type ChannelStatus string

const (
	StatusActive   ChannelStatus = "active"
	StatusDisabled ChannelStatus = "disabled"
)

// Code scenes. Only these two are issued today; the column is a plain string so a
// later feature can add its own scene without a migration.
const (
	SceneResetPassword = "reset_password"
	SceneBindPhone     = "bind_phone"
)

// principalTypeMerchant is the auth_sessions / password_resets principal tag for
// merchants. It matches identity/model.PrincipalMerchant on purpose; the constant
// is duplicated instead of imported so this package can be tested without the
// identity model, and the test suite pins the value.
const principalTypeMerchant = "merchant"

var (
	ErrNotFound           = errors.New("notification channel not found")
	ErrInvalidRequest     = errors.New("invalid request")
	ErrCipherUnavailable  = errors.New("master key is not configured")
	ErrCodeTooSoon        = errors.New("verification code requested too recently")
	ErrCodeDailyLimit     = errors.New("daily verification code limit reached")
	ErrCodeNotFound       = errors.New("verification code not found")
	ErrCodeExpired        = errors.New("verification code expired")
	ErrCodeExhausted      = errors.New("verification code attempts exhausted")
	ErrCodeInvalid        = errors.New("verification code is incorrect")
	ErrCodeConsumed       = errors.New("verification code already used")
	ErrInvalidToken       = errors.New("password reset token is invalid")
	ErrTokenExpired       = errors.New("password reset token expired")
	ErrTokenConsumed      = errors.New("password reset token already used")
	ErrInvalidPassword    = errors.New("password does not meet the policy")
	ErrInvalidOldPassword = errors.New("current password is incorrect")
	ErrDeliveryFailed     = errors.New("message delivery failed")
)

// Channel is one configured delivery provider.
type Channel struct {
	ID              string        `gorm:"type:varchar(36);primaryKey"`
	Kind            ChannelKind   `gorm:"type:varchar(10);not null"`
	Provider        string        `gorm:"type:varchar(32);not null"`
	ConfigEncrypted string        `gorm:"column:config_encrypted;type:text;not null"`
	Status          ChannelStatus `gorm:"type:varchar(20);not null;default:'active'"`
	CreatedAt       time.Time     `gorm:"not null"`
	UpdatedAt       time.Time     `gorm:"not null"`
}

func (Channel) TableName() string { return "notification_channels" }

// VerifyCode is one issued one-time code. Only the hash is stored.
type VerifyCode struct {
	ID         string    `gorm:"type:varchar(36);primaryKey"`
	Scene      string    `gorm:"type:varchar(32);not null;index:idx_verify_codes_scene_target"`
	Target     string    `gorm:"type:varchar(120);not null;index:idx_verify_codes_scene_target"`
	CodeHash   string    `gorm:"type:varchar(64);not null"`
	ExpiresAt  time.Time `gorm:"not null"`
	Attempts   int       `gorm:"not null;default:0"`
	ConsumedAt *time.Time
	CreatedAt  time.Time `gorm:"not null"`
}

func (VerifyCode) TableName() string { return "verify_codes" }

// PasswordReset is one issued reset token. Only the hash is stored, so a database
// dump cannot be replayed against the reset endpoint.
type PasswordReset struct {
	ID            string    `gorm:"type:varchar(36);primaryKey"`
	PrincipalType string    `gorm:"type:varchar(20);not null;index:idx_password_resets_principal"`
	PrincipalID   string    `gorm:"type:varchar(36);not null;index:idx_password_resets_principal"`
	TokenHash     string    `gorm:"type:varchar(64);not null;uniqueIndex"`
	ExpiresAt     time.Time `gorm:"not null"`
	ConsumedAt    *time.Time
	CreatedAt     time.Time `gorm:"not null"`
}

func (PasswordReset) TableName() string { return "password_resets" }

// ValidKind reports whether value is a deliverable channel kind.
func ValidKind(value ChannelKind) bool {
	return value == KindSMS || value == KindMail
}

// ValidStatus reports whether value is a known channel status.
func ValidStatus(value ChannelStatus) bool {
	return value == StatusActive || value == StatusDisabled
}

// normalizeScene maps the public scene string onto the supported set.
func normalizeScene(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case SceneResetPassword, SceneBindPhone:
		return normalized, true
	default:
		return "", false
	}
}

// normalizeTarget canonicalises a phone number or mailbox.
func normalizeTarget(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > 120 {
		return "", false
	}
	if strings.Contains(trimmed, "@") {
		normalized := strings.ToLower(trimmed)
		if strings.HasPrefix(normalized, "@") || strings.HasSuffix(normalized, "@") {
			return "", false
		}
		return normalized, true
	}
	for _, digit := range trimmed {
		if (digit < '0' || digit > '9') && digit != '+' && digit != '-' {
			return "", false
		}
	}
	return trimmed, true
}
