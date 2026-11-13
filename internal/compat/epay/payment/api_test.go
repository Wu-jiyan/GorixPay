package payment

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

	epaysigning "gorixpay/internal/compat/epay/signing"
	epayv2 "gorixpay/internal/compat/epay/v2"
	financemodel "gorixpay/internal/finance/model"
	financeservice "gorixpay/internal/finance/service"
	identitymodel "gorixpay/internal/identity/model"
	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	"gorixpay/internal/merchant/credential"
	paymentmodel "gorixpay/internal/payment/model"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type apiTestEnv struct {
	db          *gorm.DB
	router      *gin.Engine
	key         string
	privateKey  *rsa.PrivateKey
	platformPEM string
	merchant    identitymodel.Merchant
	now         time.Time
}

func newAPITestEnv(t *testing.T, name string) *apiTestEnv {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&identitymodel.Merchant{}, &identitymodel.MerchantCredential{}, &paymentmodel.Order{},
		&financemodel.Refund{}, &financemodel.Settlement{}, &ledgermodel.Account{}, &ledgermodel.Balance{},
		&ledgermodel.Transaction{}, &ledgermodel.Entry{}, &ledgermodel.Freeze{},
		&epayv2.PlatformSetting{}, &epayv2.RequestNonce{},
	); err != nil {
		t.Fatal(err)
	}
	uid := int64(1001)
	merchant := identitymodel.Merchant{ID: "merchant-1", LegacyUID: &uid, DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x27}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	credentials := credential.NewService(db, cipher, time.Now)
	_, key, err := credentials.CreateMD5(context.Background(), merchant.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, privatePEM, err := credentials.CreateRSA(context.Background(), merchant.ID)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := epaysigning.ParseRSAPrivateKeyPEM(privatePEM)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ledger := ledgerservice.New(db, func() time.Time { return now })
	orders := paymentservice.New(db, func() time.Time { return now })
	finance := financeservice.New(db, ledger, func() time.Time { return now })
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := epayv2.Register(router, epayv2.Deps{
		DB: db, Credentials: credentials, Orders: orders, Finance: finance,
		SiteURL: "https://pay.example", Cipher: cipher,
	}); err != nil {
		t.Fatal(err)
	}
	RegisterAPI(router, db, credentials, orders, finance)
	signer, err := epayv2.LoadSigner(context.Background(), db, cipher)
	if err != nil {
		t.Fatal(err)
	}
	return &apiTestEnv{db: db, router: router, key: key, privateKey: privateKey, platformPEM: signer.PublicKeyPEM(), merchant: merchant, now: now}
}

func (env *apiTestEnv) createPaidOrder(t *testing.T, outTradeNo string, amount int64) paymentmodel.Order {
	t.Helper()
	order := paymentmodel.Order{
		ID: "order-" + outTradeNo, TradeNo: "T" + outTradeNo, MerchantID: env.merchant.ID, OutTradeNo: outTradeNo,
		PaymentType: "alipay", Name: "test order", Amount: amount, Currency: "CNY", Status: paymentmodel.StatusPaid,
		NotifyURL: "https://merchant.example/notify", PaidAt: &env.now, ExpiresAt: env.now.Add(time.Hour),
		CreatedAt: env.now.Add(-time.Minute), UpdatedAt: env.now,
	}
	if err := env.db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	return order
}

func (env *apiTestEnv) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	env.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func (env *apiTestEnv) decode(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %s: %v", response.Body.String(), err)
	}
	return payload
}

func TestAPIQueryReturnsMerchantAccount(t *testing.T) {
	env := newAPITestEnv(t, "api-query")
	payload := env.decode(t, env.get(t, "/api.php?act=query&pid=1001&key="+env.key))
	if payload["code"] != float64(1) || payload["money"] != "0.00" || payload["active"] != float64(1) || payload["pid"] != float64(1001) {
		t.Fatalf("payload: %v", payload)
	}
	for _, field := range []string{"orders", "orders_today", "orders_lastday"} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("missing field %s: %v", field, payload)
		}
	}
}

