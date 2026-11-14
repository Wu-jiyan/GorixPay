package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type httpFixture struct {
	router        *gin.Engine
	db            *gorm.DB
	sessions      *session.Service
	clock         *clock
	logs          *[]string
	adminToken    string
	merchantToken string
	wechat        *fakeWeChatUpstream
}

func newHTTPFixture(t *testing.T, name string) httpFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, cleanup, err := openFixtureDB(name)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(cleanup)
	if err := db.AutoMigrate(&Channel{}, &VerifyCode{}, &PasswordReset{}, &merchantRecord{}, &identitymodel.Session{}, &WeChatMPConfig{}, &settingRow{}); err != nil {
		t.Fatalf("migrate fixture: %v", err)
	}
	sessions := session.NewService(db, time.Now)
	source := &clock{at: fixtureStart}
	logs := []string{}
	upstream := newFakeWeChat(t)
	router := gin.New()
	Register(router.Group("/api/v1"), db, sessions, testCipher(t),
		WithClock(source.Now),
		WithSender(&captureSender{}),
		WithTokenLogging(true),
		WithWeChatTransport(upstream.server.Client(), upstream.server.URL),
		WithLogger(func(format string, arguments ...any) {
			logs = append(logs, fmt.Sprintf(format, arguments...))
		}),
	)
	adminToken, _, err := sessions.Create(context.Background(), identitymodel.PrincipalAdministrator, "admin-1", time.Hour, "", "")
	if err != nil {
		t.Fatalf("admin session: %v", err)
	}
	merchantToken, _, err := sessions.Create(context.Background(), identitymodel.PrincipalMerchant, "merchant-1", time.Hour, "", "")
	if err != nil {
		t.Fatalf("merchant session: %v", err)
	}
	return httpFixture{
		router: router, db: db, sessions: sessions, clock: source, logs: &logs,
		adminToken: adminToken, merchantToken: merchantToken, wechat: upstream,
	}
}

