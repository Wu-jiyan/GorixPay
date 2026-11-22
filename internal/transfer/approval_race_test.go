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
		// 创建时：available 5000 → 4000，frozen 0 → 1000（内部搬移，总量仍 5000）。
		// 审核通过：先按 available 记 1000 的打款支出（4000 → 3000），再把冻结退回
		// available（3000 → 4000），frozen 归零。净结果是 available 4000 + frozen 0，
		// 也就是 5000 里少了真正打出去的 1000，账是平的。
		//
		// 这里曾经断言 3000，等于要求「冻结既退回又消耗」，商户凭空少 1000。断言一直
		// 没被真正执行过 —— 两个 goroutine 并发时通常是 reject 先抢到状态，approve
		// 分支只有加上 -race 改变调度后才真正跑到。
		if balance.Available != 4000 {
			t.Fatalf("approved available = %d, want 4000 (freeze released, then the 1000 payout debited)", balance.Available)
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
