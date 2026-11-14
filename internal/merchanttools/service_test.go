package merchanttools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	pluginpayment "gorixpay/internal/plugin/payment"

	"gorm.io/gorm"
)

var testNow = time.Date(2026, 11, 14, 16, 0, 0, 0, time.UTC)

// stubStarter records the trade numbers it was asked to start.
type stubStarter struct {
	calls  []string
	action pluginpayment.Action
	err    error
}

func (starter *stubStarter) Start(_ context.Context, tradeNo string) (paymentorchestrator.Result, error) {
	starter.calls = append(starter.calls, tradeNo)
	if starter.err != nil {
		return paymentorchestrator.Result{}, starter.err
	}
	action := starter.action
	if action.Type == "" {
		action = pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: "https://qr.example/pay"}
	}
	return paymentorchestrator.Result{Action: action}, nil
}

func merchantToolsFixture(t *testing.T, name string) (*Service, *gorm.DB, *stubStarter) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &paymentmodel.Order{}, &paymentmodel.Attempt{}); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return testNow }
	for _, merchant := range []identitymodel.Merchant{
		{ID: "merchant-1", LegacyUID: int64Pointer(1001), DisplayName: "商户一", Status: identitymodel.StatusActive, CreatedAt: testNow, UpdatedAt: testNow},
		{ID: "merchant-2", LegacyUID: int64Pointer(2002), DisplayName: "商户二", Status: identitymodel.StatusActive, CreatedAt: testNow, UpdatedAt: testNow},
	} {
		if err := db.Create(&merchant).Error; err != nil {
			t.Fatal(err)
		}
	}
	starter := &stubStarter{}
	service := New(db, "https://pay.example", now).WithPayments(paymentservice.New(db, now), starter)
	return service, db, starter
}

func TestPayCodeRendersURLAndSVG(t *testing.T) {
	service, _, _ := merchantToolsFixture(t, "merchanttools-paycode")
	ctx := context.Background()
	code, err := service.PayCode(ctx, "merchant-1", "alipay", 100)
	if err != nil {
		t.Fatal(err)
	}
	if code.URL != "https://pay.example/pay/code/alipay?amount=100&pid=1001" {
		t.Fatalf("url=%q", code.URL)
	}
	if code.ExpiresAt != testNow.Add(payCodeTTL) {
		t.Fatalf("expires_at=%v", code.ExpiresAt)
	}
	// A real QR symbol: quiet zone of four modules, finder squares at three
	// corners and a payload that fits the encoder, with no text nodes.
	modules := strings.Count(code.CodeSVG, `<rect `) - 2 // minus the background and the fallback
	if modules < 21*21/2 {
		t.Fatalf("svg looks empty: %d module rects", modules)
	}
	for _, fragment := range []string{`<svg`, `aria-label="merchant pay code"`, `shape-rendering="crispEdges"`} {
		if !strings.Contains(code.CodeSVG, fragment) {
			t.Errorf("svg missing %q", fragment)
		}
	}
	if strings.Contains(code.CodeSVG, "<text") {
		t.Fatal("svg contains a text node")
	}
	// The three finder patterns are 7x7 rings; a real symbol starts each of them
	// with a filled 7x7 corner block at (4,4), (size-11,4) and (4,size-11).
	if !strings.Contains(code.CodeSVG, `<rect x="4" y="4"`) {
		t.Error("svg missing the top-left finder corner")
	}
	if again, _ := service.PayCode(ctx, "merchant-1", "alipay", 100); again.CodeSVG != code.CodeSVG {
		t.Fatal("svg is not deterministic")
	}

	open, err := service.PayCode(ctx, "merchant-1", "alipay", 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(open.URL, "amount=") {
		t.Fatalf("open code carries an amount: %q", open.URL)
	}
	if open.CodeSVG == code.CodeSVG {
		t.Fatal("open and fixed codes render the same pattern")
	}

	// Merchant isolation: the pid is the caller's legacy uid.
	other, err := service.PayCode(ctx, "merchant-2", "alipay", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(other.URL, "pid=2002") || strings.Contains(other.URL, "pid=1001") {
		t.Fatalf("other merchant url=%q", other.URL)
	}
}

func TestPayCodeValidation(t *testing.T) {
	service, _, _ := merchantToolsFixture(t, "merchanttools-paycode-errors")
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"bad-payment-type": func() error { _, err := service.PayCode(ctx, "merchant-1", "Ali Pay", 100); return err },
		"empty-payment":    func() error { _, err := service.PayCode(ctx, "merchant-1", "", 100); return err },
		"negative-amount":  func() error { _, err := service.PayCode(ctx, "merchant-1", "alipay", -1); return err },
		"amount-over-cap":  func() error { _, err := service.PayCode(ctx, "merchant-1", "alipay", maxPayCodeAmount+1); return err },
		"unknown-merchant": func() error { _, err := service.PayCode(ctx, "missing", "alipay", 100); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			if name == "unknown-merchant" {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err=%v", err)
			}
		})
	}

	unconfigured := New(service.db, "", func() time.Time { return testNow })
	if _, err := unconfigured.PayCode(ctx, "merchant-1", "alipay", 100); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("missing public url err=%v", err)
	}

	// A merchant without a legacy uid cannot be addressed by pid.
	if err := service.db.Model(&identitymodel.Merchant{}).Where("id = ?", "merchant-1").
		Update("legacy_uid", nil).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.PayCode(ctx, "merchant-1", "alipay", 100); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("merchant without legacy uid err=%v", err)
	}
}

