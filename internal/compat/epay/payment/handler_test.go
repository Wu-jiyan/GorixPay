package payment

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	epaysigning "gorixpay/internal/compat/epay/signing"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/merchant/credential"
	paymentmodel "gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestMAPIValidRequestCreatesOrderAndReturnsJumpAction(t *testing.T) {
	router, key, db := testRouter(t, "mapi-success")
	form := validForm(key)
	response := performForm(router, http.MethodPost, "/mapi.php", form)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"code":1`) ||
		!strings.Contains(response.Body.String(), `"pay_type":"jump"`) || !strings.Contains(response.Body.String(), `"trade_no":"`) {
		t.Fatalf("response: %d %s", response.Code, response.Body.String())
	}
	var count int64
	if err := db.Model(&paymentmodel.Order{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("order count=%d err=%v", count, err)
	}
}

type starterFunc func(context.Context, string) (paymentorchestrator.Result, error)

func (function starterFunc) Start(ctx context.Context, tradeNo string) (paymentorchestrator.Result, error) {
	return function(ctx, tradeNo)
}

func TestMAPIUsesPluginPaymentAction(t *testing.T) {
	var startedTradeNo string
	starter := starterFunc(func(_ context.Context, tradeNo string) (paymentorchestrator.Result, error) {
		startedTradeNo = tradeNo
		return paymentorchestrator.Result{Action: pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: "https://channel.example/code"}}, nil
	})
	router, key, _ := testRouterWithStarter(t, "mapi-plugin", starter)
	response := performForm(router, http.MethodPost, "/mapi.php", validForm(key))
	if response.Code != http.StatusOK || startedTradeNo == "" || !strings.Contains(response.Body.String(), `"pay_type":"qrcode"`) ||
		!strings.Contains(response.Body.String(), `"pay_info":"https://channel.example/code"`) {
		t.Fatalf("response: %d %s trade_no=%q", response.Code, response.Body.String(), startedTradeNo)
	}
}

func TestMAPIHandlesLegacyMoneyPrecisionAndDuplicatePayloadConflict(t *testing.T) {
	router, key, _ := testRouter(t, "mapi-invalid")
	legacyPrecision := validForm(key)
	legacyPrecision.Set("money", "1.001")
	resign(legacyPrecision, key)
	response := performForm(router, http.MethodPost, "/mapi.php", legacyPrecision)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"code":1`) {
		t.Fatalf("legacy precision: %d %s", response.Code, response.Body.String())
	}
	valid := legacyPrecision
	valid.Set("money", "2.00")
	resign(valid, key)
	response = performForm(router, http.MethodPost, "/mapi.php", valid)
	if !strings.Contains(response.Body.String(), `"code":-1`) || !strings.Contains(response.Body.String(), "支付参数有变化") {
		t.Fatalf("conflict: %d %s", response.Code, response.Body.String())
	}
}

func TestSubmitSupportsGETAndReturnsCashierRedirectHTML(t *testing.T) {
	router, key, _ := testRouter(t, "submit-success")
	form := validForm(key)
	form.Del("clientip")
	resign(form, key)
	request := httptest.NewRequest(http.MethodGet, "/submit.php?"+form.Encode(), nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(response.Body.String(), "window.location.replace") || !strings.Contains(response.Body.String(), "/cashier.php?trade_no=") {
		t.Fatalf("response: %d %s", response.Code, response.Body.String())
	}
}

func TestParseAmountToCents(t *testing.T) {
	for input, expected := range map[string]int64{"1": 100, "1.0": 100, "1.00": 100, "1.001": 100, "1.005": 101, "0.01": 1, "99999999.99": 9999999999} {
		actual, err := ParseAmount(input)
		if err != nil || actual != expected {
			t.Fatalf("%s => %d err=%v", input, actual, err)
		}
	}
	for _, input := range []string{"", "0", "-1", ".", "1e2", "abc"} {
		if _, err := ParseAmount(input); err == nil {
			t.Fatalf("invalid amount accepted: %q", input)
		}
	}
}

func testRouter(t *testing.T, name string) (*gin.Engine, string, *gorm.DB) {
	return testRouterWithStarter(t, name, nil)
}

func testRouterWithStarter(t *testing.T, name string, starter PaymentStarter) (*gin.Engine, string, *gorm.DB) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &identitymodel.MerchantCredential{}, &paymentmodel.Order{}); err != nil {
		t.Fatal(err)
	}
	uid := int64(1001)
	merchant := identitymodel.Merchant{ID: "merchant-1", LegacyUID: &uid, DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x42}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	credentials := credential.NewService(db, cipher, time.Now)
	_, key, err := credentials.CreateMD5(context.Background(), merchant.ID)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router, db, credentials, paymentservice.New(db, time.Now), "https://pay.example", starter)
	return router, key, db
}

func validForm(key string) url.Values {
	form := url.Values{
		"pid": {"1001"}, "type": {"alipay"}, "out_trade_no": {"ORDER-1"},
		"notify_url": {"https://merchant.example/notify"}, "return_url": {"https://merchant.example/return"},
		"name": {"test order"}, "money": {"1.00"}, "clientip": {"127.0.0.1"}, "sign_type": {"MD5"},
	}
	resign(form, key)
	return form
}

func resign(form url.Values, key string) {
	params := make(map[string]any, len(form))
	for name := range form {
		params[name] = form.Get(name)
	}
	form.Set("sign", epaysigning.SignMD5(params, key))
}

func performForm(router http.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(response, request)
	return response
}