func call(t *testing.T, router *gin.Engine, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(payload)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decode(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return payload
}

func TestPublicRoutesNeedNoSession(t *testing.T) {
	fixture := newHTTPFixture(t, "notify-http-public")
	seedMerchant(t, fixture.db, merchantSeed{ID: "merchant-1", Email: "shop@example.com", Password: "old-password"})

	recorder := call(t, fixture.router, http.MethodPost, "/api/v1/auth/code", "", map[string]any{
		"scene": "reset_password", "target": "13800000001",
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("code without session = %d %s", recorder.Code, recorder.Body.String())
	}
	if payload := decode(t, recorder); payload["status"] != "sent" || payload["expires_in"] == nil {
		t.Fatalf("code payload = %v", payload)
	}
	if strings.Contains(recorder.Body.String(), "验证码") {
		t.Fatalf("response leaked the message body: %s", recorder.Body.String())
	}

	recorder = call(t, fixture.router, http.MethodPost, "/api/v1/auth/code", "", map[string]any{
		"scene": "reset_password", "target": "13800000001",
	})
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second code = %d %s", recorder.Code, recorder.Body.String())
	}
	if header := recorder.Header().Get("Retry-After"); header != "60" {
		t.Fatalf("Retry-After = %q, want 60", header)
	}

	recorder = call(t, fixture.router, http.MethodPost, "/api/v1/auth/merchant/password/forgot", "", map[string]any{"identifier": "shop@example.com"})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("forgot without session = %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = call(t, fixture.router, http.MethodPost, "/api/v1/auth/merchant/password/reset", "", map[string]any{
		"identifier": "shop@example.com", "token": "not-a-token", "password": "brand-new-password",
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("bad token = %d %s", recorder.Code, recorder.Body.String())
	}
	if code := decode(t, recorder)["code"]; code != "invalid_token" {
		t.Fatalf("bad token code = %v", code)
	}
}

func TestForgotAndResetOverHTTP(t *testing.T) {
	fixture := newHTTPFixture(t, "notify-http-reset")
	seedMerchant(t, fixture.db, merchantSeed{ID: "merchant-1", Email: "shop@example.com", Password: "old-password"})
	seedSession(t, fixture.db, "session-1", "merchant-1", fixture.clock.at)
	seedSession(t, fixture.db, "session-2", "merchant-1", fixture.clock.at)

	recorder := call(t, fixture.router, http.MethodPost, "/api/v1/auth/merchant/password/forgot", "", map[string]any{"identifier": "shop@example.com"})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("forgot = %d %s", recorder.Code, recorder.Body.String())
	}
	token := extractToken(t, *fixture.logs)
	if strings.Contains(recorder.Body.String(), token) {
		t.Fatalf("response leaked the reset token: %s", recorder.Body.String())
	}
	if payload := decode(t, recorder); payload["status"] != "accepted" || payload["token"] != nil {
		t.Fatalf("forgot payload = %v", payload)
	}

	// Unknown identifiers look identical to known ones.
	recorder = call(t, fixture.router, http.MethodPost, "/api/v1/auth/merchant/password/forgot", "", map[string]any{"identifier": "nobody@example.com"})
	if recorder.Code != http.StatusAccepted || decode(t, recorder)["status"] != "accepted" {
		t.Fatalf("unknown identifier = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = call(t, fixture.router, http.MethodPost, "/api/v1/auth/merchant/password/reset", "", map[string]any{
		"identifier": "shop@example.com", "token": token, "password": "brand-new-password",
	})
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", recorder.Code, recorder.Body.String())
	}
	if live := liveSessions(t, fixture.db, "merchant-1"); live != 0 {
		t.Fatalf("live sessions after reset = %d, want 0", live)
	}
	hash := storedPasswordHash(t, fixture.db, "merchant-1")
	if matched, _, _ := password.Verify("old-password", hash); matched {
		t.Fatal("old password still works")
	}
	if matched, _, _ := password.Verify("brand-new-password", hash); !matched {
		t.Fatal("new password does not work")
	}

	// The token is one-time.
	recorder = call(t, fixture.router, http.MethodPost, "/api/v1/auth/merchant/password/reset", "", map[string]any{
		"identifier": "shop@example.com", "token": token, "password": "another-password",
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("replayed token = %d %s", recorder.Code, recorder.Body.String())
	}

	// Short passwords are rejected with a dedicated code.
	fixture.clock.Advance(2 * time.Minute)
	recorder = call(t, fixture.router, http.MethodPost, "/api/v1/auth/merchant/password/forgot", "", map[string]any{"identifier": "shop@example.com"})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("second forgot = %d", recorder.Code)
	}
	freshToken := extractToken(t, (*fixture.logs)[len(*fixture.logs)-1:])
	recorder = call(t, fixture.router, http.MethodPost, "/api/v1/auth/merchant/password/reset", "", map[string]any{
		"identifier": "shop@example.com", "token": freshToken, "password": "short",
	})
	if recorder.Code != http.StatusBadRequest || decode(t, recorder)["code"] != "invalid_password" {
		t.Fatalf("short password = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestChangePasswordOverHTTP(t *testing.T) {
	fixture := newHTTPFixture(t, "notify-http-change")
	seedMerchant(t, fixture.db, merchantSeed{ID: "merchant-1", Email: "shop@example.com", Password: "old-password"})

	if recorder := call(t, fixture.router, http.MethodPost, "/api/v1/merchant/password/change", "", map[string]any{
		"old_password": "old-password", "new_password": "new-password-1",
	}); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("without session = %d, want 401", recorder.Code)
	}
	// An administrator session is not a merchant session.
	if recorder := call(t, fixture.router, http.MethodPost, "/api/v1/merchant/password/change", fixture.adminToken, map[string]any{
		"old_password": "old-password", "new_password": "new-password-1",
	}); recorder.Code != http.StatusForbidden {
		t.Fatalf("administrator on merchant route = %d, want 403", recorder.Code)
	}
	if recorder := call(t, fixture.router, http.MethodPost, "/api/v1/merchant/password/change", fixture.merchantToken, map[string]any{
		"old_password": "wrong-password", "new_password": "new-password-1",
	}); recorder.Code != http.StatusBadRequest || decode(t, recorder)["code"] != "invalid_old_password" {
		t.Fatalf("wrong old password = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := call(t, fixture.router, http.MethodPost, "/api/v1/merchant/password/change", fixture.merchantToken, map[string]any{
		"old_password": "old-password", "new_password": "new-password-1",
	}); recorder.Code != http.StatusNoContent {
		t.Fatalf("change = %d %s", recorder.Code, recorder.Body.String())
	}
	hash := storedPasswordHash(t, fixture.db, "merchant-1")
	if matched, _, _ := password.Verify("old-password", hash); matched {
		t.Fatal("old password still works after the change")
	}
	if matched, _, _ := password.Verify("new-password-1", hash); !matched {
		t.Fatal("new password does not work")
	}
}

