package groupshop

import (
	"context"
	"errors"
	"testing"
	"time"

	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

var testNow = time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)

func groupshopFixture(t *testing.T, name string) (*Service, *gorm.DB) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&Purchase{}, &Plan{},
		&ledgermodel.Account{}, &ledgermodel.Balance{}, &ledgermodel.Transaction{}, &ledgermodel.Entry{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE merchants (id VARCHAR(36) PRIMARY KEY, group_id VARCHAR(36))").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO merchants (id) VALUES ('merchant-1')").Error; err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return testNow }
	ledger := ledgerservice.New(db, now)
	merchant, err := ledger.CreateAccount(context.Background(), ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: "merchant-1", Purpose: "balance",
		Category: ledgermodel.CategoryLiability, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	platform, err := ledger.CreateAccount(context.Background(), ledgerservice.AccountRequest{
		OwnerType: "platform", OwnerID: "platform", Purpose: "cash",
		Category: ledgermodel.CategoryAsset, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Post(context.Background(), ledgerservice.TransactionRequest{
		IdempotencyKey: "seed:merchant-1", ReferenceType: "seed", ReferenceID: "merchant-1",
		Description: "seed merchant balance",
		Postings: []ledgerservice.Posting{
			{AccountID: platform.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: 5000},
			{AccountID: merchant.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: 5000},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return New(db, ledger, now), db
}

func seedGroup(t *testing.T, db *gorm.DB, id, name string, price int64, status string) Plan {
	t.Helper()
	plan := Plan{ID: id, Name: name, RateBp: 100, SettleMode: "normal", Status: status, PriceCents: price, CreatedAt: testNow, UpdatedAt: testNow}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestPurchaseChargesBalanceAndAssignsGroup(t *testing.T) {
	service, db := groupshopFixture(t, "groupshop-buy")
	ctx := context.Background()
	seedGroup(t, db, "group-1", "黄金组", 1000, "active")

	purchase, created, err := service.Purchase(ctx, "merchant-1", "group-1")
	if err != nil || !created {
		t.Fatalf("purchase=%+v created=%v err=%v", purchase, created, err)
	}
	if purchase.PriceCents != 1000 || purchase.Status != PurchaseActive || purchase.Attempts != 1 {
		t.Fatalf("purchase=%+v", purchase)
	}
	if got := merchantBalance(t, db); got != 4000 {
		t.Fatalf("merchant balance=%d", got)
	}
	// The seed starts the platform cash account at 5000, so the purchase leaves
	// 4000 there and 1000 on the merchant liability side moved back.
	if got := balanceOf(t, db, "platform", "platform"); got != 4000 {
		t.Fatalf("platform balance=%d", got)
	}
	if group := merchantGroup(t, db); group == nil || *group != "group-1" {
		t.Fatalf("merchant group=%v", group)
	}
	assertLedgerKey(t, db, "group-purchase:merchant-1:group-1:1")

	// A double submit returns the same purchase and charges nothing more.
	again, created, err := service.Purchase(ctx, "merchant-1", "group-1")
	if err != nil || created || again.ID != purchase.ID {
		t.Fatalf("idempotent purchase=%+v created=%v err=%v", again, created, err)
	}
	if got := merchantBalance(t, db); got != 4000 {
		t.Fatalf("merchant balance after replay=%d", got)
	}
	var count int64
	if err := db.Model(&Purchase{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("purchases=%d err=%v", count, err)
	}
}

func TestPurchaseRejectsInsufficientBalanceAndFreeGroups(t *testing.T) {
	service, db := groupshopFixture(t, "groupshop-rejects")
	ctx := context.Background()
	seedGroup(t, db, "group-expensive", "铂金组", 6000, "active")
	seedGroup(t, db, "group-free", "免费组", 0, "active")
	seedGroup(t, db, "group-off", "停用组", 500, "disabled")

	if _, _, err := service.Purchase(ctx, "merchant-1", "group-expensive"); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expensive err=%v", err)
	}
	if _, _, err := service.Purchase(ctx, "merchant-1", "group-free"); !errors.Is(err, ErrGroupNotForSale) {
		t.Fatalf("free err=%v", err)
	}
	if _, _, err := service.Purchase(ctx, "merchant-1", "group-off"); !errors.Is(err, ErrGroupNotForSale) {
		t.Fatalf("disabled err=%v", err)
	}
	if _, _, err := service.Purchase(ctx, "merchant-1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err=%v", err)
	}
	if _, _, err := service.Purchase(ctx, "", "group-free"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty merchant err=%v", err)
	}
	var count int64
	if err := db.Model(&Purchase{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("purchases=%d err=%v", count, err)
	}
	if got := merchantBalance(t, db); got != 5000 {
		t.Fatalf("balance=%d", got)
	}
	if group := merchantGroup(t, db); group != nil {
		t.Fatalf("merchant joined %v", *group)
	}
}

func TestRefundReversesAndDetaches(t *testing.T) {
	service, db := groupshopFixture(t, "groupshop-refund")
	ctx := context.Background()
	seedGroup(t, db, "group-1", "黄金组", 1000, "active")
	purchase, _, err := service.Purchase(ctx, "merchant-1", "group-1")
	if err != nil {
		t.Fatal(err)
	}
	refunded, err := service.Refund(ctx, purchase.ID)
	if err != nil || refunded.Status != PurchaseRefunded {
		t.Fatalf("refund=%+v err=%v", refunded, err)
	}
	if got := merchantBalance(t, db); got != 5000 {
		t.Fatalf("merchant balance=%d", got)
	}
	if got := balanceOf(t, db, "platform", "platform"); got != 5000 {
		t.Fatalf("platform balance=%d", got)
	}
	if group := merchantGroup(t, db); group != nil {
		t.Fatalf("merchant still in %v", *group)
	}
	assertLedgerKey(t, db, "group-purchase-refund:"+purchase.ID+":1")
	if _, err := service.Refund(ctx, purchase.ID); !errors.Is(err, ErrNotRefundable) {
		t.Fatalf("second refund err=%v", err)
	}
	if _, err := service.Refund(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing refund err=%v", err)
	}
}

func TestRepurchaseAfterRefundChargesAgain(t *testing.T) {
	service, db := groupshopFixture(t, "groupshop-repurchase")
	ctx := context.Background()
	seedGroup(t, db, "group-1", "黄金组", 1000, "active")
	first, _, err := service.Purchase(ctx, "merchant-1", "group-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refund(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	second, created, err := service.Purchase(ctx, "merchant-1", "group-1")
	if err != nil || created {
		t.Fatalf("repurchase=%+v created=%v err=%v", second, created, err)
	}
	if second.ID != first.ID || second.Attempts != 2 || second.Status != PurchaseActive {
		t.Fatalf("repurchase=%+v", second)
	}
	if got := merchantBalance(t, db); got != 4000 {
		t.Fatalf("merchant balance=%d", got)
	}
	assertLedgerKey(t, db, "group-purchase:merchant-1:group-1:2")
	if group := merchantGroup(t, db); group == nil || *group != "group-1" {
		t.Fatalf("merchant group=%v", group)
	}
}

func TestPlansAndPriceUpdates(t *testing.T) {
	service, db := groupshopFixture(t, "groupshop-plans")
	ctx := context.Background()
	seedGroup(t, db, "group-a", "A 组", 1000, "active")
	seedGroup(t, db, "group-b", "B 组", 3000, "active")
	seedGroup(t, db, "group-free", "免费组", 0, "active")
	seedGroup(t, db, "group-off", "停用组", 500, "disabled")

	plans, err := service.Plans(ctx)
	if err != nil || len(plans) != 2 || plans[0].ID != "group-a" || plans[1].ID != "group-b" {
		t.Fatalf("plans=%+v err=%v", plans, err)
	}
	all, err := service.AdminPlans(ctx)
	if err != nil || len(all) != 4 {
		t.Fatalf("admin plans=%d err=%v", len(all), err)
	}
	updated, err := service.SetPrice(ctx, "group-a", 2500)
	if err != nil || updated.PriceCents != 2500 {
		t.Fatalf("set price=%+v err=%v", updated, err)
	}
	if _, err := service.SetPrice(ctx, "group-a", -1); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("negative price err=%v", err)
	}
	if _, err := service.SetPrice(ctx, "missing", 100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing price err=%v", err)
	}
	// Setting the price to zero removes the group from the sale list.
	if _, err := service.SetPrice(ctx, "group-b", 0); err != nil {
		t.Fatal(err)
	}
	plans, err = service.Plans(ctx)
	if err != nil || len(plans) != 1 || plans[0].ID != "group-a" {
		t.Fatalf("plans after zeroing=%+v err=%v", plans, err)
	}
}

func TestPurchaseListingFilters(t *testing.T) {
	service, db := groupshopFixture(t, "groupshop-list")
	ctx := context.Background()
	seedGroup(t, db, "group-1", "黄金组", 1000, "active")
	seedGroup(t, db, "group-2", "铂金组", 2000, "active")
	if _, _, err := service.Purchase(ctx, "merchant-1", "group-1"); err != nil {
		t.Fatal(err)
	}
	second, _, err := service.Purchase(ctx, "merchant-1", "group-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refund(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	items, total, err := service.Purchases(ctx, PurchaseFilter{}, Page{Number: 1, Size: 10})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("all purchases total=%d items=%d err=%v", total, len(items), err)
	}
	refunded, total, err := service.Purchases(ctx, PurchaseFilter{Status: PurchaseRefunded}, Page{Number: 1, Size: 10})
	if err != nil || total != 1 || refunded[0].ID != second.ID {
		t.Fatalf("refunded total=%d err=%v", total, err)
	}
	_, total, err = service.Purchases(ctx, PurchaseFilter{MerchantID: "merchant-2"}, Page{Number: 1, Size: 10})
	if err != nil || total != 0 {
		t.Fatalf("other merchant total=%d err=%v", total, err)
	}
	if _, _, err := service.Purchases(ctx, PurchaseFilter{Status: "pending"}, Page{Number: 1, Size: 10}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad status err=%v", err)
	}
}

func merchantGroup(t *testing.T, db *gorm.DB) *string {
	t.Helper()
	var groupID *string
	if err := db.Raw("SELECT group_id FROM merchants WHERE id = ?", "merchant-1").Scan(&groupID).Error; err != nil {
		t.Fatal(err)
	}
	return groupID
}

func merchantBalance(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	return balanceOf(t, db, "merchant", "merchant-1")
}

func balanceOf(t *testing.T, db *gorm.DB, ownerType, ownerID string) int64 {
	t.Helper()
	var account ledgermodel.Account
	if err := db.First(&account, "owner_type = ? AND owner_id = ? AND purpose = ?", ownerType, ownerID, "balance").Error; err != nil {
		if ownerType == "platform" {
			if err := db.First(&account, "owner_type = ? AND owner_id = ? AND purpose = ?", ownerType, ownerID, "cash").Error; err != nil {
				t.Fatalf("account %s/%s: %v", ownerType, ownerID, err)
			}
		} else {
			t.Fatalf("account %s/%s: %v", ownerType, ownerID, err)
		}
	}
	var balance ledgermodel.Balance
	if err := db.First(&balance, "account_id = ?", account.ID).Error; err != nil {
		t.Fatalf("balance %s/%s: %v", ownerType, ownerID, err)
	}
	return balance.Available
}

func assertLedgerKey(t *testing.T, db *gorm.DB, key string) {
	t.Helper()
	var count int64
	if err := db.Model(&ledgermodel.Transaction{}).Where("idempotency_key = ?", key).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("ledger key %q count=%d", key, count)
	}
}
