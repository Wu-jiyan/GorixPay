package merchanttools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	identitymodel "gorixpay/internal/identity/model"
	paymentservice "gorixpay/internal/payment/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const principalContextKey = "identity_principal"

func merchantToolsRouter(t *testing.T, db *gorm.DB, publicURL string, orders *paymentservice.Service, starter PaymentStarter) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router, Deps{DB: db, Sessions: stubSessions(), PublicURL: publicURL, Orders: orders, Starter: starter})
	return router
}

func TestMerchantPayCodeEndpoint(t *testing.T) {
	_, db, starter := merchantToolsFixture(t, "merchanttools-http-paycode")
	router := merchantToolsRouter(t, db, "https://pay.example", paymentservice.New(db, nil), starter)

	response := perform(router, http.MethodGet, "/api/v1/merchant/pay-code?payment_type=alipay&amount_cents=100", "", "merchant-1")
	if response.Code != http.StatusOK {
		t.Fatalf("pay code: %d %s", response.Code, response.Body.String())
	}
	var code PayCode
	if err := json.Unmarshal(response.Body.Bytes(), &code); err != nil {
		t.Fatal(err)
	}
	if code.URL != "https://pay.example/pay/code/alipay?amount=100&pid=1001" {
		t.Fatalf("url=%q", code.URL)
	}
	if !strings.Contains(code.CodeSVG, "<svg") {
		t.Fatal("svg missing")
	}
	if code.ExpiresAt.IsZero() {
		t.Fatal("expires_at is zero")
	}

	// Merchant isolation: the session decides whose pid ends up in the code.
	other := perform(router, http.MethodGet, "/api/v1/merchant/pay-code?payment_type=alipay", "", "merchant-2")
	if other.Code != http.StatusOK {
		t.Fatalf("other merchant: %d %s", other.Code, other.Body.String())
	}
	if !strings.Contains(other.Body.String(), "pid=2002") || strings.Contains(other.Body.String(), "pid=1001") {
		t.Fatalf("other merchant body=%s", other.Body.String())
	}
	if strings.Contains(other.Body.String(), "amount=") {
		t.Fatalf("open code carries an amount: %s", other.Body.String())
	}

	if response := perform(router, http.MethodGet, "/api/v1/merchant/pay-code?payment_type=alipay&amount_cents=abc", "", "merchant-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("bad amount: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodGet, "/api/v1/merchant/pay-code?payment_type=Ali%20Pay", "", "merchant-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("bad type: %d %s", response.Code, response.Body.String())
	}

	unconfigured := merchantToolsRouter(t, db, "", paymentservice.New(db, nil), starter)
	response = perform(unconfigured, http.MethodGet, "/api/v1/merchant/pay-code?payment_type=alipay", "", "merchant-1")
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "site_url_missing") {
		t.Fatalf("missing public url: %d %s", response.Code, response.Body.String())
	}
}

func TestMerchantTestOrderEndpoint(t *testing.T) {
	_, db, starter := merchantToolsFixture(t, "merchanttools-http-testorder")
	router := merchantToolsRouter(t, db, "https://pay.example", paymentservice.New(db, nil), starter)

	response := perform(router, http.MethodPost, "/api/v1/merchant/test-order", `{"payment_type":"alipay","amount_cents":100}`, "merchant-1")
	if response.Code != http.StatusCreated {
		t.Fatalf("test order: %d %s", response.Code, response.Body.String())
	}
	var order TestOrder
	if err := json.Unmarshal(response.Body.Bytes(), &order); err != nil {
		t.Fatal(err)
	}
	if order.TradeNo == "" || order.PayType != "qrcode" || order.PayInfo != "https://qr.example/pay" ||
		!strings.HasPrefix(order.CashierURL, "https://pay.example/cashier.php?trade_no=") {
		t.Fatalf("order=%+v", order)
	}
	if response := perform(router, http.MethodPost, "/api/v1/merchant/test-order", `{"payment_type":"alipay","amount_cents":101}`, "merchant-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("over cap: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPost, "/api/v1/merchant/test-order", `{"amount_cents":50}`, "merchant-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("missing type: %d %s", response.Code, response.Body.String())
	}

	unwired := merchantToolsRouter(t, db, "https://pay.example", nil, nil)
	response = perform(unwired, http.MethodPost, "/api/v1/merchant/test-order", `{"payment_type":"alipay","amount_cents":50}`, "merchant-1")
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "service_unavailable") {
		t.Fatalf("unwired: %d %s", response.Code, response.Body.String())
	}
}

func TestPublicPayCodePageAndCheckout(t *testing.T) {
	_, db, starter := merchantToolsFixture(t, "merchanttools-http-page")
	router := merchantToolsRouter(t, db, "https://pay.example", paymentservice.New(db, nil), starter)

	response := perform(router, http.MethodGet, "/pay/code/alipay?pid=1001&amount=100", "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `<form method="post" action="/pay/code/alipay">`) {
		t.Fatalf("page: %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `value="100"`) {
		t.Fatalf("amount not prefilled: %s", response.Body.String())
	}
	if response := perform(router, http.MethodGet, "/pay/code/alipay", "", ""); response.Code != http.StatusNotFound {
		t.Fatalf("page without pid: %d %s", response.Code, response.Body.String())
	}

	form := url.Values{"pid": {"1001"}, "amount": {"100"}}
	request := httptest.NewRequest(http.MethodPost, "/pay/code/alipay", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusFound {
		t.Fatalf("checkout: %d %s", recorder.Code, recorder.Body.String())
	}
	location := recorder.Header().Get("Location")
	if !strings.HasPrefix(location, "https://pay.example/cashier.php?trade_no=") {
		t.Fatalf("location=%q", location)
	}
	if len(starter.calls) != 1 {
		t.Fatalf("starter calls=%v", starter.calls)
	}

	form.Set("amount", "0")
	request = httptest.NewRequest(http.MethodPost, "/pay/code/alipay", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("zero amount: %d %s", recorder.Code, recorder.Body.String())
	}

	// The acknowledgement endpoint keeps the platform notification quiet.
	if response := perform(router, http.MethodGet, "/pay/code/ack", "", ""); response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "success" {
		t.Fatalf("ack: %d %s", response.Code, response.Body.String())
	}
}

func stubSessions() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		principalID := strings.TrimSpace(ctx.GetHeader("X-Principal"))
		if principalID == "" {
			principalID = "merchant-1"
		}
		ctx.Set(principalContextKey, identitymodel.Session{PrincipalType: identitymodel.PrincipalMerchant, PrincipalID: principalID})
		ctx.Next()
	}
}

func perform(router http.Handler, method, path, body, principal string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if principal != "" {
		request.Header.Set("X-Principal", principal)
	}
	router.ServeHTTP(response, request)
	return response
}
