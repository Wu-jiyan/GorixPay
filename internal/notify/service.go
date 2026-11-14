package notify

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorixpay/internal/identity/password"
	"gorixpay/internal/platform/secret"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Lifetime and abuse bounds of the one-time material.
const (
	codeTTL         = 5 * time.Minute
	resetTTL        = 30 * time.Minute
	codeInterval    = time.Minute
	dailyCodeLimit  = 10
	maxCodeAttempts = 5
	codeLength      = 6
	resetTokenBytes = 32
)

// Password policy. It is stricter than merchant registration (4) on purpose: the
// reset flow is the one path an attacker with a mailbox can walk, so it must not
// be able to downgrade an account to a four character password.
const (
	minPasswordRune = 8
	maxPasswordRune = 72
)

// Bounds for an encrypted channel configuration.
const (
	maxConfigBytes  = 8 << 10
	maxConfigFields = 32
	maxConfigKey    = 64
	maxConfigValue  = 2048
)

// channelContextPrefix binds a channel ciphertext to its row id (AAD).
const channelContextPrefix = "notification-channel:"

// Config wires the service. Sender is optional: without one the development log
// sender is used and reset tokens are echoed to Logf. LogTokens overrides that
// decision explicitly (tests and production both want control over it).
type Config struct {
	DB            *gorm.DB
	Cipher        *secret.Cipher
	Sender        Sender
	HTTPClient    HTTPClient
	WeChatBaseURL string
	Now           func() time.Time
	Logf          func(format string, arguments ...any)
	LogTokens     *bool
}

// Service issues one-time codes, resets merchant passwords, owns the
// notification channel configuration and pushes 公众号 template messages.
type Service struct {
	db        *gorm.DB
	cipher    *secret.Cipher
	sender    Sender
	wechat    *WeChatMPClient
	now       func() time.Time
	logf      func(format string, arguments ...any)
	logTokens bool
}

// NewService validates the dependencies and returns the service. The cipher is
// optional: without a master key the code and password flows still work and only
// the channel endpoints report ErrCipherUnavailable, because nothing there needs
// to encrypt anything.
func NewService(config Config) (*Service, error) {
	if config.DB == nil {
		return nil, errors.New("notify: database is required")
	}
	service := &Service{
		db:     config.DB,
		cipher: config.Cipher,
		sender: config.Sender,
		now:    config.Now,
		logf:   config.Logf,
	}
	if service.now == nil {
		service.now = time.Now
	}
	if service.logf == nil {
		service.logf = func(string, ...any) {}
	}
	if service.sender == nil {
		// Nothing is delivered. In development the reset token is printed so the
		// flow can be finished locally; a production deployment that enabled
		// WithTokenLogging(false) gets a sender that records the attempt without
		// the body, because the body carries a working reset token.
		if config.LogTokens != nil && !*config.LogTokens {
			service.sender = newSuppressedSender(nil)
		} else {
			service.sender = NewLogSender(nil)
			service.logTokens = true
		}
	}
	if config.LogTokens != nil {
		service.logTokens = *config.LogTokens
	}
	service.wechat = NewWeChatMPClient(config.DB, config.Cipher,
		WithWeChatHTTPClient(config.HTTPClient),
		WithWeChatBaseURL(config.WeChatBaseURL),
		WithWeChatClock(config.Now),
		WithWeChatLogger(service.logf),
	)
	return service, nil
}

// WeChatSender exposes the template message sender so the wiring layer can hand it
// to other modules (notifyprefs uses it for the "wechat" notification channel).
func (service *Service) WeChatSender() WeChatTemplateSender {
	if service == nil {
		return nil
	}
	return service.wechat
}

// SendWeChatTestMessage pushes one template message through an explicit
// configuration row. It is what the administrator test endpoint calls, so the
// operator validates the row being edited rather than the platform default.
func (service *Service) SendWeChatTestMessage(ctx context.Context, configID, openID string) error {
	if service.wechat == nil {
		return ErrCipherUnavailable
	}
	return service.wechat.SendTemplateForConfig(ctx, configID, openID, "", "", map[string]string{
		"first":    "GorixPay 测试消息：公众号配置可用。",
		"keyword1": service.now().UTC().Format(time.RFC3339),
	})
}

