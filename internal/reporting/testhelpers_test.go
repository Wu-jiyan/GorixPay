package reporting

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	financemodel "gorixpay/internal/finance/model"
	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	ledgermodel "gorixpay/internal/ledger/model"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/transfer"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type fixture struct {
	router        http.Handler
	db            *gorm.DB
	adminToken    string
	merchantToken string
	now           time.Time
}

const testMerchantID = "merchant-1"

func newFixture(t *testing.T, name string) *fixture {
	t.Helper()
	return newFixtureWithFees(t, name, nil)
}

func newFixtureWithFees(t *testing.T, name string, fees FeeResolver) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&identitymodel.Merchant{}, &identitymodel.Session{},
		&paymentmodel.Order{}, &paymentmodel.Attempt{},
		&financemodel.Refund{}, &financemodel.Settlement{},
		&transfer.Order{},
		&ledgermodel.Account{}, &ledgermodel.Balance{}, &ledgermodel.Transaction{}, &ledgermodel.Entry{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	merchant := identitymodel.Merchant{ID: testMerchantID, DisplayName: "一号商户", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now}
	other := identitymodel.Merchant{ID: "merchant-2", DisplayName: "二号商户", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	sessions := session.NewService(db, time.Now)
	merchantToken, _, err := sessions.Create(context.Background(), identitymodel.PrincipalMerchant, merchant.ID, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := sessions.Create(context.Background(), identitymodel.PrincipalAdministrator, "admin-1", time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	Register(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions), fees)
	return &fixture{router: router, db: db, adminToken: adminToken, merchantToken: merchantToken, now: now}
}

// seedFinance creates two paid orders for the primary merchant, one paid order
// with only a client IP, one pending order for the other merchant, an approved
// refund, a pending refund, and a matching ledger trail.
func (fixture *fixture) seedFinance(t *testing.T) {
	t.Helper()
	buyer := "buyer-a"
	clientIP := "10.0.0.1"
	dayBefore := fixture.now.Add(-24 * time.Hour)
	orders := []paymentmodel.Order{
		newOrder("order-1", "T1", testMerchantID, 1000, paymentmodel.StatusPaid, fixture.now, &buyer, nil, &fixture.now),
		newOrder("order-2", "T2", testMerchantID, 2000, paymentmodel.StatusPaid, dayBefore, &buyer, nil, &dayBefore),
		newOrder("order-3", "T3", testMerchantID, 500, paymentmodel.StatusPaid, fixture.now, nil, &clientIP, &fixture.now),
		newOrder("order-4", "T4", "merchant-2", 700, paymentmodel.StatusPending, fixture.now, nil, nil, nil),
	}
	for index := range orders {
		if err := fixture.db.Create(&orders[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	refunds := []financemodel.Refund{
		{ID: "refund-1", RefundNo: "R1", MerchantID: testMerchantID, PaymentOrderID: "order-1", OutRefundNo: "OUT-R1", Amount: 300, Currency: "CNY", Reason: "退款", Status: financemodel.StatusApproved, CreatedAt: fixture.now, UpdatedAt: fixture.now},
		{ID: "refund-2", RefundNo: "R2", MerchantID: testMerchantID, PaymentOrderID: "order-1", OutRefundNo: "OUT-R2", Amount: 100, Currency: "CNY", Reason: "处理中", Status: financemodel.StatusPending, CreatedAt: fixture.now, UpdatedAt: fixture.now},
	}
	for index := range refunds {
		if err := fixture.db.Create(&refunds[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	settlement := financemodel.Settlement{ID: "settlement-1", SettlementNo: "S1", MerchantID: testMerchantID, OutSettlementNo: "OUT-S1", Amount: 200, Currency: "CNY", Destination: "招商银行 6222", Status: financemodel.StatusApproved, CreatedAt: fixture.now, UpdatedAt: fixture.now}
	if err := fixture.db.Create(&settlement).Error; err != nil {
		t.Fatal(err)
	}
	payout := transfer.Order{ID: "transfer-1", TransferNo: "P1", MerchantID: testMerchantID, OutTransferNo: "OUT-P1", Amount: 400, Currency: "CNY", AccountName: "张三", AccountNo: "6222", BankName: "招商银行", Status: transfer.StatusApproved, CreatedAt: fixture.now, UpdatedAt: fixture.now}
	if err := fixture.db.Create(&payout).Error; err != nil {
		t.Fatal(err)
	}
	balanceAccount := ledgermodel.Account{ID: "account-balance", OwnerType: "merchant", OwnerID: testMerchantID, Purpose: "balance", Category: ledgermodel.CategoryLiability, Currency: "CNY", Status: ledgermodel.AccountActive, CreatedAt: fixture.now, UpdatedAt: fixture.now}
	platformAccount := ledgermodel.Account{ID: "account-platform", OwnerType: "platform", OwnerID: "platform", Purpose: "cash", Category: ledgermodel.CategoryAsset, Currency: "CNY", Status: ledgermodel.AccountActive, CreatedAt: fixture.now, UpdatedAt: fixture.now}
	depositAccount := ledgermodel.Account{ID: "account-deposit", OwnerType: "merchant", OwnerID: testMerchantID, Purpose: "deposit", Category: ledgermodel.CategoryLiability, Currency: "CNY", Status: ledgermodel.AccountActive, CreatedAt: fixture.now, UpdatedAt: fixture.now}
	for _, account := range []ledgermodel.Account{balanceAccount, platformAccount, depositAccount} {
		if err := fixture.db.Create(&account).Error; err != nil {
			t.Fatal(err)
		}
		if err := fixture.db.Create(&ledgermodel.Balance{AccountID: account.ID, Version: 1, UpdatedAt: fixture.now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	paymentTransaction := ledgermodel.Transaction{ID: "tx-1", IdempotencyKey: "payment:order-1", RequestHash: "hash-1", ReferenceType: "payment", ReferenceID: "order-1", Description: "商户入账", Metadata: "{}", CreatedAt: fixture.now.Add(-2 * time.Hour)}
	refundTransaction := ledgermodel.Transaction{ID: "tx-2", IdempotencyKey: "refund:refund-1", RequestHash: "hash-2", ReferenceType: "refund", ReferenceID: "refund-1", Description: "退款出账", Metadata: "{}", CreatedAt: fixture.now.Add(-time.Hour)}
	depositTransaction := ledgermodel.Transaction{ID: "tx-3", IdempotencyKey: "deposit:order-9", RequestHash: "hash-3", ReferenceType: "deposit", ReferenceID: "order-9", Description: "保证金", Metadata: "{}", CreatedAt: fixture.now}
	for _, transaction := range []ledgermodel.Transaction{paymentTransaction, refundTransaction, depositTransaction} {
		if err := fixture.db.Create(&transaction).Error; err != nil {
			t.Fatal(err)
		}
	}
	entries := []ledgermodel.Entry{
		{ID: "entry-1", TransactionID: "tx-1", AccountID: "account-balance", Sequence: 1, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: 1000, BalanceAfter: 1000, CreatedAt: fixture.now.Add(-2 * time.Hour)},
		{ID: "entry-2", TransactionID: "tx-1", AccountID: "account-platform", Sequence: 2, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: 1000, BalanceAfter: 1000, CreatedAt: fixture.now.Add(-2 * time.Hour)},
		{ID: "entry-3", TransactionID: "tx-2", AccountID: "account-balance", Sequence: 1, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: 300, BalanceAfter: 700, CreatedAt: fixture.now.Add(-time.Hour)},
		{ID: "entry-4", TransactionID: "tx-3", AccountID: "account-deposit", Sequence: 1, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: 500, BalanceAfter: 500, CreatedAt: fixture.now},
	}
	for index := range entries {
		if err := fixture.db.Create(&entries[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func newOrder(id, tradeNo, merchantID string, amount int64, status paymentmodel.Status, createdAt time.Time, buyer, clientIP *string, paidAt *time.Time) paymentmodel.Order {
	return paymentmodel.Order{
		ID: id, TradeNo: tradeNo, MerchantID: merchantID, OutTradeNo: "OUT-" + id,
		PaymentType: "alipay", Name: "测试商品", Amount: amount, Currency: "CNY",
		Status: status, Kind: paymentmodel.KindSale, NotifyURL: "https://merchant.example/notify",
		Buyer: buyer, ClientIP: clientIP, PaidAt: paidAt,
		ExpiresAt: createdAt.Add(5 * time.Minute), CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}

func (fixture *fixture) get(t *testing.T, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	fixture.router.ServeHTTP(response, request)
	return response
}

// hitWithCookie authenticates with the session cookie alone, which is how a
// browser opens an export in a new window.
func hitWithCookie(fixture *fixture, t *testing.T, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(&http.Cookie{Name: "gorixpay_session", Value: token})
	fixture.router.ServeHTTP(response, request)
	return response
}

// readCSV strips the UTF-8 BOM and parses the body into records.
func readCSV(t *testing.T, body []byte) [][]string {
	t.Helper()
	if !bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("export is missing the UTF-8 BOM: % x", body[:min(len(body), 6)])
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(body), "\uFEFF")))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	return records
}
