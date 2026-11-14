package reporting

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBuyerStatsAggregateAndPaginate(t *testing.T) {
	fixture := newFixture(t, "reporting-buyers")
	fixture.seedFinance(t)
	response := fixture.get(t, "/api/v1/admin/stats/buyers", fixture.adminToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items    []buyerStat `json:"items"`
		Total    int64       `json:"total"`
		Page     int         `json:"page"`
		PageSize int         `json:"page_size"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 2 || len(body.Items) != 2 || body.Page != 1 || body.PageSize != defaultPageSize {
		t.Fatalf("body=%+v", body)
	}
	byBuyer := map[string]buyerStat{}
	for _, item := range body.Items {
		byBuyer[item.Buyer] = item
	}
	named, ok := byBuyer["buyer-a"]
	if !ok || named.OrderCount != 2 || named.PaidAmount != 3000 {
		t.Fatalf("buyer-a=%+v", named)
	}
	if named.FirstPaidAt == nil || named.LastPaidAt == nil || named.LastPaidAt.Before(*named.FirstPaidAt) {
		t.Fatalf("buyer-a first/last=%v/%v", named.FirstPaidAt, named.LastPaidAt)
	}
	ipBuyer, ok := byBuyer["10.0.0.1"]
	if !ok || ipBuyer.OrderCount != 1 || ipBuyer.PaidAmount != 500 {
		t.Fatalf("ip buyer=%+v", ipBuyer)
	}

	paged := fixture.get(t, "/api/v1/admin/stats/buyers?page=2&page_size=1", fixture.adminToken)
	var pagedBody struct {
		Items    []buyerStat `json:"items"`
		Total    int64       `json:"total"`
		Page     int         `json:"page"`
		PageSize int         `json:"page_size"`
	}
	if err := json.Unmarshal(paged.Body.Bytes(), &pagedBody); err != nil {
		t.Fatal(err)
	}
	if pagedBody.Total != 2 || len(pagedBody.Items) != 1 || pagedBody.Page != 2 || pagedBody.PageSize != 1 {
		t.Fatalf("paged=%+v", pagedBody)
	}
	keyword := fixture.get(t, "/api/v1/admin/stats/buyers?keyword=10.0.0.1", fixture.adminToken)
	var keywordBody struct {
		Items []buyerStat `json:"items"`
		Total int64       `json:"total"`
	}
	if err := json.Unmarshal(keyword.Body.Bytes(), &keywordBody); err != nil {
		t.Fatal(err)
	}
	if keywordBody.Total != 1 || len(keywordBody.Items) != 1 || keywordBody.Items[0].Buyer != "10.0.0.1" {
		t.Fatalf("keyword=%+v", keywordBody)
	}
	window := fixture.get(t, "/api/v1/admin/stats/buyers?created_from="+time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), fixture.adminToken)
	var windowBody struct {
		Items []buyerStat `json:"items"`
		Total int64       `json:"total"`
	}
	if err := json.Unmarshal(window.Body.Bytes(), &windowBody); err != nil {
		t.Fatal(err)
	}
	if windowBody.Total != 2 || len(windowBody.Items) != 2 {
		t.Fatalf("window=%+v", windowBody)
	}
}

func TestMerchantStatsReportContractFields(t *testing.T) {
	fixture := newFixture(t, "reporting-merchants")
	fixture.seedFinance(t)
	response := fixture.get(t, "/api/v1/admin/stats/merchants?rate_bp=100", fixture.adminToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items    []merchantStat `json:"items"`
		Total    int64          `json:"total"`
		Page     int            `json:"page"`
		PageSize int            `json:"page_size"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 2 || len(body.Items) != 2 || body.PageSize != defaultPageSize {
		t.Fatalf("body=%+v", body)
	}
	first := body.Items[0]
	if first.MerchantID != testMerchantID || first.DisplayName != "一号商户" {
		t.Fatalf("first=%+v", first)
	}
	if first.OrderCount != 3 || first.PaidAmount != 3500 || first.RefundAmount != 300 {
		t.Fatalf("first=%+v", first)
	}
	if first.FeeAmount != 35 {
		t.Fatalf("fee at 100bp=%d want 35", first.FeeAmount)
	}
	second := body.Items[1]
	if second.MerchantID != "merchant-2" || second.OrderCount != 1 || second.PaidAmount != 0 {
		t.Fatalf("second=%+v", second)
	}
	keyword := fixture.get(t, "/api/v1/admin/stats/merchants?keyword=二号", fixture.adminToken)
	var keywordBody struct {
		Items []merchantStat `json:"items"`
		Total int64          `json:"total"`
	}
	if err := json.Unmarshal(keyword.Body.Bytes(), &keywordBody); err != nil {
		t.Fatal(err)
	}
	if keywordBody.Total != 1 || keywordBody.Items[0].MerchantID != "merchant-2" {
		t.Fatalf("keyword=%+v", keywordBody)
	}
	if keywordBody.Total == body.Total {
		t.Fatalf("total must be the filtered merchant count: unfiltered=%d filtered=%d", body.Total, keywordBody.Total)
	}
}

func TestMerchantStatsFeeAmountUsesResolver(t *testing.T) {
	// 60 basis points is 0.6 percent; the resolver only knows merchant-1.
	resolver := func(_ context.Context, merchantID string) (int, bool) {
		if merchantID == testMerchantID {
			return 60, true
		}
		return 0, false
	}
	fixture := newFixtureWithFees(t, "reporting-merchants-fee", resolver)
	fixture.seedFinance(t)
	response := fixture.get(t, "/api/v1/admin/stats/merchants", fixture.adminToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items  []merchantStat `json:"items"`
		Total  int64          `json:"total"`
		RateBp *int           `json:"rate_bp"`
		Extra  map[string]any `json:"-"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.RateBp != nil {
		t.Fatalf("rate_bp must only appear for an explicit override: %v", *body.RateBp)
	}
	if len(body.Items) != 2 {
		t.Fatalf("items=%+v", body.Items)
	}
	for _, item := range body.Items {
		switch item.MerchantID {
		case testMerchantID:
			if item.PaidAmount != 3500 || item.FeeAmount != 21 {
				t.Fatalf("merchant-1 fee at 60bp=%+v want 21", item)
			}
		case "merchant-2":
			if item.FeeAmount != 0 {
				t.Fatalf("merchant-2 has no rate, fee=%+v", item)
			}
		default:
			t.Fatalf("unexpected merchant %+v", item)
		}
	}
	override := fixture.get(t, "/api/v1/admin/stats/merchants?rate_bp=100", fixture.adminToken)
	var overrideBody struct {
		Items  []merchantStat `json:"items"`
		RateBp *int           `json:"rate_bp"`
	}
	if err := json.Unmarshal(override.Body.Bytes(), &overrideBody); err != nil {
		t.Fatal(err)
	}
	if overrideBody.RateBp == nil || *overrideBody.RateBp != 100 {
		t.Fatalf("override rate_bp=%v", overrideBody.RateBp)
	}
	if overrideBody.Items[0].FeeAmount != 35 {
		t.Fatalf("override fee=%d want 35", overrideBody.Items[0].FeeAmount)
	}
}

func TestMerchantDailyStatsBuckets(t *testing.T) {
	fixture := newFixture(t, "reporting-daily")
	fixture.seedFinance(t)
	response := fixture.get(t, "/api/v1/merchant/stats/daily?days=2", fixture.merchantToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items []merchantDailyStat `json:"items"`
		Days  int                 `json:"days"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Days != 2 || len(body.Items) != 2 {
		t.Fatalf("body=%+v", body)
	}
	today := startOfDay(fixture.now).Format("2006-01-02")
	yesterday := startOfDay(fixture.now).Add(-24 * time.Hour).Format("2006-01-02")
	if body.Items[0].Date != yesterday || body.Items[1].Date != today {
		t.Fatalf("dates=%q/%q", body.Items[0].Date, body.Items[1].Date)
	}
	if body.Items[1].OrderCount != 2 || body.Items[1].PaidAmount != 1500 {
		t.Fatalf("today=%+v", body.Items[1])
	}
	if body.Items[0].OrderCount != 1 || body.Items[0].PaidAmount != 2000 {
		t.Fatalf("yesterday=%+v", body.Items[0])
	}
}

func TestMerchantRecordsScopedAndFiltered(t *testing.T) {
	fixture := newFixture(t, "reporting-records")
	fixture.seedFinance(t)
	response := fixture.get(t, "/api/v1/merchant/records?merchant_id=merchant-2", fixture.merchantToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items    []map[string]any `json:"items"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 2 || len(body.Items) != 2 || body.Page != 1 || body.PageSize != defaultPageSize {
		t.Fatalf("body=%+v", body)
	}
	first := body.Items[0]
	for _, field := range []string{"created_at", "type", "direction", "amount", "balance_after", "reference"} {
		if _, ok := first[field]; !ok {
			t.Fatalf("missing field %q in %+v", field, first)
		}
	}
	if first["amount"].(float64) != 300 || first["direction"] != "debit" {
		t.Fatalf("newest entry should be the refund debit: %+v", first)
	}

	typed := fixture.get(t, "/api/v1/merchant/records?type=payment", fixture.merchantToken)
	var typedBody struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(typed.Body.Bytes(), &typedBody); err != nil {
		t.Fatal(err)
	}
	if typedBody.Total != 1 || typedBody.Items[0]["type"] != "payment" {
		t.Fatalf("type filter=%+v", typedBody)
	}

	paged := fixture.get(t, "/api/v1/merchant/records?page=2&page_size=1", fixture.merchantToken)
	var pagedBody struct {
		Items    []map[string]any `json:"items"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := json.Unmarshal(paged.Body.Bytes(), &pagedBody); err != nil {
		t.Fatal(err)
	}
	if pagedBody.Total != 2 || len(pagedBody.Items) != 1 || pagedBody.Page != 2 || pagedBody.PageSize != 1 {
		t.Fatalf("paged=%+v", pagedBody)
	}
}

func TestStatsRejectInvalidParameters(t *testing.T) {
	fixture := newFixture(t, "reporting-validation")
	cases := []string{
		"/api/v1/admin/stats/buyers?page=0",
		"/api/v1/admin/stats/buyers?page_size=101",
		"/api/v1/admin/stats/buyers?created_from=2026-11-14",
		"/api/v1/admin/stats/merchants?rate_bp=10001",
		"/api/v1/admin/stats/merchants?created_from=2026-11-14T00:00:00Z&created_to=2026-11-13T00:00:00Z",
		"/api/v1/merchant/stats/daily?days=0",
		"/api/v1/merchant/stats/daily?days=91",
		"/api/v1/merchant/records?direction=bogus",
		"/api/v1/merchant/records?page=0",
		"/api/v1/merchant/records?page_size=101",
	}
	for _, path := range cases {
		token := fixture.adminToken
		if strings.HasPrefix(path, "/api/v1/merchant/") {
			token = fixture.merchantToken
		}
		if response := fixture.get(t, path, token); response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestStatsRequireTheRightRole(t *testing.T) {
	fixture := newFixture(t, "reporting-stats-auth")
	if response := fixture.get(t, "/api/v1/admin/stats/buyers", fixture.merchantToken); response.Code != http.StatusForbidden {
		t.Fatalf("merchant on admin stats=%d", response.Code)
	}
	if response := fixture.get(t, "/api/v1/merchant/stats/daily", fixture.adminToken); response.Code != http.StatusForbidden {
		t.Fatalf("admin on merchant stats=%d", response.Code)
	}
	if response := fixture.get(t, "/api/v1/admin/stats/buyers", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous=%d", response.Code)
	}
	if response := hitWithCookie(fixture, t, "/api/v1/admin/stats/summary", fixture.adminToken); response.Code != http.StatusNotFound {
		t.Fatalf("stats/summary is owned by another package, got %d", response.Code)
	}
}
