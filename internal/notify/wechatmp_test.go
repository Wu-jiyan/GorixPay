package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorixpay/internal/platform/secret"

	"gorm.io/gorm"
)

// settingRow is the system_settings slice these tests seed.
type settingRow struct {
	Key       string    `gorm:"column:setting_key;primaryKey"`
	Value     string    `gorm:"column:setting_value;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (settingRow) TableName() string { return "system_settings" }

// fakeWeChatUpstream imitates the two WeChat endpoints a template message needs.
type fakeWeChatUpstream struct {
	server *httptest.Server

	mutex           sync.Mutex
	tokenCalls      int
	sendCalls       int
	lastTemplate    map[string]any
	lastAccessToken string
	tokens          []string
	tokenErrCode    int
	tokenStatus     int
	pendingErrCodes []int
	sendStatus      int
}

func newFakeWeChat(t *testing.T) *fakeWeChatUpstream {
	t.Helper()
	upstream := &fakeWeChatUpstream{tokens: []string{"token-1", "token-2", "token-3"}}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstream.mutex.Lock()
		defer upstream.mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case weChatStableTokenPath:
			upstream.tokenCalls++
			if upstream.tokenStatus != 0 {
				writer.WriteHeader(upstream.tokenStatus)
				return
			}
			if upstream.tokenErrCode != 0 {
				_ = json.NewEncoder(writer).Encode(map[string]any{"errcode": upstream.tokenErrCode, "errmsg": "invalid appid"})
				return
			}
			index := upstream.tokenCalls - 1
			token := "token-1"
			if index < len(upstream.tokens) && upstream.tokens[index] != "" {
				token = upstream.tokens[index]
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": token, "expires_in": 7200})
		case weChatTemplateSendPath:
			upstream.sendCalls++
			upstream.lastAccessToken = request.URL.Query().Get("access_token")
			body, _ := io.ReadAll(request.Body)
			payload := map[string]any{}
			_ = json.Unmarshal(body, &payload)
			upstream.lastTemplate = payload
			if upstream.sendStatus != 0 {
				writer.WriteHeader(upstream.sendStatus)
				return
			}
			code := 0
			if len(upstream.pendingErrCodes) > 0 {
				code = upstream.pendingErrCodes[0]
				upstream.pendingErrCodes = upstream.pendingErrCodes[1:]
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"errcode": code, "errmsg": "ok"})
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func wechatFixture(t *testing.T, name string) (*WeChatMPClient, *gorm.DB, *fakeWeChatUpstream, *clock, *secret.Cipher) {
	t.Helper()
	db, cleanup, err := openFixtureDB(name)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(cleanup)
	if err := db.AutoMigrate(&WeChatMPConfig{}, &settingRow{}); err != nil {
		t.Fatalf("migrate fixture: %v", err)
	}
	cipher := testCipher(t)
	source := &clock{at: fixtureStart}
	upstream := newFakeWeChat(t)
	client := NewWeChatMPClient(db, cipher,
		WithWeChatHTTPClient(upstream.server.Client()),
		WithWeChatBaseURL(upstream.server.URL),
		WithWeChatClock(source.Now),
	)
	return client, db, upstream, source, cipher
}

func seedWeChatConfig(t *testing.T, db *gorm.DB, cipher *secret.Cipher, id, appID string, merchantID *string, status string) {
	t.Helper()
	encrypted, err := cipher.Encrypt([]byte("app-secret-"+id), []byte("wechat-mp:"+id))
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	record := WeChatMPConfig{ID: id, MerchantID: merchantID, AppID: appID, AppSecretEncrypted: encrypted, Status: status}
	if err := db.Create(&record).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
}

func seedTemplateID(t *testing.T, db *gorm.DB, value string) {
	t.Helper()
	if err := db.Create(&settingRow{Key: WeChatTemplateSettingKey, Value: value, UpdatedAt: fixtureStart}).Error; err != nil {
		t.Fatalf("seed template id: %v", err)
	}
}

func TestWeChatTokenCacheAndRefresh(t *testing.T) {
	client, db, upstream, source, cipher := wechatFixture(t, "wechat-token")
	seedWeChatConfig(t, db, cipher, "mp-1", "wx-app-1", nil, weChatConfigStatusActive)
	seedTemplateID(t, db, "tmpl-1")
	ctx := context.Background()

	if err := client.SendWeChatTemplate(ctx, "open-1", "", "", map[string]string{"first": "第一"}); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if upstream.tokenCalls != 1 || upstream.sendCalls != 1 {
		t.Fatalf("token calls=%d send calls=%d", upstream.tokenCalls, upstream.sendCalls)
	}
	if upstream.lastAccessToken != "token-1" {
		t.Fatalf("access_token = %q", upstream.lastAccessToken)
	}
	// A second message inside the cache window must not ask for a token again.
	if err := client.SendWeChatTemplate(ctx, "open-1", "", "", map[string]string{"first": "第二条"}); err != nil {
		t.Fatalf("second send: %v", err)
	}
	if upstream.tokenCalls != 1 {
		t.Fatalf("cached token was not reused: token calls=%d", upstream.tokenCalls)
	}
	// Past the refresh margin the client fetches a new token.
	source.Advance(7200*time.Second - 4*time.Minute)
	if err := client.SendWeChatTemplate(ctx, "open-1", "", "", map[string]string{"first": "第三条"}); err != nil {
		t.Fatalf("third send: %v", err)
	}
	if upstream.tokenCalls != 2 {
		t.Fatalf("token was not refreshed: token calls=%d", upstream.tokenCalls)
	}
	if upstream.lastAccessToken != "token-2" {
		t.Fatalf("access_token after refresh = %q", upstream.lastAccessToken)
	}
}

func TestWeChatTemplateNotConfigured(t *testing.T) {
	client, db, upstream, _, cipher := wechatFixture(t, "wechat-template")
	seedWeChatConfig(t, db, cipher, "mp-1", "wx-app-1", nil, weChatConfigStatusActive)
	ctx := context.Background()

	if err := client.SendWeChatTemplate(ctx, "open-1", "", "", map[string]string{"first": "x"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("missing template err = %v, want ErrNotConfigured", err)
	}
	if upstream.tokenCalls != 0 || upstream.sendCalls != 0 {
		t.Fatalf("no HTTP call should happen without a template: token=%d send=%d", upstream.tokenCalls, upstream.sendCalls)
	}
	// An explicit template id bypasses the setting.
	if err := client.SendWeChatTemplate(ctx, "open-1", "tmpl-explicit", "", map[string]string{"first": "x"}); err != nil {
		t.Fatalf("explicit template: %v", err)
	}
	if upstream.lastTemplate["template_id"] != "tmpl-explicit" {
		t.Fatalf("template_id = %v", upstream.lastTemplate["template_id"])
	}
}

func TestWeChatErrorsAreSurfaced(t *testing.T) {
	client, db, upstream, _, cipher := wechatFixture(t, "wechat-errors")
	seedWeChatConfig(t, db, cipher, "mp-1", "wx-app-1", nil, weChatConfigStatusActive)
	seedTemplateID(t, db, "tmpl-1")
	ctx := context.Background()

	upstream.pendingErrCodes = []int{40003}
	err := client.SendWeChatTemplate(ctx, "open-1", "tmpl-1", "", map[string]string{"first": "x"})
	var apiError *WeChatAPIError
	if !errors.As(err, &apiError) || apiError.Code != 40003 || apiError.Operation != "template_send" {
		t.Fatalf("send error = %v", err)
	}
	if !strings.Contains(err.Error(), "errcode=40003") {
		t.Fatalf("error text lost the errcode: %v", err)
	}

	// The token endpoint reports errcodes the same way.
	upstream.pendingErrCodes = nil
	upstream.tokenErrCode = 40125
	upstream.tokens = nil
	fresh := NewWeChatMPClient(db, cipher,
		WithWeChatHTTPClient(upstream.server.Client()),
		WithWeChatBaseURL(upstream.server.URL))
	err = fresh.SendWeChatTemplate(ctx, "open-1", "tmpl-1", "", map[string]string{"first": "x"})
	if !errors.As(err, &apiError) || apiError.Code != 40125 || apiError.Operation != "stable_token" {
		t.Fatalf("token error = %v", err)
	}
}

func TestWeChatNonOKStatusIsAnError(t *testing.T) {
	client, db, upstream, _, cipher := wechatFixture(t, "wechat-status")
	seedWeChatConfig(t, db, cipher, "mp-1", "wx-app-1", nil, weChatConfigStatusActive)
	seedTemplateID(t, db, "tmpl-1")
	ctx := context.Background()

	upstream.sendStatus = http.StatusInternalServerError
	err := client.SendWeChatTemplate(ctx, "open-1", "tmpl-1", "", map[string]string{"first": "x"})
	if err == nil || !strings.Contains(err.Error(), "upstream status 500") {
		t.Fatalf("non-200 send err = %v", err)
	}
	var apiError *WeChatAPIError
	if errors.As(err, &apiError) {
		t.Fatalf("a non-200 status must not be reported as a WeChat errcode: %v", err)
	}

	upstream.sendStatus = 0
	upstream.tokenStatus = http.StatusBadGateway
	fresh := NewWeChatMPClient(db, cipher,
		WithWeChatHTTPClient(upstream.server.Client()),
		WithWeChatBaseURL(upstream.server.URL))
	if err := fresh.SendWeChatTemplate(ctx, "open-1", "tmpl-1", "", map[string]string{"first": "x"}); err == nil || !strings.Contains(err.Error(), "upstream status 502") {
		t.Fatalf("non-200 token err = %v", err)
	}
}

func TestWeChatRetriesOnceOnInvalidToken(t *testing.T) {
	client, db, upstream, _, cipher := wechatFixture(t, "wechat-retry")
	seedWeChatConfig(t, db, cipher, "mp-1", "wx-app-1", nil, weChatConfigStatusActive)
	seedTemplateID(t, db, "tmpl-1")
	upstream.pendingErrCodes = []int{40001}
	ctx := context.Background()

	if err := client.SendWeChatTemplate(ctx, "open-1", "tmpl-1", "", map[string]string{"first": "x"}); err != nil {
		t.Fatalf("retry should have succeeded: %v", err)
	}
	if upstream.tokenCalls != 2 || upstream.sendCalls != 2 {
		t.Fatalf("token calls=%d send calls=%d, want one retry each", upstream.tokenCalls, upstream.sendCalls)
	}
	if upstream.lastAccessToken != "token-2" {
		t.Fatalf("retry used %q, want the refreshed token", upstream.lastAccessToken)
	}
}

func TestWeChatTemplatePayload(t *testing.T) {
	client, db, upstream, _, cipher := wechatFixture(t, "wechat-payload")
	seedWeChatConfig(t, db, cipher, "mp-1", "wx-app-1", nil, weChatConfigStatusActive)
	seedTemplateID(t, db, "tmpl-1")
	ctx := context.Background()

	if err := client.SendWeChatTemplate(ctx, "open-9", "tmpl-1", "https://shop.example.com/order/1",
		map[string]string{"first": "标题", "keyword1": "正文"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	payload := upstream.lastTemplate
	if payload["touser"] != "open-9" || payload["template_id"] != "tmpl-1" || payload["url"] != "https://shop.example.com/order/1" {
		t.Fatalf("payload = %v", payload)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %v", payload["data"])
	}
	for key, want := range map[string]string{"first": "标题", "keyword1": "正文"} {
		entry, ok := data[key].(map[string]any)
		if !ok || entry["value"] != want {
			t.Fatalf("data[%s] = %v, want {value:%q}", key, data[key], want)
		}
	}
	// url is omitted when empty, as WeChat treats an empty string as a link.
	upstream.lastTemplate = nil
	if err := client.SendWeChatTemplate(ctx, "open-9", "tmpl-1", "  ", map[string]string{"first": "x"}); err != nil {
		t.Fatalf("send without url: %v", err)
	}
	if _, present := upstream.lastTemplate["url"]; present {
		t.Fatalf("empty url should be omitted: %v", upstream.lastTemplate)
	}
}

func TestWeChatDefaultConfigSelection(t *testing.T) {
	client, db, upstream, _, cipher := wechatFixture(t, "wechat-select")
	seedTemplateID(t, db, "tmpl-1")
	merchant := "merchant-1"
	// Only a merchant scoped config: it is used when no platform one exists.
	seedWeChatConfig(t, db, cipher, "mp-tenant", "wx-tenant", &merchant, weChatConfigStatusActive)
	ctx := context.Background()
	if err := client.SendWeChatTemplate(ctx, "open-1", "tmpl-1", "", map[string]string{"first": "x"}); err != nil {
		t.Fatalf("send through the tenant config: %v", err)
	}
	if upstream.tokenCalls != 1 {
		t.Fatalf("token calls = %d", upstream.tokenCalls)
	}
	// A platform config takes precedence for platform notifications.
	upstream.mutex.Lock()
	upstream.tokenCalls = 0
	upstream.mutex.Unlock()
	seedWeChatConfig(t, db, cipher, "mp-platform", "wx-platform", nil, weChatConfigStatusActive)
	other := NewWeChatMPClient(db, cipher,
		WithWeChatHTTPClient(upstream.server.Client()),
		WithWeChatBaseURL(upstream.server.URL))
	if err := other.SendWeChatTemplate(ctx, "open-1", "tmpl-1", "", map[string]string{"first": "x"}); err != nil {
		t.Fatalf("send through the platform config: %v", err)
	}
	// Both configs would call the token endpoint; the platform one is chosen first
	// so the request goes out with its app id (different token cache key).
	if upstream.tokenCalls != 1 {
		t.Fatalf("token calls for the platform config = %d", upstream.tokenCalls)
	}

	// A disabled config is never picked.
	upstream.mutex.Lock()
	upstream.tokenCalls = 0
	upstream.mutex.Unlock()
	if err := db.Model(&WeChatMPConfig{}).Where("1 = 1").Update("status", "disabled").Error; err != nil {
		t.Fatalf("disable configs: %v", err)
	}
	if err := other.SendWeChatTemplate(ctx, "open-1", "tmpl-1", "", map[string]string{"first": "x"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("disabled config err = %v, want ErrNotConfigured", err)
	}
}

func TestWeChatConfigSecretUsesRowAAD(t *testing.T) {
	client, db, _, _, cipher := wechatFixture(t, "wechat-aad")
	seedWeChatConfig(t, db, cipher, "mp-1", "wx-app-1", nil, weChatConfigStatusActive)
	// A ciphertext written for another row must not decrypt.
	foreign, err := cipher.Encrypt([]byte("secret"), []byte("wechat-mp:another"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := db.Create(&WeChatMPConfig{ID: "mp-2", AppID: "wx-app-2", AppSecretEncrypted: foreign, Status: weChatConfigStatusActive}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Model(&WeChatMPConfig{}).Where("id = ?", "mp-1").Update("status", "disabled").Error; err != nil {
		t.Fatalf("disable: %v", err)
	}
	err = client.SendWeChatTemplate(context.Background(), "open-1", "tmpl-1", "", map[string]string{"first": "x"})
	if err == nil || !strings.Contains(err.Error(), "decrypt") {
		t.Fatalf("foreign AAD err = %v", err)
	}
}
