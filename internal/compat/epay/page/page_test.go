package page

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	pluginpayment "gorixpay/internal/plugin/payment"

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
	return callPageWithUserAgent(t, router, target, "")
}

func callPageWithUserAgent(t *testing.T, router *gin.Engine, target, agent string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if agent != "" {
		request.Header.Set("User-Agent", agent)
	}
	router.ServeHTTP(recorder, request)
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
// TestChannelPageFuncsRedirectToCashierWithoutStarter keeps the fallback: with no
// payment starter wired the channel pages cannot start anything, so they must
// still hand the browser to the cashier rather than answer 404 or a blank page.
func TestChannelPageFuncsRedirectToCashierWithoutStarter(t *testing.T) {
	router, db := newPageRouter(t)
	order := model.Order{ID: "order-chan", TradeNo: "T-CHAN", OutTradeNo: "OUT-CHAN", Status: model.StatusPending}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	for name := range channelPageFuncs {
		recorder := callPage(t, router, "/pay/"+name+"/T-CHAN/")
		if recorder.Code != http.StatusFound {
			t.Fatalf("%s status=%d", name, recorder.Code)
		}
		if recorder.Header().Get("Location") == "" {
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

// stubStarter answers with a fixed action so the page behaviour can be asserted
// without a channel.
type stubStarter struct {
	action  pluginpayment.Action
	method  string
	device  string
	started int
}

func (starter *stubStarter) StartWithOptions(_ context.Context, _ string, options paymentorchestrator.StartOptions) (paymentorchestrator.Result, error) {
	starter.started++
	starter.method = options.Method
	starter.device = options.Device
	return paymentorchestrator.Result{Action: starter.action}, nil
}

// TestChannelPageRendersScanPageOnDesktop mirrors upstream's qrcode templates: a
// desktop browser gets the code drawn inline, a phone is redirected into the
// channel page instead. The method hint has to come from the page name, because
// that is what distinguishes /pay/wxpay/ from /pay/wxjspay/ upstream.
func TestChannelPageRendersScanPageOnDesktop(t *testing.T) {
	starter := &stubStarter{action: pluginpayment.Action{Type: pluginpayment.ActionJump, URL: "https://channel.example/pay/1"}}
	router, db := newPageRouter(t, WithStarter(starter))
	order := model.Order{ID: "order-scan", TradeNo: "T-SCAN", OutTradeNo: "OUT-SCAN", Status: model.StatusPending,
		Name: "测试商品", Amount: 1999, PaymentType: "alipay"}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}

	desktop := callPageWithUserAgent(t, router, "/pay/alipayjs/T-SCAN/", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131")
	if desktop.Code != http.StatusOK {
		t.Fatalf("status=%d", desktop.Code)
	}
	body := desktop.Body.String()
	for _, want := range []string{"请使用支付宝扫描二维码完成支付", "19.99", "T-SCAN", "<svg", "/pay/checkpay/T-SCAN/", "复制", "getshop.php?act=poll"} {
		if !strings.Contains(body, want) {
			t.Fatalf("扫码页缺少 %q: %s", want, body)
		}
	}
	if strings.Contains(body, "打开微信") {
		t.Fatalf("支付宝订单不应出现「打开微信」: %s", body)
	}
	if starter.method != "jsapi" || starter.device != "pc" {
		t.Fatalf("method=%q device=%q", starter.method, starter.device)
	}

	mobile := callPageWithUserAgent(t, router, "/pay/wxwappay/T-SCAN/", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Mobile/15E148")
	if mobile.Code != http.StatusFound || mobile.Header().Get("Location") != "https://channel.example/pay/1" {
		t.Fatalf("mobile status=%d location=%q", mobile.Code, mobile.Header().Get("Location"))
	}
	if starter.method != "wap" || starter.device != "mobile" {
		t.Fatalf("mobile method=%q device=%q", starter.method, starter.device)
	}
}

// TestChannelPageMobileRedirectsWebQRCode pins the "a phone cannot scan its own
// screen" rule: when the channel hands back a page URL as the code, a mobile
// browser is sent to it instead of being shown a QR it cannot use. A scannable
// scheme (weixin://) still renders the page, because there is nothing to open.
func TestChannelPageMobileRedirectsWebQRCode(t *testing.T) {
	starter := &stubStarter{action: pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: "https://api.huifu.com/hostingh5/?jump_id=H1"}}
	router, db := newPageRouter(t, WithStarter(starter))
	order := model.Order{ID: "order-qr", TradeNo: "T-QR", OutTradeNo: "OUT-QR", Status: model.StatusPending, Amount: 100}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	mobile := callPageWithUserAgent(t, router, "/pay/alipay/T-QR/", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Mobile/15E148")
	if mobile.Code != http.StatusFound || mobile.Header().Get("Location") != "https://api.huifu.com/hostingh5/?jump_id=H1" {
		t.Fatalf("mobile status=%d location=%q", mobile.Code, mobile.Header().Get("Location"))
	}

	starter.action = pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: "weixin://wxpay/bizpayurl?pr=x"}
	inApp := callPageWithUserAgent(t, router, "/pay/wxpay/T-QR/", "Mozilla/5.0 (iPhone) MicroMessenger/8.0.49")
	if inApp.Code != http.StatusOK || !strings.Contains(inApp.Body.String(), "<svg") {
		t.Fatalf("scheme code status=%d", inApp.Code)
	}
}

// TestChannelPageKeepsWeChatBuyerOnThePage pins the wxpay_wap flow: outside the
// WeChat app a 微信 order must not be redirected into a page that cannot pay.
// The buyer gets the code, an "打开微信" button and the copy-the-link recipe
// instead; inside WeChat the redirect is still the happy path.
func TestChannelPageKeepsWeChatBuyerOnThePage(t *testing.T) {
	starter := &stubStarter{action: pluginpayment.Action{Type: pluginpayment.ActionJump, URL: "https://api.huifu.com/hostingh5/?jump_id=H2"}}
	router, db := newPageRouter(t, WithStarter(starter))
	order := model.Order{ID: "order-wx", TradeNo: "T-WX", OutTradeNo: "OUT-WX", Status: model.StatusPending,
		Name: "微信商品", Amount: 500, PaymentType: "wxpay"}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}

	mobile := callPageWithUserAgent(t, router, "/pay/wxpay/T-WX/", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Mobile/15E148")
	if mobile.Code != http.StatusOK {
		t.Fatalf("mobile 微信订单应停在页面上，实际 status=%d", mobile.Code)
	}
	body := mobile.Body.String()
	for _, want := range []string{"打开微信", "weixin://", "复制", "在微信顶部搜索框"} {
		if !strings.Contains(body, want) {
			t.Fatalf("微信引导页缺少 %q: %s", want, body)
		}
	}

	inside := callPageWithUserAgent(t, router, "/pay/wxpay/T-WX/", "Mozilla/5.0 (iPhone) MicroMessenger/8.0.49")
	if inside.Code != http.StatusFound || inside.Header().Get("Location") != "https://api.huifu.com/hostingh5/?jump_id=H2" {
		t.Fatalf("微信内应直接跳转，status=%d location=%q", inside.Code, inside.Header().Get("Location"))
	}
}
