package risk

import (
	"context"
	"errors"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

type settingRow struct {
	Key       string    `gorm:"column:setting_key;primaryKey"`
	Value     string    `gorm:"column:setting_value;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (settingRow) TableName() string { return "system_settings" }

func TestBlacklistHitsAreRejectedAndRecorded(t *testing.T) {
	service, db := riskFixture(t, "risk-blacklist")
	if _, err := service.AddBlacklist(context.Background(), "ip", "203.0.113.9", "abuse"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddBlacklist(context.Background(), "ip", "203.0.113.9", "dup"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate err=%v", err)
	}
	if _, err := service.AddBlacklist(context.Background(), "keyword", "casino", "gambling"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddBlacklist(context.Background(), "domain", "*.blocked.example", "bad domain"); err != nil {
		t.Fatal(err)
	}

	guard := service.Guard()
	base := paymentservice.CreateCommand{
		MerchantID: "merchant-1", Amount: 100, Name: "normal order",
		NotifyURL: "https://merchant.example/notify", ClientIP: "203.0.113.9",
	}
	var rejection paymentservice.Rejection
	if err := guard.Check(context.Background(), base); !errors.As(err, &rejection) || rejection.Reason != blacklistRejection {
		t.Fatalf("ip blacklist: %v", err)
	}
	base.ClientIP = "198.51.100.1"
	base.Name = "Casino night"
	if err := guard.Check(context.Background(), base); !errors.As(err, &rejection) {
		t.Fatalf("keyword blacklist: %v", err)
	}
	base.Name = "normal order"
	base.NotifyURL = "https://pay.blocked.example/notify"
	if err := guard.Check(context.Background(), base); !errors.As(err, &rejection) {
		t.Fatalf("domain blacklist: %v", err)
	}
	var events int64
	if err := db.Model(&Event{}).Count(&events).Error; err != nil || events != 3 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	base.NotifyURL = "https://merchant.example/notify"
	if err := guard.Check(context.Background(), base); err != nil {
		t.Fatalf("clean request should pass: %v", err)
	}
}

func TestDailyLimitsRejectOrders(t *testing.T) {
	service, db := riskFixture(t, "risk-limits")
	setting(t, db, "risk.daily_order_limit", "1")
	setting(t, db, "risk.ip_daily_limit", "1")
	seedPaidOrder(t, db, "order-1", 500, "198.51.100.7")
	guard := service.Guard()
	command := paymentservice.CreateCommand{
		MerchantID: "merchant-1", Amount: 100, Name: "second order",
		NotifyURL: "https://merchant.example/notify", ClientIP: "198.51.100.7",
	}
	var rejection paymentservice.Rejection
	err := guard.Check(context.Background(), command)
	if !errors.As(err, &rejection) {
		t.Fatalf("merchant order limit not enforced: %v", err)
	}
	setting(t, db, "risk.daily_order_limit", "0")
	command.ClientIP = "198.51.100.8"
	if err := guard.Check(context.Background(), command); err != nil {
		t.Fatalf("unlimited merchant rejected: %v", err)
	}
	command.ClientIP = "198.51.100.7"
	if err := guard.Check(context.Background(), command); !errors.As(err, &rejection) {
		t.Fatalf("ip limit not enforced: %v", err)
	}
	var events int64
	if err := db.Model(&Event{}).Count(&events).Error; err != nil || events < 2 {
		t.Fatalf("events=%d err=%v", events, err)
	}
}

func TestDailyAmountLimit(t *testing.T) {
	service, db := riskFixture(t, "risk-amount")
	setting(t, db, "risk.daily_amount_limit_cents", "1000")
	seedPaidOrder(t, db, "order-1", 900, "198.51.100.9")
	guard := service.Guard()
	command := paymentservice.CreateCommand{
		MerchantID: "merchant-1", Amount: 200, Name: "over limit",
		NotifyURL: "https://merchant.example/notify", ClientIP: "198.51.100.9",
	}
	var rejection paymentservice.Rejection
	if err := guard.Check(context.Background(), command); !errors.As(err, &rejection) {
		t.Fatalf("amount limit not enforced: %v", err)
	}
	command.Amount = 50
	if err := guard.Check(context.Background(), command); err != nil {
		t.Fatalf("within limit rejected: %v", err)
	}
}

func TestBlacklistValidation(t *testing.T) {
	service, _ := riskFixture(t, "risk-validation")
	if _, err := service.AddBlacklist(context.Background(), "unknown", "x", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad type err=%v", err)
	}
	if _, err := service.AddBlacklist(context.Background(), "ip", "", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty value err=%v", err)
	}
	if err := service.RemoveBlacklist(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove missing err=%v", err)
	}
}

func riskFixture(t *testing.T, name string) (*Service, *gorm.DB) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&BlacklistEntry{}, &Event{}, &settingRow{}, &identitymodel.Merchant{}, &paymentmodel.Order{}); err != nil {
		t.Fatal(err)
	}
	email := "merchant@example.test"
	merchant := identitymodel.Merchant{ID: "merchant-1", Email: &email, DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Now().UTC() }
	return New(db, now), db
}

func seedPaidOrder(t *testing.T, db *gorm.DB, id string, amount int64, clientIP string) {
	t.Helper()
	now := time.Now().UTC()
	ip := clientIP
	order := paymentmodel.Order{
		ID: id, TradeNo: "T" + id, MerchantID: "merchant-1", OutTradeNo: "OUT-" + id,
		PaymentType: "alipay", Name: "seed", Amount: amount, Currency: "CNY",
		Status: paymentmodel.StatusPaid, Kind: paymentmodel.KindSale, NotifyURL: "https://merchant.example/notify",
		ClientIP: &ip, PaidAt: &now, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
}

func setting(t *testing.T, db *gorm.DB, key, value string) {
	t.Helper()
	if err := db.Save(&settingRow{Key: key, Value: value, UpdatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
}
