package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gorixpay/internal/platform/secret"

	"gorm.io/gorm"
)

// WeChatTemplateSettingKey is the system setting that holds the 公众号 template id.
// It is the one piece of the template message that is not per-configuration: the
// template belongs to the official account, so a deployment can change template
// without touching credentials.
const WeChatTemplateSettingKey = "wechat.mp.template_id"

// WeChat endpoints. The base URL is a field so tests can point at httptest.
const (
	defaultWeChatBaseURL   = "https://api.weixin.qq.com"
	weChatStableTokenPath  = "/cgi-bin/stable_token"
	weChatTemplateSendPath = "/cgi-bin/message/template/send"
	// WeChat returns a 7200 second token; refresh five minutes early so a token
	// never expires between the cache check and the API call.
	weChatTokenMargin   = 5 * time.Minute
	weChatBodyLimit     = 1 << 20
	weChatContextAADFmt = "wechat-mp:%s"
)

var (
	// ErrNotConfigured reports a missing 公众号 configuration or template id.
	ErrNotConfigured = errors.New("wechat template message is not configured")
)

// WeChatTemplateSender pushes a 公众号 template message. It is a separate
// interface on purpose: existing Sender implementations (log sender, SMS/mail
// gateways) keep compiling unchanged, and callers only take the WeChat path when
// the injected sender implements it.
type WeChatTemplateSender interface {
	SendWeChatTemplate(ctx context.Context, openID, templateID, url string, data map[string]string) error
}

// WeChatAPIError wraps a WeChat response whose errcode is not zero. Every API call
// reports errcode/errmsg this way; nothing is swallowed.
type WeChatAPIError struct {
	Operation string
	Code      int
	Message   string
}

func (apiError *WeChatAPIError) Error() string {
	return fmt.Sprintf("wechat %s failed: errcode=%d errmsg=%s", apiError.Operation, apiError.Code, apiError.Message)
}

// HTTPClient is the transport every WeChat call goes through. Tests inject an
// httptest backed implementation; production uses *http.Client.
type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

// WeChatMPConfig is the slice of wechat_mp_configs this sender reads. The secret is
// decrypted with secret.Cipher using the same AAD (wechat-mp:{id}) channelops
// writes with.
type WeChatMPConfig struct {
	ID                 string    `gorm:"type:varchar(36);primaryKey"`
	MerchantID         *string   `gorm:"type:varchar(36)"`
	AppID              string    `gorm:"type:varchar(64);not null"`
	AppSecretEncrypted string    `gorm:"type:text;not null"`
	Status             string    `gorm:"type:varchar(20);not null"`
	CreatedAt          time.Time `gorm:"not null"`
	UpdatedAt          time.Time `gorm:"not null"`
}

func (WeChatMPConfig) TableName() string { return "wechat_mp_configs" }

const weChatConfigStatusActive = "active"

type cachedWeChatToken struct {
	value     string
	expiresAt time.Time
}

// WeChatMPClient sends 公众号 template messages using the credentials stored in
// wechat_mp_configs and caches access tokens in memory.
type WeChatMPClient struct {
	db      *gorm.DB
	cipher  *secret.Cipher
	client  HTTPClient
	baseURL string
	now     func() time.Time
	logf    func(format string, arguments ...any)

	mutex  sync.Mutex
	tokens map[string]cachedWeChatToken
}

// WeChatOption tweaks the client.
type WeChatOption func(*WeChatMPClient)

// WithWeChatHTTPClient injects the transport (tests, or a client with custom settings).
func WithWeChatHTTPClient(client HTTPClient) WeChatOption {
	return func(target *WeChatMPClient) {
		if client != nil {
			target.client = client
		}
	}
}

// WithWeChatBaseURL points the client at another gateway (tests only).
func WithWeChatBaseURL(baseURL string) WeChatOption {
	return func(target *WeChatMPClient) {
		if trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/"); trimmed != "" {
			target.baseURL = trimmed
		}
	}
}

// WithWeChatClock overrides the clock used for the token cache.
func WithWeChatClock(now func() time.Time) WeChatOption {
	return func(target *WeChatMPClient) {
		if now != nil {
			target.now = now
		}
	}
}

