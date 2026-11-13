package cashier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	channelrouter "gorixpay/internal/channel/router"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/outbox"
	paymentcallback "gorixpay/internal/payment/callback"
	paymentmodel "gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	pluginpayment "gorixpay/internal/plugin/payment"
	"gorixpay/internal/plugin/payment/sandbox"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type countingStarter struct {
	inner *paymentorchestrator.Service
	calls atomic.Int32
}

func (starter *countingStarter) Start(ctx context.Context, tradeNo string) (paymentorchestrator.Result, error) {
	starter.calls.Add(1)
	return starter.inner.Start(ctx, tradeNo)
}

type denyLimiter struct{}

func (denyLimiter) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return false, nil
}

type recordingLimiter struct {
	keys []string
}

func (limiter *recordingLimiter) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, error) {
	limiter.keys = append(limiter.keys, key)
	return true, nil
}

func (limiter *recordingLimiter) hasKey(key string) bool {
	for _, recorded := range limiter.keys {
		if recorded == key {
			return true
		}
	}
	return false
}

type testEnv struct {
	router  *gin.Engine
	db      *gorm.DB
	deps    Deps
	order   paymentmodel.Order
	starter *countingStarter
}

func TestStartReturnsJumpActionAndPersistsAttempt(t *testing.T) {
	env := newTestEnv(t, "cashier-start", true)
	response := env.post("/api/v1/cashier/orders/" + env.order.TradeNo + "/start")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		PayType string `json:"pay_type"`
		PayInfo string `json:"pay_info"`
		Channel struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			PluginCode string `json:"plugin_code"`
		} `json:"channel"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PayType != "jump" || !strings.HasPrefix(payload.PayInfo, "https://gorix.example/pay/sandbox/") {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.Channel.PluginCode != "sandbox" || payload.Channel.ID != "channel-1" || payload.Channel.Name != "sandbox" {
		t.Fatalf("channel=%+v", payload.Channel)
	}
	if payload.Status != "processing" {
		t.Fatalf("status=%q", payload.Status)
	}
	var attempt paymentmodel.Attempt
	if err := env.db.Where("order_id = ?", env.order.ID).First(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if attempt.Status != paymentmodel.AttemptSucceeded || attempt.ActionType != "jump" || attempt.ActionPayload == "" {
		t.Fatalf("attempt=%+v", attempt)
	}
	var order paymentmodel.Order
	if err := env.db.First(&order, "id = ?", env.order.ID).Error; err != nil || order.Status != paymentmodel.StatusProcessing {
		t.Fatalf("order=%+v err=%v", order, err)
	}
}

func TestStartReplaysStoredActionWithoutCallingOrchestrator(t *testing.T) {
	env := newTestEnv(t, "cashier-replay", true)
	if response := env.post("/api/v1/cashier/orders/" + env.order.TradeNo + "/start"); response.Code != http.StatusOK {
		t.Fatalf("first start status=%d body=%s", response.Code, response.Body.String())
	}
	response := env.post("/api/v1/cashier/orders/" + env.order.TradeNo + "/start")
	if response.Code != http.StatusOK {
		t.Fatalf("second start status=%d body=%s", response.Code, response.Body.String())
	}
	if env.starter.calls.Load() != 1 {
		t.Fatalf("orchestrator calls=%d", env.starter.calls.Load())
	}
	var count int64
	if err := env.db.Model(&paymentmodel.Attempt{}).Where("order_id = ?", env.order.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("attempt count=%d err=%v", count, err)
	}
	if !strings.Contains(response.Body.String(), "\"pay_type\":\"jump\"") {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestStartRejectsPaidClosedAndExpiredOrders(t *testing.T) {
	env := newTestEnv(t, "cashier-states", true)
	if err := env.db.Model(&paymentmodel.Order{}).Where("id = ?", env.order.ID).Update("status", paymentmodel.StatusPaid).Error; err != nil {
		t.Fatal(err)
	}
	if response := env.post("/api/v1/cashier/orders/" + env.order.TradeNo + "/start"); response.Code != http.StatusConflict {
		t.Fatalf("paid status=%d body=%s", response.Code, response.Body.String())
	}
	if err := env.db.Model(&paymentmodel.Order{}).Where("id = ?", env.order.ID).Update("status", paymentmodel.StatusClosed).Error; err != nil {
		t.Fatal(err)
	}
	if response := env.post("/api/v1/cashier/orders/" + env.order.TradeNo + "/start"); response.Code != http.StatusConflict {
		t.Fatalf("closed status=%d body=%s", response.Code, response.Body.String())
	}
	if err := env.db.Model(&paymentmodel.Order{}).Where("id = ?", env.order.ID).
		Updates(map[string]any{"status": paymentmodel.StatusPending, "expires_at": time.Now().Add(-time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	if response := env.post("/api/v1/cashier/orders/" + env.order.TradeNo + "/start"); response.Code != http.StatusGone {
		t.Fatalf("expired status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestStartReturnsNotFoundAndNoChannel(t *testing.T) {
	env := newTestEnv(t, "cashier-not-found", true)
	if response := env.post("/api/v1/cashier/orders/missing/start"); response.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", response.Code, response.Body.String())
	}
	if err := env.db.Model(&channelrouter.Channel{}).Where("id = ?", "channel-1").Update("status", channelrouter.StatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	response := env.post("/api/v1/cashier/orders/" + env.order.TradeNo + "/start")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("no channel status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "\"code\":\"no_channel\"") {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestStartHonoursLimiter(t *testing.T) {
	env := newTestEnv(t, "cashier-limiter", true)
	limiter := &recordingLimiter{}
	env.replaceDeps(func(deps *Deps) { deps.Limiter = limiter })
	if response := env.post("/api/v1/cashier/orders/" + env.order.TradeNo + "/start"); response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !limiter.hasKey("cashier:start:order:" + env.order.TradeNo) {
		t.Fatalf("order limiter key missing: %v", limiter.keys)
	}
	if !limiter.hasKey("cashier:start:ip:192.0.2.1") {
		t.Fatalf("ip limiter key missing: %v", limiter.keys)
	}
	limited := newTestEnv(t, "cashier-limiter-denied", true)
	limited.replaceDeps(func(deps *Deps) { deps.Limiter = denyLimiter{} })
	if response := limited.post("/api/v1/cashier/orders/" + limited.order.TradeNo + "/start"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("denied status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestStatusHonoursIPLimiter(t *testing.T) {
	env := newTestEnv(t, "cashier-status-limiter", true)
	limiter := &recordingLimiter{}
	env.replaceDeps(func(deps *Deps) { deps.Limiter = limiter })
	if response := env.get("/api/v1/cashier/orders/" + env.order.TradeNo + "/status"); response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !limiter.hasKey("cashier:status:ip:192.0.2.1") {
		t.Fatalf("status limiter key missing: %v", limiter.keys)
	}
	denied := newTestEnv(t, "cashier-status-limiter-denied", true)
	denied.replaceDeps(func(deps *Deps) { deps.Limiter = denyLimiter{} })
	if response := denied.get("/api/v1/cashier/orders/" + denied.order.TradeNo + "/status"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("denied status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestStatusReportsOrderState(t *testing.T) {
	env := newTestEnv(t, "cashier-status", true)
	response := env.get("/api/v1/cashier/orders/" + env.order.TradeNo + "/status")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "\"status\":\"pending\"") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response := env.get("/api/v1/cashier/orders/missing/status"); response.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d", response.Code)
	}
}

func TestSandboxCompleteMarksOrderPaidIdempotently(t *testing.T) {
	env := newTestEnv(t, "cashier-sandbox", true)
	if response := env.post("/api/v1/cashier/orders/" + env.order.TradeNo + "/start"); response.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", response.Code, response.Body.String())
	}
	response := env.post("/api/v1/sandbox/orders/" + env.order.TradeNo + "/complete")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "\"status\":\"paid\"") {
		t.Fatalf("complete status=%d body=%s", response.Code, response.Body.String())
	}
	var order paymentmodel.Order
	if err := env.db.First(&order, "id = ?", env.order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != paymentmodel.StatusPaid || order.PaidAt == nil || order.ChannelTradeNo == nil || *order.ChannelTradeNo != "SANDBOX"+env.order.TradeNo {
		t.Fatalf("order=%+v", order)
	}
	var messages int64
	if err := env.db.Model(&outbox.Message{}).Where("aggregate_id = ?", order.ID).Count(&messages).Error; err != nil || messages != 1 {
		t.Fatalf("outbox messages=%d err=%v", messages, err)
	}
	if response := env.post("/api/v1/sandbox/orders/" + env.order.TradeNo + "/complete"); response.Code != http.StatusOK {
		t.Fatalf("repeat complete status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSandboxCompleteRequiresStartedAttempt(t *testing.T) {
	env := newTestEnv(t, "cashier-sandbox-unstarted", true)
	response := env.post("/api/v1/sandbox/orders/" + env.order.TradeNo + "/complete")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "\"code\":\"no_channel\"") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSandboxCompleteRejectsOrdersRoutedToRealChannel(t *testing.T) {
	env := newTestEnv(t, "cashier-sandbox-foreign", true)
	now := time.Now().UTC()
	epayChannel := channelrouter.Channel{ID: "channel-epay", Name: "epay", PluginCode: "epay", PaymentType: "alipay", Priority: 1, Weight: 100, Status: channelrouter.StatusActive, CreatedAt: now, UpdatedAt: now}
	if err := env.db.Create(&epayChannel).Error; err != nil {
		t.Fatal(err)
	}
	channelID := epayChannel.ID
	attempt := paymentmodel.Attempt{
		ID: "attempt-epay", OrderID: env.order.ID, ChannelID: &channelID, AttemptNo: 1,
		Status: paymentmodel.AttemptSucceeded, ActionType: "jump", ActionPayload: `{"Type":"jump","URL":"https://upstream.example/pay"}`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := env.db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	response := env.post("/api/v1/sandbox/orders/" + env.order.TradeNo + "/complete")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "\"code\":\"order_not_payable\"") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var order paymentmodel.Order
	if err := env.db.First(&order, "id = ?", env.order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != paymentmodel.StatusPending || order.PaidAt != nil {
		t.Fatalf("order mutated: %+v", order)
	}
	var callbacks int64
	if err := env.db.Model(&paymentcallback.Record{}).Count(&callbacks).Error; err != nil || callbacks != 0 {
		t.Fatalf("callbacks=%d err=%v", callbacks, err)
	}
}

func TestSandboxRoutesAbsentWhenDisabled(t *testing.T) {
	env := newTestEnv(t, "cashier-sandbox-disabled", false)
	if response := env.post("/api/v1/sandbox/orders/" + env.order.TradeNo + "/complete"); response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSandboxCompleteIgnoresRequestAmount(t *testing.T) {
	env := newTestEnv(t, "cashier-sandbox-amount", true)
	if response := env.post("/api/v1/cashier/orders/" + env.order.TradeNo + "/start"); response.Code != http.StatusOK {
		t.Fatalf("start status=%d", response.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sandbox/orders/"+env.order.TradeNo+"/complete", strings.NewReader(`{"amount":1}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	env.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var order paymentmodel.Order
	if err := env.db.First(&order, "id = ?", env.order.ID).Error; err != nil || order.Status != paymentmodel.StatusPaid {
		t.Fatalf("order=%+v err=%v", order, err)
	}
	if order.Amount != env.order.Amount {
		t.Fatalf("order amount mutated: %d", order.Amount)
	}
}

