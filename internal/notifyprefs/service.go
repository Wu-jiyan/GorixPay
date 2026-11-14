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
	ChannelMail = "mail"
	ChannelSMS  = "sms"
)

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

type Service struct {
	db     *gorm.DB
	now    func() time.Time
	logger *slog.Logger
	mail   Mailer
	sms    SMSSender
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
	return service.sendMail(ctx, merchantID, scene, email, subject, body)
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
	return channel == ChannelMail || channel == ChannelSMS
}
