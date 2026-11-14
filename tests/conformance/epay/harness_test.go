// Package epayconformance drives the real GorixPay application over HTTP and
// asserts that its behaviour matches the upstream Epay PHP source. Every
// assertion in this package cites the upstream file and line it was derived
// from; fixture-only tests are not enough, because the protocol is defined by
// what a running server returns.
package epayconformance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/compat/epay/signing"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
	"gorixpay/internal/platform/app"
	"gorixpay/internal/platform/config"
)

// environment is one fully booted payment platform plus the credentials a
// merchant would hold after registering on it.
type environment struct {
	t   *testing.T
	app *app.App
	raw *httptest.Server
	url string

	adminToken string
	adminID    string

	merchantToken      string
	merchantID         string
	pid                int64
	md5Key             string
	rsaPrivatePEM      string
	platformPublicPEM  string
	alipayChannelID    string
	cashierChannelID   string
	walletChannelID    string
	merchantNotifyBase string
}

func newEnvironment(t *testing.T) *environment {
	t.Helper()
	name := "conformance-" + randomSuffix(t)
	keyPath := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x24}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Environment: "test",
		HTTP:        config.HTTPConfig{Address: ":0", PublicURL: "http://pay.example"},
		Database:    config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"},
		// The upstream fixture and the merchant receiver both live on loopback,
		// which the outbound guard blocks by default; a conformance harness
		// deliberately uses a local receiver.
		Security: config.SecurityConfig{MasterKeyFile: keyPath, AllowPrivateNotifyHosts: true},
		Sandbox:  config.SandboxConfig{Enabled: true},
	}
	application, err := app.New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("boot application: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	server := httptest.NewServer(application.Handler())
	t.Cleanup(server.Close)

	env := &environment{t: t, app: application, raw: server, url: server.URL}
	env.bootstrapAdministrator()
	env.bootstrapMerchant()
	return env
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 6)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(buffer)
}

// bootstrapAdministrator seeds the first operator directly in the database, the
// same way the installer does, and then logs in through the public API.
func (env *environment) bootstrapAdministrator() {
	env.t.Helper()
	now := time.Now().UTC()
	hash, err := password.Hash("admin12345")
	if err != nil {
		env.t.Fatal(err)
	}
	administrator := identitymodel.Administrator{
		ID: "00000000-0000-0000-0000-00000000admn", Username: "admin", PasswordHash: hash,
		DisplayName: "Admin", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := env.app.DB().Create(&administrator).Error; err != nil {
		env.t.Fatal(err)
	}
	env.adminID = administrator.ID
	status, body := env.doJSON(http.MethodPost, "/api/v1/auth/administrator/login",
		map[string]string{"identifier": "admin", "password": "admin12345"}, "")
	if status != http.StatusOK {
		env.t.Fatalf("admin login = %d %v", status, body)
	}
	env.adminToken = mustString(env.t, body["token"])
	// A cashier channel serves submit.php calls that omit type; a per-type
	// sandbox channel serves mapi.php and explicit type=alipay orders.
	env.alipayChannelID = env.createChannel("sandbox-alipay", "alipay")
	env.cashierChannelID = env.createChannel("sandbox-cashier", "cashier")
	env.walletChannelID = env.createChannel("sandbox-wallet", "wxpay")
}

func (env *environment) createChannel(name, paymentType string) string {
	env.t.Helper()
	status, body := env.doJSON(http.MethodPost, "/api/v1/admin/channels", map[string]any{
		"name": name, "plugin_code": "sandbox", "payment_type": paymentType,
		"priority": 1, "weight": 1, "status": "active",
	}, env.adminToken)
	if status != http.StatusCreated {
		env.t.Fatalf("create channel %s = %d %v", name, status, body)
	}
	return mustString(env.t, body["id"])
}

func (env *environment) bootstrapMerchant() {
	env.t.Helper()
	status, body := env.doJSON(http.MethodPost, "/api/v1/auth/register", map[string]string{
		"identifier": "conformance@example.com", "password": "conformance-pass",
	}, "")
	if status != http.StatusCreated {
		env.t.Fatalf("register merchant = %d %v", status, body)
	}
	env.merchantToken = mustString(env.t, body["token"])
	merchant, ok := body["merchant"].(map[string]any)
	if !ok {
		env.t.Fatalf("merchant block missing: %v", body)
	}
	env.merchantID = mustString(env.t, merchant["id"])
	env.pid = mustInt(env.t, merchant["legacy_uid"])
	env.merchantNotifyBase = "https://merchant.example"

	status, rotated := env.doJSON(http.MethodPost, "/api/v1/merchant/credentials/md5/rotate", nil, env.merchantToken)
	if status != http.StatusCreated {
		env.t.Fatalf("rotate md5 = %d %v", status, rotated)
	}
	env.md5Key = mustString(env.t, rotated["secret"])

	status, keys := env.doJSON(http.MethodPost, "/api/v1/merchant/credentials/v2/rotate", nil, env.merchantToken)
	if status != http.StatusCreated {
		env.t.Fatalf("rotate rsa = %d %v", status, keys)
	}
	env.rsaPrivatePEM = mustString(env.t, keys["private_key"])
	env.platformPublicPEM = mustString(env.t, keys["platform_public_key"])
}

func (env *environment) do(method, path string, values url.Values, token string) (int, []byte) {
	env.t.Helper()
	var body io.Reader
	if values != nil {
		body = strings.NewReader(values.Encode())
	}
	request, err := http.NewRequest(method, env.url+path, body)
	if err != nil {
		env.t.Fatal(err)
	}
	if values != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		env.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		env.t.Fatal(err)
	}
	return response.StatusCode, payload
}

