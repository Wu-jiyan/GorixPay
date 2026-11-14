package callback

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/outbox"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// verifierPlugin is a minimal plugin that delegates notification verification to
// the test, so the handler contract can be exercised without an upstream.
type verifierPlugin struct {
	verify func(ctx context.Context, r *http.Request, config map[string]string) (pluginpayment.CallbackResult, error)
}

func (verifierPlugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{
		Code: "stubpay", Name: "Stub Pay", Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment},
	}
}

func (verifierPlugin) CreatePayment(context.Context, pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	return pluginpayment.Action{Type: pluginpayment.ActionJump, URL: "https://stubpay.example/pay"}, nil
}

func (plugin verifierPlugin) VerifyCallback(ctx context.Context, r *http.Request, config map[string]string) (pluginpayment.CallbackResult, error) {
	if plugin.verify == nil {
		return pluginpayment.CallbackResult{}, errors.New("no verifier")
	}
	return plugin.verify(ctx, r, config)
}

// barePlugin has no CallbackVerifier, like the epay and sandbox plugins.
type barePlugin struct{}

func (barePlugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{Code: "bare", Name: "Bare", Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment}}
}

func (barePlugin) CreatePayment(context.Context, pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	return pluginpayment.Action{Type: pluginpayment.ActionJump, URL: "https://bare.example/pay"}, nil
}

func TestPluginCallbackRoutesCoexistWithEpayCallback(t *testing.T) {
	db, _ := pluginCallbackDatabase(t, "callback-plugin-routes")
	configs := pluginCallbackChannel(t, db, "stubpay", channelrouter.StatusActive)
	service := New(db, time.Now)
	plugins := pluginpayment.NewRegistry()
	if err := plugins.Register(verifierPlugin{}); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// Both registrations must be able to share the gin tree.
	RegisterEpay(router, db, configs, service)
	RegisterPlugins(router, db, configs, service, plugins)
	paths := make(map[string]bool)
	for _, route := range router.Routes() {
		paths[route.Method+" "+route.Path] = true
	}
	if !paths["POST /api/v1/payment/callback/epay/:channel_id"] {
		t.Fatalf("epay callback route missing: %v", paths)
	}
	if !paths["POST /api/v1/payment/callback/:plugin/:channel_id"] {
		t.Fatalf("generic callback route missing: %v", paths)
	}
	// The registration order must not matter for the wiring in app.go.
	reverse := gin.New()
	RegisterPlugins(reverse, db, configs, service, plugins)
	RegisterEpay(reverse, db, configs, service)
	reversePaths := make(map[string]bool)
	for _, route := range reverse.Routes() {
		reversePaths[route.Method+" "+route.Path] = true
	}
	if !reversePaths["POST /api/v1/payment/callback/epay/:channel_id"] || !reversePaths["POST /api/v1/payment/callback/:plugin/:channel_id"] {
		t.Fatalf("reverse registration lost routes: %v", reversePaths)
	}
}

