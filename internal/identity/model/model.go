package model

import "time"

type PrincipalType string

const (
	PrincipalAdministrator PrincipalType = "administrator"
	PrincipalMerchant      PrincipalType = "merchant"
)

type Status string

const (
	StatusActive Status = "active"
	// StatusPending is a merchant waiting for the operator's review (上游 Epay 的
	// 「开启注册审核」)。Such a merchant can sign in and configure, but must not be
	// able to create orders until an administrator flips it to active.
	StatusPending  Status = "pending"
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
	ID       string `gorm:"type:varchar(36);primaryKey" json:"id"`
	Username string `gorm:"type:varchar(100);uniqueIndex;not null" json:"username"`
	// PasswordHash is never serialized: the console has no business seeing it.
	PasswordHash        string    `gorm:"type:varchar(255);not null" json:"-"`
	DisplayName         string    `gorm:"type:varchar(100);not null" json:"display_name"`
	Status              Status    `gorm:"type:varchar(20);not null" json:"status"`
	TOTPSecretEncrypted *string   `gorm:"column:totp_secret_encrypted;type:text" json:"-"`
	CreatedAt           time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt           time.Time `gorm:"not null" json:"updated_at"`
}

type Merchant struct {
	ID        string  `gorm:"type:varchar(36);primaryKey" json:"id"`
	LegacyUID *int64  `gorm:"uniqueIndex" json:"legacy_uid"`
	Email     *string `gorm:"type:varchar(191);uniqueIndex" json:"email"`
	Phone     *string `gorm:"type:varchar(32);uniqueIndex" json:"phone"`
	// PasswordHash is never serialized, not even to the operator console.
	PasswordHash *string `gorm:"type:varchar(255)" json:"-"`
	DisplayName  string  `gorm:"type:varchar(128);not null" json:"display_name"`
	// SettleType and SettleAccount mirror pre_user.settle_id/account: the payout
	// channel the merchant chose and the account it pays into.
	SettleType    int     `gorm:"not null;default:0" json:"settle_type"`
	SettleAccount *string `gorm:"type:varchar(128)" json:"settle_account"`
	// GroupID is the merchant group whose rate and payment types apply. It is
	// owned by internal/merchantgroup (migration 000017) and declared here so the
	// admin merchant list can show the current group instead of only assigning it.
	GroupID    *string   `gorm:"type:varchar(36);index" json:"group_id"`
	Status     Status    `gorm:"type:varchar(20);not null" json:"status"`
	InvitedBy  *string   `gorm:"type:varchar(36);index" json:"invited_by"`
	InviteCode *string   `gorm:"type:varchar(32);uniqueIndex" json:"invite_code"`
	CreatedAt  time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt  time.Time `gorm:"not null" json:"updated_at"`
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
	ID             string         `gorm:"type:varchar(36);primaryKey" json:"id"`
	MerchantID     string         `gorm:"type:varchar(36);not null;index" json:"merchant_id"`
	CredentialType CredentialType `gorm:"type:varchar(20);not null;index" json:"credential_type"`
	KeyID          *string        `gorm:"type:varchar(100)" json:"key_id"`
	// SecretEncrypted is ciphertext at rest and never leaves the server.
	SecretEncrypted string           `gorm:"type:text;not null" json:"-"`
	PublicKey       *string          `gorm:"type:text" json:"public_key"`
	Status          CredentialStatus `gorm:"type:varchar(20);not null;index" json:"status"`
	CreatedAt       time.Time        `gorm:"not null" json:"created_at"`
	RevokedAt       *time.Time       `json:"revoked_at"`
}

func (session Session) Active(now time.Time) bool {
	return session.RevokedAt == nil && now.Before(session.ExpiresAt)
}

func (Session) TableName() string { return "auth_sessions" }
