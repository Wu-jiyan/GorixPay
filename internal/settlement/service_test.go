package settlement

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	financemodel "gorixpay/internal/finance/model"
	financeservice "gorixpay/internal/finance/service"
	identitymodel "gorixpay/internal/identity/model"
	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	transferservice "gorixpay/internal/transfer"

	"gorm.io/gorm"
)

func TestRunAutoSettleCreatesOneSettlementPerMerchantPerDay(t *testing.T) {
	env := newSettlementEnv(t, "settle-daily")
	env.seedBalance(t, env.merchant.ID, 10000)
	rule, err := env.service.CreateRule(context.Background(), RuleInput{
		Name: "daily auto", MinAmountCents: 1000, FeeBP: 100, Period: "daily", AutoTransfer: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := env.now

	generated, err := env.service.RunAutoSettle(context.Background(), now)
	if err != nil || generated != 1 {
		t.Fatalf("first run generated=%d err=%v", generated, err)
	}
	settlement := env.settlement(t, env.merchant.ID)
	if settlement.Amount != 10000 || settlement.FeeCents != 100 || settlement.Source != SourceAuto || settlement.Status != string(financemodel.StatusPending) {
		t.Fatalf("settlement=%+v", settlement)
	}
	if settlement.TransferStatus != TransferNone {
		t.Fatalf("transfer status=%q", settlement.TransferStatus)
	}

	// Scheduled rerun on the same day is skipped by the last_run_at claim.
	generated, err = env.service.RunAutoSettle(context.Background(), now.Add(2*time.Hour))
	if err != nil || generated != 0 {
		t.Fatalf("second scheduled run generated=%d err=%v", generated, err)
	}
	// Manual run bypasses the claim but the settlement number keeps it idempotent.
	generated, err = env.service.RunRule(context.Background(), rule.ID, now)
	if err != nil || generated != 0 {
		t.Fatalf("manual run generated=%d err=%v", generated, err)
	}
	var count int64
	if err := env.db.Model(&settlementRow{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("settlements=%d err=%v", count, err)
	}
}

func TestRunAutoSettleHonoursMinAmountAndFeeResolver(t *testing.T) {
	env := newSettlementEnv(t, "settle-min")
	env.seedBalance(t, env.merchant.ID, 500)
	if _, err := env.service.CreateRule(context.Background(), RuleInput{Name: "big min", MinAmountCents: 1000}); err != nil {
		t.Fatal(err)
	}
	generated, err := env.service.RunAutoSettle(context.Background(), env.now)
	if err != nil || generated != 0 {
		t.Fatalf("small balance generated=%d err=%v", generated, err)
	}

	env.seedBalance(t, env.merchant.ID, 4500)
	if _, err := env.service.CreateRule(context.Background(), RuleInput{Name: "resolver rate", MinAmountCents: 0, FeeBP: 0}); err != nil {
		t.Fatal(err)
	}
	// Two active rules would both settle; pause the first one so only the
	// resolver driven rule produces an order.
	if err := env.db.Model(&Rule{}).Where("name = ?", "big min").Update("status", RulePaused).Error; err != nil {
		t.Fatal(err)
	}
	generated, err = env.service.RunAutoSettle(context.Background(), env.now)
	if err != nil || generated != 1 {
		t.Fatalf("generated=%d err=%v", generated, err)
	}
	settlement := env.settlement(t, env.merchant.ID)
	if settlement.Amount != 5000 || settlement.FeeCents != 125 {
		t.Fatalf("amount=%d fee=%d", settlement.Amount, settlement.FeeCents)
	}
}

func TestRunAutoSettleSkipsMerchantsWithoutSettlementProfile(t *testing.T) {
	env := newSettlementEnv(t, "settle-no-profile")
	env.seedBalance(t, env.merchant.ID, 10000)
	if err := env.db.Model(&identitymodel.Merchant{}).Where("id = ?", env.merchant.ID).Update("settle_account", nil).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := env.service.CreateRule(context.Background(), RuleInput{Name: "all", MinAmountCents: 0}); err != nil {
		t.Fatal(err)
	}
	generated, err := env.service.RunAutoSettle(context.Background(), env.now)
	if err != nil || generated != 0 {
		t.Fatalf("generated=%d err=%v", generated, err)
	}
}

func TestBatchProcessApprovesAndAutoTransfers(t *testing.T) {
	env := newSettlementEnv(t, "settle-batch")
	env.seedBalance(t, env.merchant.ID, 10000)
	if _, err := env.service.CreateRule(context.Background(), RuleInput{Name: "auto", MinAmountCents: 0, AutoTransfer: true}); err != nil {
		t.Fatal(err)
	}
	if generated, err := env.service.RunAutoSettle(context.Background(), env.now); err != nil || generated != 1 {
		t.Fatalf("generated=%d err=%v", generated, err)
	}
	settlement := env.settlement(t, env.merchant.ID)
	// A new payment lands before approval so the payout freeze has funds.
	env.seedBalance(t, env.merchant.ID, 10000)

	results, err := env.service.BatchProcess(context.Background(), []string{settlement.ID}, "approve", "", "admin-1")
	if err != nil || len(results) != 1 || results[0].Status != string(financemodel.StatusApproved) {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if results[0].TransferStatus != TransferDone {
		t.Fatalf("transfer status=%q", results[0].TransferStatus)
	}
	transferred := env.settlement(t, env.merchant.ID)
	if transferred.TransferStatus != TransferDone || transferred.TransferAt == nil {
		t.Fatalf("settlement=%+v", transferred)
	}
	var payout transferservice.Order
	if err := env.db.First(&payout, "merchant_id = ?", env.merchant.ID).Error; err != nil {
		t.Fatal(err)
	}
	if payout.OutTransferNo != autoOutTransferNo(settlement.SettlementNo) || payout.Amount != settlement.Amount {
		t.Fatalf("payout=%+v", payout)
	}
	// Replaying the approval must not create a second payout.
	if _, err := env.service.BatchProcess(context.Background(), []string{settlement.ID}, "approve", "", "admin-1"); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := env.db.Model(&transferservice.Order{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("payouts=%d err=%v", count, err)
	}
}

func TestAutoTransferFailsWithoutSettlementProfile(t *testing.T) {
	env := newSettlementEnv(t, "settle-transfer-fail")
	if _, err := env.service.CreateRule(context.Background(), RuleInput{Name: "auto", MinAmountCents: 0, AutoTransfer: true}); err != nil {
		t.Fatal(err)
	}
	env.seedBalance(t, env.merchant.ID, 10000)
	settlement, created, err := env.finance.CreateSettlement(context.Background(), financeservice.CreateSettlementRequest{
		MerchantID: env.merchant.ID, OutSettlementNo: "MANUAL-1", Amount: 1000, Currency: "CNY", Destination: "6222333444555",
	})
	if err != nil || !created {
		t.Fatalf("create settlement created=%v err=%v", created, err)
	}
	if _, err := env.finance.ProcessSettlement(context.Background(), financeservice.ProcessRequest{ID: settlement.ID, Action: "approve", ProcessedBy: "admin-1"}); err != nil {
		t.Fatal(err)
	}
	if err := env.db.Model(&identitymodel.Merchant{}).Where("id = ?", env.merchant.ID).Update("settle_account", nil).Error; err != nil {
		t.Fatal(err)
	}
	result, err := env.service.AutoTransfer(context.Background(), settlement.ID)
	if err != nil || !result.Skipped || result.TransferStatus != TransferFailed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	stored := env.settlement(t, env.merchant.ID)
	if stored.RejectionReason == nil || stored.TransferStatus != TransferFailed {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestAutoTransferSkipsWithoutRuleAndBeforeApproval(t *testing.T) {
	env := newSettlementEnv(t, "settle-transfer-skip")
	env.seedBalance(t, env.merchant.ID, 10000)
	settlement, created, err := env.finance.CreateSettlement(context.Background(), financeservice.CreateSettlementRequest{
		MerchantID: env.merchant.ID, OutSettlementNo: "MANUAL-2", Amount: 1000, Currency: "CNY", Destination: "6222333444555",
	})
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if _, err := env.service.AutoTransfer(context.Background(), settlement.ID); !errors.Is(err, ErrSettlementNotReady) {
		t.Fatalf("pending settlement err=%v", err)
	}
	if _, err := env.finance.ProcessSettlement(context.Background(), financeservice.ProcessRequest{ID: settlement.ID, Action: "approve", ProcessedBy: "admin-1"}); err != nil {
		t.Fatal(err)
	}
	result, err := env.service.AutoTransfer(context.Background(), settlement.ID)
	if err != nil || !result.Skipped || result.TransferStatus != TransferNone {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestStartRunsOnceAndStopsWithContext(t *testing.T) {
	env := newSettlementEnv(t, "settle-start")
	env.seedBalance(t, env.merchant.ID, 10000)
	if _, err := env.service.CreateRule(context.Background(), RuleInput{Name: "ticker", MinAmountCents: 0}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	env.service.Start(ctx, 10*time.Millisecond)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := env.db.Model(&settlementRow{}).Count(&count).Error; err == nil && count == 1 {
			cancel()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	t.Fatal("scheduler did not create a settlement in time")
}

type settlementEnv struct {
	db        *gorm.DB
	service   *Service
	finance   *financeservice.Service
	ledger    *ledgerservice.Service
	transfers *transferservice.Service
	merchant  identitymodel.Merchant
	now       time.Time
}

func newSettlementEnv(t *testing.T, name string) *settlementEnv {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Rule{}, &settlementRow{}, &identitymodel.Merchant{},
		&ledgermodel.Account{}, &ledgermodel.Balance{}, &ledgermodel.Transaction{}, &ledgermodel.Entry{},
		&ledgermodel.Freeze{}, &transferservice.Order{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	ledger := ledgerservice.New(db, func() time.Time { return now })
	finance := financeservice.New(db, ledger, func() time.Time { return now })
	transfers := transferservice.New(db, ledger, func() time.Time { return now })
	feeResolver := func(context.Context, string) (int, bool) { return 250, true }
	service := New(db, finance, transfers, feeResolver, func() time.Time { return now })
	account := "6222333444555"
	merchant := identitymodel.Merchant{ID: "merchant-1", DisplayName: "merchant", Status: identitymodel.StatusActive, SettleAccount: &account}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	return &settlementEnv{db: db, service: service, finance: finance, ledger: ledger, transfers: transfers, merchant: merchant, now: now}
}

// seedSequence keeps seeded ledger posts distinct across calls; a wall clock
// stamp is not enough on Windows where two calls can land in the same tick.
var seedSequence atomic.Int64

func (env *settlementEnv) seedBalance(t *testing.T, merchantID string, amount int64) {
	t.Helper()
	ctx := context.Background()
	merchantAccount, err := env.ledger.CreateAccount(ctx, ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: merchantID, Purpose: "balance", Category: ledgermodel.CategoryLiability, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	platformAccount, err := env.ledger.CreateAccount(ctx, ledgerservice.AccountRequest{
		OwnerType: "platform", OwnerID: "platform", Purpose: "cash", Category: ledgermodel.CategoryAsset, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("seed:%s:%d", merchantID, seedSequence.Add(1))
	if _, err := env.ledger.Post(ctx, ledgerservice.TransactionRequest{
		IdempotencyKey: key, ReferenceType: "seed", ReferenceID: merchantID, Description: "seed",
		Postings: []ledgerservice.Posting{
			{AccountID: platformAccount.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: amount},
			{AccountID: merchantAccount.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: amount},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func (env *settlementEnv) settlement(t *testing.T, merchantID string) settlementRow {
	t.Helper()
	var row settlementRow
	if err := env.db.Order("created_at DESC").First(&row, "merchant_id = ?", merchantID).Error; err != nil {
		t.Fatal(err)
	}
	return row
}
