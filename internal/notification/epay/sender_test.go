package epay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/compat/epay/signing"
	epayv2 "gorixpay/internal/compat/epay/v2"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/merchant/credential"
	"gorixpay/internal/outbox"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"

	"gorm.io/gorm"
)

func TestSenderGetsSignedEpayNotificationAndAcceptsSuccessBody(t *testing.T) {
	var received url.Values
	var method string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		method = request.Method
		received = request.URL.Query()
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte("SUCCESS"))
	}))
	defer server.Close()
	db, credentials, order, key := senderDatabase(t, "notify-success", server.URL)
	sender := NewSender(db, credentials, server.Client())
	message := outbox.Message{Topic: "merchant.payment.notify", AggregateID: order.ID, Payload: `{"order_id":"` + order.ID + `"}`}
	if err := sender.Handle(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet {
		t.Fatalf("notification method = %s, want GET (Epay/includes/functions.php:522)", method)
	}
	if received.Get("trade_no") != order.TradeNo || received.Get("out_trade_no") != order.OutTradeNo || received.Get("trade_status") != "TRADE_SUCCESS" || received.Get("money") != "1.00" {
		t.Fatalf("received=%v", received)
	}
	params := make(map[string]any, len(received))
	for name := range received {
		params[name] = received.Get(name)
	}
	if !signing.VerifyMD5(params, key, received.Get("sign")) {
		t.Fatal("notification signature is invalid")
	}
}

func TestSenderRejectsOKAndEmptyHTTP200Bodies(t *testing.T) {
	for _, body := range []string{"ok", ""} {
		t.Run("body-"+body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(body))
			}))
			defer server.Close()
			db, credentials, order, _ := senderDatabase(t, "notify-reject-"+body, server.URL)
			err := NewSender(db, credentials, server.Client()).Handle(context.Background(), outbox.Message{
				Topic: "merchant.payment.notify", AggregateID: order.ID, Payload: `{"order_id":"` + order.ID + `"}`,
			})
			if err == nil || !strings.Contains(err.Error(), "notification acknowledgement") {
				t.Fatalf("body %q error=%v", body, err)
			}
		})
	}
}

