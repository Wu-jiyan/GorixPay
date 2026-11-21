package service

import (
	"context"
	"testing"
	"time"

	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func freezeFixture(t *testing.T) (*Service, *gorm.DB, paymentmodel.Order) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&paymentmodel.Order{}, &ledgermodel.Account{}, &ledgermodel.Entry{}, &ledgermodel.Transaction{}, &ledgermodel.Balance{}); err != nil {
		t.Fatal(err)
	}
	ledger := ledgerservice.New(db, time.Now)
	service := New(db, ledger, time.Now)

	order := paymentmodel.Order{
		ID: "order-freeze-1", TradeNo: "T-FREEZE-1", OutTradeNo: "OUT-FREEZE-1",
		MerchantID: "merchant-1", PaymentType: "alipay", Name: "冻结测试", Amount: 1000,
		Currency: "CNY", Status: paymentmodel.StatusPaid, NotifyURL: "https://m.example/n",
		ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	// The payment credited the merchant's available bucket, which is what a freeze
	// has to move.
	merchant, platform, err := service.accounts(db, order.MerchantID, order.Currency)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Post(context.Background(), ledgerservice.TransactionRequest{
		IdempotencyKey: "payment:" + order.ID, ReferenceType: "payment", ReferenceID: order.ID,
		Description: "payment", Postings: []ledgerservice.Posting{
			{AccountID: platform.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: 1000},
			{AccountID: merchant.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: 1000},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return service, db, order
}

// TestFreezeMovesCreditedAmountToFrozenBucket pins the accounting: freezing an
// order moves exactly what it credited, unfreezing moves it back, and both are
// idempotent so a double click cannot freeze twice.
func TestFreezeMovesCreditedAmountToFrozenBucket(t *testing.T) {
	service, db, order := freezeFixture(t)
	merchant, _, err := service.accounts(db, order.MerchantID, order.Currency)
	if err != nil {
		t.Fatal(err)
	}
	bucket := func(name ledgermodel.Bucket) int64 {
		balance := ledgermodel.Balance{}
		if err := db.Where("account_id = ?", merchant.ID).First(&balance).Error; err != nil {
			return 0
		}
		if name == ledgermodel.BucketFrozen {
			return balance.Frozen
		}
		return balance.Available
	}

	frozen, err := service.SetOrderFreeze(context.Background(), order.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.FrozenAt == nil {
		t.Fatal("frozen_at not set")
	}
	if got := bucket(ledgermodel.BucketFrozen); got != 1000 {
		t.Fatalf("frozen bucket=%d want 1000", got)
	}
	if got := bucket(ledgermodel.BucketAvailable); got != 0 {
		t.Fatalf("available bucket=%d want 0", got)
	}

	// A second freeze must be a no-op, not a second transfer.
	if _, err := service.SetOrderFreeze(context.Background(), order.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := bucket(ledgermodel.BucketFrozen); got != 1000 {
		t.Fatalf("frozen bucket after repeat=%d want 1000", got)
	}

	unfrozen, err := service.SetOrderFreeze(context.Background(), order.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if unfrozen.FrozenAt != nil {
		t.Fatal("frozen_at still set")
	}
	if got := bucket(ledgermodel.BucketAvailable); got != 1000 {
		t.Fatalf("available bucket after unfreeze=%d want 1000", got)
	}
}

// TestFreezeRejectsUnpaidOrders keeps the guard: only a paid order holds money.
func TestFreezeRejectsUnpaidOrders(t *testing.T) {
	service, db, order := freezeFixture(t)
	if err := db.Model(&paymentmodel.Order{}).Where("id = ?", order.ID).
		Update("status", paymentmodel.StatusPending).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetOrderFreeze(context.Background(), order.ID, true); err == nil {
		t.Fatal("expected ErrOrderNotFreezable")
	}
	_ = uuid.NewString
}
