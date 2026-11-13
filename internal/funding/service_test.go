package funding

import (
	"context"
	"errors"
	"testing"
	"time"

	financemodel "gorixpay/internal/finance/model"
	financeservice "gorixpay/internal/finance/service"
	identitymodel "gorixpay/internal/identity/model"
	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	"gorixpay/internal/outbox"
	paymentcallback "gorixpay/internal/payment/callback"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

const testSiteURL = "https://gorix.example"

func TestCreateRechargeWritesFundingOrder(t *testing.T) {
	service, db, _, _ := fundingDatabase(t, "funding-create-recharge")
	ctx := context.Background()
	result, err := service.CreateRecharge(ctx, "merchant-1", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if result.Amount != 5000 || result.TradeNo == "" {
		t.Fatalf("result=%+v", result)
	}
	var order paymentmodel.Order
	if err := db.Where("trade_no = ?", result.TradeNo).First(&order).Error; err != nil {
		t.Fatal(err)
	}
	if order.Kind != paymentmodel.KindRecharge || order.PaymentType != "cashier" || order.Status != paymentmodel.StatusPending {
		t.Fatalf("order=%+v", order)
	}
	if order.NotifyURL != testSiteURL+"/api/v1/funding/notify" || order.Amount != 5000 {
		t.Fatalf("order=%+v", order)
	}
	if !order.ExpiresAt.After(time.Now().Add(20 * time.Minute)) {
		t.Fatalf("funding expiry not extended: %v", order.ExpiresAt)
	}
	if result.CashierURL != testSiteURL+"/cashier.php?trade_no="+order.TradeNo {
		t.Fatalf("cashier url=%q", result.CashierURL)
	}
	for _, amount := range []int64{0, -1} {
		if _, err := service.CreateRecharge(ctx, "merchant-1", amount); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("amount %d err=%v", amount, err)
		}
	}
	if _, err := service.CreateRecharge(ctx, "  ", 100); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty merchant err=%v", err)
	}
}

func TestCreateFundingOrderRequiresSiteURL(t *testing.T) {
	_, db, _, ledgerWriter := fundingDatabase(t, "funding-no-site")
	service := New(db, ledgerWriter, "")
	if _, err := service.CreateRecharge(context.Background(), "merchant-1", 100); !errors.Is(err, ErrSiteURLMissing) {
		t.Fatalf("err=%v", err)
	}
}

func TestRechargePaymentCreditsMerchantBalanceOnce(t *testing.T) {
	service, db, callbacks, _ := fundingDatabase(t, "funding-recharge-paid")
	ctx := context.Background()
	result, err := service.CreateRecharge(ctx, "merchant-1", 5000)
	if err != nil {
		t.Fatal(err)
	}
	order := loadOrder(t, db, result.TradeNo)
	paid, applied, err := callbacks.Complete(ctx, paymentcallback.Completion{
		OrderID: order.ID, ChannelID: "channel-sandbox", ChannelTradeNo: "SANDBOX" + order.TradeNo,
		Amount: order.Amount, Digest: "recharge-digest",
	})
	if err != nil || !applied || paid.Status != paymentmodel.StatusPaid {
		t.Fatalf("complete applied=%v status=%v err=%v", applied, paid.Status, err)
	}
	if balance := accountTotal(t, db, "merchant-1", "balance"); balance != 5000 {
		t.Fatalf("merchant balance=%d", balance)
	}
	var messages int64
	if err := db.Model(&outbox.Message{}).Where("aggregate_id = ?", order.ID).Count(&messages).Error; err != nil || messages != 0 {
		t.Fatalf("recharge must not notify the merchant: messages=%d err=%v", messages, err)
	}
	_, applied, err = callbacks.Complete(ctx, paymentcallback.Completion{
		OrderID: order.ID, ChannelID: "channel-sandbox", ChannelTradeNo: "SANDBOX" + order.TradeNo,
		Amount: order.Amount, Digest: "recharge-digest",
	})
	if err != nil || applied {
		t.Fatalf("replayed complete applied=%v err=%v", applied, err)
	}
	if balance := accountTotal(t, db, "merchant-1", "balance"); balance != 5000 {
		t.Fatalf("merchant balance after replay=%d", balance)
	}
}

