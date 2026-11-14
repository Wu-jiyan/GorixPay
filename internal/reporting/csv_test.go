package reporting

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAdminOrderExportHasBOMYuanAmountsAndFilters(t *testing.T) {
	fixture := newFixture(t, "reporting-orders")
	fixture.seedFinance(t)
	response := fixture.get(t, "/api/v1/admin/export/orders.csv", fixture.adminToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "text/csv") {
		t.Fatalf("content-type=%q", contentType)
	}
	disposition := response.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disposition, "attachment;") || !strings.Contains(disposition, `filename="orders-`) {
		t.Fatalf("content-disposition=%q", disposition)
	}
	if truncated := response.Header().Get("X-Export-Truncated"); truncated != "false" {
		t.Fatalf("x-export-truncated=%q", truncated)
	}
	records := readCSV(t, response.Body.Bytes())
	if len(records) != 5 {
		t.Fatalf("rows=%d want header + 4", len(records))
	}
	if strings.Join(records[0], ",") != strings.Join(orderHeader, ",") {
		t.Fatalf("header=%v", records[0])
	}
	paidAt := ""
	amount := ""
	for _, record := range records[1:] {
		if record[0] == "T1" {
			amount = record[6]
			paidAt = record[12]
		}
	}
	if amount != "10.00" {
		t.Fatalf("amount column=%q", amount)
	}
	if _, err := time.Parse(time.RFC3339, paidAt); err != nil {
		t.Fatalf("paid_at=%q is not RFC3339: %v", paidAt, err)
	}

	filtered := fixture.get(t, "/api/v1/admin/export/orders.csv?status=paid&merchant_id="+testMerchantID, fixture.adminToken)
	if rows := readCSV(t, filtered.Body.Bytes()); len(rows) != 4 {
		t.Fatalf("filtered rows=%d want header + 3", len(rows))
	}
	keyword := fixture.get(t, "/api/v1/admin/export/orders.csv?keyword=OUT-order-4", fixture.adminToken)
	if rows := readCSV(t, keyword.Body.Bytes()); len(rows) != 2 || rows[1][0] != "T4" {
		t.Fatalf("keyword rows=%v", rows)
	}
	if response := fixture.get(t, "/api/v1/admin/export/orders.csv?status=bogus", fixture.adminToken); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid status code=%d", response.Code)
	}
	if response := fixture.get(t, "/api/v1/admin/export/orders.csv?kind=bogus", fixture.adminToken); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid kind code=%d", response.Code)
	}
}

func TestMerchantOrderExportIgnoresMerchantQuery(t *testing.T) {
	fixture := newFixture(t, "reporting-merchant-orders")
	fixture.seedFinance(t)
	response := fixture.get(t, "/api/v1/merchant/export/orders.csv?merchant_id=merchant-2", fixture.merchantToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	records := readCSV(t, response.Body.Bytes())
	if strings.Join(records[0], ",") != strings.Join(merchantOrderHeader, ",") {
		t.Fatalf("header=%v", records[0])
	}
	if len(records) != 4 {
		t.Fatalf("rows=%d want header + 3 merchant-1 orders", len(records))
	}
	for _, record := range records[1:] {
		if record[0] == "T4" {
			t.Fatal("merchant export leaked another merchant's order")
		}
	}
}

func TestExportAcceptsSessionCookie(t *testing.T) {
	fixture := newFixture(t, "reporting-cookie")
	fixture.seedFinance(t)
	response := hitWithCookie(fixture, t, "/api/v1/admin/export/orders.csv", fixture.adminToken)
	if response.Code != http.StatusOK {
		t.Fatalf("cookie auth status=%d body=%s", response.Code, response.Body.String())
	}
	if rows := readCSV(t, response.Body.Bytes()); len(rows) != 5 {
		t.Fatalf("cookie export rows=%d", len(rows))
	}
	merchantExport := hitWithCookie(fixture, t, "/api/v1/merchant/export/records.csv", fixture.merchantToken)
	if merchantExport.Code != http.StatusOK {
		t.Fatalf("merchant cookie status=%d body=%s", merchantExport.Code, merchantExport.Body.String())
	}
}

func TestExportTruncationAddsNoticeAndHeaders(t *testing.T) {
	fixture := newFixture(t, "reporting-truncation")
	fixture.seedFinance(t)
	original := maxExportRows
	maxExportRows = 2
	defer func() { maxExportRows = original }()

	response := fixture.get(t, "/api/v1/admin/export/orders.csv", fixture.adminToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Export-Truncated") != "true" {
		t.Fatalf("x-export-truncated=%q", response.Header().Get("X-Export-Truncated"))
	}
	if !strings.Contains(response.Header().Get("Content-Disposition"), "orders-truncated-") {
		t.Fatalf("content-disposition=%q", response.Header().Get("Content-Disposition"))
	}
	records := readCSV(t, response.Body.Bytes())
	if len(records) != 4 {
		t.Fatalf("rows=%d want notice + header + 2", len(records))
	}
	if !strings.Contains(records[0][0], "已截断") {
		t.Fatalf("notice=%q", records[0][0])
	}
	if len(records[0]) != len(orderHeader) {
		t.Fatalf("notice row width=%d want %d", len(records[0]), len(orderHeader))
	}
	if strings.Join(records[1], ",") != strings.Join(orderHeader, ",") {
		t.Fatalf("header=%v", records[1])
	}
}

func TestAdminFinanceAndMerchantExports(t *testing.T) {
	fixture := newFixture(t, "reporting-finance")
	fixture.seedFinance(t)
	cases := []struct {
		path     string
		header   []string
		dataRows int
		expected []string
	}{
		{"/api/v1/admin/export/refunds.csv", refundHeader, 2, []string{"R1", "R2"}},
		{"/api/v1/admin/export/settlements.csv", settlementHeader, 1, []string{"S1"}},
		{"/api/v1/admin/export/transfers.csv", transferHeader, 1, []string{"P1"}},
	}
	for _, item := range cases {
		response := fixture.get(t, item.path, fixture.adminToken)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", item.path, response.Code, response.Body.String())
		}
		records := readCSV(t, response.Body.Bytes())
		if len(records) != item.dataRows+1 {
			t.Fatalf("%s rows=%d want %d", item.path, len(records), item.dataRows+1)
		}
		if strings.Join(records[0], ",") != strings.Join(item.header, ",") {
			t.Fatalf("%s header=%v", item.path, records[0])
		}
		seen := map[string]bool{}
		for _, record := range records[1:] {
			seen[record[0]] = true
		}
		for _, value := range item.expected {
			if !seen[value] {
				t.Fatalf("%s missing %q in %v", item.path, value, seen)
			}
		}
	}
	merchants := fixture.get(t, "/api/v1/admin/export/merchants.csv?status=active", fixture.adminToken)
	merchantRecords := readCSV(t, merchants.Body.Bytes())
	if len(merchantRecords) != 3 {
		t.Fatalf("merchant rows=%d", len(merchantRecords))
	}
	if strings.Join(merchantRecords[0], ",") != strings.Join(merchantHeader, ",") {
		t.Fatalf("merchant header=%v", merchantRecords[0])
	}
}

