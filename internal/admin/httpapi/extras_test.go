package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	financemodel "gorixpay/internal/finance/model"
	identitymodel "gorixpay/internal/identity/model"
	ledgermodel "gorixpay/internal/ledger/model"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestMerchantDetailReportsCredentialsAndBalances(t *testing.T) {
	router, db := extrasRouter(t, "admin-merchant-detail")
	now := time.Now().UTC()
	legacyUID := int64(200002)
	email := "m@example.com"
	merchant := identitymodel.Merchant{ID: "merchant-1", LegacyUID: &legacyUID, Email: &email, DisplayName: "商户", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	keyID := "MD5-1"
	rsaKeyID := "RSA-1"
	credentials := []identitymodel.MerchantCredential{
		{ID: "credential-md5", MerchantID: merchant.ID, CredentialType: identitymodel.CredentialMD5, KeyID: &keyID, SecretEncrypted: "secret-md5", Status: identitymodel.CredentialActive, CreatedAt: now},
		{ID: "credential-rsa", MerchantID: merchant.ID, CredentialType: identitymodel.CredentialRSA, KeyID: &rsaKeyID, SecretEncrypted: "secret-rsa", PublicKey: stringPointer("PUBLIC"), Status: identitymodel.CredentialActive, CreatedAt: now},
	}
	for index := range credentials {
		if err := db.Create(&credentials[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	account := ledgermodel.Account{ID: "account-1", OwnerType: "merchant", OwnerID: merchant.ID, Purpose: "balance", Category: ledgermodel.CategoryLiability, Currency: "CNY", Status: ledgermodel.AccountActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ledgermodel.Balance{AccountID: account.ID, Available: 1234, Frozen: 56, Version: 1, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	response := performExtras(t, router, "/api/v1/admin/merchants/"+merchant.ID)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret-md5") || strings.Contains(response.Body.String(), "secret-rsa") {
		t.Fatalf("encrypted secret leaked: %s", response.Body.String())
	}
	var body struct {
		ID          string `json:"id"`
		LegacyUID   *int64 `json:"legacy_uid"`
		DisplayName string `json:"display_name"`
		Credentials []struct {
			ID             string `json:"id"`
			CredentialType string `json:"credential_type"`
			KeyID          string `json:"key_id"`
			Status         string `json:"status"`
		} `json:"credentials"`
		Accounts []struct {
			ID        string `json:"id"`
			Purpose   string `json:"purpose"`
			Currency  string `json:"currency"`
			Available int64  `json:"available"`
			Frozen    int64  `json:"frozen"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ID != merchant.ID || body.LegacyUID == nil || *body.LegacyUID != legacyUID || body.DisplayName != "商户" {
		t.Fatalf("body=%+v", body)
	}
	if len(body.Credentials) != 2 {
		t.Fatalf("credentials=%+v", body.Credentials)
	}
	if len(body.Accounts) != 1 || body.Accounts[0].Available != 1234 || body.Accounts[0].Frozen != 56 || body.Accounts[0].Purpose != "balance" {
		t.Fatalf("accounts=%+v", body.Accounts)
	}
	if response := performExtras(t, router, "/api/v1/admin/merchants/missing"); response.Code != http.StatusNotFound {
		t.Fatalf("missing merchant status=%d", response.Code)
	}
}

func TestStatsDailyCoversRequestedWindow(t *testing.T) {
	router, db := extrasRouter(t, "admin-stats-daily")
	now := time.Now().UTC()
	orders := []paymentmodel.Order{
		extrasOrder("order-today", "T1", 1000, paymentmodel.StatusPaid, now, &now),
		extrasOrder("order-today-pending", "T2", 500, paymentmodel.StatusPending, now, nil),
		extrasOrder("order-yesterday", "T3", 2000, paymentmodel.StatusPaid, now.AddDate(0, 0, -1), timePointerValue(now.AddDate(0, 0, -1))),
		extrasOrder("order-old", "T4", 4000, paymentmodel.StatusPaid, now.AddDate(0, 0, -30), timePointerValue(now.AddDate(0, 0, -30))),
	}
	for index := range orders {
		if err := db.Create(&orders[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	response := performExtras(t, router, "/api/v1/admin/stats/daily?days=3")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items []dailyStat `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 3 {
		t.Fatalf("items=%+v", body.Items)
	}
	today := startOfDay(now)
	if body.Items[2].Date != today.Format("2006-01-02") {
		t.Fatalf("last item=%+v", body.Items[2])
	}
	if body.Items[2].OrderCount != 2 || body.Items[2].PaidCount != 1 || body.Items[2].PaidAmount != 1000 {
		t.Fatalf("today item=%+v", body.Items[2])
	}
	if body.Items[1].OrderCount != 1 || body.Items[1].PaidAmount != 2000 {
		t.Fatalf("yesterday item=%+v", body.Items[1])
	}
	defaulted := performExtras(t, router, "/api/v1/admin/stats/daily")
	if defaulted.Code != http.StatusOK {
		t.Fatalf("default days status=%d", defaulted.Code)
	}
	var defaultBody struct {
		Items []dailyStat `json:"items"`
	}
	if err := json.Unmarshal(defaulted.Body.Bytes(), &defaultBody); err != nil {
		t.Fatal(err)
	}
	if len(defaultBody.Items) != defaultStatsDays {
		t.Fatalf("default items=%d", len(defaultBody.Items))
	}
	for _, invalid := range []string{"0", "-1", "91", "abc"} {
		if response := performExtras(t, router, "/api/v1/admin/stats/daily?days="+invalid); response.Code != http.StatusBadRequest {
			t.Fatalf("days=%s status=%d", invalid, response.Code)
		}
	}
}

func TestStatsSummaryCountsPlatformWide(t *testing.T) {
	router, db := extrasRouter(t, "admin-stats-summary")
	now := time.Now().UTC()
	merchants := []identitymodel.Merchant{
		{ID: "merchant-1", DisplayName: "a", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "merchant-2", DisplayName: "b", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now},
	}
	for index := range merchants {
		if err := db.Create(&merchants[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	orders := []paymentmodel.Order{
		extrasOrderWithMerchant("order-1", "T1", "merchant-1", 1000, paymentmodel.StatusPaid, now, &now),
		extrasOrderWithMerchant("order-2", "T2", "merchant-2", 2500, paymentmodel.StatusPaid, now, &now),
		extrasOrderWithMerchant("order-3", "T3", "merchant-1", 700, paymentmodel.StatusPending, now, nil),
		extrasOrderWithMerchant("order-4", "T4", "merchant-1", 900, paymentmodel.StatusPaid, now.AddDate(0, 0, -2), timePointerValue(now.AddDate(0, 0, -2))),
	}
	for index := range orders {
		if err := db.Create(&orders[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&financemodel.Refund{ID: "refund-1", RefundNo: "R1", MerchantID: "merchant-1", PaymentOrderID: "order-1", OutRefundNo: "OUT-R1", Amount: 100, Currency: "CNY", Reason: "r", Status: financemodel.StatusPending, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&financemodel.Settlement{ID: "settlement-1", SettlementNo: "S1", MerchantID: "merchant-1", OutSettlementNo: "OUT-S1", Amount: 100, Currency: "CNY", Destination: "bank", Status: financemodel.StatusPending, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	response := performExtras(t, router, "/api/v1/admin/stats/summary")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var summary summaryStat
	if err := json.Unmarshal(response.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.MerchantCount != 2 || summary.OrderCount != 4 || summary.PaidOrderCount != 3 {
		t.Fatalf("summary=%+v", summary)
	}
	if summary.PaidAmount != 4400 || summary.TodayPaidAmount != 3500 {
		t.Fatalf("summary=%+v", summary)
	}
	if summary.PendingRefunds != 1 || summary.PendingSettlements != 1 {
		t.Fatalf("summary=%+v", summary)
	}
}

func extrasRouter(t *testing.T, name string) (http.Handler, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &identitymodel.MerchantCredential{}, &paymentmodel.Order{},
		&financemodel.Refund{}, &financemodel.Settlement{}, &ledgermodel.Account{}, &ledgermodel.Balance{}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterExtras(router.Group("/api/v1/admin"), db)
	return router, db
}

func extrasOrder(id, tradeNo string, amount int64, status paymentmodel.Status, createdAt time.Time, paidAt *time.Time) paymentmodel.Order {
	return extrasOrderWithMerchant(id, tradeNo, "merchant-1", amount, status, createdAt, paidAt)
}

func extrasOrderWithMerchant(id, tradeNo, merchantID string, amount int64, status paymentmodel.Status, createdAt time.Time, paidAt *time.Time) paymentmodel.Order {
	return paymentmodel.Order{
		ID: id, TradeNo: tradeNo, MerchantID: merchantID, OutTradeNo: "OUT-" + id, PaymentType: "alipay",
		Name: "order", Amount: amount, Currency: "CNY", Status: status, NotifyURL: "https://merchant.example/notify",
		PaidAt: paidAt, ExpiresAt: createdAt.Add(5 * time.Minute), CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}

func timePointerValue(value time.Time) *time.Time { return &value }

func stringPointer(value string) *string { return &value }

func performExtras(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}
