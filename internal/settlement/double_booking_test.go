package settlement

import (
	"context"
	"testing"

	financemodel "gorixpay/internal/finance/model"
	ledgermodel "gorixpay/internal/ledger/model"
	transferservice "gorixpay/internal/transfer"
)

// TestSettlementApprovalAndAutoTransferMoveTheMoneyOnce pins the fix for the
// double payout: approving a settlement already debited the merchant balance, and
// the automatic payout that followed froze and debited the same amount again, so
// a 4000 settlement left the account as 8000.
func TestSettlementApprovalAndAutoTransferMoveTheMoneyOnce(t *testing.T) {
	env := newSettlementEnv(t, "settle-double-book")
	env.seedBalance(t, env.merchant.ID, 4000)
	if _, err := env.service.CreateRule(context.Background(), RuleInput{
		Name: "auto payout", MinAmountCents: 0, FeeBP: 0, AutoTransfer: true,
	}); err != nil {
		t.Fatal(err)
	}
	if generated, err := env.service.RunAutoSettle(context.Background(), env.now); err != nil || generated != 1 {
		t.Fatalf("generated=%d err=%v", generated, err)
	}
	settlement := env.settlement(t, env.merchant.ID)
	if settlement.Amount != 4000 {
		t.Fatalf("settlement amount = %d, want 4000", settlement.Amount)
	}

	results, err := env.service.BatchProcess(context.Background(), []string{settlement.ID}, "approve", "", "admin-1")
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if results[0].Status != string(financemodel.StatusApproved) {
		t.Fatalf("status=%q", results[0].Status)
	}
	if results[0].TransferStatus != TransferDone {
		t.Fatalf("transfer status=%q reason=%q, want done", results[0].TransferStatus, results[0].Error)
	}

	// The merchant paid out exactly the settlement amount: nothing frozen, nothing
	// left behind.
	merchantAccount, available, frozen := env.accountBalance(t, "merchant", env.merchant.ID, "balance")
	if available != 0 || frozen != 0 {
		t.Fatalf("merchant available=%d frozen=%d, want 0/0", available, frozen)
	}
	var debits int64
	if err := env.db.Model(&ledgermodel.Entry{}).
		Where("account_id = ? AND direction = ?", merchantAccount, ledgermodel.DirectionDebit).
		Count(&debits).Error; err != nil {
		t.Fatal(err)
	}
	if debits != 1 {
		t.Fatalf("merchant debit entries = %d, want exactly 1 (the settlement itself)", debits)
	}

	// The payout row is documentation of the bank transfer: it must not carry its
	// own ledger transaction, because the settlement approval already booked it.
	var order transferservice.Order
	if err := env.db.First(&order, "merchant_id = ?", env.merchant.ID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Amount != 4000 || order.Status != transferservice.StatusApproved {
		t.Fatalf("payout=%+v", order)
	}
	if order.LedgerTransactionID == nil || *order.LedgerTransactionID == "" {
		t.Fatal("auto payout lost its link to the settlement ledger transaction")
	}
	var transferPostings int64
	if err := env.db.Model(&ledgermodel.Entry{}).
		Where("transaction_id IN (SELECT id FROM ledger_transactions WHERE reference_type = ? AND reference_id = ?)", "transfer", order.ID).
		Count(&transferPostings).Error; err != nil {
		t.Fatal(err)
	}
	if transferPostings != 0 {
		t.Fatalf("auto payout posted %d ledger entries, want 0", transferPostings)
	}

	// Platform clearing nets to zero once the merchant has been paid out.
	_, platformAvailable, _ := env.accountBalance(t, "platform", "platform", "cash")
	if platformAvailable != 0 {
		t.Fatalf("platform cash = %d, want 0 after the payout cleared the liability", platformAvailable)
	}
}

// TestAutoTransferNeedsNoExtraBalance checks the operational consequence: a
// settlement equal to the whole balance used to fail its own payout with
// "insufficient transfer balance" because the approval had already taken it.
func TestAutoTransferNeedsNoExtraBalance(t *testing.T) {
	env := newSettlementEnv(t, "settle-exact-balance")
	env.seedBalance(t, env.merchant.ID, 2500)
	if _, err := env.service.CreateRule(context.Background(), RuleInput{
		Name: "exact", MinAmountCents: 0, FeeBP: 0, AutoTransfer: true,
	}); err != nil {
		t.Fatal(err)
	}
	if generated, err := env.service.RunAutoSettle(context.Background(), env.now); err != nil || generated != 1 {
		t.Fatalf("generated=%d err=%v", generated, err)
	}
	settlement := env.settlement(t, env.merchant.ID)
	result, err := env.service.AutoTransfer(context.Background(), settlement.ID)
	if err == nil {
		t.Fatalf("auto transfer before approval unexpectedly succeeded: %+v", result)
	}
	results, err := env.service.BatchProcess(context.Background(), []string{settlement.ID}, "approve", "", "admin-1")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].TransferStatus != TransferDone {
		t.Fatalf("transfer status=%q reason=%q", results[0].TransferStatus, results[0].Error)
	}
	if _, available, frozen := env.accountBalance(t, "merchant", env.merchant.ID, "balance"); available != 0 || frozen != 0 {
		t.Fatalf("available=%d frozen=%d, want 0/0", available, frozen)
	}
}

func (env *settlementEnv) accountBalance(t *testing.T, ownerType, ownerID, purpose string) (string, int64, int64) {
	t.Helper()
	var account ledgermodel.Account
	if err := env.db.First(&account, "owner_type = ? AND owner_id = ? AND purpose = ?", ownerType, ownerID, purpose).Error; err != nil {
		t.Fatalf("account %s/%s/%s: %v", ownerType, ownerID, purpose, err)
	}
	var balance ledgermodel.Balance
	if err := env.db.First(&balance, "account_id = ?", account.ID).Error; err != nil {
		t.Fatalf("balance for %s: %v", account.ID, err)
	}
	return account.ID, balance.Available, balance.Frozen
}