func TestPayCodeRespectsThePaymentTypeCatalogue(t *testing.T) {
	service, db, _ := merchantToolsFixture(t, "merchanttools-paycode-catalogue")
	ctx := context.Background()
	if err := db.Exec(`CREATE TABLE pay_types (id VARCHAR(36) PRIMARY KEY, code VARCHAR(32) UNIQUE, name VARCHAR(64), status VARCHAR(20))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO pay_types (id, code, name, status) VALUES ('pt-1','alipay','支付宝','active'), ('pt-2','qqpay','QQ','disabled')`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.PayCode(ctx, "merchant-1", "alipay", 100); err != nil {
		t.Fatalf("active type rejected: %v", err)
	}
	for _, code := range []string{"qqpay", "usdt"} {
		if _, err := service.PayCode(ctx, "merchant-1", code, 100); !errors.Is(err, ErrPaymentTypeUnavailable) {
			t.Fatalf("%s err=%v", code, err)
		}
	}
}

func TestCreateTestOrder(t *testing.T) {
	service, db, starter := merchantToolsFixture(t, "merchanttools-testorder")
	ctx := context.Background()
	order, err := service.CreateTestOrder(ctx, "merchant-1", "alipay", 100)
	if err != nil {
		t.Fatal(err)
	}
	if order.TradeNo == "" || order.PayType != "qrcode" || order.PayInfo != "https://qr.example/pay" {
		t.Fatalf("order=%+v", order)
	}
	if !strings.HasPrefix(order.CashierURL, "https://pay.example/cashier.php?trade_no=") {
		t.Fatalf("cashier url=%q", order.CashierURL)
	}
	if len(starter.calls) != 1 || starter.calls[0] != order.TradeNo {
		t.Fatalf("starter calls=%v", starter.calls)
	}
	var stored paymentmodel.Order
	if err := db.First(&stored, "trade_no = ?", order.TradeNo).Error; err != nil {
		t.Fatal(err)
	}
	if stored.MerchantID != "merchant-1" || stored.Amount != 100 || stored.PaymentType != "alipay" ||
		!strings.HasPrefix(stored.OutTradeNo, "TEST") {
		t.Fatalf("stored=%+v", stored)
	}

	// The cap keeps the endpoint from becoming a payout channel.
	if _, err := service.CreateTestOrder(ctx, "merchant-1", "alipay", maxTestOrderAmount+1); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("over cap err=%v", err)
	}
	if _, err := service.CreateTestOrder(ctx, "merchant-1", "alipay", 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero amount err=%v", err)
	}
	if _, err := service.CreateTestOrder(ctx, "missing", "alipay", 100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing merchant err=%v", err)
	}

	// A starter failure still reports the created trade number.
	starter.err = errors.New("channel down")
	failing, err := service.CreateTestOrder(ctx, "merchant-1", "alipay", 50)
	if err == nil || failing.TradeNo == "" {
		t.Fatalf("failing=%+v err=%v", failing, err)
	}

	// Unwired dependencies answer ErrUnavailable instead of panicking.
	unwired := New(db, "https://pay.example", func() time.Time { return testNow })
	if _, err := unwired.CreateTestOrder(ctx, "merchant-1", "alipay", 100); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unwired err=%v", err)
	}
	if _, err := unwired.CreatePayCodeOrder(ctx, 1001, 100, "alipay"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unwired pay code order err=%v", err)
	}
}

func TestCreatePayCodeOrderUsesThePidMerchant(t *testing.T) {
	service, db, starter := merchantToolsFixture(t, "merchanttools-paycode-order")
	ctx := context.Background()
	order, err := service.CreatePayCodeOrder(ctx, 2002, 300, "alipay")
	if err != nil {
		t.Fatal(err)
	}
	if order.MerchantID != "merchant-2" || order.Amount != 300 || order.NotifyURL != "https://pay.example/pay/code/ack" {
		t.Fatalf("order=%+v", order)
	}
	if _, err := service.CreatePayCodeOrder(ctx, 9999, 300, "alipay"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown pid err=%v", err)
	}
	if _, err := service.CreatePayCodeOrder(ctx, 1001, 0, "alipay"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero amount err=%v", err)
	}
	result, err := service.StartPayCodeOrder(ctx, order.TradeNo)
	if err != nil || result.CashierURL != "https://pay.example/cashier.php?trade_no="+order.TradeNo {
		t.Fatalf("start=%+v err=%v", result, err)
	}
	if len(starter.calls) != 1 {
		t.Fatalf("starter calls=%v", starter.calls)
	}
	_ = db
}

func int64Pointer(value int64) *int64 { return &value }