// merchantRecord is the slice of `merchants` this package reads and writes.
type merchantRecord struct {
	ID           string  `gorm:"type:varchar(36);primaryKey"`
	LegacyUID    *int64  `gorm:"uniqueIndex"`
	Email        *string `gorm:"type:varchar(191)"`
	Phone        *string `gorm:"type:varchar(32)"`
	PasswordHash *string `gorm:"type:varchar(255)"`
	DisplayName  string  `gorm:"type:varchar(128);not null"`
	Status       string  `gorm:"type:varchar(20);not null"`
}

func (merchantRecord) TableName() string { return "merchants" }

// --- one-time codes ---------------------------------------------------------

// SentCode describes an issued verification code. The code itself is not returned
// to the caller: it only leaves the process through the Sender.
type SentCode struct {
	Scene     string    `json:"scene"`
	Target    string    `json:"target"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SendVerifyCode issues a six digit code for scene/target and delivers it.
//
// Limits are per target (not per scene): one request per minute and ten per UTC
// day, so rotating scenes cannot be used to spam a mailbox or a phone.
func (service *Service) SendVerifyCode(ctx context.Context, scene, target string) (SentCode, error) {
	normalizedScene, ok := normalizeScene(scene)
	if !ok {
		return SentCode{}, ErrInvalidRequest
	}
	normalizedTarget, ok := normalizeTarget(target)
	if !ok {
		return SentCode{}, ErrInvalidRequest
	}
	code, err := randomDigits(codeLength)
	if err != nil {
		return SentCode{}, err
	}
	now := service.now().UTC()
	record := VerifyCode{
		ID:        uuid.NewString(),
		Scene:     normalizedScene,
		Target:    normalizedTarget,
		CodeHash:  hashCode(normalizedScene, normalizedTarget, code),
		ExpiresAt: now.Add(codeTTL),
		CreatedAt: now,
	}
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var latest VerifyCode
		switch err := tx.Where("target = ?", normalizedTarget).Order("created_at DESC").First(&latest).Error; {
		case err == nil:
			if now.Sub(latest.CreatedAt) < codeInterval {
				return ErrCodeTooSoon
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
		default:
			return err
		}
		var today int64
		if err := tx.Model(&VerifyCode{}).
			Where("target = ? AND created_at >= ?", normalizedTarget, startOfDay(now)).
			Count(&today).Error; err != nil {
			return err
		}
		if today >= dailyCodeLimit {
			return ErrCodeDailyLimit
		}
		return tx.Create(&record).Error
	})
	if err != nil {
		return SentCode{}, err
	}
	if err := service.deliverCode(ctx, record, code); err != nil {
		// The recipient never got it: burn the row so a leaked code cannot be used.
		service.db.WithContext(ctx).Model(&VerifyCode{}).
			Where("id = ?", record.ID).Update("consumed_at", service.now().UTC())
		return SentCode{}, err
	}
	return SentCode{Scene: record.Scene, Target: record.Target, ExpiresAt: record.ExpiresAt}, nil
}

// VerifyCode checks and consumes a code. A wrong guess burns one attempt, and the
// fifth wrong guess burns the whole code.
func (service *Service) VerifyCode(ctx context.Context, scene, target, code string) error {
	normalizedScene, ok := normalizeScene(scene)
	if !ok {
		return ErrInvalidRequest
	}
	normalizedTarget, ok := normalizeTarget(target)
	if !ok {
		return ErrInvalidRequest
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return ErrCodeInvalid
	}
	now := service.now().UTC()
	var record VerifyCode
	err := service.db.WithContext(ctx).
		Where("scene = ? AND target = ? AND consumed_at IS NULL", normalizedScene, normalizedTarget).
		Order("created_at DESC").First(&record).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return ErrCodeNotFound
	case err != nil:
		return err
	}
	if !now.Before(record.ExpiresAt) {
		return ErrCodeExpired
	}
	if record.Attempts >= maxCodeAttempts {
		return ErrCodeExhausted
	}
	expected := hashCode(normalizedScene, normalizedTarget, code)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(record.CodeHash)) != 1 {
		attempts := record.Attempts + 1
		updates := map[string]any{"attempts": attempts}
		if attempts >= maxCodeAttempts {
			updates["consumed_at"] = now
		}
		if err := service.db.WithContext(ctx).Model(&VerifyCode{}).
			Where("id = ?", record.ID).Updates(updates).Error; err != nil {
			return err
		}
		return ErrCodeInvalid
	}
	consumed := service.db.WithContext(ctx).Model(&VerifyCode{}).
		Where("id = ? AND consumed_at IS NULL", record.ID).Update("consumed_at", now)
	if consumed.Error != nil {
		return consumed.Error
	}
	if consumed.RowsAffected != 1 {
		return ErrCodeConsumed
	}
	return nil
}

// --- password reset ---------------------------------------------------------

// ResetTicket is the issued material. Token never reaches an HTTP response: the
// handler logs it in development only so the flow can be finished locally.
type ResetTicket struct {
	ID         string    `json:"id"`
	MerchantID string    `json:"merchant_id"`
	ExpiresAt  time.Time `json:"expires_at"`
	Token      string    `json:"-"`
}

// RequestPasswordReset issues a one-time reset token for identifier.
//
// An unknown identifier is not an error: the caller answers 202 either way so the
// endpoint cannot be used to enumerate merchants.
func (service *Service) RequestPasswordReset(ctx context.Context, identifier string) (bool, error) {
	merchant, found, err := service.findMerchant(service.db.WithContext(ctx), identifier)
	if err != nil {
		return false, err
	}
	if !found {
		service.logf("notify: password reset requested for unknown identifier %q", maskTarget(strings.TrimSpace(identifier)))
		return false, nil
	}
	ticket, err := service.issueResetToken(ctx, merchant.ID)
	if err != nil {
		return false, err
	}
	if service.logTokens {
		service.logf("notify: password reset token for merchant=%s token=%s expires_at=%s",
			merchant.ID, ticket.Token, ticket.ExpiresAt.Format(time.RFC3339))
	}
	service.deliverResetTicket(ctx, merchant, ticket)
	return true, nil
}

// ResetPassword consumes a reset token, stores the new password and revokes every
// existing session of that merchant.
func (service *Service) ResetPassword(ctx context.Context, identifier, token, newPassword string) error {
	passwordValue, err := validatePassword(newPassword)
	if err != nil {
		return ErrInvalidPassword
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrInvalidToken
	}
	now := service.now().UTC()
	return service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		merchant, found, err := service.findMerchant(tx, identifier)
		if err != nil {
			return err
		}
		if !found {
			return ErrInvalidToken
		}
		var record PasswordReset
		err = tx.Where("token_hash = ? AND principal_type = ? AND principal_id = ? AND consumed_at IS NULL",
			hashToken(token), principalTypeMerchant, merchant.ID).First(&record).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return ErrInvalidToken
		case err != nil:
			return err
		}
		if !now.Before(record.ExpiresAt) {
			return ErrTokenExpired
		}
		consumed := tx.Model(&PasswordReset{}).Where("id = ? AND consumed_at IS NULL", record.ID).
			Update("consumed_at", now)
		if consumed.Error != nil {
			return consumed.Error
		}
		if consumed.RowsAffected != 1 {
			return ErrTokenConsumed
		}
		hash, err := password.Hash(passwordValue)
		if err != nil {
			return err
		}
		if err := tx.Model(&merchantRecord{}).Where("id = ?", merchant.ID).
			Update("password_hash", hash).Error; err != nil {
			return err
		}
		// Any other outstanding token for this merchant dies with the reset.
		if err := tx.Model(&PasswordReset{}).
			Where("principal_type = ? AND principal_id = ? AND consumed_at IS NULL", principalTypeMerchant, merchant.ID).
			Update("consumed_at", now).Error; err != nil {
			return err
		}
		return revokeSessions(tx, merchant.ID, now)
	})
}

// ChangePassword verifies the current password and stores a new one. The caller's
// own session stays valid; other devices are left alone (this is not a reset).
func (service *Service) ChangePassword(ctx context.Context, merchantID, oldPassword, newPassword string) error {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return ErrInvalidRequest
	}
	passwordValue, err := validatePassword(newPassword)
	if err != nil {
		return ErrInvalidPassword
	}
	if strings.TrimSpace(oldPassword) == "" {
		return ErrInvalidOldPassword
	}
	return service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var merchant merchantRecord
		if err := tx.First(&merchant, "id = ?", merchantID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if merchant.PasswordHash == nil {
			return ErrInvalidOldPassword
		}
		salt := ""
		if merchant.LegacyUID != nil {
			salt = strconv.FormatInt(*merchant.LegacyUID, 10)
		}
		matched, _, err := password.VerifyWithSalt(oldPassword, *merchant.PasswordHash, salt)
		if err != nil || !matched {
			return ErrInvalidOldPassword
		}
		hash, err := password.Hash(passwordValue)
		if err != nil {
			return err
		}
		return tx.Model(&merchantRecord{}).Where("id = ?", merchant.ID).
			Update("password_hash", hash).Error
	})
}

// --- notification channels --------------------------------------------------

// ChannelInput is the editable part of a channel. ConfigSet distinguishes "not
// provided" (keep the stored secret) from an explicit replacement.
type ChannelInput struct {
	Kind      ChannelKind
	Provider  string
	Config    map[string]string
	ConfigSet bool
	Status    ChannelStatus
}

// ChannelView is a channel as the console sees it: the secret stays hidden, only
// the configured field names are echoed back.
type ChannelView struct {
	ID         string        `json:"id"`
	Kind       ChannelKind   `json:"kind"`
	Provider   string        `json:"provider"`
	Status     ChannelStatus `json:"status"`
	ConfigKeys []string      `json:"config_keys"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

// ChannelFilter narrows the administrator list.
type ChannelFilter struct {
	Kind   string
	Status string
}

// Page is the shared pagination shape.
type Page struct {
	Number int
	Size   int
}

// ListChannels returns one page of channels without any secret material.
func (service *Service) ListChannels(ctx context.Context, filter ChannelFilter, page Page) ([]ChannelView, int64, error) {
	if service.cipher == nil {
		return nil, 0, ErrCipherUnavailable
	}
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 || page.Size > 100 {
		page.Size = 20
	}
	query := service.db.WithContext(ctx).Model(&Channel{})
	if kind := strings.TrimSpace(filter.Kind); kind != "" {
		normalized := ChannelKind(strings.ToLower(kind))
		if !ValidKind(normalized) {
			return nil, 0, ErrInvalidRequest
		}
		query = query.Where("kind = ?", normalized)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		normalized := ChannelStatus(strings.ToLower(status))
		if !ValidStatus(normalized) {
			return nil, 0, ErrInvalidRequest
		}
		query = query.Where("status = ?", normalized)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var channels []Channel
	if err := query.Order("created_at DESC, id DESC").
		Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&channels).Error; err != nil {
		return nil, 0, err
	}
	views := make([]ChannelView, 0, len(channels))
	for _, channel := range channels {
		view, err := service.viewOf(channel)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, view)
	}
	return views, total, nil
}

