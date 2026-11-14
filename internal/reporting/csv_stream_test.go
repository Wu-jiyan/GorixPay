package reporting

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/transfer"

	"github.com/gin-gonic/gin"
)

// TestStreamExportWritesLargeDataset checks that a 3000 row export streams out
// with the right width, the BOM and the header before any data row.
func TestStreamExportWritesLargeDataset(t *testing.T) {
	fixture := newFixture(t, "reporting-stream-large")
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	orders := make([]paymentmodel.Order, 0, 3000)
	for index := 0; index < 3000; index++ {
		orders = append(orders, newOrder(
			fmt.Sprintf("order-%04d", index), fmt.Sprintf("T-%04d", index), testMerchantID,
			int64(100+index), paymentmodel.StatusPaid, now.Add(time.Duration(index)*time.Second), nil, nil, nil,
		))
	}
	if err := fixture.db.CreateInBatches(orders, 500).Error; err != nil {
		t.Fatal(err)
	}
	response := fixture.get(t, "/api/v1/admin/export/orders.csv", fixture.adminToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-Export-Rows"); got != "3000" {
		t.Fatalf("X-Export-Rows=%q want 3000", got)
	}
	if got := response.Header().Get("X-Export-Truncated"); got != "false" {
		t.Fatalf("X-Export-Truncated=%q want false", got)
	}
	body := response.Body.Bytes()
	if len(body) < 3 || body[0] != 0xEF || body[1] != 0xBB || body[2] != 0xBF {
		t.Fatal("response does not start with a UTF-8 BOM")
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(body), "\uFEFF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3001 {
		t.Fatalf("records=%d want 3001 (header + 3000 rows)", len(records))
	}
	if strings.Join(records[0], ",") != strings.Join(orderHeader, ",") {
		t.Fatalf("header=%v", records[0])
	}
	for index, record := range records[1:] {
		if len(record) != len(orderHeader) {
			t.Fatalf("row %d has %d columns, want %d", index, len(record), len(orderHeader))
		}
		if record[0] == "" {
			t.Fatalf("row %d lost its trade_no", index)
		}
	}
}

// TestStreamExportMidwayFailureKeepsRowsComplete simulates a query failing after
// the body already started: the status stays 200, every written record is a
// complete CSV record, and one clear notice row explains the interruption.
func TestStreamExportMidwayFailureKeepsRowsComplete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	header := []string{"a", "b", "c"}
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/export.csv", nil)

	writeStreamedExport(ctx, "orders", header, 10, func(yield func([]string) bool) error {
		for index := 0; index < 3; index++ {
			if !yield([]string{fmt.Sprintf("row-%d", index), "x", "y"}) {
				return nil
			}
		}
		return errors.New("数据库连接中断")
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d, headers are already sent so it must stay 200", response.Code)
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(response.Body.String(), "\uFEFF"))).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid csv: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("records=%d want 5 (header + 3 rows + notice)", len(records))
	}
	for index, record := range records {
		if len(record) != len(header) {
			t.Fatalf("record %d has %d columns, want %d: %v", index, len(record), len(header), record)
		}
	}
	if !strings.Contains(records[len(records)-1][0], "导出中断") {
		t.Fatalf("missing interruption notice: %v", records[len(records)-1])
	}
	if len(ctx.Errors) == 0 {
		t.Fatal("the query error should be recorded for the access log")
	}
}

// TestStreamExportKeepsFormulaNeutralization guards the previous fix on the new
// streaming path.
func TestStreamExportKeepsFormulaNeutralization(t *testing.T) {
	fixture := newFixture(t, "reporting-stream-formula")
	malicious := transferOrderForExport()
	if err := fixture.db.Create(&malicious).Error; err != nil {
		t.Fatal(err)
	}
	response := fixture.get(t, "/api/v1/admin/export/transfers.csv", fixture.adminToken)
	records := readCSV(t, response.Body.Bytes())
	if len(records) < 2 {
		t.Fatalf("records=%v", records)
	}
	if records[1][5] != "'=cmd|' /C calc'!A0" {
		t.Fatalf("formula prefix survived streaming: %q", records[1][5])
	}
}

func transferOrderForExport() transfer.Order {
	return transfer.Order{
		ID: "transfer-stream", TransferNo: "P-STREAM", MerchantID: testMerchantID,
		OutTransferNo: "OUT-STREAM", Amount: 100, Currency: "CNY",
		AccountName: "=cmd|' /C calc'!A0", AccountNo: "6222", BankName: "招商银行",
		Status: transfer.StatusPending, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}
