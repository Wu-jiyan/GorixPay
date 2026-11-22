package notifyprefs

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"time"

	"gorixpay/internal/platform/netguard"
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
	portNumber, portErr := strconv.Atoi(port)
	if portErr != nil || portNumber <= 0 || portNumber > 65535 {
		return ErrChannelNotConfigured
	}
	// 运营可在控制台填任意 host：出站前按商户回调同一套 netguard 规则拦内网/回环，
	// 否则这里就是一条能打内网端口 25/465/587 的 SSRF 通道。
	if err := netguard.ValidateHostPort(host, portNumber); err != nil {
		return ErrChannelNotConfigured
	}
	var auth smtp.Auth
	if username := settings["notify.smtp_username"]; username != "" {
		auth = smtp.PlainAuth("", username, settings["notify.smtp_password"], host)
	}
	message := []byte("From: " + from + "\r\nTo: " + to + "\r\nSubject: " + subject +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n")
	address := net.JoinHostPort(host, port)
	// 拨号走 netguard 的受控 dialer：主机名解析出来的地址逐个检查，挡住
	// 「域名解析到 169.254.169.254」这类绕过。
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: netguard.Control()}
	done := make(chan error, 1)
	go func() {
		done <- sendMailWithDialer(address, auth, from, []string{to}, message, dialer)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sendMailWithDialer is smtp.SendMail with a caller supplied dialer, so the SMTP
// path can carry netguard's Control hook (the stdlib helper always dials plainly).
func sendMailWithDialer(address string, auth smtp.Auth, from string, to []string, message []byte, dialer *net.Dialer) error {
	connection, err := dialer.Dial("tcp", address)
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(connection, smtpHostOnly(address))
	if err != nil {
		_ = connection.Close()
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: smtpHostOnly(address), MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("smtp server does not support auth")
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// smtpHostOnly drops the port from a host:port pair (the TLS server name and
// smtp.NewClient both want the bare host).
func smtpHostOnly(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
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
	// 运营填的 webhook 地址同样要过 netguard：这是设置项里唯一的出站 URL。
	parsed, parseErr := url.Parse(endpoint)
	if parseErr != nil {
		return ErrChannelNotConfigured
	}
	if err := netguard.ValidateURL(parsed); err != nil {
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