// WithWeChatLogger sets where delivery failures are logged.
func WithWeChatLogger(logf func(format string, arguments ...any)) WeChatOption {
	return func(target *WeChatMPClient) {
		if logf != nil {
			target.logf = logf
		}
	}
}

// NewWeChatMPClient builds the sender. A nil cipher keeps the client constructible
// while every send fails with ErrCipherUnavailable, which is what a deployment
// without a master key looks like.
func NewWeChatMPClient(db *gorm.DB, cipher *secret.Cipher, options ...WeChatOption) *WeChatMPClient {
	client := &WeChatMPClient{
		db:      db,
		cipher:  cipher,
		client:  &http.Client{Timeout: 10 * time.Second},
		baseURL: defaultWeChatBaseURL,
		now:     time.Now,
		logf:    func(string, ...any) {},
		tokens:  map[string]cachedWeChatToken{},
	}
	for _, option := range options {
		option(client)
	}
	return client
}

// SendWeChatTemplate implements WeChatTemplateSender using the platform default
// 公众号: the configuration with no merchant owner and status=active, falling back
// to any active configuration.
func (client *WeChatMPClient) SendWeChatTemplate(ctx context.Context, openID, templateID, url string, data map[string]string) error {
	config, err := client.defaultConfig(ctx)
	if err != nil {
		return err
	}
	return client.sendWithConfig(ctx, config, openID, templateID, url, data)
}

// SendTemplateForConfig sends through one explicit configuration row, which is what
// the administrator "test message" endpoint needs: it must test the row being
// edited, not the platform default.
func (client *WeChatMPClient) SendTemplateForConfig(ctx context.Context, configID, openID, templateID, url string, data map[string]string) error {
	configID = strings.TrimSpace(configID)
	if configID == "" {
		return ErrInvalidRequest
	}
	var config WeChatMPConfig
	if err := client.db.WithContext(ctx).First(&config, "id = ?", configID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	return client.sendWithConfig(ctx, config, openID, templateID, url, data)
}

func (client *WeChatMPClient) sendWithConfig(ctx context.Context, config WeChatMPConfig, openID, templateID, url string, data map[string]string) error {
	if client.db == nil {
		return ErrNotConfigured
	}
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return ErrInvalidRequest
	}
	templateID = strings.TrimSpace(templateID)
	if templateID == "" {
		resolved, err := client.templateID(ctx)
		if err != nil {
			return err
		}
		templateID = resolved
	}
	if templateID == "" {
		return ErrNotConfigured
	}
	message := map[string]any{
		"touser":      openID,
		"template_id": templateID,
		"data":        weChatTemplateData(data),
	}
	if trimmed := strings.TrimSpace(url); trimmed != "" {
		message["url"] = trimmed
	}
	token, err := client.accessToken(ctx, config)
	if err != nil {
		return err
	}
	err = client.sendOnce(ctx, token, message)
	var apiError *WeChatAPIError
	if errors.As(err, &apiError) && weChatTokenInvalid(apiError.Code) {
		// The cached token was revoked out of band: drop it and retry once, but
		// report the original error if the retry cannot even get a token.
		client.forgetToken(config.AppID)
		refreshed, refreshErr := client.accessToken(ctx, config)
		if refreshErr != nil {
			return err
		}
		err = client.sendOnce(ctx, refreshed, message)
	}
	if err != nil {
		client.logf("notify: wechat template message to %s failed: %v", maskTarget(openID), err)
	}
	return err
}

// defaultConfig resolves which 公众号 sends platform notifications.
func (client *WeChatMPClient) defaultConfig(ctx context.Context) (WeChatMPConfig, error) {
	if client.db == nil {
		return WeChatMPConfig{}, ErrNotConfigured
	}
	var config WeChatMPConfig
	err := client.db.WithContext(ctx).
		Where("merchant_id IS NULL AND status = ?", weChatConfigStatusActive).
		Order("created_at ASC, id ASC").First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = client.db.WithContext(ctx).
			Where("status = ?", weChatConfigStatusActive).
			Order("created_at ASC, id ASC").First(&config).Error
	}
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return WeChatMPConfig{}, ErrNotConfigured
	case err != nil:
		return WeChatMPConfig{}, err
	}
	return config, nil
}

