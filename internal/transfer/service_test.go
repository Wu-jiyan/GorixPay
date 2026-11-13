package transfer

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

func TestCreateFreezesFundsAndIsIdempotent(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-create")
	request := Request{
		MerchantID: merchantID, OutTransferNo: "OUT-1", Amount: 1000, Currency: "CNY",
		AccountName: "张三", AccountNo: "6222000000000001", BankName: "ICBC", BranchName: "上海分行",
	}
	order, created, err := service.Create(context.Background(), request)
	if err != nil || !created || order.Status != StatusPending {
		t.Fatalf("create: %+v created=%v err=%v", order, created, err)
	}
	balance, err := service.Balance(context.Background(), merchantID)
	if err != nil || balance.Available != 4000 || balance.Frozen != 1000 {
		t.Fatalf("balance: %+v err=%v", balance, err)
	}
	replay, created, err := service.Create(context.Background(), request)
	if err != nil || created || replay.ID != order.ID {
		t.Fatalf("idempotent create: %+v created=%v err=%v", replay, created, err)
	}
	conflicting := request
	conflicting.Amount = 2000
	if _, _, err := service.Create(context.Background(), conflicting); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	var count int64
	if err := db.Model(&Order{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestCreateRejectsInsufficientBalance(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-insufficient")
	_, _, err := service.Create(context.Background(), Request{
		MerchantID: merchantID, OutTransferNo: "OUT-BIG", Amount: 6000, Currency: "CNY", AccountNo: "6222000000000002",
	})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("err=%v", err)
	}
	var count int64
	if err := db.Model(&Order{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected order persisted: count=%d err=%v", count, err)
	}
}

func TestApproveConsumesFreezeAndDebitsPlatform(t *testing.T) {
	service, _, merchantID := transferFixture(t, "transfer-approve")
	order, _, err := service.Create(context.Background(), Request{
		MerchantID: merchantID, OutTransferNo: "OUT-APPROVE", Amount: 1000, Currency: "CNY", AccountNo: "6222000000000003",
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.Approve(context.Background(), order.ID, "admin-1")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != StatusApproved || approved.LedgerTransactionID == nil || approved.ProcessedBy == nil || *approved.ProcessedBy != "admin-1" {
		t.Fatalf("approved: %+v", approved)
	}
	balance, err := service.Balance(context.Background(), merchantID)
	if err != nil || balance.Available != 4000 || balance.Frozen != 0 {
		t.Fatalf("balance: %+v err=%v", balance, err)
	}
	replay, err := service.Approve(context.Background(), order.ID, "admin-1")
	if err != nil || replay.Status != StatusApproved {
		t.Fatalf("approve replay: %+v err=%v", replay, err)
	}
	if _, err := service.Reject(context.Background(), order.ID, "admin-1", "too late"); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("reject after approve err=%v", err)
	}
}

func TestRejectReleasesFreeze(t *testing.T) {
	service, _, merchantID := transferFixture(t, "transfer-reject")
	order, _, err := service.Create(context.Background(), Request{
		MerchantID: merchantID, OutTransferNo: "OUT-REJECT", Amount: 1000, Currency: "CNY", AccountNo: "6222000000000004",
	})
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := service.Reject(context.Background(), order.ID, "admin-2", "账户信息有误")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != StatusRejected || rejected.RejectionReason == nil || *rejected.RejectionReason != "账户信息有误" {
		t.Fatalf("rejected: %+v", rejected)
	}
	balance, err := service.Balance(context.Background(), merchantID)
	if err != nil || balance.Available != 5000 || balance.Frozen != 0 {
		t.Fatalf("balance: %+v err=%v", balance, err)
	}
	if _, err := service.Reject(context.Background(), order.ID, "admin-2", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty reason err=%v", err)
	}
}

func TestListGetAndFindRespectMerchantScope(t *testing.T) {
	service, _, merchantID := transferFixture(t, "transfer-list")
	first, _, err := service.Create(context.Background(), Request{
		MerchantID: merchantID, OutTransferNo: "OUT-L1", Amount: 100, Currency: "CNY", AccountNo: "A1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Create(context.Background(), Request{
		MerchantID: merchantID, OutTransferNo: "OUT-L2", Amount: 200, Currency: "CNY", AccountNo: "A2",
	}); err != nil {
		t.Fatal(err)
	}
	orders, total, err := service.List(context.Background(), Filter{MerchantID: merchantID}, Page{Number: 1, Size: 10})
	if err != nil || total != 2 || len(orders) != 2 {
		t.Fatalf("list: total=%d len=%d err=%v", total, len(orders), err)
	}
	if _, err := service.Get(context.Background(), "other-merchant", first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross merchant get err=%v", err)
	}
	found, err := service.Find(context.Background(), merchantID, "", "OUT-L1")
	if err != nil || found.ID != first.ID {
		t.Fatalf("find by out transfer no: %+v err=%v", found, err)
	}
	found, err = service.Find(context.Background(), merchantID, first.TransferNo, "")
	if err != nil || found.ID != first.ID {
		t.Fatalf("find by transfer no: %+v err=%v", found, err)
	}
	if _, err := service.Find(context.Background(), "other-merchant", first.TransferNo, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross merchant find err=%v", err)
	}
}

func transferFixture(t *testing.T, name string) (*Service, *gorm.DB, string) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Order{}, &ledgermodel.Account{}, &ledgermodel.Balance{}, &ledgermodel.Transaction{}, &ledgermodel.Entry{}, &ledgermodel.Freeze{}); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC) }
	ledger := ledgerservice.New(db, now)
	merchantID := "merchant-1"
	merchant, err := ledger.CreateAccount(context.Background(), ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: merchantID, Purpose: "balance", Category: ledgermodel.CategoryLiability, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	platform, err := ledger.CreateAccount(context.Background(), ledgerservice.AccountRequest{
		OwnerType: "platform", OwnerID: "platform", Purpose: "cash", Category: ledgermodel.CategoryAsset, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Post(context.Background(), ledgerservice.TransactionRequest{
		IdempotencyKey: "seed:" + merchantID, ReferenceType: "seed", ReferenceID: merchantID, Description: "seed balance",
		Postings: []ledgerservice.Posting{
			{AccountID: platform.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: 5000},
			{AccountID: merchant.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: 5000},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return New(db, ledger, now), db, merchantID
}
