package domainauth

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestNormalizeAndMatchDomains(t *testing.T) {
	cases := map[string]string{
		"Example.COM.":  "example.com",
		"*.example.com": "*.example.com",
		"1.2.3.4":       "1.2.3.4",
	}
	for input, want := range cases {
		got, ok := Normalize(input)
		if !ok || got != want {
			t.Fatalf("normalize %q = %q ok=%v", input, got, ok)
		}
	}
	for _, input := range []string{"", "http://example.com", "example.com/path", "*.", "bad host"} {
		if _, ok := Normalize(input); ok {
			t.Fatalf("invalid domain accepted: %q", input)
		}
	}
	if !Match("example.com", "example.com") || Match("example.com", "a.example.com") {
		t.Fatal("exact match semantics changed")
	}
	if !Match("*.example.com", "a.example.com") || !Match("*.example.com", "a.b.example.com") || Match("*.example.com", "example.com") {
		t.Fatal("wildcard match semantics changed")
	}
}

func TestDomainReviewFlowAndGuard(t *testing.T) {
	service, db := domainFixture(t, "domain-flow")
	submitted, err := service.Submit(context.Background(), "merchant-1", "*.Example.com.")
	if err != nil || submitted.Domain != "*.example.com" || submitted.Status != StatusPending {
		t.Fatalf("submit: %+v err=%v", submitted, err)
	}
	if _, err := service.Submit(context.Background(), "merchant-1", "*.example.com"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate err=%v", err)
	}
	if _, err := service.Review(context.Background(), submitted.ID, "admin-1", "approve", ""); err != nil {
		t.Fatal(err)
	}
	setting(t, db, "payment.domain_whitelist_required", "true")
	guard := service.Guard()
	command := paymentservice.CreateCommand{
		MerchantID: "merchant-1", Amount: 100, Name: "test",
		NotifyURL: "https://api.example.com/notify",
	}
	if err := guard.Check(context.Background(), command); err != nil {
		t.Fatalf("approved wildcard rejected: %v", err)
	}
	var rejection paymentservice.Rejection
	command.NotifyURL = "https://other.example.net/notify"
	if err := guard.Check(context.Background(), command); !errors.As(err, &rejection) || rejection.Reason != RejectionReason {
		t.Fatalf("unauthorized domain accepted: %v", err)
	}
	command.NotifyURL = "https://api.example.com/notify"
	command.ReturnURL = "https://evil.example.net/return"
	if err := guard.Check(context.Background(), command); !errors.As(err, &rejection) {
		t.Fatalf("unauthorized return url accepted: %v", err)
	}
}

func TestDomainReviewRejectRequiresReason(t *testing.T) {
	service, _ := domainFixture(t, "domain-reject")
	submitted, err := service.Submit(context.Background(), "merchant-1", "shop.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Review(context.Background(), submitted.ID, "admin-1", "reject", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty reason err=%v", err)
	}
	reviewed, err := service.Review(context.Background(), submitted.ID, "admin-1", "reject", "域名信息不完整")
	if err != nil || reviewed.Status != StatusRejected || reviewed.RejectionReason == nil {
		t.Fatalf("reject: %+v err=%v", reviewed, err)
	}
	if err := service.Delete(context.Background(), "merchant-1", submitted.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rejected domain delete err=%v", err)
	}
}

func domainFixture(t *testing.T, name string) (*Service, *gorm.DB) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Domain{}, &settingRow{}); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC) }
	return New(db, now), db
}

func setting(t *testing.T, db *gorm.DB, key, value string) {
	t.Helper()
	if err := db.Save(&settingRow{Key: key, Value: value, UpdatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
}
