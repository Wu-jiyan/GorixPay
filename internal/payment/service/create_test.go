package service

import (
	"context"
	"errors"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

func TestCreateOrderIsIdempotentForSamePayload(t *testing.T) {
	db := testDatabase(t, "create-idempotent")
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	service := New(db, func() time.Time { return now })
	command := CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "ORDER-1", PaymentType: "alipay",
		Name: "test order", Amount: 100, NotifyURL: "https://merchant.example/notify",
	}
	first, created, err := service.Create(context.Background(), command)
	if err != nil || !created {
		t.Fatalf("first create: created=%v err=%v", created, err)
	}
	second, created, err := service.Create(context.Background(), command)
	if err != nil || created || second.ID != first.ID || second.TradeNo != first.TradeNo {
		t.Fatalf("second create: order=%+v created=%v err=%v", second, created, err)
	}
	var count int64
	if err := db.Model(&paymentmodel.Order{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("order count=%d err=%v", count, err)
	}
}

func TestCreateOrderRejectsIdempotencyConflict(t *testing.T) {
	db := testDatabase(t, "create-conflict")
	service := New(db, time.Now)
	command := CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "ORDER-1", PaymentType: "alipay",
		Name: "test order", Amount: 100, NotifyURL: "https://merchant.example/notify",
	}
	if _, _, err := service.Create(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	command.Amount = 200
	if _, _, err := service.Create(context.Background(), command); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict error=%v", err)
	}
}

func TestCreateOrderValidatesCommand(t *testing.T) {
	db := testDatabase(t, "create-validation")
	service := New(db, time.Now)
	valid := CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "ORDER-1", PaymentType: "alipay",
		Name: "test order", Amount: 100, NotifyURL: "https://merchant.example/notify",
	}
	tests := []CreateCommand{
		func() CreateCommand { value := valid; value.Amount = 0; return value }(),
		func() CreateCommand { value := valid; value.OutTradeNo = ""; return value }(),
		func() CreateCommand { value := valid; value.NotifyURL = "javascript:alert(1)"; return value }(),
		func() CreateCommand { value := valid; value.PaymentType = ""; return value }(),
	}
	for _, command := range tests {
		if _, _, err := service.Create(context.Background(), command); !errors.Is(err, ErrInvalidCommand) {
			t.Fatalf("invalid command accepted: %+v err=%v", command, err)
		}
	}
}

func testDatabase(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &paymentmodel.Order{}); err != nil {
		t.Fatal(err)
	}
	merchant := identitymodel.Merchant{ID: "merchant-1", DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	return db
}