func TestPluginCallbackCompletesPaidOrder(t *testing.T) {
	db, order := pluginCallbackDatabase(t, "callback-plugin-success")
	configs := pluginCallbackChannel(t, db, "stubpay", channelrouter.StatusActive)
	plugins := pluginpayment.NewRegistry()
	if err := plugins.Register(verifierPlugin{verify: func(_ context.Context, r *http.Request, config map[string]string) (pluginpayment.CallbackResult, error) {
		if config["channel_marker"] != "marker" {
			t.Errorf("plugin did not receive the decrypted config: %+v", config)
		}
		if got := readCallbackBody(t, r); got != "trade_status=SUCCESS" {
			t.Errorf("body=%q", got)
		}
		return pluginpayment.CallbackResult{TradeNo: order.TradeNo, ChannelTradeNo: "STUB-1", Amount: order.Amount, Paid: true, Raw: map[string]string{"trade_status": "SUCCESS"}}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	router := pluginCallbackRouter(t, db, configs, plugins)
	response := performCallback(t, router, "/api/v1/payment/callback/stubpay/channel-1", "trade_status=SUCCESS")
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "success" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	var updated paymentmodel.Order
	if err := db.First(&updated, "id = ?", order.ID).Error; err != nil || updated.Status != paymentmodel.StatusPaid {
		t.Fatalf("order=%+v err=%v", updated, err)
	}
	if updated.ChannelTradeNo == nil || *updated.ChannelTradeNo != "STUB-1" {
		t.Fatalf("channel trade no=%v", updated.ChannelTradeNo)
	}
	var outboxCount int64
	if err := db.Model(&outbox.Message{}).Count(&outboxCount).Error; err != nil || outboxCount != 1 {
		t.Fatalf("outbox=%d err=%v", outboxCount, err)
	}
	var recordCount int64
	if err := db.Model(&Record{}).Count(&recordCount).Error; err != nil || recordCount != 1 {
		t.Fatalf("callbacks=%d err=%v", recordCount, err)
	}
}

// TestPluginCallbackRejectsCallbackFromAnotherChannel pins the fix for the
// cross-channel confirmation hole: the callback used to be matched on trade_no
// alone, so a channel that never processed the order could book it.
func TestPluginCallbackRejectsCallbackFromAnotherChannel(t *testing.T) {
	db, order := pluginCallbackDatabase(t, "callback-plugin-cross-channel")
	configs := pluginCallbackChannel(t, db, "stubpay", channelrouter.StatusActive)
	// A second, perfectly valid channel of the same plugin. The order never went
	// to it.
	encrypted := pluginCallbackEncrypt(t, "channel-2", "marker-2")
	other := channelrouter.Channel{
		ID: "channel-2", Name: "stubpay-2", PluginCode: "stubpay", PaymentType: "alipay",
		Weight: 1, Status: channelrouter.StatusActive, ConfigEncrypted: encrypted,
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	plugins := pluginpayment.NewRegistry()
	if err := plugins.Register(verifierPlugin{verify: func(_ context.Context, _ *http.Request, _ map[string]string) (pluginpayment.CallbackResult, error) {
		return pluginpayment.CallbackResult{
			TradeNo: order.TradeNo, ChannelTradeNo: "STUB-OTHER", Amount: order.Amount, Paid: true,
			Raw: map[string]string{"trade_status": "SUCCESS"},
		}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	router := pluginCallbackRouter(t, db, configs, plugins)
	response := performCallback(t, router, "/api/v1/payment/callback/stubpay/channel-2", "trade_status=SUCCESS")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("cross-channel callback status=%d body=%q, want 400", response.Code, response.Body.String())
	}
	assertOrderPending(t, db, order.ID)
	var records int64
	if err := db.Model(&Record{}).Count(&records).Error; err != nil || records != 0 {
		t.Fatalf("callbacks=%d err=%v, want 0", records, err)
	}

	// The channel the order was actually routed to still works.
	response = performCallback(t, router, "/api/v1/payment/callback/stubpay/channel-1", "trade_status=SUCCESS")
	if response.Code != http.StatusOK {
		t.Fatalf("own-channel callback status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestPluginCallbackIsIdempotent(t *testing.T) {
	db, order := pluginCallbackDatabase(t, "callback-plugin-idempotent")
	configs := pluginCallbackChannel(t, db, "stubpay", channelrouter.StatusActive)
	plugins := pluginpayment.NewRegistry()
	if err := plugins.Register(verifierPlugin{verify: func(context.Context, *http.Request, map[string]string) (pluginpayment.CallbackResult, error) {
		return pluginpayment.CallbackResult{TradeNo: order.TradeNo, ChannelTradeNo: "STUB-1", Amount: order.Amount, Paid: true}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	router := pluginCallbackRouter(t, db, configs, plugins)
	for attempt := 0; attempt < 3; attempt++ {
		response := performCallback(t, router, "/api/v1/payment/callback/stubpay/channel-1", "trade_status=SUCCESS")
		if response.Code != http.StatusOK {
			t.Fatalf("attempt %d status=%d body=%q", attempt, response.Code, response.Body.String())
		}
	}
	var records int64
	if err := db.Model(&Record{}).Count(&records).Error; err != nil || records != 1 {
		t.Fatalf("records=%d err=%v", records, err)
	}
	var outboxCount int64
	if err := db.Model(&outbox.Message{}).Count(&outboxCount).Error; err != nil || outboxCount != 1 {
		t.Fatalf("outbox=%d err=%v", outboxCount, err)
	}
}

func TestPluginCallbackRejectsForgedSignature(t *testing.T) {
	db, order := pluginCallbackDatabase(t, "callback-plugin-forged")
	configs := pluginCallbackChannel(t, db, "stubpay", channelrouter.StatusActive)
	plugins := pluginpayment.NewRegistry()
	if err := plugins.Register(verifierPlugin{verify: func(context.Context, *http.Request, map[string]string) (pluginpayment.CallbackResult, error) {
		return pluginpayment.CallbackResult{}, pluginpayment.ErrCallbackRejected
	}}); err != nil {
		t.Fatal(err)
	}
	router := pluginCallbackRouter(t, db, configs, plugins)
	response := performCallback(t, router, "/api/v1/payment/callback/stubpay/channel-1", "sign=forged")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	assertOrderPending(t, db, order.ID)
}

func TestPluginCallbackRejectsAmountMismatch(t *testing.T) {
	db, order := pluginCallbackDatabase(t, "callback-plugin-amount")
	configs := pluginCallbackChannel(t, db, "stubpay", channelrouter.StatusActive)
	plugins := pluginpayment.NewRegistry()
	if err := plugins.Register(verifierPlugin{verify: func(context.Context, *http.Request, map[string]string) (pluginpayment.CallbackResult, error) {
		return pluginpayment.CallbackResult{TradeNo: order.TradeNo, ChannelTradeNo: "STUB-1", Amount: 1, Paid: true}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	router := pluginCallbackRouter(t, db, configs, plugins)
	response := performCallback(t, router, "/api/v1/payment/callback/stubpay/channel-1", "money=0.01")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	assertOrderPending(t, db, order.ID)
	var records int64
	if err := db.Model(&Record{}).Count(&records).Error; err != nil || records != 0 {
		t.Fatalf("records=%d err=%v", records, err)
	}
}

func TestPluginCallbackAcknowledgesUnpaidNotification(t *testing.T) {
	db, order := pluginCallbackDatabase(t, "callback-plugin-unpaid")
	configs := pluginCallbackChannel(t, db, "stubpay", channelrouter.StatusActive)
	plugins := pluginpayment.NewRegistry()
	if err := plugins.Register(verifierPlugin{verify: func(context.Context, *http.Request, map[string]string) (pluginpayment.CallbackResult, error) {
		return pluginpayment.CallbackResult{TradeNo: order.TradeNo, ChannelTradeNo: "STUB-1", Paid: false}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	router := pluginCallbackRouter(t, db, configs, plugins)
	response := performCallback(t, router, "/api/v1/payment/callback/stubpay/channel-1", "trade_status=WAIT_BUYER_PAY")
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "success" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	assertOrderPending(t, db, order.ID)
}

func TestPluginCallbackRejectsUnknownChannelPluginOrPluginWithoutVerifier(t *testing.T) {
	db, order := pluginCallbackDatabase(t, "callback-plugin-unknown")
	configs := pluginCallbackChannel(t, db, "stubpay", channelrouter.StatusActive)
	inactive := channelrouter.Channel{ID: "channel-inactive", Name: "stubpay", PluginCode: "stubpay", PaymentType: "alipay", Weight: 1, Status: channelrouter.StatusDisabled}
	if err := db.Create(&inactive).Error; err != nil {
		t.Fatal(err)
	}
	bareConfigs := pluginCallbackEncrypt(t, "channel-bare", "bare")
	bare := channelrouter.Channel{ID: "channel-bare", Name: "bare", PluginCode: "bare", PaymentType: "alipay", Weight: 1, Status: channelrouter.StatusActive, ConfigEncrypted: bareConfigs}
	if err := db.Create(&bare).Error; err != nil {
		t.Fatal(err)
	}
	plugins := pluginpayment.NewRegistry()
	if err := plugins.Register(verifierPlugin{verify: func(context.Context, *http.Request, map[string]string) (pluginpayment.CallbackResult, error) {
		return pluginpayment.CallbackResult{TradeNo: order.TradeNo, ChannelTradeNo: "STUB-1", Amount: order.Amount, Paid: true}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := plugins.Register(barePlugin{}); err != nil {
		t.Fatal(err)
	}
	router := pluginCallbackRouter(t, db, configs, plugins)
	for name, path := range map[string]string{
		"unknown-channel":       "/api/v1/payment/callback/stubpay/missing",
		"plugin-mismatch":       "/api/v1/payment/callback/alipay/channel-1",
		"inactive-channel":      "/api/v1/payment/callback/stubpay/channel-inactive",
		"plugin-without-verify": "/api/v1/payment/callback/bare/channel-bare",
	} {
		t.Run(name, func(t *testing.T) {
			response := performCallback(t, router, path, "trade_status=SUCCESS")
			if response.Code != http.StatusNotFound {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
	assertOrderPending(t, db, order.ID)
}

func TestPluginCallbackDigestDiffersPerRawBody(t *testing.T) {
	first := notificationDigest("stubpay", "channel-1", []byte("a=1"))
	if first == notificationDigest("stubpay", "channel-1", []byte("a=2")) {
		t.Fatal("digest ignores the body")
	}
	if first == notificationDigest("stubpay", "channel-2", []byte("a=1")) {
		t.Fatal("digest ignores the channel")
	}
	if first == notificationDigest("other", "channel-1", []byte("a=1")) {
		t.Fatal("digest ignores the plugin")
	}
	if len(first) != 64 {
		t.Fatalf("digest length=%d", len(first))
	}
}

func pluginCallbackRouter(t *testing.T, db *gorm.DB, configs *channelconfig.Service, plugins *pluginpayment.Registry) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterPlugins(router, db, configs, New(db, time.Now), plugins)
	return router
}

func pluginCallbackDatabase(t *testing.T, name string) (*gorm.DB, paymentmodel.Order) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &channelrouter.Channel{}, &paymentmodel.Order{}, &paymentmodel.Attempt{}, &Record{}, &outbox.Message{}); err != nil {
		t.Fatal(err)
	}
	merchant := identitymodel.Merchant{ID: "merchant-1", DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	order := paymentmodel.Order{
		ID: "order-1", TradeNo: "T-ORDER-1", MerchantID: merchant.ID, OutTradeNo: "OUT-ORDER-1",
		PaymentType: "alipay", Name: "test", Amount: 100, Currency: "CNY", Status: paymentmodel.StatusPending,
		Kind: paymentmodel.KindSale, NotifyURL: "https://merchant.example/notify",
		ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	// The order went to channel-1. A callback from any other channel must be
	// refused, so the fixture records the attempt the gateway would have made.
	channelOne := "channel-1"
	attempt := paymentmodel.Attempt{
		ID: "attempt-1", OrderID: order.ID, ChannelID: &channelOne, AttemptNo: 1,
		Status: paymentmodel.AttemptSucceeded, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	return db, order
}

func pluginCallbackChannel(t *testing.T, db *gorm.DB, pluginCode string, status channelrouter.Status) *channelconfig.Service {
	t.Helper()
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	configs := channelconfig.NewService(cipher)
	encrypted, err := configs.Encrypt("channel-1", channelconfig.Config{Extra: map[string]string{"channel_marker": "marker"}})
	if err != nil {
		t.Fatal(err)
	}
	channel := channelrouter.Channel{ID: "channel-1", Name: pluginCode, PluginCode: pluginCode, PaymentType: "alipay", Weight: 1, Status: status, ConfigEncrypted: encrypted}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	return configs
}

func pluginCallbackEncrypt(t *testing.T, channelID, marker string) string {
	t.Helper()
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := channelconfig.NewService(cipher).Encrypt(channelID, channelconfig.Config{Extra: map[string]string{"channel_marker": marker}})
	if err != nil {
		t.Fatal(err)
	}
	return encrypted
}

func performCallback(t *testing.T, router http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertOrderPending(t *testing.T, db *gorm.DB, orderID string) {
	t.Helper()
	var order paymentmodel.Order
	if err := db.First(&order, "id = ?", orderID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != paymentmodel.StatusPending {
		t.Fatalf("order status=%s", order.Status)
	}
}

func readCallbackBody(t *testing.T, r *http.Request) string {
	t.Helper()
	if r.Body == nil {
		return ""
	}
	defer func() { _ = r.Body.Close() }()
	buffer := make([]byte, 4096)
	read, err := r.Body.Read(buffer)
	if err != nil && read == 0 {
		t.Fatalf("read body: %v", err)
	}
	return string(buffer[:read])
}
