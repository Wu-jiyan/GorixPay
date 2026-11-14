package security

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	identityauth "gorixpay/internal/identity/auth"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/platform/secret"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidRequest     = errors.New("invalid security request")
	ErrCipherMissing      = errors.New("security service has no cipher")
	ErrCaptchaInvalid     = errors.New("captcha invalid")
	ErrCaptchaExpired     = errors.New("captcha expired")
	ErrCaptchaUsed        = errors.New("captcha already used")
	ErrTOTPNotSetup       = errors.New("totp not set up")
	ErrTOTPAlreadyEnabled = errors.New("totp already enabled")
	ErrTOTPInvalid        = errors.New("totp code invalid")
	ErrTOTPReused         = errors.New("totp code already used")
	ErrLockedOut          = errors.New("too many failed login attempts")
)

const (
	settingCaptchaRequired = "security.captcha_required"
	captchaTTL             = 2 * time.Minute
	defaultIssuer          = "GorixPay"
	loginPrincipalAdmin    = "administrator"
	defaultPageSize        = 20
	maxPageSize            = 100
	// loginLockoutWindow and maxLoginFailures bound password guessing: ten failed
	// attempts for one identifier inside fifteen minutes lock it out.
	loginLockoutWindow = 15 * time.Minute
	maxLoginFailures   = 10
)

