package model

import "time"

type PrincipalType string

const (
	PrincipalAdministrator PrincipalType = "administrator"
	PrincipalMerchant      PrincipalType = "merchant"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

type CredentialType string
type CredentialStatus string

const (
	CredentialMD5 CredentialType = "md5"
	CredentialRSA CredentialType = "rsa"

	CredentialActive  CredentialStatus = "active"
	CredentialRevoked CredentialStatus = "revoked"
)

type Administrator struct {
	ID                  string    `gorm:"type:varchar(36);primaryKey"`
	Username            string    `gorm:"type:varchar(100);uniqueIndex;not null"`
	PasswordHash        string    `gorm:"type:varchar(255);not null"`
	DisplayName         string    `gorm:"type:varchar(100);not null"`
	Status              Status    `gorm:"type:varchar(20);not null"`
	TOTPSecretEncrypted *string   `gorm:"column:totp_secret_encrypted;type:text"`
	CreatedAt           time.Time `gorm:"not null"`
	UpdatedAt           time.Time `gorm:"not null"`
}

type Merchant struct {
	ID           string  `gorm:"type:varchar(36);primaryKey"`
	LegacyUID    *int64  `gorm:"uniqueIndex"`
	Email        *string `gorm:"type:varchar(191);uniqueIndex"`
	Phone        *string `gorm:"type:varchar(32);uniqueIndex"`
	PasswordHash *string `gorm:"type:varchar(255)"`
	DisplayName  string  `gorm:"type:varchar(128);not null"`
	// SettleType and SettleAccount mirror pre_user.settle_id/account: the payout
	// channel the merchant chose and the account it pays into.
	SettleType    int       `gorm:"not null;default:0"`
	SettleAccount *string   `gorm:"type:varchar(128)"`
	Status        Status    `gorm:"type:varchar(20);not null"`
	InvitedBy     *string   `gorm:"type:varchar(36);index"`
	InviteCode    *string   `gorm:"type:varchar(32);uniqueIndex"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
}

type Role struct {
	ID        string    `gorm:"type:varchar(36);primaryKey"`
	Code      string    `gorm:"type:varchar(100);uniqueIndex;not null"`
	Name      string    `gorm:"type:varchar(100);not null"`
	DataScope string    `gorm:"type:varchar(30);not null"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

type Permission struct {
	ID        string    `gorm:"type:varchar(36);primaryKey"`
	Code      string    `gorm:"type:varchar(150);uniqueIndex;not null"`
	Name      string    `gorm:"type:varchar(100);not null"`
	CreatedAt time.Time `gorm:"not null"`
}

type RolePermission struct {
	RoleID       string `gorm:"type:varchar(36);primaryKey"`
	PermissionID string `gorm:"type:varchar(36);primaryKey"`
}

type PrincipalRole struct {
	PrincipalType PrincipalType `gorm:"type:varchar(20);primaryKey"`
	PrincipalID   string        `gorm:"type:varchar(36);primaryKey"`
	RoleID        string        `gorm:"type:varchar(36);primaryKey"`
}

type Session struct {
	ID            string        `gorm:"type:varchar(36);primaryKey"`
	PrincipalType PrincipalType `gorm:"type:varchar(20);not null;index"`
	PrincipalID   string        `gorm:"type:varchar(36);not null;index"`
	TokenHash     string        `gorm:"type:varchar(64);uniqueIndex;not null"`
	IPAddress     *string       `gorm:"type:varchar(45)"`
	UserAgent     *string       `gorm:"type:varchar(500)"`
	ExpiresAt     time.Time     `gorm:"not null;index"`
	RevokedAt     *time.Time
	CreatedAt     time.Time `gorm:"not null"`
}

type MerchantCredential struct {
	ID              string           `gorm:"type:varchar(36);primaryKey"`
	MerchantID      string           `gorm:"type:varchar(36);not null;index"`
	CredentialType  CredentialType   `gorm:"type:varchar(20);not null;index"`
	KeyID           *string          `gorm:"type:varchar(100)"`
	SecretEncrypted string           `gorm:"type:text;not null"`
	PublicKey       *string          `gorm:"type:text"`
	Status          CredentialStatus `gorm:"type:varchar(20);not null;index"`
	CreatedAt       time.Time        `gorm:"not null"`
	RevokedAt       *time.Time
}

func (session Session) Active(now time.Time) bool {
	return session.RevokedAt == nil && now.Before(session.ExpiresAt)
}

func (Session) TableName() string { return "auth_sessions" }
