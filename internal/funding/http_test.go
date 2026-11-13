package funding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestMerchantFundingRoutes(t *testing.T) {
	router, _, token := fundingRouter(t, "funding-http-merchant")
	created := performFunding(t, router, http.MethodPost, "/api/v1/merchant/recharge", token, `{"amount":1000}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var result OrderResult
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.TradeNo == "" || result.Amount != 1000 || result.CashierURL != testSiteURL+"/cashier.php?trade_no="+result.TradeNo {
		t.Fatalf("result=%+v", result)
	}
	rechargeList := performFunding(t, router, http.MethodGet, "/api/v1/merchant/recharge", token, "")
	if rechargeList.Code != http.StatusOK || !strings.Contains(rechargeList.Body.String(), result.TradeNo) || !strings.Contains(rechargeList.Body.String(), "\"total\":1") {
		t.Fatalf("list status=%d body=%s", rechargeList.Code, rechargeList.Body.String())
	}
	info := performFunding(t, router, http.MethodGet, "/api/v1/merchant/deposit", token, "")
	if info.Code != http.StatusOK || !strings.Contains(info.Body.String(), "\"deposit_balance\":0") || !strings.Contains(info.Body.String(), "\"enabled\":false") {
		t.Fatalf("deposit info status=%d body=%s", info.Code, info.Body.String())
	}
	for _, body := range []string{`{"amount":0}`, `{"amount":-5}`, `{}`, `not-json`} {
		if response := performFunding(t, router, http.MethodPost, "/api/v1/merchant/deposit", token, body); response.Code != http.StatusBadRequest {
			t.Fatalf("body %q status=%d", body, response.Code)
		}
	}
	deposit := performFunding(t, router, http.MethodPost, "/api/v1/merchant/deposit", token, `{"amount":2000}`)
	if deposit.Code != http.StatusCreated {
		t.Fatalf("deposit status=%d body=%s", deposit.Code, deposit.Body.String())
	}
	depositOrders := performFunding(t, router, http.MethodGet, "/api/v1/merchant/deposit/orders", token, "")
	if depositOrders.Code != http.StatusOK || !strings.Contains(depositOrders.Body.String(), "\"total\":1") {
		t.Fatalf("deposit orders status=%d body=%s", depositOrders.Code, depositOrders.Body.String())
	}
	if response := performFunding(t, router, http.MethodGet, "/api/v1/merchant/deposit", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", response.Code)
	}
}

func TestAdminFundingListsFilterByMerchant(t *testing.T) {
	router, db, _ := fundingRouter(t, "funding-http-admin")
	service := New(db, nil, testSiteURL)
	ctx := context.Background()
	if _, err := service.CreateRecharge(ctx, "merchant-1", 500); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateDeposit(ctx, "merchant-1", 700); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRecharge(ctx, "merchant-2", 900); err != nil {
		t.Fatal(err)
	}
	all := performFunding(t, router, http.MethodGet, "/api/v1/admin/funding/recharge", "", "")
	if all.Code != http.StatusOK || !strings.Contains(all.Body.String(), "\"total\":2") {
		t.Fatalf("admin recharge status=%d body=%s", all.Code, all.Body.String())
	}
	filtered := performFunding(t, router, http.MethodGet, "/api/v1/admin/funding/deposit?merchant_id=merchant-1", "", "")
	if filtered.Code != http.StatusOK || !strings.Contains(filtered.Body.String(), "\"total\":1") {
		t.Fatalf("admin deposit status=%d body=%s", filtered.Code, filtered.Body.String())
	}
}

func TestFundingListPaginatesWithoutExceedingMaxPageSize(t *testing.T) {
	_, db, _, _ := fundingDatabase(t, "funding-http-pagination")
	service := New(db, nil, testSiteURL)
	ctx := context.Background()
	for index := 0; index < 3; index++ {
		if _, err := service.CreateRecharge(ctx, "merchant-1", int64(100+index)); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := service.List(ctx, ListFilter{Kind: paymentmodel.KindRecharge}, Page{Number: 0, Size: 1000})
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("items=%d total=%d err=%v", len(items), total, err)
	}
	amounts := map[int64]bool{}
	for _, item := range items {
		amounts[item.Amount] = true
	}
	for _, expected := range []int64{100, 101, 102} {
		if !amounts[expected] {
			t.Fatalf("missing amount %d in %v", expected, amounts)
		}
	}
}

func fundingRouter(t *testing.T, name string) (http.Handler, *gorm.DB, string) {
	t.Helper()
	service, db, _, _ := fundingDatabase(t, name)
	if err := db.AutoMigrate(&identitymodel.Session{}); err != nil {
		t.Fatal(err)
	}
	sessions := session.NewService(db, time.Now)
	token, _, err := sessions.Create(context.Background(), identitymodel.PrincipalMerchant, "merchant-1", time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterMerchant(router.Group("/api/v1/merchant", identityhttp.Authenticate(sessions)), service)
	RegisterAdmin(router.Group("/api/v1/admin"), service)
	return router, db, token
}

func performFunding(t *testing.T, router http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(response, request)
	return response
}