func TestSenderRejectsUnsupportedTopicAndOversizedBody(t *testing.T) {
	db, credentials, _, _ := senderDatabase(t, "notify-errors", "https://unused.example")
	sender := NewSender(db, credentials, http.DefaultClient)
	if err := sender.Handle(context.Background(), outbox.Message{Topic: "unknown"}); err == nil {
		t.Fatal("unsupported topic accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
	}))
	defer server.Close()
	var order paymentmodel.Order
	if err := db.First(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&paymentmodel.Order{}).Where("id = ?", order.ID).Update("notify_url", server.URL).Error; err != nil {
		t.Fatal(err)
	}
	if err := NewSender(db, credentials, server.Client()).Handle(context.Background(), outbox.Message{Topic: "merchant.payment.notify", AggregateID: order.ID}); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func senderDatabase(t *testing.T, name, notifyURL string) (*gorm.DB, *credential.Service, paymentmodel.Order, string) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &identitymodel.MerchantCredential{}, &paymentmodel.Order{}, &epayv2.PlatformSetting{}); err != nil {
		t.Fatal(err)
	}
	uid := int64(1001)
	merchant := identitymodel.Merchant{ID: "merchant-1", LegacyUID: &uid, DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	credentials := credential.NewService(db, cipher, time.Now)
	_, key, err := credentials.CreateMD5(context.Background(), merchant.ID)
	if err != nil {
		t.Fatal(err)
	}
	paidAt := time.Now().UTC()
	order := paymentmodel.Order{
		ID: "order-1", TradeNo: "2026071212000012345", MerchantID: merchant.ID, OutTradeNo: "ORDER-1", PaymentType: "alipay",
		Name: "test", Amount: 100, Currency: "CNY", Status: paymentmodel.StatusPaid, NotifyURL: notifyURL,
		PaidAt: &paidAt, ExpiresAt: paidAt.Add(time.Hour), CreatedAt: paidAt.Add(-time.Minute), UpdatedAt: paidAt,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	return db, credentials, order, key
}

func TestSenderUsesPlatformRSAForV2Merchants(t *testing.T) {
	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received = request.URL.Query()
		_, _ = writer.Write([]byte("SUCCESS"))
	}))
	defer server.Close()
	db, credentials, order, key := senderDatabase(t, "notify-v2", server.URL)
	if _, _, err := credentials.CreateRSA(context.Background(), order.MerchantID); err != nil {
		t.Fatal(err)
	}
	// Only orders created through the V2 API are version 1 and get the RSA
	// payload (Epay/includes/lib/api/Pay.php:171, Epay/includes/functions.php:503).
	if err := db.Model(&paymentmodel.Order{}).Where("id = ?", order.ID).Update("version", 1).Error; err != nil {
		t.Fatal(err)
	}
	channelTradeNo := "CH-2026"
	if err := db.Model(&paymentmodel.Order{}).Where("id = ?", order.ID).Update("channel_trade_no", channelTradeNo).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := epayv2.LoadSigner(context.Background(), db, cipher)
	if err != nil {
		t.Fatal(err)
	}
	sender := NewSenderWithSigner(db, credentials, server.Client(), signer)
	if err := sender.Handle(context.Background(), outbox.Message{Topic: "merchant.payment.notify", AggregateID: order.ID}); err != nil {
		t.Fatal(err)
	}
	if received.Get("sign_type") != "RSA" || received.Get("api_trade_no") != channelTradeNo || received.Get("trade_status") != "TRADE_SUCCESS" {
		t.Fatalf("received=%v", received)
	}
	if received.Get("timestamp") == "" || received.Get("money") != "1.00" || received.Get("pid") != "1001" {
		t.Fatalf("received=%v", received)
	}
	params := make(map[string]any, len(received))
	for name := range received {
		params[name] = received.Get(name)
	}
	publicKey, err := signing.ParseRSAPublicKeyPEM(signer.PublicKeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	if !signing.VerifyRSA(params, publicKey, received.Get("sign")) {
		t.Fatal("v2 notification signature is invalid")
	}
	if signing.VerifyMD5(params, key, received.Get("sign")) {
		t.Fatal("v2 notification must not be signed with the merchant md5 key")
	}
}

// TestSenderKeepsLegacyMd5ForV1OrdersWhenMerchantHasRsa locks the upstream rule
// that the notification generation comes from the order version, not from the
// merchant's credential set: a merchant with an RSA key still gets MD5 payloads
// for orders created by the legacy V1 entrypoints.
func TestSenderKeepsLegacyMd5ForV1OrdersWhenMerchantHasRsa(t *testing.T) {
	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received = request.URL.Query()
		_, _ = writer.Write([]byte("success"))
	}))
	defer server.Close()
	db, credentials, order, key := senderDatabase(t, "notify-v1-with-rsa", server.URL)
	if _, _, err := credentials.CreateRSA(context.Background(), order.MerchantID); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := epayv2.LoadSigner(context.Background(), db, cipher)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewSenderWithSigner(db, credentials, server.Client(), signer).Handle(context.Background(), outbox.Message{
		Topic: "merchant.payment.notify", AggregateID: order.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if received.Get("sign_type") != "MD5" {
		t.Fatalf("v1 order notification sign_type = %q, want MD5", received.Get("sign_type"))
	}
	if received.Get("timestamp") != "" || received.Get("api_trade_no") != "" {
		t.Fatalf("v1 notification must not carry v2 fields: %v", received)
	}
	params := make(map[string]any, len(received))
	for name := range received {
		params[name] = received.Get(name)
	}
	if !signing.VerifyMD5(params, key, received.Get("sign")) {
		t.Fatal("v1 notification signature does not verify with the merchant md5 key")
	}
}