func TestDepositPaymentCreditsDepositAccountOnce(t *testing.T) {
	service, db, callbacks, _ := fundingDatabase(t, "funding-deposit-paid")
	ctx := context.Background()
	result, err := service.CreateDeposit(ctx, "merchant-1", 2000)
	if err != nil {
		t.Fatal(err)
	}
	order := loadOrder(t, db, result.TradeNo)
	if order.Kind != paymentmodel.KindDeposit {
		t.Fatalf("order kind=%q", order.Kind)
	}
	if balance, err := service.DepositBalance(ctx, "merchant-1"); err != nil || balance != 0 {
		t.Fatalf("deposit before payment=%d err=%v", balance, err)
	}
	if _, applied, err := callbacks.Complete(ctx, paymentcallback.Completion{
		OrderID: order.ID, ChannelID: "channel-sandbox", ChannelTradeNo: "SANDBOX" + order.TradeNo,
		Amount: order.Amount, Digest: "deposit-digest",
	}); err != nil || !applied {
		t.Fatalf("complete applied=%v err=%v", applied, err)
	}
	if balance, err := service.DepositBalance(ctx, "merchant-1"); err != nil || balance != 2000 {
		t.Fatalf("deposit balance=%d err=%v", balance, err)
	}
	if balance := accountTotal(t, db, "merchant-1", "balance"); balance != 0 {
		t.Fatalf("deposit must not credit the spendable balance: %d", balance)
	}
	var transactionCount int64
	if err := db.Model(&ledgermodel.Transaction{}).Where("idempotency_key = ?", "deposit:"+order.ID).Count(&transactionCount).Error; err != nil || transactionCount != 1 {
		t.Fatalf("deposit transactions=%d err=%v", transactionCount, err)
	}
	if _, applied, err := callbacks.Complete(ctx, paymentcallback.Completion{
		OrderID: order.ID, ChannelID: "channel-sandbox", ChannelTradeNo: "SANDBOX" + order.TradeNo,
		Amount: order.Amount, Digest: "deposit-digest",
	}); err != nil || applied {
		t.Fatalf("replayed complete applied=%v err=%v", applied, err)
	}
	if balance, err := service.DepositBalance(ctx, "merchant-1"); err != nil || balance != 2000 {
		t.Fatalf("deposit balance after replay=%d err=%v", balance, err)
	}
	if err := db.Model(&ledgermodel.Transaction{}).Where("idempotency_key = ?", "deposit:"+order.ID).Count(&transactionCount).Error; err != nil || transactionCount != 1 {
		t.Fatalf("deposit transactions after replay=%d err=%v", transactionCount, err)
	}
}

func TestDepositInfoReadsRequiredSetting(t *testing.T) {
	service, db, _, _ := fundingDatabase(t, "funding-deposit-info")
	ctx := context.Background()
	info, err := service.DepositInfo(ctx, "merchant-1")
	if err != nil {
		t.Fatal(err)
	}
	if info.DepositBalance != 0 || info.RequiredCents != 0 || info.Enabled {
		t.Fatalf("default info=%+v", info)
	}
	if err := db.Create(&systemSetting{Key: SettingDepositRequired, Value: "1000"}).Error; err != nil {
		t.Fatal(err)
	}
	info, err = service.DepositInfo(ctx, "merchant-1")
	if err != nil {
		t.Fatal(err)
	}
	if info.RequiredCents != 1000 || !info.Enabled {
		t.Fatalf("required info=%+v", info)
	}
}

