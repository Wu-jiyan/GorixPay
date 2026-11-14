// Package notifyprefs stores merchant notification preferences and performs the
// best-effort delivery used by payment, settlement and complaint scenes.
//
// It lives in its own package because internal/notify is the notification
// channel/verification-code module owned by another workstream; this package
// only depends on it through the Mailer and SMSSender interfaces.
package notifyprefs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	identitymodel "gorixpay/internal/identity/model"

	"gorixpay/internal/platform/secret"

	"gorm.io/gorm"
)

var (
	ErrInvalidRequest       = errors.New("invalid notification request")
	ErrChannelNotConfigured = errors.New("notification channel is not configured")
)

type Scene string

const (
	ScenePayment    Scene = "payment"
	SceneSettlement Scene = "settlement"
	SceneComplaint  Scene = "complaint"
)

const (
	ChannelMail   = "mail"
	ChannelSMS    = "sms"
	ChannelWeChat = "wechat"
)

// WeChatTemplateSettingKey is the 公众号 template used by the wechat channel. It is
// the same key internal/notify reads, so one setting drives both senders.
const WeChatTemplateSettingKey = "wechat.mp.template_id"

// weChatProvider is the oauth_bindings provider value that stores a 公众号 openid.
const weChatProvider = "wechat"

type Prefs struct {
	MerchantID       string    `gorm:"type:varchar(36);primaryKey" json:"merchant_id"`
	NotifyPayment    bool      `gorm:"not null" json:"notify_payment"`
	NotifySettlement bool      `gorm:"not null" json:"notify_settlement"`
	NotifyComplaint  bool      `gorm:"not null" json:"notify_complaint"`
	NotifyChannel    string    `gorm:"type:varchar(20);not null" json:"notify_channel"`
	UpdatedAt        time.Time `gorm:"not null" json:"updated_at"`
}

func (Prefs) TableName() string { return "merchant_notification_prefs" }

type systemSetting struct {
	Key   string `gorm:"column:setting_key;primaryKey"`
	Value string `gorm:"column:setting_value;type:text;not null"`
}

func (systemSetting) TableName() string { return "system_settings" }

type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

type SMSSender interface {
	Send(ctx context.Context, phone, message string) error
}

// WeChatTemplateSender pushes a 公众号 template message. The shape matches
// internal/notify's sender on purpose, so this package never imports it: the wiring
// layer hands over whatever implements the method set.
type WeChatTemplateSender interface {
	SendWeChatTemplate(ctx context.Context, openID, templateID, url string, data map[string]string) error
}

// oauthBinding is the slice of oauth_bindings this package reads to find the
// merchant's 公众号 openid.
type oauthBinding struct {
	ID            string `gorm:"type:varchar(36);primaryKey"`
	PrincipalType string `gorm:"type:varchar(20);not null"`
	PrincipalID   string `gorm:"type:varchar(36);not null"`
	Provider      string `gorm:"type:varchar(20);not null"`
	OpenID        string `gorm:"column:open_id;type:varchar(128);not null"`
}

func (oauthBinding) TableName() string { return "oauth_bindings" }

type Service struct {
	db     *gorm.DB
	now    func() time.Time
	logger *slog.Logger
	mail   Mailer
	sms    SMSSender
	wechat WeChatTemplateSender
	// cipher decrypts the credential settings (SMTP password, SMS token) that the
	// admin API stores encrypted.
	cipher *secret.Cipher
}

func New(db *gorm.DB, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Service{
		db: db, now: time.Now, logger: logger,
		mail: &settingsMailer{db: db},
		sms:  &settingsSMS{db: db},
	}
}

// WithCipher lets the settings backed senders decrypt stored credentials. Without
// it an encrypted SMTP password or SMS token is reported as unconfigured instead
// of being sent as ciphertext.
func (service *Service) WithCipher(cipher *secret.Cipher) *Service {
	copied := *service
	copied.cipher = cipher
	copied.mail = &settingsMailer{db: service.db, cipher: cipher}
	copied.sms = &settingsSMS{db: service.db, cipher: cipher}
	return &copied
}

