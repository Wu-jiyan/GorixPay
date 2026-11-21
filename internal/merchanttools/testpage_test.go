package merchanttools

import (
	"context"
	"errors"
	"strings"
	"testing"

	channelrouter "gorixpay/internal/channel/router"
	paymentmodel "gorixpay/internal/payment/model"
	pluginpayment "gorixpay/internal/plugin/payment"

	"gorm.io/gorm"
)

// stubSelector answers the "is a channel pickable" question from a fixed set.
type stubSelector struct {
	allowed map[string]bool
}

func (selector stubSelector) Select(_ context.Context, paymentType string) (channelrouter.Channel, pluginpayment.Plugin, error) {
	if selector.allowed[paymentType] {
		return channelrouter.Channel{ID: "channel-1", PaymentType: paymentType}, nil, nil
	}
	return channelrouter.Channel{}, nil, channelrouter.ErrNoAvailableChannel
}

// payTypeRow mirrors the catalogue table; the test only needs code/name/sort.
type payTypeRow struct {
	ID     string `gorm:"type:varchar(36);primaryKey"`
	Code   string `gorm:"type:varchar(32);uniqueIndex"`
	Name   string `gorm:"type:varchar(64)"`
	Sort   int
	Status string `gorm:"type:varchar(20)"`
}

func (payTypeRow) TableName() string { return "pay_types" }

// settingRow mirrors system_settings.
type settingRow struct {
	SettingKey   string `gorm:"column:setting_key;type:varchar(150);primaryKey"`
	SettingValue string `gorm:"column:setting_value;type:text"`
}

func (settingRow) TableName() string { return "system_settings" }

// testPageFixture builds the service with a catalogue, the channel selector and
// the settings table the page reads.
func testPageFixture(t *testing.T, name string, allowed ...string) (*Service, *gorm.DB) {
	t.Helper()
	service, db, _ := merchantToolsFixture(t, name)
	if err := db.AutoMigrate(&payTypeRow{}, &settingRow{}); err != nil {
		t.Fatal(err)
	}
	for index, entry := range []payTypeRow{
		{ID: "t-alipay", Code: "alipay", Name: "支付宝", Sort: 1, Status: "active"},
		{ID: "t-wxpay", Code: "wxpay", Name: "微信支付", Sort: 2, Status: "active"},
		{ID: "t-qqpay", Code: "qqpay", Name: "QQ 钱包", Sort: 3, Status: "disabled"},
	} {
		entry.Sort = index + 1
		if err := db.Create(&entry).Error; err != nil {
			t.Fatal(err)
		}
	}
	allow := map[string]bool{}
	for _, code := range allowed {
		allow[code] = true
	}
	return service.WithChannels(stubSelector{allowed: allow}), db
}

func setSetting(t *testing.T, db *gorm.DB, key, value string) {
	t.Helper()
	row := settingRow{SettingKey: key, SettingValue: value}
	if err := db.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
}

// TestTestPageOffersOnlyPayableMethods pins the filter that keeps the page from
// advertising a method the payer cannot actually use.
func TestTestPageOffersOnlyPayableMethods(t *testing.T) {
	service, _ := testPageFixture(t, "testpage-methods", "alipay")
	page, err := service.TestPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !page.Enabled {
		t.Fatalf("page should be enabled by default: %+v", page)
	}
	if len(page.Methods) != 1 || page.Methods[0].Code != "alipay" {
		t.Fatalf("methods = %+v, want only alipay", page.Methods)
	}
	if page.Methods[0].Name != "支付宝" {
		t.Fatalf("method name = %q", page.Methods[0].Name)
	}
	if page.MaxCents != defaultTestMaxCents || page.HourlyLimit != defaultTestHourlyLimit {
		t.Fatalf("defaults = %d/%d", page.MaxCents, page.HourlyLimit)
	}
	// qqpay is disabled in the catalogue and wxpay has no channel: both are out.
	for _, method := range page.Methods {
		if method.Code == "qqpay" || method.Code == "wxpay" {
			t.Fatalf("unusable method leaked into the page: %+v", method)
		}
	}
	if page.Merchant == "" {
		t.Fatal("the page must name the demo merchant it pays into")
	}
}