func TestListFiltersByMerchantAndKind(t *testing.T) {
	service, _, _, _ := fundingDatabase(t, "funding-list")
	ctx := context.Background()
	if _, err := service.CreateRecharge(ctx, "merchant-1", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateDeposit(ctx, "merchant-1", 200); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRecharge(ctx, "merchant-2", 300); err != nil {
		t.Fatal(err)
	}
	items, total, err := service.List(ctx, ListFilter{MerchantID: "merchant-1", Kind: paymentmodel.KindRecharge}, Page{Number: 1, Size: 10})
	if err != nil || total != 1 || len(items) != 1 || items[0].Amount != 100 {
		t.Fatalf("recharge items=%d total=%d err=%v", len(items), total, err)
	}
	items, total, err = service.List(ctx, ListFilter{MerchantID: "merchant-1", Kind: paymentmodel.KindDeposit}, Page{Number: 1, Size: 10})
	if err != nil || total != 1 || items[0].Amount != 200 {
		t.Fatalf("deposit items=%d total=%d err=%v", len(items), total, err)
	}
	items, total, err = service.List(ctx, ListFilter{Kind: paymentmodel.KindRecharge}, Page{Number: 1, Size: 10})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("all recharge items=%d total=%d err=%v", len(items), total, err)
	}
	if _, _, err := service.List(ctx, ListFilter{Kind: paymentmodel.KindSale}, Page{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("sale kind err=%v", err)
	}
}

func TestEnsureRefundableRejectsFundingOrders(t *testing.T) {
	if err := EnsureRefundable(paymentmodel.Order{Kind: paymentmodel.KindSale}); err != nil {
		t.Fatalf("sale order rejected: %v", err)
	}
	if err := EnsureRefundable(paymentmodel.Order{}); err != nil {
		t.Fatalf("default kind should behave as sale: %v", err)
	}
	for _, kind := range []paymentmodel.Kind{paymentmodel.KindRecharge, paymentmodel.KindDeposit} {
		if err := EnsureRefundable(paymentmodel.Order{Kind: kind}); !errors.Is(err, ErrRefundNotAllowed) {
			t.Fatalf("kind %q err=%v", kind, err)
		}
	}
}

func TestFinanceRefundRejectsFundingOrder(t *testing.T) {
	service, db, callbacks, ledgerWriter := fundingDatabase(t, "funding-refund-reject")
	ctx := context.Background()
	result, err := service.CreateRecharge(ctx, "merchant-1", 3000)
	if err != nil {
		t.Fatal(err)
	}
	order := loadOrder(t, db, result.TradeNo)
	if _, _, err := callbacks.Complete(ctx, paymentcallback.Completion{
		OrderID: order.ID, ChannelID: "channel-sandbox", ChannelTradeNo: "SANDBOX" + order.TradeNo,
		Amount: order.Amount, Digest: "refund-reject-digest",
	}); err != nil {
		t.Fatal(err)
	}
	finance := financeservice.New(db, ledgerWriter, time.Now)
	_, _, err = finance.CreateRefund(ctx, financeservice.CreateRefundRequest{
		MerchantID: "merchant-1", PaymentOrderID: order.ID, OutRefundNo: "OUT-RF-1", Amount: 1000, Reason: "want money back",
	})
	if !errors.Is(err, financeservice.ErrRefundNotSale) {
		t.Fatalf("err=%v", err)
	}
}

func fundingDatabase(t *testing.T, name string) (*Service, *gorm.DB, *paymentcallback.Service, *ledgerservice.Service) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &paymentmodel.Order{}, &paymentmodel.Attempt{},
		&paymentcallback.Record{}, &outbox.Message{}, &ledgermodel.Account{}, &ledgermodel.Balance{},
		&ledgermodel.Transaction{}, &ledgermodel.Entry{}, &financemodel.Refund{}, &systemSetting{}); err != nil {
		t.Fatal(err)
	}
	merchant := identitymodel.Merchant{ID: "merchant-1", DisplayName: "merchant", Status: identitymodel.StatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now
	ledgerWriter := ledgerservice.New(db, now)
	service := New(db, ledgerWriter, testSiteURL)
	callbacks := paymentcallback.NewWithLedger(db, now, ledgerWriter).WithHooks(service)
	return service, db, callbacks, ledgerWriter
}

func loadOrder(t *testing.T, db *gorm.DB, tradeNo string) paymentmodel.Order {
	t.Helper()
	var order paymentmodel.Order
	if err := db.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		t.Fatal(err)
	}
	return order
}

func accountTotal(t *testing.T, db *gorm.DB, merchantID, purpose string) int64 {
	t.Helper()
	var account ledgermodel.Account
	if err := db.Where("owner_type = ? AND owner_id = ? AND purpose = ?", "merchant", merchantID, purpose).First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0
		}
		t.Fatal(err)
	}
	var balance ledgermodel.Balance
	if err := db.Where("account_id = ?", account.ID).First(&balance).Error; err != nil {
		t.Fatal(err)
	}
	return balance.Available + balance.Frozen
}

func TestDecodeInt64HandlesJSONAndPlainValues(t *testing.T) {
	cases := map[string]int64{
		"1000":    1000,
		`"2500"`:  2500,
		" 42 ":    42,
		"":        0,
		"abc":     0,
		"0":       0,
		`"0"`:     0,
		"0001234": 1234,
	}
	for raw, expected := range cases {
		if decoded := decodeInt64(raw); decoded != expected {
			t.Fatalf("decodeInt64(%q)=%d want %d", raw, decoded, expected)
		}
	}
}
