package v2

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	channelrouter "gorixpay/internal/channel/router"
	"gorixpay/internal/compat/epay/signing"
	financemodel "gorixpay/internal/finance/model"
	financeservice "gorixpay/internal/finance/service"
	identitymodel "gorixpay/internal/identity/model"
	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	"gorixpay/internal/merchant/credential"
	paymentmodel "gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"
	"gorixpay/internal/transfer"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type stubStarter struct {
	result paymentorchestrator.Result
	err    error
}

func (starter stubStarter) Start(context.Context, string) (paymentorchestrator.Result, error) {
	return starter.result, starter.err
}

type testEnv struct {
	t           *testing.T
	router      *gin.Engine
	db          *gorm.DB
	credentials *credential.Service
	orders      *paymentservice.Service
	finance     *financeservice.Service
	transfers   *transfer.Service
	merchant    identitymodel.Merchant
	privateKey  *rsa.PrivateKey
	platformPEM string
	cipher      *secret.Cipher
	now         time.Time
}

func newTestEnv(t *testing.T, name string, starter PaymentStarter) *testEnv {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&identitymodel.Merchant{}, &identitymodel.MerchantCredential{}, &paymentmodel.Order{},
		&financemodel.Refund{}, &ledgermodel.Account{}, &ledgermodel.Balance{}, &ledgermodel.Transaction{},
		&ledgermodel.Entry{}, &ledgermodel.Freeze{}, &transfer.Order{}, &RequestNonce{}, &PlatformSetting{},
	); err != nil {
		t.Fatal(err)
	}
	uid := int64(1001)
	merchant := identitymodel.Merchant{ID: "merchant-1", LegacyUID: &uid, DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x51}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	credentials := credential.NewService(db, cipher, time.Now)
	if _, _, err := credentials.CreateMD5(context.Background(), merchant.ID); err != nil {
		t.Fatal(err)
	}
	_, privatePEM, err := credentials.CreateRSA(context.Background(), merchant.ID)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := signing.ParseRSAPrivateKeyPEM(privatePEM)
	if err != nil {
		t.Fatal(err)
	}
	ledger := ledgerservice.New(db, time.Now)
	now := time.Now().UTC()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := Register(router, Deps{
		DB:          db,
		Credentials: credentials,
		Orders:      paymentservice.New(db, func() time.Time { return now }),
		Finance:     financeservice.New(db, ledger, func() time.Time { return now }),
		Transfers:   transfer.New(db, ledger, func() time.Time { return now }),
		Starter:     starter,
		SiteURL:     "https://pay.example",
		Cipher:      cipher,
	}); err != nil {
		t.Fatal(err)
	}
	signer, err := LoadSigner(context.Background(), db, cipher)
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{
		t: t, router: router, db: db, credentials: credentials,
		orders:    paymentservice.New(db, func() time.Time { return now }),
		finance:   financeservice.New(db, ledger, func() time.Time { return now }),
		transfers: transfer.New(db, ledger, func() time.Time { return now }),
		merchant:  merchant, privateKey: privateKey, platformPEM: signer.PublicKeyPEM(), cipher: cipher, now: now,
	}
}

func (env *testEnv) signedForm(values map[string]string) url.Values {
	env.t.Helper()
	values["pid"] = "1001"
	if _, ok := values["timestamp"]; !ok {
		values["timestamp"] = strconv.FormatInt(env.now.Unix(), 10)
	}
	values["sign_type"] = "RSA"
	signature, err := SignValuesRSA(values, env.privateKey)
	if err != nil {
		env.t.Fatal(err)
	}
	values["sign"] = signature
	form := url.Values{}
	for name, value := range values {
		form.Set(name, value)
	}
	return form
}

func (env *testEnv) post(path string, form url.Values) *httptest.ResponseRecorder {
	env.t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	env.router.ServeHTTP(response, request)
	return response
}

func (env *testEnv) decode(response *httptest.ResponseRecorder) map[string]any {
	env.t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		env.t.Fatalf("decode %s: %v", response.Body.String(), err)
	}
	return payload
}

