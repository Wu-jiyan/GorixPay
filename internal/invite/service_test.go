package invite

import (
	"context"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	paymentmodel "gorixpay/internal/payment/model"
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

func TestRebateHookCreditsInviterOnce(t *testing.T) {
	service, db := inviteFixture(t, "invite-hook")
	setting(t, db, "invite.enabled", "true")
	setting(t, db, "invite.rate_bp", "1000")
	hook := service.Hook()
	now := time.Now().UTC()
	order := paidOrder("order-1", "invitee-1", 1000, paymentmodel.KindSale, now)

	if err := db.Transaction(func(tx *gorm.DB) error { return hook.OnPayment(context.Background(), tx, order) }); err != nil {
		t.Fatal(err)
	}
	var rebates []Rebate
	if err := db.Find(&rebates).Error; err != nil || len(rebates) != 1 {
		t.Fatalf("rebates=%d err=%v", len(rebates), err)
	}
	if rebates[0].RebateAmount != 100 || rebates[0].RateBP != 1000 || rebates[0].InviterID != "inviter-1" {
		t.Fatalf("rebate: %+v", rebates[0])
	}
	if balance := inviterBalance(t, db); balance != 100 {
		t.Fatalf("inviter balance = %d", balance)
	}
	// A replayed hook must not double credit.
	if err := db.Transaction(func(tx *gorm.DB) error { return hook.OnPayment(context.Background(), tx, order) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Find(&rebates).Error; err != nil || len(rebates) != 1 {
		t.Fatalf("rebates after replay=%d err=%v", len(rebates), err)
	}
	if balance := inviterBalance(t, db); balance != 100 {
		t.Fatalf("inviter balance after replay = %d", balance)
	}
}

func TestRebateHookSkipsNonSaleOrdersAndDisabledFeature(t *testing.T) {
	service, db := inviteFixture(t, "invite-skip")
	setting(t, db, "invite.enabled", "true")
	setting(t, db, "invite.rate_bp", "1000")
	hook := service.Hook()
	now := time.Now().UTC()
	recharge := paidOrder("order-recharge", "invitee-1", 5000, paymentmodel.KindRecharge, now)
	if err := db.Transaction(func(tx *gorm.DB) error { return hook.OnPayment(context.Background(), tx, recharge) }); err != nil {
		t.Fatal(err)
	}
	setting(t, db, "invite.enabled", "false")
	sale := paidOrder("order-sale", "invitee-1", 5000, paymentmodel.KindSale, now)
	if err := db.Transaction(func(tx *gorm.DB) error { return hook.OnPayment(context.Background(), tx, sale) }); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&Rebate{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rebates=%d err=%v", count, err)
	}
	if balance := inviterBalance(t, db); balance != 0 {
		t.Fatalf("inviter balance = %d", balance)
	}
}

func TestInviteStatsAndRecords(t *testing.T) {
	service, db := inviteFixture(t, "invite-stats")
	setting(t, db, "invite.enabled", "true")
	setting(t, db, "invite.rate_bp", "500")
	hook := service.Hook()
	now := time.Now().UTC()
	order := paidOrder("order-1", "invitee-1", 2000, paymentmodel.KindSale, now)
	if err := db.Transaction(func(tx *gorm.DB) error { return hook.OnPayment(context.Background(), tx, order) }); err != nil {
		t.Fatal(err)
	}
	stats, err := service.Stats(context.Background(), "inviter-1")
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Enabled || stats.RateBP != 500 || stats.InvitedCount != 1 || stats.TotalIncome != 100 {
		t.Fatalf("stats: %+v", stats)
	}
	if stats.InviteCode != "INVITER1" || stats.InviteURL != "https://pay.example/register?invite=INVITER1" {
		t.Fatalf("invite identity: %+v", stats)
	}
	views, total, err := service.Records(context.Background(), "inviter-1", Page{Number: 1, Size: 10})
	if err != nil || total != 1 || len(views) != 1 {
		t.Fatalf("records total=%d len=%d err=%v", total, len(views), err)
	}
	if views[0].InviteeName != "invitee" {
		t.Fatalf("invitee name = %q", views[0].InviteeName)
	}
}

func inviteFixture(t *testing.T, name string) (*Service, *gorm.DB) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Rebate{}, &identitymodel.Merchant{}, &settingRow{},
		&ledgermodel.Account{}, &ledgermodel.Balance{}, &ledgermodel.Transaction{}, &ledgermodel.Entry{}); err != nil {
		t.Fatal(err)
	}
	code := "INVITER1"
	inviter := identitymodel.Merchant{ID: "inviter-1", DisplayName: "inviter", Status: identitymodel.StatusActive, InviteCode: &code}
	if err := db.Create(&inviter).Error; err != nil {
		t.Fatal(err)
	}
	inviterID := inviter.ID
	invitee := identitymodel.Merchant{ID: "invitee-1", DisplayName: "invitee", Status: identitymodel.StatusActive, InvitedBy: &inviterID}
	if err := db.Create(&invitee).Error; err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC) }
	ledger := ledgerservice.New(db, now)
	platform, err := ledger.CreateAccount(context.Background(), ledgerservice.AccountRequest{
		OwnerType: "platform", OwnerID: "platform", Purpose: "cash", Category: ledgermodel.CategoryAsset, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	inviterAccount, err := ledger.CreateAccount(context.Background(), ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: inviter.ID, Purpose: "balance", Category: ledgermodel.CategoryLiability, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Post(context.Background(), ledgerservice.TransactionRequest{
		IdempotencyKey: "seed:platform", ReferenceType: "seed", ReferenceID: "platform", Description: "seed",
		Postings: []ledgerservice.Posting{
			{AccountID: platform.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: 100000},
			{AccountID: inviterAccount.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: 100000},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// Reset the inviter balance to zero so rebates are the only income asserted.
	if err := db.Model(&ledgermodel.Balance{}).Where("account_id = ?", inviterAccount.ID).Updates(map[string]any{"available": 0}).Error; err != nil {
		t.Fatal(err)
	}
	return New(db, ledger, "https://pay.example", now), db
}

func paidOrder(id, merchantID string, amount int64, kind paymentmodel.Kind, now time.Time) paymentmodel.Order {
	return paymentmodel.Order{
		ID: id, TradeNo: "T" + id, MerchantID: merchantID, OutTradeNo: "OUT-" + id,
		PaymentType: "alipay", Name: "sale", Amount: amount, Currency: "CNY",
		Status: paymentmodel.StatusPaid, Kind: kind, NotifyURL: "https://merchant.example/notify",
		PaidAt: &now, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}
}

func inviterBalance(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var account ledgermodel.Account
	if err := db.First(&account, "owner_type = ? AND owner_id = ? AND purpose = ?", "merchant", "inviter-1", "balance").Error; err != nil {
		t.Fatal(err)
	}
	var balance ledgermodel.Balance
	if err := db.First(&balance, "account_id = ?", account.ID).Error; err != nil {
		t.Fatal(err)
	}
	return balance.Available
}

func setting(t *testing.T, db *gorm.DB, key, value string) {
	t.Helper()
	if err := db.Save(&settingRow{Key: key, Value: value, UpdatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestRebateRoundsDownAndSkipsZero(t *testing.T) {
	service, db := inviteFixture(t, "invite-rounding")
	setting(t, db, "invite.enabled", "true")
	setting(t, db, "invite.rate_bp", "1000")
	hook := service.Hook()
	now := time.Now().UTC()
	small := paidOrder("order-small", "invitee-1", 99, paymentmodel.KindSale, now)
	if err := db.Transaction(func(tx *gorm.DB) error { return hook.OnPayment(context.Background(), tx, small) }); err != nil {
		t.Fatal(err)
	}
	tiny := paidOrder("order-tiny", "invitee-1", 9, paymentmodel.KindSale, now)
	if err := db.Transaction(func(tx *gorm.DB) error { return hook.OnPayment(context.Background(), tx, tiny) }); err != nil {
		t.Fatal(err)
	}
	var rebates []Rebate
	if err := db.Order("order_id ASC").Find(&rebates).Error; err != nil {
		t.Fatal(err)
	}
	if len(rebates) != 1 || rebates[0].OrderID != "order-small" || rebates[0].RebateAmount != 9 {
		t.Fatalf("rebates: %+v", rebates)
	}
}
