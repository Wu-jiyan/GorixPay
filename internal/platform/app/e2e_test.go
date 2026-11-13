package app

import (
	"bytes"
	"context"
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
	ledgermodel "gorixpay/internal/ledger/model"
	"gorixpay/internal/outbox"
	"gorixpay/internal/platform/config"
)

type e2eClient struct {
	t     *testing.T
	base  string
	token string
}

func newE2EApplication(t *testing.T) (*e2eClient, *App) {
	return newE2EApplicationNamed(t, "app-e2e")
}

func newE2EApplicationNamed(t *testing.T, name string) (*e2eClient, *App) {
	t.Helper()
	keyPath := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x24}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Environment: "test",
		HTTP:        config.HTTPConfig{Address: ":0", PublicURL: "http://pay.example"},
		Database:    config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"},
		Security:    config.SecurityConfig{MasterKeyFile: keyPath},
		Sandbox:     config.SandboxConfig{Enabled: true},
	}
	application, err := New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	server := httptest.NewServer(application.Handler())
	t.Cleanup(server.Close)
	return &e2eClient{t: t, base: server.URL}, application
}

func (client *e2eClient) request(method, path, contentType string, body io.Reader) (int, []byte) {
	client.t.Helper()
	request, err := http.NewRequest(method, client.base+path, body)
	if err != nil {
		client.t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if client.token != "" {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		client.t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		client.t.Fatal(err)
	}
	return response.StatusCode, payload
}

func decodeObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	decoded := map[string]any{}
	if len(raw) == 0 {
		return decoded
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("response is not a JSON object: %s", raw)
	}
	return decoded
}

func (client *e2eClient) json(method, path string, payload any) (int, map[string]any) {
	client.t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			client.t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	status, raw := client.request(method, path, "application/json", body)
	if status >= 400 && len(raw) > 0 && raw[0] != '{' {
		client.t.Fatalf("%s %s failed: %d %s", method, path, status, raw)
	}
	return status, decodeObject(client.t, raw)
}

func (client *e2eClient) form(path string, values url.Values) (int, map[string]any) {
	client.t.Helper()
	status, raw := client.request(http.MethodPost, path, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	return status, decodeObject(client.t, raw)
}

func (client *e2eClient) rawForm(path string, values url.Values) (int, string) {
	client.t.Helper()
	status, raw := client.request(http.MethodPost, path, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	return status, string(raw)
}

func jsonInt(t *testing.T, value any) int64 {
	t.Helper()
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			t.Fatalf("number %v: %v", typed, err)
		}
		return parsed
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			t.Fatalf("number string %q: %v", typed, err)
		}
		return parsed
	default:
		t.Fatalf("not a number: %#v", value)
		return 0
	}
}

func jsonString(t *testing.T, value any) string {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("not a string: %#v", value)
	}
	return text
}