type AdminTOTP struct {
	ID              string     `gorm:"type:varchar(36);primaryKey"`
	AdministratorID string     `gorm:"type:varchar(36);not null;uniqueIndex"`
	SecretEncrypted string     `gorm:"type:text;not null"`
	EnabledAt       *time.Time `json:"enabled_at"`
	// LastUsedStep is the newest accepted RFC 6238 time step. Without it the same
	// six digits stay valid for the whole +-1 window and can be replayed.
	LastUsedStep int64     `gorm:"not null;default:0"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (AdminTOTP) TableName() string { return "admin_totp" }

type LoginAttempt struct {
	ID            string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	PrincipalType string    `gorm:"type:varchar(20);not null" json:"principal_type"`
	Identifier    string    `gorm:"type:varchar(120);not null" json:"identifier"`
	ClientIP      *string   `gorm:"type:varchar(64)" json:"client_ip"`
	Success       bool      `gorm:"not null" json:"success"`
	CreatedAt     time.Time `gorm:"not null" json:"created_at"`
}

func (LoginAttempt) TableName() string { return "login_attempts" }

type CaptchaChallenge struct {
	ID         string    `gorm:"type:varchar(36);primaryKey"`
	AnswerHash string    `gorm:"type:varchar(64);not null"`
	ExpiresAt  time.Time `gorm:"not null"`
	ConsumedAt *time.Time
	CreatedAt  time.Time `gorm:"not null"`
}

func (CaptchaChallenge) TableName() string { return "captcha_challenges" }

type systemSetting struct {
	Key   string `gorm:"column:setting_key;primaryKey"`
	Value string `gorm:"column:setting_value;type:text;not null"`
}

func (systemSetting) TableName() string { return "system_settings" }

type TOTPStatus struct {
	Enabled   bool       `json:"enabled"`
	EnabledAt *time.Time `json:"enabled_at"`
}

type LoginAttemptFilter struct {
	Identifier string
	Success    *bool
}

type Page struct {
	Number int
	Size   int
}

// Service owns administrator TOTP, login attempts and captcha challenges.
type Service struct {
	db     *gorm.DB
	cipher *secret.Cipher
	now    func() time.Time
	issuer string
}

// The identity package consumes this service through its Challenge interface.
var _ identityauth.Challenge = (*Service)(nil)

// New wires the service. cipher protects TOTP secrets at rest; without it TOTP
// setup is refused rather than storing a plaintext secret.
func New(db *gorm.DB, cipher *secret.Cipher) *Service {
	return &Service{db: db, cipher: cipher, now: time.Now, issuer: defaultIssuer}
}

// WithIssuer returns a copy with a different authenticator issuer label.
func (service *Service) WithIssuer(issuer string) *Service {
	copied := *service
	if trimmed := strings.TrimSpace(issuer); trimmed != "" {
		copied.issuer = trimmed
	}
	return &copied
}

func (service *Service) CreateCaptcha(ctx context.Context) (Captcha, error) {
	generated, err := NewCaptcha()
	if err != nil {
		return Captcha{}, err
	}
	now := service.now().UTC()
	id := uuid.NewString()
	challenge := CaptchaChallenge{
		ID:         id,
		AnswerHash: captchaHash(id, generated.Code),
		ExpiresAt:  now.Add(captchaTTL),
		CreatedAt:  now,
	}
	if err := service.db.WithContext(ctx).Create(&challenge).Error; err != nil {
		return Captcha{}, err
	}
	generated.ID = id
	return generated, nil
}

// VerifyCaptcha consumes the challenge. A second call with the same id returns
// ErrCaptchaUsed, which is what makes a solved captcha useless for retries.
func (service *Service) VerifyCaptcha(ctx context.Context, id, code string) error {
	id = strings.TrimSpace(id)
	code = strings.TrimSpace(code)
	if id == "" || code == "" {
		return ErrCaptchaInvalid
	}
	var challenge CaptchaChallenge
	if err := service.db.WithContext(ctx).First(&challenge, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCaptchaInvalid
		}
		return err
	}
	if challenge.ConsumedAt != nil {
		return ErrCaptchaUsed
	}
	now := service.now().UTC()
	if !challenge.ExpiresAt.After(now) {
		return ErrCaptchaExpired
	}
	if subtle.ConstantTimeCompare([]byte(captchaHash(id, code)), []byte(challenge.AnswerHash)) != 1 {
		return ErrCaptchaInvalid
	}
	result := service.db.WithContext(ctx).Model(&CaptchaChallenge{}).
		Where("id = ? AND consumed_at IS NULL", id).
		Updates(map[string]any{"consumed_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrCaptchaUsed
	}
	return nil
}

func (service *Service) CaptchaRequired(ctx context.Context) (bool, error) {
	var record systemSetting
	err := service.db.WithContext(ctx).Where("setting_key = ?", settingCaptchaRequired).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return decodeBool(record.Value), nil
}

func (service *Service) TOTPStatus(ctx context.Context, administratorID string) (TOTPStatus, error) {
	administratorID = strings.TrimSpace(administratorID)
	if administratorID == "" {
		return TOTPStatus{}, ErrInvalidRequest
	}
	var record AdminTOTP
	if err := service.db.WithContext(ctx).Where("administrator_id = ?", administratorID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return TOTPStatus{}, nil
		}
		return TOTPStatus{}, err
	}
	return TOTPStatus{Enabled: record.EnabledAt != nil, EnabledAt: record.EnabledAt}, nil
}

// TOTPEnabled satisfies identityauth.Challenge.
func (service *Service) TOTPEnabled(ctx context.Context, administratorID string) (bool, error) {
	status, err := service.TOTPStatus(ctx, administratorID)
	if err != nil {
		return false, err
	}
	return status.Enabled, nil
}

// SetupTOTP creates (or replaces) the pending secret and returns it together
// with the otpauth URL. Replacing is refused once TOTP is enabled.
func (service *Service) SetupTOTP(ctx context.Context, administratorID, account string) (string, string, error) {
	administratorID = strings.TrimSpace(administratorID)
	if administratorID == "" {
		return "", "", ErrInvalidRequest
	}
	if service.cipher == nil {
		return "", "", ErrCipherMissing
	}
	status, err := service.TOTPStatus(ctx, administratorID)
	if err != nil {
		return "", "", err
	}
	if status.Enabled {
		return "", "", ErrTOTPAlreadyEnabled
	}
	plaintext, err := GenerateTOTPSecret()
	if err != nil {
		return "", "", err
	}
	encrypted, err := service.cipher.Encrypt([]byte(plaintext), totpContext(administratorID))
	if err != nil {
		return "", "", err
	}
	now := service.now().UTC()
	record := AdminTOTP{
		ID:              uuid.NewString(),
		AdministratorID: administratorID,
		SecretEncrypted: encrypted,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := service.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "administrator_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"secret_encrypted", "enabled_at", "updated_at"}),
	}).Create(&record).Error; err != nil {
		return "", "", err
	}
	account = strings.TrimSpace(account)
	if account == "" {
		account = administratorID
	}
	return plaintext, TOTPAuthURL(service.issuer, account, plaintext), nil
}

func (service *Service) EnableTOTP(ctx context.Context, administratorID, code string) (TOTPStatus, error) {
	record, err := service.totpRecord(ctx, administratorID)
	if err != nil {
		return TOTPStatus{}, err
	}
	plaintext, err := service.decryptSecret(record)
	if err != nil {
		return TOTPStatus{}, err
	}
	if err := service.consumeTOTPStep(ctx, record, plaintext, code); err != nil {
		return TOTPStatus{}, err
	}
	now := service.now().UTC()
	if err := service.db.WithContext(ctx).Model(&AdminTOTP{}).
		Where("administrator_id = ?", record.AdministratorID).
		Updates(map[string]any{"enabled_at": now, "updated_at": now}).Error; err != nil {
		return TOTPStatus{}, err
	}
	return TOTPStatus{Enabled: true, EnabledAt: &now}, nil
}

// DisableTOTP removes the secret after verifying a current code, so a hijacked
// session alone cannot turn the second factor off.
func (service *Service) DisableTOTP(ctx context.Context, administratorID, code string) error {
	record, err := service.totpRecord(ctx, administratorID)
	if err != nil {
		return err
	}
	plaintext, err := service.decryptSecret(record)
	if err != nil {
		return err
	}
	if err := service.consumeTOTPStep(ctx, record, plaintext, code); err != nil {
		return err
	}
	return service.db.WithContext(ctx).Where("administrator_id = ?", record.AdministratorID).Delete(&AdminTOTP{}).Error
}

// VerifyTOTP satisfies identityauth.Challenge.
func (service *Service) VerifyTOTP(ctx context.Context, administratorID, code string) error {
	administratorID = strings.TrimSpace(administratorID)
	if administratorID == "" {
		return ErrInvalidRequest
	}
	var record AdminTOTP
	if err := service.db.WithContext(ctx).
		Where("administrator_id = ? AND enabled_at IS NOT NULL", administratorID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTOTPNotSetup
		}
		return err
	}
	plaintext, err := service.decryptSecret(record)
	if err != nil {
		return err
	}
	return service.consumeTOTPStep(ctx, record, plaintext, code)
}

// consumeTOTPStep verifies the code and claims its time step in one atomic
// update. Two logins racing with the same code therefore cannot both succeed,
// and a captured code is dead as soon as it has been used once.
func (service *Service) consumeTOTPStep(ctx context.Context, record AdminTOTP, plaintext, code string) error {
	step, ok := VerifyTOTPStep(plaintext, code, service.now().UTC())
	if !ok {
		return ErrTOTPInvalid
	}
	result := service.db.WithContext(ctx).Model(&AdminTOTP{}).
		Where("id = ? AND last_used_step < ?", record.ID, step).
		Update("last_used_step", step)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTOTPReused
	}
	return nil
}

// LoginAllowed satisfies identityauth.LoginGuard: it refuses a login for an
// identifier that already failed too many times inside the lockout window. The
// check runs before the password hash, so a brute force run cannot turn Argon2id
// into a memory amplifier. An audit read failure never blocks a login.
func (service *Service) LoginAllowed(ctx context.Context, principalType, identifier string) error {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil
	}
	since := service.now().UTC().Add(-loginLockoutWindow)
	query := service.db.WithContext(ctx).Model(&LoginAttempt{}).
		Where("identifier = ? AND success = ? AND created_at > ?", identifier, false, since)
	if principal := strings.TrimSpace(principalType); principal != "" {
		query = query.Where("principal_type = ?", principal)
	}
	var failures int64
	if err := query.Count(&failures).Error; err != nil {
		return nil
	}
	if failures >= maxLoginFailures {
		return ErrLockedOut
	}
	return nil
}

// RecordLoginAttempt satisfies identityauth.Challenge. Audit writes must never
// block a login, so a failure here is dropped.
func (service *Service) RecordLoginAttempt(ctx context.Context, principalType, identifier, clientIP string, success bool) {
	principalType = strings.TrimSpace(principalType)
	if principalType == "" {
		principalType = loginPrincipalAdmin
	}
	now := service.now().UTC()
	attempt := LoginAttempt{
		ID:            uuid.NewString(),
		PrincipalType: principalType,
		Identifier:    strings.TrimSpace(identifier),
		ClientIP:      optionalString(clientIP),
		Success:       success,
		CreatedAt:     now,
	}
	_ = service.db.WithContext(ctx).Create(&attempt).Error
}

func (service *Service) ListLoginAttempts(ctx context.Context, filter LoginAttemptFilter, page Page) ([]LoginAttempt, int64, error) {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 {
		page.Size = defaultPageSize
	}
	if page.Size > maxPageSize {
		page.Size = maxPageSize
	}
	query := service.db.WithContext(ctx).Model(&LoginAttempt{})
	if identifier := strings.TrimSpace(filter.Identifier); identifier != "" {
		query = query.Where("identifier LIKE ?", "%"+identifier+"%")
	}
	if filter.Success != nil {
		query = query.Where("success = ?", *filter.Success)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []LoginAttempt
	if err := query.Order("created_at DESC, id DESC").
		Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// AdministratorName resolves the login name used in the otpauth account label.
func (service *Service) AdministratorName(ctx context.Context, administratorID string) (string, error) {
	var administrator identitymodel.Administrator
	if err := service.db.WithContext(ctx).First(&administrator, "id = ?", administratorID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrInvalidRequest
		}
		return "", err
	}
	return administrator.Username, nil
}

func (service *Service) totpRecord(ctx context.Context, administratorID string) (AdminTOTP, error) {
	administratorID = strings.TrimSpace(administratorID)
	if administratorID == "" {
		return AdminTOTP{}, ErrInvalidRequest
	}
	var record AdminTOTP
	if err := service.db.WithContext(ctx).Where("administrator_id = ?", administratorID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AdminTOTP{}, ErrTOTPNotSetup
		}
		return AdminTOTP{}, err
	}
	return record, nil
}

func (service *Service) decryptSecret(record AdminTOTP) (string, error) {
	if service.cipher == nil {
		return "", ErrCipherMissing
	}
	plaintext, _, err := service.cipher.Decrypt(record.SecretEncrypted, totpContext(record.AdministratorID))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func totpContext(administratorID string) []byte {
	return []byte("admin_totp:" + administratorID)
}

func captchaHash(id, code string) string {
	digest := sha256.Sum256([]byte(id + ":" + strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(digest[:])
}

func decodeBool(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	// Settings are stored as JSON, and the console writes the string "true"
	// rather than the literal true, so accept both shapes.
	var anyValue any
	if err := json.Unmarshal([]byte(trimmed), &anyValue); err == nil {
		switch value := anyValue.(type) {
		case bool:
			return value
		case string:
			trimmed = strings.ToLower(strings.TrimSpace(value))
		case float64:
			return value != 0
		}
	}
	switch strings.ToLower(strings.Trim(trimmed, `"`)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