func (env *testEnv) createOrder(outTradeNo string, amount int64, status paymentmodel.Status) paymentmodel.Order {
	env.t.Helper()
	order, _, err := env.orders.Create(context.Background(), paymentservice.CreateCommand{
		MerchantID: env.merchant.ID, OutTradeNo: outTradeNo, PaymentType: "alipay", Name: "test order",
		Amount: amount, NotifyURL: "https://merchant.example/notify", ClientIP: "127.0.0.1",
	})
	if err != nil {
		env.t.Fatal(err)
	}
	if status != paymentmodel.StatusPending {
		paidAt := env.now
		updates := map[string]any{"status": status, "updated_at": env.now}
		if status == paymentmodel.StatusPaid {
			updates["paid_at"] = paidAt
		}
		if err := env.db.Model(&paymentmodel.Order{}).Where("id = ?", order.ID).Updates(updates).Error; err != nil {
			env.t.Fatal(err)
		}
		order.Status = status
		order.PaidAt = &paidAt
	}
	return order
}

func TestPayQueryReturnsVerifiableSignedResponse(t *testing.T) {
	env := newTestEnv(t, "v2-query-success", nil)
	order := env.createOrder("ORDER-QUERY", 100, paymentmodel.StatusPaid)
	response := env.post("/api/pay/query", env.signedForm(map[string]string{"trade_no": order.TradeNo}))
	if response.Code != http.StatusOK {
		t.Fatalf("status: %d %s", response.Code, response.Body.String())
	}
	payload := verifySignedResponse(t, response.Body.Bytes(), env.platformPEM)
	if payload["code"] != float64(0) {
		t.Fatalf("code: %v", payload["code"])
	}
	if payload["money"] != "1.00" || payload["status"] != float64(1) || payload["type"] != "alipay" || payload["pid"] != "1001" {
		t.Fatalf("payload: %v", payload)
	}
	if payload["sign_type"] != "RSA" || payload["timestamp"] == "" || payload["sign"] == "" {
		t.Fatalf("envelope: %v", payload)
	}
}

func TestPayQueryRejectsMissingOrStaleTimestamp(t *testing.T) {
	env := newTestEnv(t, "v2-query-timestamp", nil)
	order := env.createOrder("ORDER-TS", 100, paymentmodel.StatusPending)

	missing := env.signedForm(map[string]string{"trade_no": order.TradeNo})
	missing.Del("timestamp")
	payload := env.decode(env.post("/api/pay/query", missing))
	if payload["code"] != float64(-3) || !strings.Contains(payload["msg"].(string), "时间戳") {
		t.Fatalf("missing timestamp: %v", payload)
	}

	stale := env.signedForm(map[string]string{"trade_no": order.TradeNo, "timestamp": strconv.FormatInt(env.now.Add(-400*time.Second).Unix(), 10)})
	payload = env.decode(env.post("/api/pay/query", stale))
	if payload["code"] != float64(-3) || !strings.Contains(payload["msg"].(string), "时间戳") {
		t.Fatalf("stale timestamp: %v", payload)
	}
}

func TestPayQueryRejectsTamperedParameters(t *testing.T) {
	env := newTestEnv(t, "v2-query-tamper", nil)
	order := env.createOrder("ORDER-TAMPER", 100, paymentmodel.StatusPending)

	tampered := env.signedForm(map[string]string{"trade_no": order.TradeNo})
	tampered.Set("trade_no", order.TradeNo+"9")
	payload := env.decode(env.post("/api/pay/query", tampered))
	if payload["code"] != float64(-3) || !strings.Contains(payload["msg"].(string), "签名") {
		t.Fatalf("tampered trade_no: %v", payload)
	}

	wrongSignature := env.signedForm(map[string]string{"trade_no": order.TradeNo})
	wrongSignature.Set("sign", "not-a-signature")
	payload = env.decode(env.post("/api/pay/query", wrongSignature))
	if payload["code"] != float64(-3) {
		t.Fatalf("wrong signature: %v", payload)
	}

	passwordless := env.signedForm(map[string]string{"trade_no": order.TradeNo})
	passwordless.Set("sign_type", "MD5")
	payload = env.decode(env.post("/api/pay/query", passwordless))
	if payload["code"] != float64(-3) {
		t.Fatalf("md5 sign type: %v", payload)
	}
}

