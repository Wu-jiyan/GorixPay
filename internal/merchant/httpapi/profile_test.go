package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestProfileReturnsIdentityFields(t *testing.T) {
	router, db, token, merchant := setupMerchant(t, "merchant-profile")
	response := performJSON(t, router, http.MethodGet, "/api/v1/merchant/profile", token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["id"] != merchant.ID || body["display_name"] != "merchant" || body["status"] != "active" {
		t.Fatalf("body=%v", body)
	}
	if body["legacy_uid"] == nil || int64(body["legacy_uid"].(float64)) != 100001 {
		t.Fatalf("legacy_uid=%v", body["legacy_uid"])
	}
	if body["email"] != "merchant@example.com" || body["phone"] != "13800000000" {
		t.Fatalf("email/phone=%v/%v", body["email"], body["phone"])
	}
	var stored identitymodel.Merchant
	if err := db.First(&stored, "id = ?", merchant.ID).Error; err != nil || stored.LegacyUID == nil || *stored.LegacyUID != 100001 {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestProfileRequiresMerchantSession(t *testing.T) {
	router, _, _, _ := setupMerchant(t, "merchant-profile-auth")
	if response := performJSON(t, router, http.MethodGet, "/api/v1/merchant/profile", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUpdateProfileTrimsAndValidatesDisplayName(t *testing.T) {
	router, db, token, _ := setupMerchant(t, "merchant-profile-update")
	response := performJSON(t, router, http.MethodPut, "/api/v1/merchant/profile", token, map[string]string{"display_name": "  新的商户名  "})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "新的商户名") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var stored identitymodel.Merchant
	if err := db.Where("id = ?", "merchant-1").First(&stored).Error; err != nil || stored.DisplayName != "新的商户名" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	for _, invalid := range []string{"", "   ", strings.Repeat("a", 129)} {
		if response := performJSON(t, router, http.MethodPut, "/api/v1/merchant/profile", token, map[string]string{"display_name": invalid}); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid %q status=%d body=%s", invalid, response.Code, response.Body.String())
		}
	}
	if response := performJSON(t, router, http.MethodPut, "/api/v1/merchant/profile", token, map[string]string{"display_name": strings.Repeat("a", 128)}); response.Code != http.StatusOK {
		t.Fatalf("128 chars status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDashboardAggregatesOrdersBalancesAndPendingReviews(t *testing.T) {
	router, db, token, _ := setupMerchant(t, "merchant-dashboard")
	now := time.Now().UTC()
	today := startOfDay(now)
	yesterday := today.Add(-24 * time.Hour)
	orders := []paymentmodel.Order{
		newOrder("order-pending-today", "T-PENDING", 1000, paymentmodel.StatusPending, now, nil),
		newOrder("order-paid-today", "T-PAID-TODAY", 2000, paymentmodel.StatusPaid, now, &now),
		newOrder("order-paid-yesterday", "T-PAID-YESTERDAY", 5000, paymentmodel.StatusPaid, yesterday.Add(time.Hour), timePointer(yesterday.Add(2*time.Hour))),
	}
	for index := range orders {
		if err := db.Create(&orders[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	account := ledgermodel.Account{ID: "account-1", OwnerType: "merchant", OwnerID: "merchant-1", Purpose: "balance", Category: ledgermodel.CategoryLiability, Currency: "CNY", Status: ledgermodel.AccountActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ledgermodel.Balance{AccountID: account.ID, Available: 500, Frozen: 200, Version: 1, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	otherAccount := ledgermodel.Account{ID: "account-platform", OwnerType: "platform", OwnerID: "platform", Purpose: "cash", Category: ledgermodel.CategoryAsset, Currency: "CNY", Status: ledgermodel.AccountActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&otherAccount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ledgermodel.Balance{AccountID: otherAccount.ID, Available: 99999, Version: 1, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	refunds := []financemodel.Refund{
		{ID: "refund-1", RefundNo: "R1", MerchantID: "merchant-1", PaymentOrderID: "order-paid-today", OutRefundNo: "OUT-R1", Amount: 100, Currency: "CNY", Reason: "r", Status: financemodel.StatusPending, CreatedAt: now, UpdatedAt: now},
		{ID: "refund-2", RefundNo: "R2", MerchantID: "merchant-1", PaymentOrderID: "order-paid-today", OutRefundNo: "OUT-R2", Amount: 100, Currency: "CNY", Reason: "r", Status: financemodel.StatusApproved, CreatedAt: now, UpdatedAt: now},
	}
	for index := range refunds {
		if err := db.Create(&refunds[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	settlement := financemodel.Settlement{ID: "settlement-1", SettlementNo: "S1", MerchantID: "merchant-1", OutSettlementNo: "OUT-S1", Amount: 100, Currency: "CNY", Destination: "bank", Status: financemodel.StatusPending, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&settlement).Error; err != nil {
		t.Fatal(err)
	}
	response := performJSON(t, router, http.MethodGet, "/api/v1/merchant/dashboard", token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body dashboardResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TodayOrderCount != 2 {
		t.Fatalf("today_order_count=%d", body.TodayOrderCount)
	}
	if body.TodayPaidAmount != 2000 || body.TotalPaidAmount != 7000 {
		t.Fatalf("paid amounts=%d/%d", body.TodayPaidAmount, body.TotalPaidAmount)
	}
	if body.AvailableBalance != 500 || body.FrozenBalance != 200 {
		t.Fatalf("balances=%d/%d", body.AvailableBalance, body.FrozenBalance)
	}
	if body.PendingRefunds != 1 || body.PendingSettlements != 1 {
		t.Fatalf("pending=%d/%d", body.PendingRefunds, body.PendingSettlements)
	}
}

func TestDashboardWithoutLedgerAccountReportsZeroBalance(t *testing.T) {
	router, _, token, _ := setupMerchant(t, "merchant-dashboard-empty")
	response := performJSON(t, router, http.MethodGet, "/api/v1/merchant/dashboard", token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body dashboardResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.AvailableBalance != 0 || body.FrozenBalance != 0 || body.TodayOrderCount != 0 || body.TotalPaidAmount != 0 || body.PendingRefunds != 0 || body.PendingSettlements != 0 {
		t.Fatalf("body=%+v", body)
	}
}

func setupMerchant(t *testing.T, name string) (http.Handler, *gorm.DB, string, identitymodel.Merchant) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &identitymodel.Session{}, &paymentmodel.Order{},
		&financemodel.Refund{}, &financemodel.Settlement{}, &ledgermodel.Account{}, &ledgermodel.Balance{}); err != nil {
		t.Fatal(err)
	}
	legacyUID := int64(100001)
	email := "merchant@example.com"
	phone := "13800000000"
	now := time.Now().UTC()
	merchant := identitymodel.Merchant{ID: "merchant-1", LegacyUID: &legacyUID, Email: &email, Phone: &phone, DisplayName: "merchant", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	sessions := session.NewService(db, time.Now)
	token, _, err := sessions.Create(context.Background(), identitymodel.PrincipalMerchant, merchant.ID, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	group := router.Group("/api/v1/merchant", identityhttp.Authenticate(sessions))
	RegisterProfile(group, db)
	return router, db, token, merchant
}

func newOrder(id, tradeNo string, amount int64, status paymentmodel.Status, createdAt time.Time, paidAt *time.Time) paymentmodel.Order {
	return paymentmodel.Order{
		ID: id, TradeNo: tradeNo, MerchantID: "merchant-1", OutTradeNo: "OUT-" + id, PaymentType: "alipay",
		Name: "order", Amount: amount, Currency: "CNY", Status: status, NotifyURL: "https://merchant.example/notify",
		PaidAt: paidAt, ExpiresAt: createdAt.Add(5 * time.Minute), CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func performJSON(t *testing.T, router http.Handler, method, path, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(response, request)
	return response
}