func newTestEnv(t *testing.T, name string, sandboxEnabled bool) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &paymentmodel.Order{}, &paymentmodel.Attempt{}, &channelrouter.Channel{}, &paymentcallback.Record{}, &outbox.Message{}); err != nil {
		t.Fatal(err)
	}
	merchant := identitymodel.Merchant{ID: "merchant-1", DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now
	order, _, err := paymentservice.New(db, now).Create(context.Background(), paymentservice.CreateCommand{
		MerchantID: merchant.ID, OutTradeNo: "ORDER-1", PaymentType: "alipay", Name: "test", Amount: 1999,
		NotifyURL: "https://merchant.example/notify",
	})
	if err != nil {
		t.Fatal(err)
	}
	channel := channelrouter.Channel{ID: "channel-1", Name: "sandbox", PluginCode: "sandbox", PaymentType: "alipay", Priority: 1, Weight: 100, Status: channelrouter.StatusActive, CreatedAt: now().UTC(), UpdatedAt: now().UTC()}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(sandbox.New()); err != nil {
		t.Fatal(err)
	}
	starter := &countingStarter{inner: paymentorchestrator.NewConfigured(db, channelrouter.New(db, registry, func(int) (int, error) { return 0, nil }), now, nil, "https://gorix.example")}
	env := &testEnv{
		db:      db,
		order:   order,
		starter: starter,
		deps:    Deps{DB: db, Orders: paymentservice.New(db, now), Starter: starter, Callbacks: paymentcallback.New(db, now), SandboxEnabled: sandboxEnabled},
	}
	env.mount()
	return env
}

func (env *testEnv) mount() {
	router := gin.New()
	Register(router, env.deps)
	env.router = router
}

func (env *testEnv) replaceDeps(apply func(*Deps)) {
	apply(&env.deps)
	env.mount()
}

func (env *testEnv) post(path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	env.router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
	return response
}

func (env *testEnv) get(path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	env.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}