// WithSenders replaces the outbound channels, which is how tests stub delivery.
func (service *Service) WithSenders(mail Mailer, sms SMSSender) *Service {
	copied := *service
	if mail != nil {
		copied.mail = mail
	}
	if sms != nil {
		copied.sms = sms
	}
	return &copied
}

// WithWeChat attaches the 公众号 template sender used by the "wechat" channel.
// Without one the channel falls back to mail, so an unwired deployment keeps
// delivering notifications.
func (service *Service) WithWeChat(sender WeChatTemplateSender) *Service {
	copied := *service
	if sender != nil {
		copied.wechat = sender
	}
	return &copied
}

func defaultPrefs(merchantID string) Prefs {
	return Prefs{
		MerchantID: merchantID, NotifyPayment: true, NotifySettlement: true,
		NotifyComplaint: true, NotifyChannel: ChannelMail,
	}
}

func (service *Service) Prefs(ctx context.Context, merchantID string) (Prefs, error) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return Prefs{}, ErrInvalidRequest
	}
	var stored Prefs
	if err := service.db.WithContext(ctx).First(&stored, "merchant_id = ?", merchantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return defaultPrefs(merchantID), nil
		}
		return Prefs{}, err
	}
	if !validChannel(stored.NotifyChannel) {
		stored.NotifyChannel = ChannelMail
	}
	return stored, nil
}

type UpdateInput struct {
	NotifyPayment    *bool   `json:"notify_payment"`
	NotifySettlement *bool   `json:"notify_settlement"`
	NotifyComplaint  *bool   `json:"notify_complaint"`
	NotifyChannel    *string `json:"notify_channel"`
}

func (service *Service) UpdatePrefs(ctx context.Context, merchantID string, input UpdateInput) (Prefs, error) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return Prefs{}, ErrInvalidRequest
	}
	current, err := service.Prefs(ctx, merchantID)
	if err != nil {
		return Prefs{}, err
	}
	if input.NotifyPayment != nil {
		current.NotifyPayment = *input.NotifyPayment
	}
	if input.NotifySettlement != nil {
		current.NotifySettlement = *input.NotifySettlement
	}
	if input.NotifyComplaint != nil {
		current.NotifyComplaint = *input.NotifyComplaint
	}
	if input.NotifyChannel != nil {
		channel := strings.ToLower(strings.TrimSpace(*input.NotifyChannel))
		if !validChannel(channel) {
			return Prefs{}, ErrInvalidRequest
		}
		current.NotifyChannel = channel
	}
	current.MerchantID = merchantID
	current.UpdatedAt = service.now().UTC()
	if err := service.db.WithContext(ctx).Save(&current).Error; err != nil {
		return Prefs{}, err
	}
	return current, nil
}

// Notify sends one notification for a scene, honouring the merchant's switches
// and channel. Delivery problems are logged and swallowed; only a bad scene or
// missing merchant id is an error, so a notification can never block the caller.
func (service *Service) Notify(ctx context.Context, merchantID, scene, subject, body string) error {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return ErrInvalidRequest
	}
	enabled, ok := sceneSwitch(scene)
	if !ok {
		return ErrInvalidRequest
	}
	prefs, err := service.Prefs(ctx, merchantID)
	if err != nil {
		return err
	}
	if !enabled(prefs) {
		return nil
	}
	var merchant identitymodel.Merchant
	if err := service.db.WithContext(ctx).First(&merchant, "id = ?", merchantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			service.logger.Warn("notify: merchant not found", "merchant_id", merchantID, "scene", scene)
			return nil
		}
		return err
	}
	email := ""
	if merchant.Email != nil {
		email = strings.TrimSpace(*merchant.Email)
	}
	phone := ""
	if merchant.Phone != nil {
		phone = strings.TrimSpace(*merchant.Phone)
	}
	if prefs.NotifyChannel == ChannelSMS {
		if phone == "" {
			service.logger.Warn("notify: sms selected but the merchant has no phone, sending mail",
				"merchant_id", merchantID, "scene", scene)
			return service.sendMail(ctx, merchantID, scene, email, subject, body)
		}
		if err := service.sms.Send(ctx, phone, subject+" "+body); err != nil {
			if errors.Is(err, ErrChannelNotConfigured) {
				service.logger.Warn("notify: sms channel is not configured, sending mail",
					"merchant_id", merchantID, "scene", scene)
				return service.sendMail(ctx, merchantID, scene, email, subject, body)
			}
			service.logger.Error("notify: sms delivery failed", "merchant_id", merchantID, "scene", scene, "error", err)
			return nil
		}
		return nil
	}
	if prefs.NotifyChannel == ChannelWeChat {
		return service.sendWeChat(ctx, merchantID, scene, email, subject, body)
	}
	return service.sendMail(ctx, merchantID, scene, email, subject, body)
}

