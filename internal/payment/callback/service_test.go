package callback

import (
	"context"
	"errors"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/outbox"
	paymentmodel "gorixpay/internal/payment/model"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

func TestCompleteMarksOrderPaidAndIsIdempotent(t *testing.T) {
	db, order := callbackDatabase(t, "callback-complete")
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	service := New(db, func() time.Time { return now })
	result, applied, err := service.Complete(context.Background(), Completion{
		OrderID: order.ID, ChannelID: "channel-1", ChannelTradeNo: "CHANNEL-1", Amount: order.Amount, Digest: "digest-1",
	})
	if err != nil || !applied || result.Status != paymentmodel.StatusPaid || result.PaidAt == nil {
		t.Fatalf("complete: %+v applied=%v err=%v", result, applied, err)
	}
	result, applied, err = service.Complete(context.Background(), Completion{
		OrderID: order.ID, ChannelID: "channel-1", ChannelTradeNo: "CHANNEL-1", Amount: order.Amount, Digest: "digest-1",
	})
	if err != nil || applied || result.Status != paymentmodel.StatusPaid {
		t.Fatalf("duplicate: %+v applied=%v err=%v", result, applied, err)
	}
	var count int64
	if err := db.Model(&Record{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("callback count=%d err=%v", count, err)
	}
}

func TestCompleteRejectsAmountMismatchAndChannelTradeReuse(t *testing.T) {
	db, first := callbackDatabase(t, "callback-validation")
	service := New(db, time.Now)
	if _, _, err := service.Complete(context.Background(), Completion{
		OrderID: first.ID, ChannelID: "channel-1", ChannelTradeNo: "CHANNEL-1", Amount: first.Amount + 1, Digest: "digest-mismatch",
	}); !errors.Is(err, ErrAmountMismatch) {
		t.Fatalf("amount mismatch error=%v", err)
	}
	if _, _, err := service.Complete(context.Background(), Completion{
		OrderID: first.ID, ChannelID: "channel-1", ChannelTradeNo: "CHANNEL-1", Amount: first.Amount, Digest: "digest-first",
	}); err != nil {
		t.Fatal(err)
	}
	orders := paymentservice.New(db, time.Now)
	second, _, err := orders.Create(context.Background(), paymentservice.CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "ORDER-2", PaymentType: "alipay", Name: "second", Amount: 100,
		NotifyURL: "https://merchant.example/notify",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Complete(context.Background(), Completion{
		OrderID: second.ID, ChannelID: "channel-1", ChannelTradeNo: "CHANNEL-1", Amount: second.Amount, Digest: "digest-second",
	}); !errors.Is(err, ErrChannelTradeConflict) {
		t.Fatalf("channel trade reuse error=%v", err)
	}
}

func TestCompleteRejectsClosedOrder(t *testing.T) {
	db, order := callbackDatabase(t, "callback-closed")
	if err := db.Model(&paymentmodel.Order{}).Where("id = ?", order.ID).Update("status", paymentmodel.StatusClosed).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := New(db, time.Now).Complete(context.Background(), Completion{
		OrderID: order.ID, ChannelID: "channel-1", ChannelTradeNo: "CHANNEL-1", Amount: order.Amount, Digest: "digest-closed",
	}); !errors.Is(err, ErrOrderNotPayable) {
		t.Fatalf("closed order error=%v", err)
	}
}

func callbackDatabase(t *testing.T, name string) (*gorm.DB, paymentmodel.Order) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &paymentmodel.Order{}, &Record{}, &outbox.Message{}); err != nil {
		t.Fatal(err)
	}
	merchant := identitymodel.Merchant{ID: "merchant-1", DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	order, _, err := paymentservice.New(db, time.Now).Create(context.Background(), paymentservice.CreateCommand{
		MerchantID: merchant.ID, OutTradeNo: "ORDER-1", PaymentType: "alipay", Name: "test", Amount: 100,
		NotifyURL: "https://merchant.example/notify",
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, order
}