func TestChannelAdminRoutes(t *testing.T) {
	fixture := newHTTPFixture(t, "notify-http-channels")

	if recorder := call(t, fixture.router, http.MethodGet, "/api/v1/admin/notification-channels", "", nil); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", recorder.Code)
	}
	if recorder := call(t, fixture.router, http.MethodGet, "/api/v1/admin/notification-channels", fixture.merchantToken, nil); recorder.Code != http.StatusForbidden {
		t.Fatalf("merchant list = %d, want 403", recorder.Code)
	}

	recorder := call(t, fixture.router, http.MethodPost, "/api/v1/admin/notification-channels", fixture.adminToken, map[string]any{
		"kind": "sms", "provider": "aliyun-sms", "status": "active",
		"config": map[string]string{"access_key": "AK-secret-value", "sign_name": "GorixPay"},
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "AK-secret-value") {
		t.Fatalf("create echoed the secret: %s", recorder.Body.String())
	}
	created := decode(t, recorder)
	channelID, _ := created["id"].(string)
	if channelID == "" || created["kind"] != "sms" || created["provider"] != "aliyun-sms" {
		t.Fatalf("created = %v", created)
	}
	if keys, ok := created["config_keys"].([]any); !ok || len(keys) != 2 {
		t.Fatalf("config_keys = %v", created["config_keys"])
	}

	recorder = call(t, fixture.router, http.MethodGet, "/api/v1/admin/notification-channels", fixture.adminToken, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list = %d %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "AK-secret-value") || strings.Contains(recorder.Body.String(), `"config"`) {
		t.Fatalf("list leaked config: %s", recorder.Body.String())
	}
	listing := decode(t, recorder)
	if listing["total"] != float64(1) || listing["page"] != float64(1) || listing["page_size"] != float64(20) {
		t.Fatalf("listing = %v", listing)
	}

	// A status flip without config keeps the stored secret.
	recorder = call(t, fixture.router, http.MethodPut, "/api/v1/admin/notification-channels/"+channelID, fixture.adminToken, map[string]any{
		"kind": "sms", "provider": "aliyun-sms", "status": "disabled",
	})
	if recorder.Code != http.StatusOK || decode(t, recorder)["status"] != "disabled" {
		t.Fatalf("update = %d %s", recorder.Code, recorder.Body.String())
	}
	var stored Channel
	if err := fixture.db.First(&stored, "id = ?", channelID).Error; err != nil {
		t.Fatalf("reread channel: %v", err)
	}
	plain, _, err := newTestCipher(t, 0x2a).Decrypt(stored.ConfigEncrypted, []byte(channelContextPrefix+channelID))
	if err != nil || !strings.Contains(string(plain), "AK-secret-value") {
		t.Fatalf("stored config was lost: %s (%v)", plain, err)
	}

	recorder = call(t, fixture.router, http.MethodPost, "/api/v1/admin/notification-channels/"+channelID+"/test", fixture.adminToken, nil)
	if recorder.Code != http.StatusOK || decode(t, recorder)["ok"] != true {
		t.Fatalf("test = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = call(t, fixture.router, http.MethodPost, "/api/v1/admin/notification-channels", fixture.adminToken, map[string]any{
		"kind": "push", "provider": "x",
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid kind = %d %s", recorder.Code, recorder.Body.String())
	}

	if recorder = call(t, fixture.router, http.MethodDelete, "/api/v1/admin/notification-channels/"+channelID, fixture.adminToken, nil); recorder.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder = call(t, fixture.router, http.MethodDelete, "/api/v1/admin/notification-channels/"+channelID, fixture.adminToken, nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d", recorder.Code)
	}
}

// TestRegisterWithoutCipherKeepsPasswordFlows mirrors the deployment rule: no
// master key means no encrypted feature, but a merchant must still be able to
// recover the password.
func TestRegisterWithoutCipherKeepsPasswordFlows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    "file:notify-http-nocipher?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&Channel{}, &VerifyCode{}, &PasswordReset{}, &merchantRecord{}, &identitymodel.Session{}); err != nil {
		t.Fatalf("migrate fixture: %v", err)
	}
	seedMerchant(t, db, merchantSeed{ID: "merchant-1", Email: "shop@example.com", Password: "old-password"})
	sessions := session.NewService(db, time.Now)
	adminToken, _, err := sessions.Create(context.Background(), identitymodel.PrincipalAdministrator, "admin-1", time.Hour, "", "")
	if err != nil {
		t.Fatalf("admin session: %v", err)
	}
	source := &clock{at: fixtureStart}
	router := gin.New()
	Register(router.Group("/api/v1"), db, sessions, nil,
		WithClock(source.Now),
		WithSender(&captureSender{}),
		WithLogger(func(string, ...any) {}),
	)

	if recorder := call(t, router, http.MethodPost, "/api/v1/auth/code", "", map[string]any{
		"scene": "reset_password", "target": "13800000002",
	}); recorder.Code != http.StatusAccepted {
		t.Fatalf("code without cipher = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := call(t, router, http.MethodPost, "/api/v1/auth/merchant/password/forgot", "", map[string]any{
		"identifier": "shop@example.com",
	}); recorder.Code != http.StatusAccepted {
		t.Fatalf("forgot without cipher = %d %s", recorder.Code, recorder.Body.String())
	}
	recorder := call(t, router, http.MethodGet, "/api/v1/admin/notification-channels", adminToken, nil)
	if recorder.Code != http.StatusServiceUnavailable || decode(t, recorder)["code"] != "cipher_unavailable" {
		t.Fatalf("channel list without cipher = %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestRegisterWithoutDependenciesMountsNothing keeps a half wired module from
// exposing unauthenticated routes.
func TestRegisterWithoutDependenciesMountsNothing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router.Group("/api/v1"), nil, nil, nil)
	if recorder := call(t, router, http.MethodPost, "/api/v1/auth/code", "", map[string]any{"scene": "reset_password", "target": "13800000001"}); recorder.Code != http.StatusNotFound {
		t.Fatalf("nil dependencies registered routes: %d", recorder.Code)
	}
}