func (env *environment) doJSON(method, path string, payload any, token string) (int, map[string]any) {
	env.t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			env.t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, env.url+path, body)
	if err != nil {
		env.t.Fatal(err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		env.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		env.t.Fatal(err)
	}
	decoded := map[string]any{}
	if len(raw) > 0 && raw[0] == '{' {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			env.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return response.StatusCode, decoded
}

// form posts an application/x-www-form-urlencoded body and decodes JSON.
func (env *environment) form(path string, values url.Values) (int, map[string]any) {
	env.t.Helper()
	status, raw := env.do(http.MethodPost, path, values, "")
	return status, decodeJSON(env.t, raw)
}

// get performs a GET with query parameters and decodes JSON.
func (env *environment) get(path string, values url.Values) (int, map[string]any) {
	env.t.Helper()
	target := path
	if len(values) > 0 {
		target += "?" + values.Encode()
	}
	status, raw := env.do(http.MethodGet, target, nil, "")
	return status, decodeJSON(env.t, raw)
}

func decodeJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	decoded := map[string]any{}
	if len(raw) == 0 {
		return decoded
	}
	if raw[0] != '{' {
		t.Fatalf("expected a JSON object, got %q", raw)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return decoded
}

// signedValues returns a V1 parameter set signed with the merchant MD5 key the
// way every 易支付 SDK does it: MD5(ksort(params) + key).
func (env *environment) signedValues(id string, extra map[string]string) url.Values {
	env.t.Helper()
	values := url.Values{
		"pid":          {strconv.FormatInt(env.pid, 10)},
		"out_trade_no": {id},
		"notify_url":   {env.merchantNotifyBase + "/notify"},
		"return_url":   {env.merchantNotifyBase + "/return"},
		"name":         {"兼容性测试商品"},
		"money":        {"10.00"},
		"clientip":     {"127.0.0.1"},
	}
	for key, value := range extra {
		values.Set(key, value)
	}
	signV1(env.t, values, env.md5Key)
	return values
}

// signV1 signs url.Values in place, matching Epay/includes/lib/Payment.php:8.
func signV1(t *testing.T, values url.Values, key string) {
	t.Helper()
	delete(values, "sign")
	params := make(map[string]any, len(values))
	for name, entries := range values {
		if len(entries) > 0 {
			params[name] = entries[0]
		}
	}
	values.Set("sign", signing.SignMD5(params, key))
}

// paySandboxOrder completes a sandbox payment so refund and query tests operate
// on a paid order, mirroring what the cashier page does in a browser.
func (env *environment) paySandboxOrder(tradeNo string) {
	env.t.Helper()
	status, body := env.doJSON(http.MethodPost, "/api/v1/sandbox/orders/"+tradeNo+"/complete", nil, "")
	if status != http.StatusOK {
		env.t.Fatalf("sandbox complete %s = %d %v", tradeNo, status, body)
	}
	if body["status"] != "paid" {
		env.t.Fatalf("sandbox complete %s status = %v", tradeNo, body["status"])
	}
}

// createPaidOrder runs mapi.php and pays the returned order through the sandbox
// channel, returning the platform trade number.
func (env *environment) createPaidOrder(id string) string {
	env.t.Helper()
	status, body := env.form("/mapi.php", env.signedValues(id, map[string]string{"type": "alipay"}))
	if status != http.StatusOK {
		env.t.Fatalf("mapi %s = %d %v", id, status, body)
	}
	if code := intOf(env.t, body["code"]); code != 1 {
		env.t.Fatalf("mapi %s code = %d (%v)", id, code, body)
	}
	tradeNo := mustString(env.t, body["trade_no"])
	env.paySandboxOrder(tradeNo)
	return tradeNo
}

func mustString(t *testing.T, value any) string {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("not a string: %#v", value)
	}
	return text
}

func mustInt(t *testing.T, value any) int64 {
	t.Helper()
	return int64(intOf(t, value))
}

func intOf(t *testing.T, value any) int {
	t.Helper()
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			t.Fatalf("number %v: %v", typed, err)
		}
		return int(parsed)
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			t.Fatalf("number string %q: %v", typed, err)
		}
		return parsed
	default:
		t.Fatalf("not a number: %#v", value)
		return 0
	}
}

// requireKeys asserts that a response carries every upstream field. Extra
// fields are tolerated: clients ignore them, but a missing field breaks SDKs.
func requireKeys(t *testing.T, body map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := body[key]; !ok {
			t.Errorf("response missing %q: %v", key, body)
		}
	}
}

// forbidKeys asserts that fields a client must not receive are absent.
func forbidKeys(t *testing.T, body map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := body[key]; ok {
			t.Errorf("response unexpectedly contains %q: %v", key, body)
		}
	}
}

func TestHarnessBootsRealApplication(t *testing.T) {
	env := newEnvironment(t)
	if env.pid <= 0 || env.md5Key == "" || env.rsaPrivatePEM == "" {
		t.Fatalf("merchant bootstrap incomplete: pid=%d", env.pid)
	}
	status, site := env.doJSON(http.MethodGet, "/api/v1/public/site", nil, "")
	if status != http.StatusOK {
		t.Fatalf("public site = %d %v", status, site)
	}
	if env.alipayChannelID == "" || env.cashierChannelID == "" {
		t.Fatal("sandbox channels not created")
	}
}