func TestEndToEndSandboxPaymentAndEpayProtocols(t *testing.T) {
	client, application := newE2EApplication(t)

	// 1. Administrator bootstrap + login.
	now := time.Now().UTC()
	hash, err := password.Hash("admin12345")
	if err != nil {
		t.Fatal(err)
	}
	administrator := identitymodel.Administrator{
		ID: "00000000-0000-0000-0000-00000000admn", Username: "admin", PasswordHash: hash,
		DisplayName: "Admin", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := application.DB().Create(&administrator).Error; err != nil {
		t.Fatal(err)
	}
	status, adminLogin := client.json(http.MethodPost, "/api/v1/auth/administrator/login", map[string]string{
		"identifier": "admin", "password": "admin12345",
	})
	if status != http.StatusOK {
		t.Fatalf("admin login = %d %v", status, adminLogin)
	}
	adminToken := jsonString(t, adminLogin["token"])
	if adminToken == "" {
		t.Fatalf("admin token missing: %v", adminLogin)
	}

	// 2. Sandbox channel through the admin API.
	client.token = adminToken
	status, channel := client.json(http.MethodPost, "/api/v1/admin/channels", map[string]any{
		"name": "sandbox", "plugin_code": "sandbox", "payment_type": "alipay",
		"priority": 1, "weight": 1, "status": "active",
	})
	if status != http.StatusCreated {
		t.Fatalf("create channel = %d %v", status, channel)
	}
	channelID := jsonString(t, channel["id"])
	if channelID == "" {
		t.Fatalf("channel id missing: %v", channel)
	}

	// 3. Merchant registration + MD5 credential.
	status, register := client.json(http.MethodPost, "/api/v1/auth/register", map[string]string{
		"identifier": "e2e@example.com", "password": "e2e-pass-123",
	})
	if status != http.StatusCreated {
		t.Fatalf("register = %d %v", status, register)
	}
	merchantToken := jsonString(t, register["token"])
	merchantBlock, ok := register["merchant"].(map[string]any)
	if !ok {
		t.Fatalf("merchant block missing: %v", register)
	}
	pid := jsonInt(t, merchantBlock["legacy_uid"])
	merchantID := jsonString(t, merchantBlock["id"])
	if merchantToken == "" || pid <= 0 || merchantID == "" {
		t.Fatalf("merchant registration payload = %v", register)
	}
	client.token = merchantToken
	status, rotated := client.json(http.MethodPost, "/api/v1/merchant/credentials/md5/rotate", nil)
	if status != http.StatusCreated {
		t.Fatalf("rotate md5 = %d %v", status, rotated)
	}
	merchantKey := jsonString(t, rotated["secret"])
	if merchantKey == "" {
		t.Fatalf("md5 secret missing: %v", rotated)
	}

	// 4. Zero-amount orders are rejected before touching the channel.
	client.token = ""
	pidText := strconv.FormatInt(pid, 10)
	badValues := url.Values{
		"pid": {pidText}, "type": {"alipay"}, "out_trade_no": {"BAD-ZERO"},
		"notify_url": {"https://merchant.example/notify"}, "return_url": {"https://merchant.example/return"},
		"name": {"bad"}, "money": {"0.00"}, "clientip": {"127.0.0.1"}, "sign_type": {"MD5"},
	}
	badValues.Set("sign", signing.SignMD5(formMap(badValues), merchantKey))
	status, badOrder := client.form("/mapi.php", badValues)
	if status != http.StatusOK || jsonInt(t, badOrder["code"]) != -1 {
		t.Fatalf("zero amount order accepted: %d %v", status, badOrder)
	}

	// 5. V1 order -> cashier start -> sandbox completion.
	orderValues := url.Values{
		"pid": {pidText}, "type": {"alipay"}, "out_trade_no": {"E2E-ORDER-1"},
		"notify_url": {"https://merchant.example/notify"}, "return_url": {"https://merchant.example/return"},
		"name": {"E2E 商品"}, "money": {"10.00"}, "clientip": {"127.0.0.1"}, "sign_type": {"MD5"},
	}
	orderValues.Set("sign", signing.SignMD5(formMap(orderValues), merchantKey))
	status, orderResponse := client.form("/mapi.php", orderValues)
	if status != http.StatusOK || jsonInt(t, orderResponse["code"]) != 1 {
		t.Fatalf("create order = %d %v", status, orderResponse)
	}
	tradeNo := jsonString(t, orderResponse["trade_no"])
	if tradeNo == "" {
		t.Fatalf("trade_no missing: %v", orderResponse)
	}
	status, start := client.json(http.MethodPost, "/api/v1/cashier/orders/"+tradeNo+"/start", nil)
	if status != http.StatusOK || start["pay_type"] != "jump" {
		t.Fatalf("cashier start = %d %v", status, start)
	}
	payInfo := jsonString(t, start["pay_info"])
	if !strings.Contains(payInfo, tradeNo) {
		t.Fatalf("sandbox pay_info = %v", payInfo)
	}
	status, completed := client.json(http.MethodPost, "/api/v1/sandbox/orders/"+tradeNo+"/complete", nil)
	if status != http.StatusOK || completed["status"] != "paid" {
		t.Fatalf("sandbox complete = %d %v", status, completed)
	}

	// 6. Ledger: payment credited the merchant exactly once.
	var merchantAccount ledgermodel.Account
	if err := application.DB().Where("owner_type = ? AND owner_id = ? AND purpose = ?", "merchant", merchantID, "balance").First(&merchantAccount).Error; err != nil {
		t.Fatalf("merchant ledger account: %v", err)
	}
	var merchantBalance ledgermodel.Balance
	if err := application.DB().First(&merchantBalance, "account_id = ?", merchantAccount.ID).Error; err != nil {
		t.Fatalf("merchant ledger balance: %v", err)
	}
	if merchantBalance.Available != 1000 {
		t.Fatalf("merchant balance = %d, want 1000", merchantBalance.Available)
	}
	var outboxCount int64
	if err := application.DB().Model(&outbox.Message{}).Where("topic = ?", "merchant.payment.notify").Count(&outboxCount).Error; err != nil || outboxCount != 1 {
		t.Fatalf("outbox messages = %d err=%v", outboxCount, err)
	}

	// 7. Merchant profile/dashboard and admin console endpoints used by the SPA.
	client.token = merchantToken
	status, profile := client.json(http.MethodGet, "/api/v1/merchant/profile", nil)
	if status != http.StatusOK || jsonString(t, profile["display_name"]) == "" {
		t.Fatalf("merchant profile = %d %v", status, profile)
	}
	if jsonInt(t, profile["legacy_uid"]) != pid {
		t.Fatalf("profile legacy_uid = %v, want %d", profile["legacy_uid"], pid)
	}
	status, updatedProfile := client.json(http.MethodPut, "/api/v1/merchant/profile", map[string]string{"display_name": "E2E Merchant"})
	if status != http.StatusOK || updatedProfile["display_name"] != "E2E Merchant" {
		t.Fatalf("update profile = %d %v", status, updatedProfile)
	}
	status, dashboard := client.json(http.MethodGet, "/api/v1/merchant/dashboard", nil)
	if status != http.StatusOK || jsonInt(t, dashboard["today_paid_amount"]) < 1000 || jsonInt(t, dashboard["available_balance"]) < 1000 {
		t.Fatalf("merchant dashboard = %d %v", status, dashboard)
	}
	client.token = adminToken
	status, merchantDetail := client.json(http.MethodGet, "/api/v1/admin/merchants/"+merchantID, nil)
	if status != http.StatusOK {
		t.Fatalf("admin merchant detail = %d %v", status, merchantDetail)
	}
	status, stats := client.json(http.MethodGet, "/api/v1/admin/stats/daily?days=7", nil)
	if status != http.StatusOK {
		t.Fatalf("admin daily stats = %d %v", status, stats)
	}
	if _, ok := stats["items"].([]any); !ok {
		t.Fatalf("daily stats items missing: %v", stats)
	}
	status, announcement := client.json(http.MethodPost, "/api/v1/admin/announcements", map[string]string{
		"title": "系统公告", "content": "E2E 公告内容", "status": "published",
	})
	if status != http.StatusCreated {
		t.Fatalf("create announcement = %d %v", status, announcement)
	}
	client.token = ""
	status, publicAnnouncements := client.json(http.MethodGet, "/api/v1/public/announcements", nil)
	if status != http.StatusOK {
		t.Fatalf("public announcements = %d %v", status, publicAnnouncements)
	}
	status, site := client.json(http.MethodGet, "/api/v1/public/site", nil)
	if status != http.StatusOK {
		t.Fatalf("public site = %d %v", status, site)
	}
	client.token = adminToken
	status, outboxList := client.json(http.MethodGet, "/api/v1/admin/outbox/messages", nil)
	if status != http.StatusOK {
		t.Fatalf("outbox messages = %d %v", status, outboxList)
	}

	// 8. V2 RSA credential + signed query + response signature verification.
	client.token = merchantToken
	status, v2Keys := client.json(http.MethodPost, "/api/v1/merchant/credentials/v2/rotate", nil)
	if status != http.StatusCreated {
		t.Fatalf("rotate rsa = %d %v", status, v2Keys)
	}
	privatePEM := jsonString(t, v2Keys["private_key"])
	platformPEM := jsonString(t, v2Keys["platform_public_key"])
	if privatePEM == "" || platformPEM == "" {
		t.Fatalf("v2 keys missing: %v", v2Keys)
	}
	privateKey, err := signing.ParseRSAPrivateKeyPEM(privatePEM)
	if err != nil {
		t.Fatal(err)
	}
	platformKey, err := signing.ParseRSAPublicKeyPEM(platformPEM)
	if err != nil {
		t.Fatal(err)
	}
	client.token = ""
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	queryParams := map[string]any{
		"pid": pidText, "out_trade_no": "E2E-ORDER-1", "timestamp": timestamp, "sign_type": "RSA",
	}
	querySign, err := signing.SignRSA(queryParams, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	queryValues := url.Values{
		"pid": {pidText}, "out_trade_no": {"E2E-ORDER-1"}, "timestamp": {timestamp},
		"sign_type": {"RSA"}, "sign": {querySign},
	}
	status, queryResponse := client.form("/api/pay/query", queryValues)
	if status != http.StatusOK || jsonInt(t, queryResponse["code"]) != 0 {
		t.Fatalf("v2 query = %d %v", status, queryResponse)
	}
	if queryResponse["trade_no"] != tradeNo || jsonInt(t, queryResponse["status"]) != 1 {
		t.Fatalf("v2 query payload = %v", queryResponse)
	}
	responseSignature := jsonString(t, queryResponse["sign"])
	delete(queryResponse, "sign")
	delete(queryResponse, "sign_type")
	if responseSignature == "" || !signing.VerifyRSA(queryResponse, platformKey, responseSignature) {
		t.Fatal("v2 response signature does not verify")
	}

	// 8. Clock skew and replay rejection.
	expiredParams := map[string]any{
		"pid": pidText, "out_trade_no": "E2E-ORDER-1", "timestamp": "1000000000", "sign_type": "RSA",
	}
	expiredSign, _ := signing.SignRSA(expiredParams, privateKey)
	expiredValues := url.Values{
		"pid": {pidText}, "out_trade_no": {"E2E-ORDER-1"}, "timestamp": {"1000000000"},
		"sign_type": {"RSA"}, "sign": {expiredSign},
	}
	_, expiredResponse := client.form("/api/pay/query", expiredValues)
	if jsonInt(t, expiredResponse["code"]) != -3 {
		t.Fatalf("expired timestamp accepted: %v", expiredResponse)
	}

	refundParams := map[string]any{
		"pid": pidText, "trade_no": tradeNo, "money": "1.00",
		"out_refund_no": "E2E-REFUND-1", "timestamp": timestamp, "sign_type": "RSA",
	}
	refundSign, _ := signing.SignRSA(refundParams, privateKey)
	refundValues := url.Values{
		"pid": {pidText}, "trade_no": {tradeNo}, "money": {"1.00"},
		"out_refund_no": {"E2E-REFUND-1"}, "timestamp": {timestamp}, "sign_type": {"RSA"}, "sign": {refundSign},
	}
	_, refundResponse := client.form("/api/pay/refund", refundValues)
	if jsonInt(t, refundResponse["code"]) != 0 {
		t.Fatalf("v2 refund = %v", refundResponse)
	}
	_, replayResponse := client.form("/api/pay/refund", refundValues)
	if jsonInt(t, replayResponse["code"]) != -3 {
		t.Fatalf("replayed refund accepted: %v", replayResponse)
	}

	// 9. V1 api.php account and order queries.
	_, accountQuery := client.form("/api.php?act=query", url.Values{"pid": {pidText}, "key": {merchantKey}})
	if jsonInt(t, accountQuery["code"]) != 1 {
		t.Fatalf("api.php query = %v", accountQuery)
	}
	_, orderQuery := client.form("/api.php?act=order", url.Values{"pid": {pidText}, "key": {merchantKey}, "trade_no": {tradeNo}})
	if jsonInt(t, orderQuery["code"]) != 1 || orderQuery["trade_no"] != tradeNo {
		t.Fatalf("api.php order = %v", orderQuery)
	}

	// 10. Transfer (代付): freeze on create, debit on approval.
	client.token = merchantToken
	status, transfer := client.json(http.MethodPost, "/api/v1/merchant/transfers", map[string]any{
		"out_transfer_no": "E2E-TRANSFER-1", "amount": 500, "currency": "CNY",
		"account_name": "张三", "account_no": "6222020202020202", "bank_name": "招商银行",
	})
	if status != http.StatusCreated {
		t.Fatalf("create transfer = %d %v", status, transfer)
	}
	transferID := jsonString(t, transfer["id"])
	var frozen ledgermodel.Balance
	if err := application.DB().First(&frozen, "account_id = ?", merchantAccount.ID).Error; err != nil {
		t.Fatal(err)
	}
	if frozen.Available != 500 || frozen.Frozen != 500 {
		t.Fatalf("after freeze available=%d frozen=%d, want 500/500", frozen.Available, frozen.Frozen)
	}
	client.token = adminToken
	status, approved := client.json(http.MethodPost, "/api/v1/admin/transfers/"+transferID+"/process", map[string]string{"action": "approve"})
	if status != http.StatusOK || approved["status"] != "approved" {
		t.Fatalf("approve transfer = %d %v", status, approved)
	}
	var settled ledgermodel.Balance
	if err := application.DB().First(&settled, "account_id = ?", merchantAccount.ID).Error; err != nil {
		t.Fatal(err)
	}
	if settled.Available != 500 || settled.Frozen != 0 {
		t.Fatalf("after approve available=%d frozen=%d, want 500/0", settled.Available, settled.Frozen)
	}

	// 11. Forged channel callback never marks an order paid.
	client.token = ""
	forged := url.Values{
		"pid": {pidText}, "trade_status": {"TRADE_SUCCESS"}, "out_trade_no": {tradeNo},
		"trade_no": {"FORGED"}, "money": {"10.00"}, "sign": {"deadbeef"},
	}
	status, _ = client.rawForm("/api/v1/payment/callback/epay/"+channelID, forged)
	if status < 400 {
		t.Fatalf("forged callback status = %d, want >= 400", status)
	}
}

func formMap(values url.Values) map[string]any {
	output := make(map[string]any, len(values))
	for key := range values {
		if key == "sign" || key == "sign_type" {
			continue
		}
		output[key] = values.Get(key)
	}
	return output
}