// TestWeChatTestMessageEndpoint covers the administrator test button: it must
// surface a missing template as 400 and a WeChat rejection as 502 with the errcode.
func TestWeChatTestMessageEndpoint(t *testing.T) {
	fixture := newHTTPFixture(t, "notify-http-wechat")
	seedWeChatConfig(t, fixture.db, testCipher(t), "mp-1", "wx-app-1", nil, weChatConfigStatusActive)
	path := "/api/v1/admin/wechat-mp-configs/mp-1/test-message"

	if recorder := call(t, fixture.router, http.MethodPost, path, "", map[string]any{"openid": "open-1"}); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous test message = %d, want 401", recorder.Code)
	}
	if recorder := call(t, fixture.router, http.MethodPost, path, fixture.merchantToken, map[string]any{"openid": "open-1"}); recorder.Code != http.StatusForbidden {
		t.Fatalf("merchant test message = %d, want 403", recorder.Code)
	}
	// No template id configured yet.
	recorder := call(t, fixture.router, http.MethodPost, path, fixture.adminToken, map[string]any{"openid": "open-1"})
	if recorder.Code != http.StatusBadRequest || decode(t, recorder)["code"] != "template_not_configured" {
		t.Fatalf("missing template = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := call(t, fixture.router, http.MethodPost, path, fixture.adminToken, map[string]any{}); recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing openid = %d, want 400", recorder.Code)
	}

	seedTemplateID(t, fixture.db, "tmpl-1")
	recorder = call(t, fixture.router, http.MethodPost, path, fixture.adminToken, map[string]any{"openid": "open-1"})
	if recorder.Code != http.StatusOK || decode(t, recorder)["ok"] != true {
		t.Fatalf("test message = %d %s", recorder.Code, recorder.Body.String())
	}
	if fixture.wechat.lastTemplate["touser"] != "open-1" {
		t.Fatalf("touser = %v", fixture.wechat.lastTemplate["touser"])
	}

	// A WeChat errcode is reported verbatim with 502.
	fixture.wechat.mutex.Lock()
	fixture.wechat.pendingErrCodes = []int{40003}
	fixture.wechat.mutex.Unlock()
	recorder = call(t, fixture.router, http.MethodPost, path, fixture.adminToken, map[string]any{"openid": "open-1"})
	payload := decode(t, recorder)
	if recorder.Code != http.StatusBadGateway || payload["code"] != "wechat_upstream_error" || payload["errcode"] != float64(40003) {
		t.Fatalf("upstream error = %d %s", recorder.Code, recorder.Body.String())
	}

	if recorder := call(t, fixture.router, http.MethodPost, "/api/v1/admin/wechat-mp-configs/missing/test-message", fixture.adminToken, map[string]any{"openid": "open-1"}); recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown config = %d, want 404", recorder.Code)
	}
}