// sendWeChat pushes the notification as a 公众号 template message and falls back to
// mail whenever WeChat cannot be used: no sender wired, merchant never bound an
// openid, template id not configured, or the send itself failed. A notification is
// best effort, so nothing here returns an error.
func (service *Service) sendWeChat(ctx context.Context, merchantID, scene, email, subject, body string) error {
	fallback := func(reason string, attributes ...any) error {
		service.logger.Warn("notify: wechat channel unavailable, sending mail",
			append([]any{"merchant_id", merchantID, "scene", scene, "reason", reason}, attributes...)...)
		return service.sendMail(ctx, merchantID, scene, email, subject, body)
	}
	if service.wechat == nil {
		return fallback("sender_not_wired")
	}
	openID, err := service.weChatOpenID(ctx, merchantID)
	if err != nil {
		return fallback("openid_lookup_failed", "error", err)
	}
	if openID == "" {
		return fallback("no_openid")
	}
	settings, err := loadSettings(ctx, service.db, service.cipher, WeChatTemplateSettingKey)
	if err != nil {
		return fallback("settings_lookup_failed", "error", err)
	}
	templateID := settings[WeChatTemplateSettingKey]
	if templateID == "" {
		return fallback("template_not_configured")
	}
	// Epay style template: first carries the title, keyword1 the detail.
	data := map[string]string{"first": subject, "keyword1": body}
	if err := service.wechat.SendWeChatTemplate(ctx, openID, templateID, "", data); err != nil {
		return fallback("delivery_failed", "error", err)
	}
	return nil
}

// weChatOpenID returns the merchant's 公众号 openid, or "" when it never bound one.
func (service *Service) weChatOpenID(ctx context.Context, merchantID string) (string, error) {
	var binding oauthBinding
	err := service.db.WithContext(ctx).
		Where("principal_type = ? AND principal_id = ? AND provider = ?",
			string(identitymodel.PrincipalMerchant), merchantID, weChatProvider).
		First(&binding).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "", nil
	case err != nil:
		return "", err
	}
	return strings.TrimSpace(binding.OpenID), nil
}

func (service *Service) sendMail(ctx context.Context, merchantID, scene, to, subject, body string) error {
	if to == "" {
		service.logger.Warn("notify: merchant has no email address", "merchant_id", merchantID, "scene", scene)
		return nil
	}
	if err := service.mail.Send(ctx, to, subject, body); err != nil {
		if errors.Is(err, ErrChannelNotConfigured) {
			service.logger.Warn("notify: mail channel is not configured, skipping",
				"merchant_id", merchantID, "scene", scene)
			return nil
		}
		service.logger.Error("notify: mail delivery failed", "merchant_id", merchantID, "scene", scene, "error", err)
		return nil
	}
	return nil
}

func sceneSwitch(scene string) (func(Prefs) bool, bool) {
	switch Scene(strings.ToLower(strings.TrimSpace(scene))) {
	case ScenePayment:
		return func(prefs Prefs) bool { return prefs.NotifyPayment }, true
	case SceneSettlement:
		return func(prefs Prefs) bool { return prefs.NotifySettlement }, true
	case SceneComplaint:
		return func(prefs Prefs) bool { return prefs.NotifyComplaint }, true
	default:
		return nil, false
	}
}

func validChannel(channel string) bool {
	return channel == ChannelMail || channel == ChannelSMS || channel == ChannelWeChat
}