// CreateChannel stores a channel with its configuration encrypted.
func (service *Service) CreateChannel(ctx context.Context, input ChannelInput) (ChannelView, error) {
	if service.cipher == nil {
		return ChannelView{}, ErrCipherUnavailable
	}
	kind, provider, status, err := validateChannelInput(input)
	if err != nil {
		return ChannelView{}, err
	}
	config, err := normalizeChannelConfig(input.Config)
	if err != nil {
		return ChannelView{}, err
	}
	now := service.now().UTC()
	channel := Channel{
		ID:        uuid.NewString(),
		Kind:      kind,
		Provider:  provider,
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}
	encrypted, err := service.encryptConfig(channel.ID, config)
	if err != nil {
		return ChannelView{}, err
	}
	channel.ConfigEncrypted = encrypted
	if err := service.db.WithContext(ctx).Create(&channel).Error; err != nil {
		return ChannelView{}, err
	}
	return ChannelView{
		ID: channel.ID, Kind: channel.Kind, Provider: channel.Provider, Status: channel.Status,
		ConfigKeys: sortedKeys(config), CreatedAt: channel.CreatedAt, UpdatedAt: channel.UpdatedAt,
	}, nil
}

// UpdateChannel updates a channel. An omitted config keeps the stored secret, so
// the console can flip a status without resending credentials.
func (service *Service) UpdateChannel(ctx context.Context, id string, input ChannelInput) (ChannelView, error) {
	if service.cipher == nil {
		return ChannelView{}, ErrCipherUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ChannelView{}, ErrInvalidRequest
	}
	kind, provider, status, err := validateChannelInput(input)
	if err != nil {
		return ChannelView{}, err
	}
	var channel Channel
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&channel, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		now := service.now().UTC()
		updates := map[string]any{
			"kind":       string(kind),
			"provider":   provider,
			"status":     string(status),
			"updated_at": now,
		}
		if input.ConfigSet {
			config, err := normalizeChannelConfig(input.Config)
			if err != nil {
				return err
			}
			encrypted, err := service.encryptConfig(channel.ID, config)
			if err != nil {
				return err
			}
			updates["config_encrypted"] = encrypted
		}
		if err := tx.Model(&Channel{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		channel.Kind = kind
		channel.Provider = provider
		channel.Status = status
		channel.UpdatedAt = now
		return nil
	})
	if err != nil {
		return ChannelView{}, err
	}
	return service.viewOf(channel)
}

