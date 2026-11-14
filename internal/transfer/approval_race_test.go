package transfer

import (
	"context"
	"sync"
	"testing"

	ledgermodel "gorixpay/internal/ledger/model"
)

// TestApproveAndRejectRaceKeepsMoneyAndStatusConsistent pins the fix for the
// non-atomic approval path: releasing the freeze and posting the payout in
// separate transactions, then updating the order last, let a concurrent reject
// close a payout whose money had already been debited.
func TestApproveAndRejectRaceKeepsMoneyAndStatusConsistent(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-race")
	order, created, err := service.CreateAdmin(context.Background(), AdminRequest{
		MerchantID: merchantID, Amount: 1000, Destination: "6222000000000099", IdempotencyKey: "race-1",
	})
	if err != nil || !created {
		t.Fatalf("create payout: created=%v err=%v", created, err)
	}
	before, err := service.Balance(context.Background(), merchantID)
	if err != nil || before.Available != 4000 || before.Frozen != 1000 {
		t.Fatalf("balance before = %+v err=%v", before, err)
	}

	var wait sync.WaitGroup
	results := make([]error, 2)
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, results[0] = service.Approve(context.Background(), order.ID, "admin-approve")
	}()
	go func() {
		defer wait.Done()
		_, results[1] = service.Reject(context.Background(), order.ID, "admin-reject", "客户取消")
	}()
	wait.Wait()

	successes := 0
	for _, result := range results {
		if result == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("approve/reject results = %v, want exactly one success", results)
	}

	var stored Order
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	balance, err := service.Balance(context.Background(), merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.Frozen != 0 {
		t.Fatalf("frozen = %d, want 0 after the payout was decided", balance.Frozen)
	}
	var settlements int64
	if err := db.Model(&ledgermodel.Transaction{}).
		Where("idempotency_key = ?", "transfer-settle:"+order.ID).Count(&settlements).Error; err != nil {
		t.Fatal(err)
	}
	switch stored.Status {
	case StatusApproved:
		if balance.Available != 3000 {
			t.Fatalf("approved available = %d, want 3000 (5000 - 1000 payout)", balance.Available)
		}
		if settlements != 1 {
			t.Fatalf("approved settlement transactions = %d, want 1", settlements)
		}
		if stored.LedgerTransactionID == nil {
			t.Fatal("approved payout lost its ledger transaction id")
		}
	case StatusRejected:
		// The payout must not have moved a single fen: the freeze is released
		// back to available and nothing is debited.
		if balance.Available != 5000 {
			t.Fatalf("rejected available = %d, want 5000 (freeze released, nothing debited)", balance.Available)
		}
		if settlements != 0 {
			t.Fatalf("rejected settlement transactions = %d, want 0", settlements)
		}
	default:
		t.Fatalf("stored status = %q, want approved or rejected", stored.Status)
	}
}

// TestApproveIsIdempotent checks that a retried approval returns the stored
// order instead of posting a second payout.
func TestApproveIsIdempotent(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-approve-replay")
	order, _, err := service.CreateAdmin(context.Background(), AdminRequest{
		MerchantID: merchantID, Amount: 700, Destination: "6222000000000100", IdempotencyKey: "replay-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Approve(context.Background(), order.ID, "admin-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Approve(context.Background(), order.ID, "admin-1")
	if err != nil {
		t.Fatalf("second approval: %v", err)
	}
	if second.Status != StatusApproved || first.LedgerTransactionID == nil || second.LedgerTransactionID == nil {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if *first.LedgerTransactionID != *second.LedgerTransactionID {
		t.Fatalf("ledger transaction changed between replays: %s vs %s", *first.LedgerTransactionID, *second.LedgerTransactionID)
	}
	balance, err := service.Balance(context.Background(), merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.Available != 4300 || balance.Frozen != 0 {
		t.Fatalf("balance = %+v, want available 4300 frozen 0", balance)
	}
	var count int64
	if err := db.Model(&ledgermodel.Transaction{}).Where("idempotency_key = ?", "transfer-settle:"+order.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("settlement transactions = %d, want 1", count)
	}
}
