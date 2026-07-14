package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/merchant/credential"
	"gorixpay/internal/outbox"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/secret"
)

func TestNewBuildsReadyApplicationWithSQLite(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x42}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Environment: "test",
		HTTP:        config.HTTPConfig{Address: ":0"},
		Database:    config.DatabaseConfig{Driver: "sqlite", DSN: "file:app?mode=memory&cache=shared"},
		Security:    config.SecurityConfig{MasterKeyFile: keyPath},
	}
	application, err := New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("readiness status = %d, body = %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	application.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/merchant/login", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("identity API status = %d, body = %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	application.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/merchant/credentials", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("credential API status = %d, body = %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	application.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mapi.php", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("Epay API status = %d, body = %s", response.Code, response.Body.String())
	}
	if !application.DB().Migrator().HasTable("system_settings") {
		t.Fatal("platform migrations were not applied")
	}
	for _, table := range []string{"administrators", "merchants", "roles", "permissions", "auth_sessions", "api_tokens", "merchant_credentials", "payment_orders", "payment_attempts"} {
		if !application.DB().Migrator().HasTable(table) {
			t.Fatalf("identity table %s was not migrated", table)
		}
	}
}

func TestNewRejectsInvalidMasterKeyFile(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(keyPath, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Environment: "test",
		HTTP:        config.HTTPConfig{Address: ":0"},
		Database:    config.DatabaseConfig{Driver: "sqlite", DSN: "file:invalid-key?mode=memory&cache=shared"},
		Security:    config.SecurityConfig{MasterKeyFile: keyPath},
	}
	if _, err := New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("invalid master key accepted")
	}
}

func TestNewProcessesMerchantNotificationOutbox(t *testing.T) {
	notified := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil || request.Form.Get("trade_status") != "TRADE_SUCCESS" {
			t.Errorf("notification form=%v err=%v", request.Form, err)
		}
		notified <- struct{}{}
		_, _ = writer.Write([]byte("success"))
	}))
	defer server.Close()
	key := bytes.Repeat([]byte{0x42}, 32)
	keyPath := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Environment: "test",
		HTTP:        config.HTTPConfig{Address: ":0"},
		Database:    config.DatabaseConfig{Driver: "sqlite", DSN: "file:app-worker?mode=memory&cache=shared"},
		Security:    config.SecurityConfig{MasterKeyFile: keyPath},
	}
	application, err := New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	now := time.Now().UTC()
	uid := int64(1001)
	merchant := identitymodel.Merchant{ID: "merchant-worker", LegacyUID: &uid, DisplayName: "merchant", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now}
	if err := application.DB().Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(key, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := credential.NewService(application.DB(), cipher, time.Now).CreateMD5(context.Background(), merchant.ID); err != nil {
		t.Fatal(err)
	}
	paidAt := now
	order := paymentmodel.Order{
		ID: "order-worker", TradeNo: "2026071212000012345", MerchantID: merchant.ID, OutTradeNo: "ORDER-WORKER",
		PaymentType: "alipay", Name: "test", Amount: 100, Currency: "CNY", Status: paymentmodel.StatusPaid,
		NotifyURL: server.URL, PaidAt: &paidAt, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	if err := application.DB().Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	message := outbox.Message{
		ID: "message-worker", Topic: "merchant.payment.notify", AggregateID: order.ID,
		IdempotencyKey: "merchant-payment-notify:" + order.ID, Payload: `{}`, Status: outbox.StatusPending,
		AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := application.DB().Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	select {
	case <-notified:
	case <-time.After(3 * time.Second):
		t.Fatal("application worker did not send notification")
	}
}