func TestAPIQueryRejectsWrongKeyAndUnknownMerchant(t *testing.T) {
	env := newAPITestEnv(t, "api-query-bad")
	payload := env.decode(t, env.get(t, "/api.php?act=query&pid=1001&key=wrong"))
	if payload["code"] != float64(-3) || !strings.Contains(payload["msg"].(string), "密钥") {
		t.Fatalf("wrong key: %v", payload)
	}
	payload = env.decode(t, env.get(t, "/api.php?act=query&pid=9999&key="+env.key))
	if payload["code"] != float64(-3) || !strings.Contains(payload["msg"].(string), "商户ID") {
		t.Fatalf("unknown merchant: %v", payload)
	}
}

func TestAPIOrderReturnsPaidOrderFields(t *testing.T) {
	env := newAPITestEnv(t, "api-order")
	order := env.createPaidOrder(t, "ORDER-1", 100)
	payload := env.decode(t, env.get(t, "/api.php?act=order&pid=1001&key="+env.key+"&trade_no="+order.TradeNo))
	if payload["code"] != float64(1) || payload["trade_no"] != order.TradeNo || payload["out_trade_no"] != "ORDER-1" {
		t.Fatalf("payload: %v", payload)
	}
	if payload["type"] != "alipay" || payload["money"] != "1.00" || payload["status"] != float64(1) || payload["pid"] != float64(1001) {
		t.Fatalf("payload: %v", payload)
	}
	for _, field := range []string{"name", "addtime", "endtime", "param", "buyer"} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("missing field %s: %v", field, payload)
		}
	}
}

func TestAPIOrderMissingNumberAndUnknownOrder(t *testing.T) {
	env := newAPITestEnv(t, "api-order-errors")
	payload := env.decode(t, env.get(t, "/api.php?act=order&pid=1001&key="+env.key))
	if payload["code"] != float64(-4) {
		t.Fatalf("missing number: %v", payload)
	}
	payload = env.decode(t, env.get(t, "/api.php?act=order&pid=1001&key="+env.key+"&out_trade_no=missing"))
	if payload["code"] != float64(-1) || !strings.Contains(payload["msg"].(string), "不存在") {
		t.Fatalf("unknown order: %v", payload)
	}
}

