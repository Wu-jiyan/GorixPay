package page

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorixpay/internal/payment/model"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newPageRouter(t *testing.T, options ...Option) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Order{}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	Register(router, db, "https://pay.example", nil, options...)
	return router, db
}

func callPage(t *testing.T, router *gin.Engine, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func decodeGetShop(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body=%q err=%v", recorder.Body.String(), err)
	}
	return payload
}

// TestGetShopPollsOrderStatus keeps the cashier polling contract: HTTP 200 with a
// code field, and an unknown order answers code -1 rather than 404.
func TestGetShopPollsOrderStatus(t *testing.T) {
	router, db := newPageRouter(t)
	order := model.Order{ID: "order-poll", TradeNo: "T-POLL", OutTradeNo: "OUT-POLL", Status: model.StatusPaid}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	recorder := callPage(t, router, "/getshop.php?act=poll&trade_no=T-POLL")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	payload := decodeGetShop(t, recorder)
	if payload["code"].(float64) != 1 || payload["status"] != "paid" || payload["paid"] != true {
		t.Fatalf("payload=%v", payload)
	}
	missing := decodeGetShop(t, callPage(t, router, "/getshop.php?act=poll&trade_no=nope"))
	if missing["code"].(float64) != -1 {
		t.Fatalf("missing order payload=%v", missing)
	}
}

// TestGetShopCaptchaVerifyNeedsAVerifier keeps the endpoint honest: without a
// wired verifier it must not hand out a key.
func TestGetShopCaptchaVerifyNeedsAVerifier(t *testing.T) {
	router, _ := newPageRouter(t)
	payload := decodeGetShop(t, callPage(t, router, "/getshop.php?act=captcha_verify&captcha_id=a&captcha_code=b"))
	if payload["code"].(float64) != -1 {
		t.Fatalf("payload=%v", payload)
	}
	if _, ok := payload["key"]; ok {
		t.Fatal("a key was issued without a verifier")
	}
}

func TestGetShopCaptchaVerifyIssuesKeyOnSuccess(t *testing.T) {
	var seenID, seenCode string
	verifier := func(_ context.Context, id, code string) error {
		seenID, seenCode = id, code
		return nil
	}
	router, _ := newPageRouter(t, WithCaptchaVerifier(verifier))
	payload := decodeGetShop(t, callPage(t, router, "/getshop.php?act=captcha_verify&captcha_id=abc&captcha_code=1234"))
	if payload["code"].(float64) != 1 {
		t.Fatalf("payload=%v", payload)
	}
	if key, ok := payload["key"].(string); !ok || len(key) != 32 {
		t.Fatalf("key=%v", payload["key"])
	}
	if seenID != "abc" || seenCode != "1234" {
		t.Fatalf("verifier saw %q %q", seenID, seenCode)
	}
}

func TestGetShopCaptchaVerifyRejectsWrongCode(t *testing.T) {
	verifier := func(context.Context, string, string) error { return errors.New("bad code") }
	router, _ := newPageRouter(t, WithCaptchaVerifier(verifier))
	payload := decodeGetShop(t, callPage(t, router, "/getshop.php?act=captcha_verify&captcha_id=abc&captcha_code=0000"))
	if payload["code"].(float64) != -1 {
		t.Fatalf("payload=%v", payload)
	}
}

func TestGetShopRejectsUnknownAction(t *testing.T) {
	router, _ := newPageRouter(t)
	payload := decodeGetShop(t, callPage(t, router, "/getshop.php?act=delete"))
	if payload["code"].(float64) != -1 {
		t.Fatalf("payload=%v", payload)
	}
}

// TestChannelPageFuncsRedirectToCashier keeps the per-channel page names working
// instead of answering 404 for a URL a merchant may have cached.
func TestChannelPageFuncsRedirectToCashier(t *testing.T) {
	router, db := newPageRouter(t)
	order := model.Order{ID: "order-chan", TradeNo: "T-CHAN", OutTradeNo: "OUT-CHAN", Status: model.StatusPending}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range channelPageFuncs {
		recorder := callPage(t, router, "/pay/"+name+"/T-CHAN/")
		if recorder.Code != http.StatusFound {
			t.Fatalf("%s status=%d", name, recorder.Code)
		}
		location := recorder.Header().Get("Location")
		if location == "" {
			t.Fatalf("%s has no Location", name)
		}
	}
	paid := model.Order{ID: "order-paid", TradeNo: "T-PAID", OutTradeNo: "OUT-PAID", Status: model.StatusPaid}
	if err := db.Create(&paid).Error; err != nil {
		t.Fatal(err)
	}
	recorder := callPage(t, router, "/pay/scan/T-PAID/")
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") == "" {
		t.Fatalf("paid order status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
}