func TestPaySubmitRejectsInvalidAmounts(t *testing.T) {
	env := newTestEnv(t, "v2-submit-amount", nil)
	for _, money := range []string{"0", "0.00", "-1", "0.001", "100000.01", "1e3"} {
		form := env.signedForm(map[string]string{
			"out_trade_no": "ORDER-AMOUNT-" + money, "notify_url": "https://merchant.example/notify",
			"name": "test", "money": money,
		})
		payload := env.decode(env.post("/api/pay/submit", form))
		if payload["code"] != float64(-1) {
			t.Fatalf("money %q accepted: %v", money, payload)
		}
	}
}

func TestPaySubmitRedirectsToCashier(t *testing.T) {
	env := newTestEnv(t, "v2-submit-redirect", nil)
	form := env.signedForm(map[string]string{
		"out_trade_no": "ORDER-SUBMIT", "notify_url": "https://merchant.example/notify",
		"name": "test", "money": "1.00",
	})
	response := env.post("/api/pay/submit", form)
	if response.Code != http.StatusFound {
		t.Fatalf("status: %d %s", response.Code, response.Body.String())
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "https://pay.example/cashier.php?trade_no=") {
		t.Fatalf("location: %q", location)
	}
}

func TestPayCloseRejectsReplayedSignature(t *testing.T) {
	env := newTestEnv(t, "v2-close-replay", nil)
	order := env.createOrder("ORDER-CLOSE", 100, paymentmodel.StatusPending)
	form := env.signedForm(map[string]string{"trade_no": order.TradeNo})
	first := env.decode(env.post("/api/pay/close", form))
	if first["code"] != float64(0) {
		t.Fatalf("first close: %v", first)
	}
	second := env.decode(env.post("/api/pay/close", form))
	if second["code"] != float64(-3) || !strings.Contains(second["msg"].(string), "重复请求") {
		t.Fatalf("replayed close: %v", second)
	}
}

func TestPayCreateReportsMissingChannel(t *testing.T) {
	env := newTestEnv(t, "v2-create-nochannel", stubStarter{err: channelrouter.ErrNoAvailableChannel})
	form := env.signedForm(map[string]string{
		"out_trade_no": "ORDER-NOCHANNEL", "notify_url": "https://merchant.example/notify",
		"name": "test", "money": "1.00", "type": "alipay", "clientip": "127.0.0.1",
	})
	payload := env.decode(env.post("/api/pay/create", form))
	if payload["code"] != float64(-1) || payload["msg"] != "没有可用的支付通道" {
		t.Fatalf("payload: %v", payload)
	}
}

func TestUnknownMethodReturnsMinusFive(t *testing.T) {
	env := newTestEnv(t, "v2-unknown", nil)
	response := env.post("/api/pay/unknown", url.Values{"pid": {"1001"}})
	payload := env.decode(response)
	if payload["code"] != float64(-5) {
		t.Fatalf("payload: %v", payload)
	}
}

func verifySignedResponse(t *testing.T, body []byte, platformPEM string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	signature, ok := payload["sign"].(string)
	if !ok || signature == "" {
		t.Fatalf("response is not signed: %s", body)
	}
	publicKey, err := signing.ParseRSAPublicKeyPEM(platformPEM)
	if err != nil {
		t.Fatal(err)
	}
	if !signing.VerifyRSA(payload, publicKey, signature) {
		t.Fatalf("response signature did not verify: %s", body)
	}
	return payload
}

func TestWriteOrderErrorSurfacesPolicyRejection(t *testing.T) {
	env := newTestEnv(t, "v2-rejection", nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/pay/create", nil)
	service := New(Deps{DB: env.db, Cipher: env.cipher})
	service.writeOrderError(&request{ctx: ctx, method: "pay/create"}, "ORDER-1", paymentservice.Rejection{Reason: "请先完成实名认证"})
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != float64(-1) || payload["msg"] != "请先完成实名认证" {
		t.Fatalf("payload: %v", payload)
	}
}