// TestTestPageHonoursTheKillSwitch keeps the operator able to close the page:
// both the form and the create call must refuse.
func TestTestPageHonoursTheKillSwitch(t *testing.T) {
	service, db := testPageFixture(t, "testpage-disabled", "alipay")
	setSetting(t, db, SettingTestEnabled, "false")

	page, err := service.TestPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if page.Enabled || page.Reason == "" {
		t.Fatalf("page = %+v, want disabled with a reason", page)
	}
	if _, err := service.CreatePublicTestOrder(context.Background(), "alipay", 1, "203.0.113.9"); !errors.Is(err, ErrTestPageDisabled) {
		t.Fatalf("create = %v, want ErrTestPageDisabled", err)
	}
}

func TestTestPageRejectsAmountOutOfRange(t *testing.T) {
	service, db := testPageFixture(t, "testpage-amount", "alipay")
	setSetting(t, db, SettingTestMaxCents, "50")

	for _, amount := range []int64{0, -1, 51} {
		if _, err := service.CreatePublicTestOrder(context.Background(), "alipay", amount, "203.0.113.9"); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("amount %d = %v, want ErrInvalidRequest", amount, err)
		}
	}
}

// TestTestPageRateLimitsPerClientIP proves the quota counts rows instead of
// trusting the caller: the third order from one IP is refused at a limit of 2.
func TestTestPageRateLimitsPerClientIP(t *testing.T) {
	service, db := testPageFixture(t, "testpage-quota", "alipay")
	setSetting(t, db, SettingTestHourlyLimit, "2")
	ctx := context.Background()

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := service.CreatePublicTestOrder(ctx, "alipay", 1, "198.51.100.7"); err != nil {
			t.Fatalf("attempt %d = %v", attempt, err)
		}
	}
	if _, err := service.CreatePublicTestOrder(ctx, "alipay", 1, "198.51.100.7"); !errors.Is(err, ErrTestRateLimited) {
		t.Fatalf("third attempt = %v, want ErrTestRateLimited", err)
	}
	// Another client keeps its own quota.
	if _, err := service.CreatePublicTestOrder(ctx, "alipay", 1, "198.51.100.8"); err != nil {
		t.Fatalf("other client = %v", err)
	}

	var orders []paymentmodel.Order
	if err := db.Find(&orders).Error; err != nil {
		t.Fatal(err)
	}
	for _, order := range orders {
		if order.Name != testOrderName {
			t.Fatalf("order %s name = %q", order.TradeNo, order.Name)
		}
		if order.Amount != 1 {
			t.Fatalf("order %s amount = %d", order.TradeNo, order.Amount)
		}
		if order.NotifyURL != "https://pay.example/pay/test/ack" {
			t.Fatalf("order %s notify = %q", order.TradeNo, order.NotifyURL)
		}
	}
}

// TestTestPageRejectsAMethodWithoutChannel stops a hand-crafted request for a
// method the page never offered.
func TestTestPageRejectsAMethodWithoutChannel(t *testing.T) {
	service, _ := testPageFixture(t, "testpage-nomethod", "alipay")
	if _, err := service.CreatePublicTestOrder(context.Background(), "wxpay", 1, "203.0.113.9"); !errors.Is(err, ErrPaymentTypeUnavailable) {
		t.Fatalf("wxpay = %v, want ErrPaymentTypeUnavailable", err)
	}
	if _, err := service.CreatePublicTestOrder(context.Background(), "../../etc/passwd", 1, "203.0.113.9"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad code = %v, want ErrInvalidRequest", err)
	}
}

// TestTestPageUsesTheConfiguredDemoMerchant pins the pid setting: the orders
// must belong to the merchant the operator chose, not the first row.
func TestTestPageUsesTheConfiguredDemoMerchant(t *testing.T) {
	service, db := testPageFixture(t, "testpage-pid", "alipay")
	setSetting(t, db, SettingTestPID, "2002")

	order, err := service.CreatePublicTestOrder(context.Background(), "alipay", 1, "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	var stored paymentmodel.Order
	if err := db.First(&stored, "trade_no = ?", order.TradeNo).Error; err != nil {
		t.Fatal(err)
	}
	if stored.MerchantID != "merchant-2" {
		t.Fatalf("merchant = %q, want merchant-2", stored.MerchantID)
	}
	if !strings.HasPrefix(order.CashierURL, "https://pay.example/cashier.php?trade_no=") {
		t.Fatalf("cashier url = %q", order.CashierURL)
	}
	if order.PayType != string(pluginpayment.ActionQRCode) {
		t.Fatalf("pay type = %q", order.PayType)
	}
}
