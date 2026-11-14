package reporting

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/complaint"
	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"github.com/gin-gonic/gin"
)

type profitReceiverRow struct {
	ID           string    `gorm:"type:varchar(36);primaryKey"`
	MerchantID   string    `gorm:"column:merchant_id;type:varchar(36);not null"`
	ReceiverType string    `gorm:"column:receiver_type;type:varchar(20);not null"`
	Account      string    `gorm:"column:account;type:varchar(191);not null"`
	Name         string    `gorm:"column:name;type:varchar(128);not null"`
	RateBP       int       `gorm:"column:rate_bp;not null"`
	Status       string    `gorm:"column:status;type:varchar(20);not null"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
}

func (profitReceiverRow) TableName() string { return "profit_sharing_receivers" }

type profitOrderRow struct {
	ID         string    `gorm:"type:varchar(36);primaryKey"`
	OrderID    string    `gorm:"column:order_id;type:varchar(36);not null"`
	MerchantID string    `gorm:"column:merchant_id;type:varchar(36);not null"`
	ReceiverID string    `gorm:"column:receiver_id;type:varchar(36);not null"`
	Amount     int64     `gorm:"column:amount;not null"`
	Status     string    `gorm:"column:status;type:varchar(20);not null"`
	CreatedAt  time.Time `gorm:"column:created_at;not null"`
	UpdatedAt  time.Time `gorm:"column:updated_at;not null"`
}

func (profitOrderRow) TableName() string { return "profit_sharing_orders" }

func TestMerchantComplaintAndProfitSharingExports(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:merchant-exports?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &identitymodel.Session{}, &complaint.Complaint{}, &profitReceiverRow{}, &profitOrderRow{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, merchant := range []identitymodel.Merchant{
		{ID: testMerchantID, DisplayName: "一号商户", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "merchant-2", DisplayName: "二号商户", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&merchant).Error; err != nil {
			t.Fatal(err)
		}
	}
	past := now.Add(-time.Hour)
	complaints := []complaint.Complaint{
		{ID: "c-1", MerchantID: testMerchantID, Channel: "alipay", ComplaintNo: "CP-1", Status: complaint.StatusPending, Reason: "未到账", AmountCents: 1234, DeadlineAt: &past, CreatedAt: now},
		{ID: "c-2", MerchantID: testMerchantID, Channel: "wxpay", ComplaintNo: "CP-2", Status: complaint.StatusReplied, Reason: "重复扣款", AmountCents: 500, CreatedAt: now.Add(-time.Minute)},
		{ID: "c-3", MerchantID: "merchant-2", Channel: "alipay", ComplaintNo: "CP-3", Status: complaint.StatusPending, Reason: "别家投诉", AmountCents: 999, CreatedAt: now},
	}
	for _, item := range complaints {
		item := item
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	receiver := profitReceiverRow{ID: "r-1", MerchantID: testMerchantID, ReceiverType: "merchant", Account: "acct-1", Name: "合作方甲", RateBP: 100, Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&receiver).Error; err != nil {
		t.Fatal(err)
	}
	orders := []profitOrderRow{
		{ID: "p-1", OrderID: "order-1", MerchantID: testMerchantID, ReceiverID: "r-1", Amount: 1234, Status: "settled", CreatedAt: now, UpdatedAt: now},
		{ID: "p-2", OrderID: "order-2", MerchantID: "merchant-2", ReceiverID: "r-1", Amount: 999, Status: "settled", CreatedAt: now, UpdatedAt: now},
	}
	for _, item := range orders {
		item := item
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}

	sessions := session.NewService(db, time.Now)
	token, _, err := sessions.Create(context.Background(), identitymodel.PrincipalMerchant, testMerchantID, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterMerchantExports(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions))

	complaintResponse := exportRequest(t, router, token, "/api/v1/merchant/export/complaints.csv")
	if complaintResponse.Code != http.StatusOK {
		t.Fatalf("complaints status=%d body=%s", complaintResponse.Code, complaintResponse.Body.String())
	}
	if !strings.HasPrefix(complaintResponse.Body.String(), "\uFEFF") {
		t.Fatal("complaints CSV is missing the UTF-8 BOM")
	}
	if complaintResponse.Header().Get("Content-Disposition") == "" || complaintResponse.Header().Get("X-Export-Rows") != "2" {
		t.Fatalf("complaints headers=%v", complaintResponse.Header())
	}
	rows := parseCSV(t, complaintResponse.Body.String())
	if len(rows) != 3 || rows[0][0] != "投诉单号" || rows[0][4] != "金额" {
		t.Fatalf("complaint rows=%v", rows)
	}
	if rows[1][0] != "CP-1" || rows[1][2] != "overdue" || rows[1][4] != "12.34" {
		t.Fatalf("complaint row=%v", rows[1])
	}
	if rows[2][0] != "CP-2" || rows[2][2] != "replied" {
		t.Fatalf("complaint row=%v", rows[2])
	}

	profitResponse := exportRequest(t, router, token, "/api/v1/merchant/export/profit-sharing.csv")
	if profitResponse.Code != http.StatusOK {
		t.Fatalf("profit status=%d body=%s", profitResponse.Code, profitResponse.Body.String())
	}
	if !strings.HasPrefix(profitResponse.Body.String(), "\uFEFF") || profitResponse.Header().Get("X-Export-Rows") != "1" {
		t.Fatalf("profit headers=%v", profitResponse.Header())
	}
	profitRows := parseCSV(t, profitResponse.Body.String())
	if len(profitRows) != 2 || profitRows[0][0] != "分账单号" || profitRows[1][0] != "p-1" {
		t.Fatalf("profit rows=%v", profitRows)
	}
	if profitRows[1][2] != "合作方甲" || profitRows[1][3] != "12.34" || profitRows[1][4] != "settled" {
		t.Fatalf("profit row=%v", profitRows[1])
	}

	if response := exportRequest(t, router, "", "/api/v1/merchant/export/complaints.csv"); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", response.Code)
	}
}

func exportRequest(t *testing.T, router http.Handler, token, path string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(response, request)
	return response
}

func parseCSV(t *testing.T, body string) [][]string {
	t.Helper()
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\uFEFF")))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	return rows
}