func TestBuyersExportAggregatesPayers(t *testing.T) {
	fixture := newFixture(t, "reporting-buyers-export")
	fixture.seedFinance(t)
	response := fixture.get(t, "/api/v1/admin/export/buyers.csv", fixture.adminToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	records := readCSV(t, response.Body.Bytes())
	if strings.Join(records[0], ",") != strings.Join(buyerHeader, ",") {
		t.Fatalf("header=%v", records[0])
	}
	if len(records) != 3 {
		t.Fatalf("rows=%d want header + 2 payers", len(records))
	}
	found := false
	for _, record := range records[1:] {
		if record[0] != "buyer-a" {
			continue
		}
		found = true
		if record[1] != "2" || record[2] != "30.00" {
			t.Fatalf("buyer-a row=%v", record)
		}
		if _, err := time.Parse(time.RFC3339, record[3]); err != nil {
			t.Fatalf("first_paid_at=%q: %v", record[3], err)
		}
	}
	if !found {
		t.Fatalf("buyer-a missing: %v", records)
	}
	keyword := fixture.get(t, "/api/v1/admin/export/buyers.csv?keyword=10.0.0.1", fixture.adminToken)
	if rows := readCSV(t, keyword.Body.Bytes()); len(rows) != 2 || rows[1][0] != "10.0.0.1" {
		t.Fatalf("keyword rows=%v", rows)
	}
}

func TestLedgerExportFiltersOnAccountAndDirection(t *testing.T) {
	fixture := newFixture(t, "reporting-ledger")
	fixture.seedFinance(t)
	all := readCSV(t, fixture.get(t, "/api/v1/admin/export/ledger.csv", fixture.adminToken).Body.Bytes())
	if len(all) != 5 {
		t.Fatalf("ledger rows=%d want header + 4", len(all))
	}
	balances := readCSV(t, fixture.get(t, "/api/v1/admin/export/ledger.csv?purpose=balance", fixture.adminToken).Body.Bytes())
	if len(balances) != 3 {
		t.Fatalf("purpose filter rows=%d", len(balances))
	}
	credits := readCSV(t, fixture.get(t, "/api/v1/admin/export/ledger.csv?direction=credit", fixture.adminToken).Body.Bytes())
	if len(credits) != 3 {
		t.Fatalf("direction filter rows=%d", len(credits))
	}
	if response := fixture.get(t, "/api/v1/admin/export/ledger.csv?direction=bogus", fixture.adminToken); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid direction code=%d", response.Code)
	}
}

func TestMerchantRecordsExportIsScopedToSession(t *testing.T) {
	fixture := newFixture(t, "reporting-records-export")
	fixture.seedFinance(t)
	response := fixture.get(t, "/api/v1/merchant/export/records.csv?merchant_id=merchant-2", fixture.merchantToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	records := readCSV(t, response.Body.Bytes())
	if strings.Join(records[0], ",") != strings.Join(recordHeader, ",") {
		t.Fatalf("header=%v", records[0])
	}
	if len(records) != 3 {
		t.Fatalf("rows=%d want header + 2 balance entries", len(records))
	}
	for _, record := range records[1:] {
		if record[1] == "deposit" {
			t.Fatal("records export leaked the deposit account")
		}
		if !strings.HasSuffix(record[3], ".00") || !strings.HasSuffix(record[4], ".00") {
			t.Fatalf("amount columns are not two decimals: %v", record)
		}
	}
}

func TestExportRequiresTheRightRole(t *testing.T) {
	fixture := newFixture(t, "reporting-auth")
	if response := fixture.get(t, "/api/v1/admin/export/orders.csv", fixture.merchantToken); response.Code != http.StatusForbidden {
		t.Fatalf("merchant on admin export status=%d", response.Code)
	}
	if response := fixture.get(t, "/api/v1/merchant/export/orders.csv", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", response.Code)
	}
	if response := fixture.get(t, "/api/v1/merchant/export/orders.csv", fixture.adminToken); response.Code != http.StatusForbidden {
		t.Fatalf("admin on merchant export status=%d", response.Code)
	}
}