// DeleteChannel removes a channel.
func (service *Service) DeleteChannel(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrInvalidRequest
	}
	result := service.db.WithContext(ctx).Delete(&Channel{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// TestChannel decrypts the stored configuration (proving the master key still
// opens it) and sends one test message through the matching Sender method.
func (service *Service) TestChannel(ctx context.Context, id string) (ChannelView, error) {
	if service.cipher == nil {
		return ChannelView{}, ErrCipherUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ChannelView{}, ErrInvalidRequest
	}
	var channel Channel
	if err := service.db.WithContext(ctx).First(&channel, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelView{}, ErrNotFound
		}
		return ChannelView{}, err
	}
	config, err := service.decryptConfig(channel)
	if err != nil {
		return ChannelView{}, err
	}
	text := fmt.Sprintf("GorixPay 测试消息：通道 %s（%s）配置可用。", channel.ID, channel.Provider)
	switch channel.Kind {
	case KindSMS:
		target := config["test_target"]
		if target == "" {
			target = "+8610000000000"
		}
		if err := service.sender.SendSMS(ctx, target, text); err != nil {
			service.logf("notify: test sms for channel %s failed: %v", channel.ID, err)
			return ChannelView{}, ErrDeliveryFailed
		}
	case KindMail:
		target := config["test_target"]
		if target == "" {
			target = "devnull@example.com"
		}
		if err := service.sender.SendMail(ctx, target, "GorixPay 通知通道测试", text); err != nil {
			service.logf("notify: test mail for channel %s failed: %v", channel.ID, err)
			return ChannelView{}, ErrDeliveryFailed
		}
	default:
		return ChannelView{}, ErrInvalidRequest
	}
	return service.viewOf(channel)
}

// --- internals --------------------------------------------------------------

func startOfDay(value time.Time) time.Time {
	year, month, day := value.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func randomDigits(length int) (string, error) {
	var builder strings.Builder
	for index := 0; index < length; index++ {
		digit, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		builder.WriteString(digit.String())
	}
	return builder.String(), nil
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// hashCode binds the code to its row so a hash lifted from one row cannot be
// replayed against another scene/target pair. The code also expires and burns
// after five wrong guesses, which is what actually stops online guessing.
func hashCode(scene, target, code string) string {
	sum := sha256.Sum256([]byte(scene + "\x00" + target + "\x00" + code))
	return hex.EncodeToString(sum[:])
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func validatePassword(value string) (string, error) {
	if utf8.RuneCountInString(value) < minPasswordRune || utf8.RuneCountInString(value) > maxPasswordRune {
		return "", ErrInvalidPassword
	}
	return value, nil
}

// findMerchant mirrors the merchant login lookup: legacy uid, email or phone.
func (service *Service) findMerchant(db *gorm.DB, identifier string) (merchantRecord, bool, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return merchantRecord{}, false, nil
	}
	query := db
	if uid, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		query = query.Where("legacy_uid = ? OR LOWER(email) = ? OR phone = ?", uid, strings.ToLower(trimmed), trimmed)
	} else {
		query = query.Where("LOWER(email) = ? OR phone = ?", strings.ToLower(trimmed), trimmed)
	}
	var merchant merchantRecord
	if err := query.First(&merchant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return merchantRecord{}, false, nil
		}
		return merchantRecord{}, false, err
	}
	return merchant, true, nil
}

func (service *Service) issueResetToken(ctx context.Context, merchantID string) (ResetTicket, error) {
	token, err := randomToken(resetTokenBytes)
	if err != nil {
		return ResetTicket{}, err
	}
	now := service.now().UTC()
	record := PasswordReset{
		ID:            uuid.NewString(),
		PrincipalType: principalTypeMerchant,
		PrincipalID:   merchantID,
		TokenHash:     hashToken(token),
		ExpiresAt:     now.Add(resetTTL),
		CreatedAt:     now,
	}
	if err := service.db.WithContext(ctx).Create(&record).Error; err != nil {
		return ResetTicket{}, err
	}
	return ResetTicket{ID: record.ID, MerchantID: merchantID, ExpiresAt: record.ExpiresAt, Token: token}, nil
}

// deliverResetTicket sends the reset token through the commerce channel of the
// merchant; a merchant without email or phone simply gets nothing (the token is
// still in the log during development).
func (service *Service) deliverResetTicket(ctx context.Context, merchant merchantRecord, ticket ResetTicket) {
	text := "GorixPay 密码重置令牌：" + ticket.Token + "（30 分钟内有效，使用一次即失效）"
	if merchant.Email != nil && strings.TrimSpace(*merchant.Email) != "" {
		if err := service.sender.SendMail(ctx, strings.TrimSpace(*merchant.Email), "GorixPay 密码重置", text); err != nil {
			service.logf("notify: reset mail to merchant=%s failed: %v", merchant.ID, err)
		}
		return
	}
	if merchant.Phone != nil && strings.TrimSpace(*merchant.Phone) != "" {
		if err := service.sender.SendSMS(ctx, strings.TrimSpace(*merchant.Phone), text); err != nil {
			service.logf("notify: reset sms to merchant=%s failed: %v", merchant.ID, err)
		}
	}
}

func (service *Service) deliverCode(ctx context.Context, record VerifyCode, code string) error {
	text := fmt.Sprintf("GorixPay 验证码：%s，%d 分钟内有效。请勿泄露给他人。", code, int(codeTTL.Minutes()))
	if strings.Contains(record.Target, "@") {
		if err := service.sender.SendMail(ctx, record.Target, "GorixPay 验证码", text); err != nil {
			service.logf("notify: code mail to %s failed: %v", maskTarget(record.Target), err)
			return ErrDeliveryFailed
		}
		return nil
	}
	if err := service.sender.SendSMS(ctx, record.Target, text); err != nil {
		service.logf("notify: code sms to %s failed: %v", maskTarget(record.Target), err)
		return ErrDeliveryFailed
	}
	return nil
}

// revokeSessions invalidates every live session of the merchant. The reset path is
// the one place a password can change without the old secret, so everything that
// was signed in with the old password has to go.
func revokeSessions(tx *gorm.DB, merchantID string, now time.Time) error {
	return tx.Table("auth_sessions").
		Where("principal_type = ? AND principal_id = ? AND revoked_at IS NULL", principalTypeMerchant, merchantID).
		Update("revoked_at", now).Error
}

func validateChannelInput(input ChannelInput) (ChannelKind, string, ChannelStatus, error) {
	kind := ChannelKind(strings.ToLower(strings.TrimSpace(string(input.Kind))))
	if !ValidKind(kind) {
		return "", "", "", ErrInvalidRequest
	}
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider == "" || len(provider) > 32 {
		return "", "", "", ErrInvalidRequest
	}
	status := ChannelStatus(strings.ToLower(strings.TrimSpace(string(input.Status))))
	if status == "" {
		status = StatusActive
	}
	if !ValidStatus(status) {
		return "", "", "", ErrInvalidRequest
	}
	return kind, provider, status, nil
}

func normalizeChannelConfig(config map[string]string) (map[string]string, error) {
	if len(config) > maxConfigFields {
		return nil, ErrInvalidRequest
	}
	normalized := make(map[string]string, len(config))
	total := 0
	for key, value := range config {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" || len(trimmedKey) > maxConfigKey || len(value) > maxConfigValue {
			return nil, ErrInvalidRequest
		}
		normalized[trimmedKey] = value
		total += len(trimmedKey) + len(value)
	}
	if total > maxConfigBytes {
		return nil, ErrInvalidRequest
	}
	return normalized, nil
}

