package plugins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	"gorixpay/internal/compat/epay/signing"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/outbox"
	paymentcallback "gorixpay/internal/payment/callback"
	paymentmodel "gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentservice "gorixpay/internal/payment/service"
	platformconfig "gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"
	pluginpayment "gorixpay/internal/plugin/payment"
	"gorixpay/internal/plugin/payment/alipay"
	"gorixpay/internal/plugin/payment/paymenttest"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TestAlipayChannelRunsFromCreateToCallback drives the whole loop against a mock
// upstream: encrypted channel config -> orchestrator -> plugin precreate ->
// signed notification -> order paid exactly once.
func TestAlipayChannelRunsFromCreateToCallback(t *testing.T) {
	merchantKeys := paymenttest.MustMerchantKeys(t)
	platformKeys := paymenttest.MustPlatformKeys(t)
	var notifyURLSeen string
	gateway := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		params := make(map[string]any, len(request.PostForm))
		for name := range request.PostForm {
			params[name] = request.PostForm.Get(name)
		}
		if !signing.VerifyRSA(params, &merchantKeys.Private.PublicKey, request.PostForm.Get("sign")) {
			t.Errorf("precreate request signature does not verify")
		}
		notifyURLSeen = request.PostForm.Get("notify_url")
		_, _ = writer.Write([]byte(`{"alipay_trade_precreate_response":{"code":"10000","msg":"Success","qr_code":"https://qr.alipay.com/e2e"}}`))
	}))
	defer gateway.Close()

	db, order, configs := e2eDatabase(t, "contracts-plugin-e2e")
	encrypted, err := configs.Encrypt("channel-alipay", channelconfig.Config{Extra: map[string]string{
		"app_id": "2021000000000001", "private_key": merchantKeys.PrivatePEM,
		"alipay_public_key": platformKeys.PublicPEM, "gateway_url": gateway.URL,
	}})
	if err != nil {
		t.Fatal(err)
	}
	channel := channelrouter.Channel{
		ID: "channel-alipay", Name: "支付宝", PluginCode: "alipay", PaymentType: "alipay",
		Priority: 1, Weight: 100, Status: channelrouter.StatusActive, ConfigEncrypted: encrypted,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}

	registry := pluginpayment.NewRegistry()
	if err := registry.Register(alipay.New(gateway.Client())); err != nil {
		t.Fatal(err)
	}
	orchestrator := paymentorchestrator.NewConfigured(
		db, channelrouter.New(db, registry, func(int) (int, error) { return 0, nil }),
		time.Now, configs, "https://pay.example",
	)
	started, err := orchestrator.Start(context.Background(), order.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	if started.Action.Type != pluginpayment.ActionQRCode || started.Action.URL != "https://qr.alipay.com/e2e" {
		t.Fatalf("action=%+v", started.Action)
	}
	if notifyURLSeen != "https://pay.example/api/v1/payment/callback/alipay/channel-alipay" {
		t.Fatalf("notify url=%q", notifyURLSeen)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	paymentcallback.RegisterPlugins(engine, db, configs, paymentcallback.New(db, time.Now), registry)

	notification := alipayNotification(order.TradeNo, "ALI-E2E-1", "12.34", platformKeys, "2021000000000001")
	const path = "/api/v1/payment/callback/alipay/channel-alipay"
	if response := postNotification(t, engine, path, notification.Encode()); response.Code != http.StatusOK {
		t.Fatalf("callback status=%d body=%q", response.Code, response.Body.String())
	}
	assertPaidOnce(t, db, order.ID, "ALI-E2E-1")

	// A replay of the very same notification must not book twice.
	if response := postNotification(t, engine, path, notification.Encode()); response.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%q", response.Code, response.Body.String())
	}
	assertPaidOnce(t, db, order.ID, "ALI-E2E-1")

	// A forged signature and a tampered amount are both refused.
	forged := alipayNotification(order.TradeNo, "ALI-E2E-1", "12.34", platformKeys, "2021000000000001")
	forged.Set("sign", "Zm9yZ2Vk")
	if response := postNotification(t, engine, path, forged.Encode()); response.Code != http.StatusUnauthorized {
		t.Fatalf("forged status=%d body=%q", response.Code, response.Body.String())
	}
	cheap := alipayNotification(order.TradeNo, "ALI-E2E-2", "0.01", platformKeys, "2021000000000001")
	if response := postNotification(t, engine, path, cheap.Encode()); response.Code != http.StatusBadRequest {
		t.Fatalf("cheap status=%d body=%q", response.Code, response.Body.String())
	}
	assertPaidOnce(t, db, order.ID, "ALI-E2E-1")
}

func e2eDatabase(t *testing.T, name string) (*gorm.DB, paymentmodel.Order, *channelconfig.Service) {
	t.Helper()
	db, _, err := database.Open(context.Background(), platformconfig.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&identitymodel.Merchant{}, &paymentmodel.Order{}, &paymentmodel.Attempt{},
		&channelrouter.Channel{}, &paymentcallback.Record{}, &outbox.Message{},
	); err != nil {
		t.Fatal(err)
	}
	merchant := identitymodel.Merchant{ID: "merchant-e2e", DisplayName: "商户", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	order, _, err := paymentservice.New(db, time.Now).Create(context.Background(), paymentservice.CreateCommand{
		MerchantID: merchant.ID, OutTradeNo: "OUT-E2E-1", PaymentType: "alipay", Name: "端到端测试", Amount: 1234,
		NotifyURL: "https://merchant.example/notify", ReturnURL: "https://merchant.example/return", ClientIP: "203.0.113.5",
	})
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	return db, order, channelconfig.NewService(cipher)
}

// alipayNotification builds a notification signed with the platform key exactly
// the way Alipay signs it.
func alipayNotification(tradeNo, channelTradeNo, amount string, platform paymenttest.Pair, appID string) url.Values {
	values := url.Values{
		"app_id": {appID}, "charset": {"utf-8"}, "notify_type": {"trade_status_sync"},
		"out_trade_no": {tradeNo}, "total_amount": {amount}, "trade_no": {channelTradeNo},
		"trade_status": {"TRADE_SUCCESS"}, "sign_type": {"RSA2"},
	}
	params := make(map[string]any, len(values))
	for name := range values {
		params[name] = values.Get(name)
	}
	signature, err := signing.SignRSA(params, platform.Private)
	if err != nil {
		panic(err)
	}
	values.Set("sign", signature)
	return values
}

func postNotification(t *testing.T, engine *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func assertPaidOnce(t *testing.T, db *gorm.DB, orderID, wantChannelTradeNo string) {
	t.Helper()
	var order paymentmodel.Order
	if err := db.First(&order, "id = ?", orderID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != paymentmodel.StatusPaid {
		t.Fatalf("order status=%s", order.Status)
	}
	if order.ChannelTradeNo == nil || *order.ChannelTradeNo != wantChannelTradeNo {
		t.Fatalf("channel trade no=%v", order.ChannelTradeNo)
	}
	var records, messages int64
	if err := db.Model(&paymentcallback.Record{}).Count(&records).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&outbox.Message{}).Count(&messages).Error; err != nil {
		t.Fatal(err)
	}
	if records != 1 || messages != 1 {
		t.Fatalf("records=%d messages=%d", records, messages)
	}
}
