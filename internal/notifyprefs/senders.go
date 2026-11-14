package notifyprefs

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/smtp"
	"strconv"
	"time"

	"gorixpay/internal/platform/secret"

	"gorixpay/internal/settings"
	"gorm.io/gorm"
)

// settingsMailer sends through the SMTP settings kept in system_settings. It is
// evaluated on every send, so credentials can be filled in after startup.
type settingsMailer struct {
	db     *gorm.DB
	cipher *secret.Cipher
}

func (mailer *settingsMailer) Send(ctx context.Context, to, subject, body string) error {
	settings, err := loadSettings(ctx, mailer.db, mailer.cipher,
		"notify.smtp_host", "notify.smtp_port", "notify.smtp_username",
		"notify.smtp_password", "notify.smtp_from")
	if err != nil {
		return err
	}
	host := settings["notify.smtp_host"]
	from := settings["notify.smtp_from"]
	if host == "" || from == "" {
		return ErrChannelNotConfigured
	}
	port := settings["notify.smtp_port"]
	if port == "" {
		port = "587"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return ErrChannelNotConfigured
	}
	var auth smtp.Auth
	if username := settings["notify.smtp_username"]; username != "" {
		auth = smtp.PlainAuth("", username, settings["notify.smtp_password"], host)
	}
	message := []byte("From: " + from + "\r\nTo: " + to + "\r\nSubject: " + subject +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n")
	address := host + ":" + port
	done := make(chan error, 1)
	go func() {
		done <- smtp.SendMail(address, auth, from, []string{to}, message)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// settingsSMS posts to a webhook style SMS gateway. Without notify.sms_endpoint
// the channel reports itself as unconfigured.
type settingsSMS struct {
	db     *gorm.DB
	client *http.Client
	cipher *secret.Cipher
}

func (sender *settingsSMS) Send(ctx context.Context, phone, message string) error {
	settings, err := loadSettings(ctx, sender.db, sender.cipher, "notify.sms_endpoint", "notify.sms_token")
	if err != nil {
		return err
	}
	endpoint := settings["notify.sms_endpoint"]
	if endpoint == "" {
		return ErrChannelNotConfigured
	}
	payload, err := json.Marshal(map[string]string{"phone": phone, "message": message})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if token := settings["notify.sms_token"]; token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := sender.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrChannelNotConfigured
	}
	return nil
}

// loadSettings reads the given keys and returns their usable values: the stored
// JSON is unwrapped, and credential keys are decrypted. A read failure for a
// credential reports the channel as unconfigured rather than handing a ciphertext
// to the SMTP or SMS gateway.
func loadSettings(ctx context.Context, db *gorm.DB, cipher *secret.Cipher, keys ...string) (map[string]string, error) {
	var records []systemSetting
	if err := db.WithContext(ctx).Where("setting_key IN ?", keys).Find(&records).Error; err != nil {
		return nil, err
	}
	values := make(map[string]string, len(records))
	for _, record := range records {
		if settings.Secret(record.Key) {
			plain, err := settings.Decrypt(cipher, record.Key, record.Value)
			if err != nil {
				continue
			}
			values[record.Key] = plain
			continue
		}
		values[record.Key] = settings.Text(record.Value)
	}
	return values, nil
}