func TestAPIOrdersPaginates(t *testing.T) {
	env := newAPITestEnv(t, "api-orders")
	env.createPaidOrder(t, "ORDER-A", 100)
	env.createPaidOrder(t, "ORDER-B", 200)
	env.createPaidOrder(t, "ORDER-C", 300)
	payload := env.decode(t, env.get(t, "/api.php?act=orders&pid=1001&key="+env.key))
	if payload["code"] != float64(1) || payload["count"] != float64(3) {
		t.Fatalf("payload: %v", payload)
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 3 {
		t.Fatalf("data: %v", payload["data"])
	}
	payload = env.decode(t, env.get(t, "/api.php?act=orders&pid=1001&key="+env.key+"&limit=1&offset=1"))
	if payload["count"] != float64(3) || len(payload["data"].([]any)) != 1 {
		t.Fatalf("paged payload: %v", payload)
	}
}

func TestAPIRefundCreatesFinanceRefund(t *testing.T) {
	env := newAPITestEnv(t, "api-refund")
	order := env.createPaidOrder(t, "ORDER-REFUND", 100)
	form := url.Values{"pid": {"1001"}, "key": {env.key}, "trade_no": {order.TradeNo}, "money": {"0.50"}}
	response := performForm(env.router, http.MethodPost, "/api.php?act=refund", form)
	payload := env.decode(t, response)
	if payload["code"] != float64(0) || !strings.Contains(payload["msg"].(string), "退款成功") || payload["money"] != "0.50" {
		t.Fatalf("payload: %v", payload)
	}
	var refund financemodel.Refund
	if err := env.db.First(&refund).Error; err != nil || refund.Amount != 50 {
		t.Fatalf("refund row: %+v err=%v", refund, err)
	}
	// Repeating the same out_refund_no is idempotent and returns code 0.
	form.Set("out_refund_no", refund.OutRefundNo)
	replay := env.decode(t, performForm(env.router, http.MethodPost, "/api.php?act=refund", form))
	if replay["code"] != float64(0) || replay["refund_no"] != refund.RefundNo {
		t.Fatalf("idempotent refund: %v", replay)
	}
	var count int64
	if err := env.db.Model(&financemodel.Refund{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("refund count=%d err=%v", count, err)
	}
}

func TestAPIRefundErrorCodes(t *testing.T) {
	env := newAPITestEnv(t, "api-refund-errors")
	order := env.createPaidOrder(t, "ORDER-REFUND-ERR", 100)
	cases := []struct {
		name string
		form url.Values
		code float64
	}{
		{"wrong-key", url.Values{"pid": {"1001"}, "key": {"nope"}, "trade_no": {order.TradeNo}, "money": {"0.50"}}, -3},
		{"missing-number", url.Values{"pid": {"1001"}, "key": {env.key}, "money": {"0.50"}}, -4},
		{"bad-money", url.Values{"pid": {"1001"}, "key": {env.key}, "trade_no": {order.TradeNo}, "money": {"0.001"}}, -1},
		{"unknown-order", url.Values{"pid": {"1001"}, "key": {env.key}, "trade_no": {"missing"}, "money": {"0.50"}}, -1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := env.decode(t, performForm(env.router, http.MethodPost, "/api.php?act=refund", testCase.form))
			if payload["code"] != testCase.code {
				t.Fatalf("code = %v, want %v (%v)", payload["code"], testCase.code, payload)
			}
		})
	}
}

func TestAPIUnknownActReturnsMinusFive(t *testing.T) {
	env := newAPITestEnv(t, "api-unknown-act")
	payload := env.decode(t, env.get(t, "/api.php?act=nope&pid=1001&key="+env.key))
	if payload["code"] != float64(-5) {
		t.Fatalf("payload: %v", payload)
	}
}

func TestAPIPHPSuffixDispatchesV2Query(t *testing.T) {
	env := newAPITestEnv(t, "api-v2-suffix")
	order := env.createPaidOrder(t, "ORDER-V2", 100)
	values := map[string]string{
		"pid": "1001", "trade_no": order.TradeNo, "timestamp": strconv.FormatInt(time.Now().Unix(), 10), "sign_type": "RSA",
	}
	signature, err := epayv2.SignValuesRSA(values, env.privateKey)
	if err != nil {
		t.Fatal(err)
	}
	values["sign"] = signature
	// ApiHelper.php:47-51 reads the V2 parameters from $_POST only, so they travel
	// in the form body while the query string carries nothing but the s= selector.
	form := url.Values{}
	for name, value := range values {
		form.Set(name, value)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api.php?s=pay/query", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	env.router.ServeHTTP(response, request)
	payload := env.decode(t, response)
	if payload["code"] != float64(0) || payload["money"] != "1.00" || payload["status"] != float64(1) {
		t.Fatalf("payload: %v", payload)
	}
	publicKey, err := epaysigning.ParseRSAPublicKeyPEM(env.platformPEM)
	if err != nil {
		t.Fatal(err)
	}
	sign, ok := payload["sign"].(string)
	if !ok || !epaysigning.VerifyRSA(payload, publicKey, sign) {
		t.Fatalf("V2 response signature did not verify: %v", payload)
	}
}

func TestHandleOrderErrorSurfacesPolicyRejection(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mapi.php", nil)
	handleOrderError(ctx, "ORDER-1", paymentservice.Rejection{Reason: "当前支付域名未授权"}, false)
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != float64(-1) || payload["msg"] != "当前支付域名未授权" {
		t.Fatalf("payload: %v", payload)
	}
}
