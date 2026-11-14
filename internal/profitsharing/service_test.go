package profitsharing

import (
	"context"
	"errors"
	"testing"
	"time"

	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

func TestReceiverValidationAndRateCap(t *testing.T) {
	service, db, _ := profitsharingFixture(t, "ps-receiver-validation")
	ctx := context.Background()

	first, err := service.CreateReceiver(ctx, ReceiverRequest{MerchantID: "merchant-1", Account: "acc-a", Name: "渠道A", RateBp: 5000})
	if err != nil || first.Status != ReceiverActive || first.ReceiverType != ReceiverInternal {
		t.Fatalf("first receiver: %+v err=%v", first, err)
	}
	if _, err := service.CreateReceiver(ctx, ReceiverRequest{MerchantID: "merchant-1", Account: "acc-b", Name: "渠道B", RateBp: 5000}); err != nil {
		t.Fatalf("second receiver: %v", err)
	}
	if _, err := service.CreateReceiver(ctx, ReceiverRequest{MerchantID: "merchant-1", Account: "acc-c", Name: "渠道C", RateBp: 1}); !errors.Is(err, ErrRateExceeded) {
		t.Fatalf("rate cap err=%v", err)
	}
	for name, request := range map[string]ReceiverRequest{
		"missing-merchant": {Account: "acc", Name: "n", RateBp: 100},
		"bad-type":         {MerchantID: "merchant-1", Account: "acc", Name: "n", RateBp: 100, ReceiverType: "nope"},
		"bad-rate":         {MerchantID: "merchant-1", Account: "acc", Name: "n", RateBp: 10001},
		"missing-account":  {MerchantID: "merchant-1", Name: "n", RateBp: 100},
		"duplicate":        {MerchantID: "merchant-1", Account: "acc-a", Name: "n", RateBp: 100},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.CreateReceiver(ctx, request); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err=%v", err)
			}
		})
	}

	// Raising one receiver above the remaining budget is refused; lowering is fine.
	if _, err := service.UpdateReceiver(ctx, "merchant-1", first.ID, ReceiverUpdate{RateBp: intPointer(6000)}); !errors.Is(err, ErrRateExceeded) {
		t.Fatalf("update over cap err=%v", err)
	}
	updated, err := service.UpdateReceiver(ctx, "merchant-1", first.ID, ReceiverUpdate{RateBp: intPointer(4000), Name: stringPointer("渠道A2")})
	if err != nil || updated.RateBp != 4000 || updated.Name != "渠道A2" {
		t.Fatalf("update: %+v err=%v", updated, err)
	}
	if _, err := service.UpdateReceiver(ctx, "merchant-2", first.ID, ReceiverUpdate{Name: stringPointer("x")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross merchant update err=%v", err)
	}
	if _, err := service.UpdateReceiver(ctx, "", "missing", ReceiverUpdate{Name: stringPointer("x")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("admin update missing err=%v", err)
	}

	var stored int64
	if err := db.Model(&Receiver{}).Count(&stored).Error; err != nil || stored != 2 {
		t.Fatalf("receivers=%d err=%v", stored, err)
	}
}

func TestExecuteSplitsAfterFeeAndPostsLedger(t *testing.T) {
	service, db, payment := profitsharingFixture(t, "ps-execute")
	service = service.WithFeeResolver(staticFee(100))
	ctx := context.Background()
	if _, err := service.CreateReceiver(ctx, ReceiverRequest{MerchantID: "merchant-1", Account: "acc-a", Name: "渠道A", RateBp: 3000}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateReceiver(ctx, ReceiverRequest{MerchantID: "merchant-1", Account: "acc-b", Name: "渠道B", RateBp: 2000}); err != nil {
		t.Fatal(err)
	}

	rows, err := service.Execute(ctx, payment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%+v", rows)
	}
	byAccount := map[string]int64{}
	for _, row := range rows {
		if row.Status != OrderDone || row.LedgerTransactionID == nil {
			t.Fatalf("row=%+v", row)
		}
		var receiver Receiver
		if err := db.First(&receiver, "id = ?", row.ReceiverID).Error; err != nil {
			t.Fatal(err)
		}
		byAccount[receiver.Account] = row.Amount
	}
	// fee 1% of 10000 = 100, base 9900, A 30% = 2970, B 20% = 1980.
	if byAccount["acc-a"] != 2970 || byAccount["acc-b"] != 1980 {
		t.Fatalf("amounts=%v", byAccount)
	}
	if got := balanceOf(t, db, "receiver", "acc-a"); got != 2970 {
		t.Fatalf("receiver A balance=%d", got)
	}
	if got := balanceOf(t, db, "receiver", "acc-b"); got != 1980 {
		t.Fatalf("receiver B balance=%d", got)
	}
	if got := balanceOf(t, db, "merchant", "merchant-1"); got != 10000-2970-1980 {
		t.Fatalf("merchant balance=%d", got)
	}
	assertSplitTransaction(t, db, payment.ID, 2970)

	// Replaying the split adds nothing.
	if _, err := service.Execute(ctx, payment.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&Order{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("split rows=%d err=%v", count, err)
	}
	if got := balanceOf(t, db, "merchant", "merchant-1"); got != 10000-2970-1980 {
		t.Fatalf("merchant balance after replay=%d", got)
	}
}

func TestExecuteKeepsChannelReceiverPending(t *testing.T) {
	service, db, payment := profitsharingFixture(t, "ps-channel")
	ctx := context.Background()
	if _, err := service.CreateReceiver(ctx, ReceiverRequest{MerchantID: "merchant-1", Account: "acc-channel", Name: "上游分账", RateBp: 1000, ReceiverType: ReceiverChannel}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateReceiver(ctx, ReceiverRequest{MerchantID: "merchant-1", Account: "acc-internal", Name: "内部方", RateBp: 1000}); err != nil {
		t.Fatal(err)
	}
	rows, err := service.Execute(ctx, payment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%+v", rows)
	}
	for _, row := range rows {
		if row.ReceiverID == "" {
			t.Fatalf("row=%+v", row)
		}
		if row.Status == OrderPending {
			if row.LedgerTransactionID != nil || row.Error == nil || row.ChannelBatchNo != nil {
				t.Fatalf("pending row=%+v", row)
			}
		} else if row.Status != OrderDone {
			t.Fatalf("row=%+v", row)
		}
	}
	if got := balanceOf(t, db, "receiver", "acc-internal"); got != 1000 {
		t.Fatalf("internal receiver balance=%d", got)
	}
	// A channel receiver never gets a ledger account until a channel split lands.
	var accounts int64
	if err := db.Model(&ledgermodel.Account{}).
		Where("owner_type = ? AND owner_id = ?", "receiver", "acc-channel").
		Count(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	if accounts != 0 {
		t.Fatalf("channel receiver has %d ledger accounts", accounts)
	}
}

func TestHookRunsInsidePaymentTransaction(t *testing.T) {
	service, db, payment := profitsharingFixture(t, "ps-hook")
	ctx := context.Background()
	if _, err := service.CreateReceiver(ctx, ReceiverRequest{MerchantID: "merchant-1", Account: "acc-hook", Name: "分账方", RateBp: 5000}); err != nil {
		t.Fatal(err)
	}
	hook := service.Hook()
	// The payment transaction credits the merchant, then runs the hook.
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := hook.OnPayment(ctx, tx, payment); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&Order{}).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("split not visible inside transaction: %d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := balanceOf(t, db, "receiver", "acc-hook"); got != 5000 {
		t.Fatalf("receiver balance=%d", got)
	}

	// Funding orders never split.
	deposit := payment
	deposit.ID = "order-deposit"
	deposit.TradeNo = "T-DEPOSIT"
	deposit.OutTradeNo = "OUT-DEPOSIT"
	deposit.Kind = paymentmodel.KindDeposit
	if err := db.Create(&deposit).Error; err != nil {
		t.Fatal(err)
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return hook.OnPayment(ctx, tx, deposit)
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&Order{}).Where("order_id = ?", deposit.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("deposit split rows=%d err=%v", count, err)
	}
}

func TestRetryRevivesFailedSplitAndKeepsChannelPending(t *testing.T) {
	service, db, payment := profitsharingFixture(t, "ps-retry")
	ctx := context.Background()
	internal, err := service.CreateReceiver(ctx, ReceiverRequest{MerchantID: "merchant-1", Account: "acc-retry", Name: "内部方", RateBp: 1000})
	if err != nil {
		t.Fatal(err)
	}
	channel, err := service.CreateReceiver(ctx, ReceiverRequest{MerchantID: "merchant-1", Account: "acc-wait", Name: "上游方", RateBp: 1000, ReceiverType: ReceiverChannel})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	failed := Order{ID: "split-failed", OrderID: payment.ID, MerchantID: "merchant-1", ReceiverID: internal.ID, Amount: 1000, Status: OrderFailed, CreatedAt: now, UpdatedAt: now}
	pending := Order{ID: "split-pending", OrderID: payment.ID, MerchantID: "merchant-1", ReceiverID: channel.ID, Amount: 1000, Status: OrderPending, CreatedAt: now, UpdatedAt: now}
	for _, row := range []*Order{&failed, &pending} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	recovered, err := service.Retry(ctx, failed.ID)
	if err != nil || recovered.Status != OrderDone || recovered.LedgerTransactionID == nil {
		t.Fatalf("retry failed row: %+v err=%v", recovered, err)
	}
	if got := balanceOf(t, db, "receiver", "acc-retry"); got != 1000 {
		t.Fatalf("retry balance=%d", got)
	}
	waited, err := service.Retry(ctx, pending.ID)
	if err != nil || waited.Status != OrderPending || waited.Error == nil {
		t.Fatalf("retry pending row: %+v err=%v", waited, err)
	}
	if _, err := service.Retry(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retry missing err=%v", err)
	}
}

func TestExecuteRejectsUnpaidOrder(t *testing.T) {
	service, db, payment := profitsharingFixture(t, "ps-unpaid")
	ctx := context.Background()
	if err := db.Model(&paymentmodel.Order{}).Where("id = ?", payment.ID).
		Update("status", paymentmodel.StatusPending).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, payment.ID); !errors.Is(err, ErrOrderNotPaid) {
		t.Fatalf("err=%v", err)
	}
	if _, err := service.Execute(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err=%v", err)
	}
}

// staticFee is a FeeResolver returning a fixed rate for every merchant.
type staticFee int

func (fee staticFee) RateBp(context.Context, string) (int, bool) { return int(fee), true }

func profitsharingFixture(t *testing.T, name string) (*Service, *gorm.DB, paymentmodel.Order) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&Receiver{}, &Order{}, &paymentmodel.Order{},
		&ledgermodel.Account{}, &ledgermodel.Balance{}, &ledgermodel.Transaction{},
		&ledgermodel.Entry{}, &ledgermodel.Freeze{},
	); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC) }
	ledger := ledgerservice.New(db, now)
	merchantAccount, err := ledger.CreateAccount(context.Background(), ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: "merchant-1", Purpose: "balance",
		Category: ledgermodel.CategoryLiability, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	platformAccount, err := ledger.CreateAccount(context.Background(), ledgerservice.AccountRequest{
		OwnerType: "platform", OwnerID: "platform", Purpose: "cash",
		Category: ledgermodel.CategoryAsset, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The payment income that a real callback posts before the hook runs.
	if _, err := ledger.Post(context.Background(), ledgerservice.TransactionRequest{
		IdempotencyKey: "payment:order-1", ReferenceType: "payment", ReferenceID: "order-1",
		Description: "merchant payment income",
		Postings: []ledgerservice.Posting{
			{AccountID: merchantAccount.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: 10000},
			{AccountID: platformAccount.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: 10000},
		},
	}); err != nil {
		t.Fatal(err)
	}
	at := now().UTC()
	payment := paymentmodel.Order{
		ID: "order-1", TradeNo: "T-ORDER-1", MerchantID: "merchant-1", OutTradeNo: "OUT-1",
		PaymentType: "alipay", Name: "test", Amount: 10000, Currency: "CNY",
		Status: paymentmodel.StatusPaid, Kind: paymentmodel.KindSale, NotifyURL: "https://merchant.example/notify",
		PaidAt: &at, ExpiresAt: at.Add(10 * time.Minute), CreatedAt: at, UpdatedAt: at,
	}
	if err := db.Create(&payment).Error; err != nil {
		t.Fatal(err)
	}
	return New(db, ledger, now), db, payment
}

func balanceOf(t *testing.T, db *gorm.DB, ownerType, ownerID string) int64 {
	t.Helper()
	var account ledgermodel.Account
	if err := db.First(&account, "owner_type = ? AND owner_id = ? AND purpose = ?", ownerType, ownerID, "balance").Error; err != nil {
		t.Fatalf("account %s/%s: %v", ownerType, ownerID, err)
	}
	var balance ledgermodel.Balance
	if err := db.First(&balance, "account_id = ?", account.ID).Error; err != nil {
		t.Fatalf("balance %s/%s: %v", ownerType, ownerID, err)
	}
	return balance.Available
}

func assertSplitTransaction(t *testing.T, db *gorm.DB, orderID string, amount int64) {
	t.Helper()
	var count int64
	if err := db.Model(&ledgermodel.Transaction{}).
		Where("idempotency_key LIKE ? AND reference_type = ?", "split:"+orderID+":%", "profit-sharing").
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("split transactions=%d", count)
	}
	var entry ledgermodel.Entry
	if err := db.Where("amount = ?", amount).First(&entry).Error; err != nil {
		t.Fatalf("entry with %d: %v", amount, err)
	}
}

func intPointer(value int) *int { return &value }