// templateID reads wechat.mp.template_id from system_settings. The value may be a
// bare string or a JSON string, so both are accepted.
func (client *WeChatMPClient) templateID(ctx context.Context) (string, error) {
	type settingRow struct {
		Value string `gorm:"column:setting_value"`
	}
	var record settingRow
	err := client.db.WithContext(ctx).Table("system_settings").
		Select("setting_value").Where("setting_key = ?", WeChatTemplateSettingKey).Take(&record).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "", nil
	case err != nil:
		return "", err
	}
	return strings.Trim(strings.TrimSpace(record.Value), `"`), nil
}

// accessToken returns a cached token when it is still good, otherwise asks WeChat.
// A failed refresh leaves the cache untouched: a still valid token keeps working.
func (client *WeChatMPClient) accessToken(ctx context.Context, config WeChatMPConfig) (string, error) {
	if client.cipher == nil {
		return "", ErrCipherUnavailable
	}
	now := client.now().UTC()
	client.mutex.Lock()
	cached, ok := client.tokens[config.AppID]
	client.mutex.Unlock()
	if ok && now.Before(cached.expiresAt) {
		return cached.value, nil
	}
	token, expiresIn, err := client.fetchToken(ctx, config)
	if err != nil {
		return "", err
	}
	if usable := expiresIn - weChatTokenMargin; usable > 0 {
		client.mutex.Lock()
		client.tokens[config.AppID] = cachedWeChatToken{value: token, expiresAt: now.Add(usable)}
		client.mutex.Unlock()
	}
	return token, nil
}

func (client *WeChatMPClient) forgetToken(appID string) {
	client.mutex.Lock()
	delete(client.tokens, appID)
	client.mutex.Unlock()
}

func (client *WeChatMPClient) fetchToken(ctx context.Context, config WeChatMPConfig) (string, time.Duration, error) {
	plain, _, err := client.cipher.Decrypt(config.AppSecretEncrypted, []byte(fmt.Sprintf(weChatContextAADFmt, config.ID)))
	if err != nil {
		return "", 0, fmt.Errorf("decrypt wechat app secret of config %s: %w", config.ID, err)
	}
	payload := map[string]string{
		"grant_type": "client_credential",
		"appid":      config.AppID,
		"secret":     string(plain),
	}
	body, err := client.post(ctx, weChatStableTokenPath, payload)
	if err != nil {
		return "", 0, err
	}
	var response struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", 0, fmt.Errorf("wechat stable_token: decode response: %w", err)
	}
	if response.ErrCode != 0 {
		return "", 0, &WeChatAPIError{Operation: "stable_token", Code: response.ErrCode, Message: response.ErrMsg}
	}
	if strings.TrimSpace(response.AccessToken) == "" {
		return "", 0, errors.New("wechat stable_token: empty access_token")
	}
	expiresIn := time.Duration(response.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 2 * time.Hour
	}
	return response.AccessToken, expiresIn, nil
}

func (client *WeChatMPClient) sendOnce(ctx context.Context, token string, message map[string]any) error {
	endpoint := weChatTemplateSendPath + "?access_token=" + url.QueryEscape(token)
	body, err := client.post(ctx, endpoint, message)
	if err != nil {
		return err
	}
	var response struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("wechat template send: decode response: %w", err)
	}
	if response.ErrCode != 0 {
		return &WeChatAPIError{Operation: "template_send", Code: response.ErrCode, Message: response.ErrMsg}
	}
	return nil
}

// post performs one JSON POST and returns the body. A non 200 status is an error
// even when the body happens to be JSON: WeChat only answers 200 on success.
func (client *WeChatMPClient) post(ctx context.Context, path string, payload any) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("wechat %s: %w", path, err)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, weChatBodyLimit))
	if readErr != nil {
		return nil, readErr
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wechat %s: upstream status %d", path, response.StatusCode)
	}
	return body, nil
}

// weChatTemplateData converts {key: value} into WeChat's {key: {"value": value}}
// envelope.
func weChatTemplateData(data map[string]string) map[string]map[string]string {
	converted := make(map[string]map[string]string, len(data))
	for key, value := range data {
		converted[key] = map[string]string{"value": value}
	}
	return converted
}

// weChatTokenInvalid lists the errcodes that mean "the access token is no longer
// usable", which is the only case worth a retry with a fresh token.
func weChatTokenInvalid(code int) bool {
	switch code {
	case 40001, 40014, 42001:
		return true
	default:
		return false
	}
}