func (service *Service) encryptConfig(channelID string, config map[string]string) (string, error) {
	if service.cipher == nil {
		return "", ErrCipherUnavailable
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return service.cipher.Encrypt(payload, []byte(channelContextPrefix+channelID))
}

func (service *Service) decryptConfig(channel Channel) (map[string]string, error) {
	if service.cipher == nil {
		return nil, ErrCipherUnavailable
	}
	payload, _, err := service.cipher.Decrypt(channel.ConfigEncrypted, []byte(channelContextPrefix+channel.ID))
	if err != nil {
		return nil, err
	}
	config := map[string]string{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &config); err != nil {
			return nil, err
		}
	}
	return config, nil
}

// viewOf renders a channel without its secret: the field names are enough for the
// console to show what is configured.
func (service *Service) viewOf(channel Channel) (ChannelView, error) {
	config, err := service.decryptConfig(channel)
	if err != nil {
		return ChannelView{}, err
	}
	return ChannelView{
		ID:         channel.ID,
		Kind:       channel.Kind,
		Provider:   channel.Provider,
		Status:     channel.Status,
		ConfigKeys: sortedKeys(config),
		CreatedAt:  channel.CreatedAt,
		UpdatedAt:  channel.UpdatedAt,
	}, nil
}

func sortedKeys(config map[string]string) []string {
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
