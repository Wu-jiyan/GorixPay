package groupshop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identitymodel "gorixpay/internal/identity/model"
	ledgerservice "gorixpay/internal/ledger/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const principalContextKey = "identity_principal"

func TestMerchantPlansPurchaseAndAdminRefund(t *testing.T) {
	_, db := groupshopFixture(t, "groupshop-http")
	seedGroup(t, db, "group-1", "黄金组", 1000, "active")
	seedGroup(t, db, "group-free", "免费组", 0, "active")
	router := groupshopRouter(db)

	response := perform(router, http.MethodGet, "/api/v1/merchant/group-plans", "", "merchant-1")
	if response.Code != http.StatusOK {
		t.Fatalf("plans: %d %s", response.Code, response.Body.String())
	}
	var plans struct {
		Items []Plan `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &plans); err != nil {
		t.Fatal(err)
	}
	if len(plans.Items) != 1 || plans.Items[0].ID != "group-1" || plans.Items[0].PriceCents != 1000 {
		t.Fatalf("plans=%+v", plans.Items)
	}

	response = perform(router, http.MethodPost, "/api/v1/merchant/group-purchases", `{"group_id":"group-1"}`, "merchant-1")
	if response.Code != http.StatusCreated {
		t.Fatalf("purchase: %d %s", response.Code, response.Body.String())
	}
	var created purchaseView
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.PurchaseID == "" || created.GroupID != "group-1" || created.Status != PurchaseActive || created.PriceCents != 1000 {
		t.Fatalf("created=%+v", created)
	}
	response = perform(router, http.MethodPost, "/api/v1/merchant/group-purchases", `{"group_id":"group-1"}`, "merchant-1")
	if response.Code != http.StatusOK {
		t.Fatalf("repeat purchase: %d %s", response.Code, response.Body.String())
	}
	var repeated purchaseView
	if err := json.Unmarshal(response.Body.Bytes(), &repeated); err != nil {
		t.Fatal(err)
	}
	if repeated.PurchaseID != created.PurchaseID {
		t.Fatalf("repeat created a second purchase: %+v", repeated)
	}

	if response := perform(router, http.MethodPost, "/api/v1/merchant/group-purchases", `{"group_id":"group-free"}`, "merchant-1"); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "group_not_for_sale") {
		t.Fatalf("free group: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPost, "/api/v1/merchant/group-purchases", `{"group_id":"missing"}`, "merchant-1"); response.Code != http.StatusNotFound {
		t.Fatalf("missing group: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPost, "/api/v1/merchant/group-purchases", `{}`, "merchant-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("empty body: %d %s", response.Code, response.Body.String())
	}

	response = perform(router, http.MethodGet, "/api/v1/admin/group-purchases?page=1&page_size=10&status=active", "", "admin-1")
	if response.Code != http.StatusOK {
		t.Fatalf("admin list: %d %s", response.Code, response.Body.String())
	}
	var listing struct {
		Items    []purchaseView `json:"items"`
		Total    int64          `json:"total"`
		Page     int            `json:"page"`
		PageSize int            `json:"page_size"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.Total != 1 || len(listing.Items) != 1 || listing.Page != 1 || listing.PageSize != 10 {
		t.Fatalf("listing=%+v", listing)
	}
	if response := perform(router, http.MethodGet, "/api/v1/admin/group-purchases?status=nope", "", "admin-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("bad status: %d %s", response.Code, response.Body.String())
	}

	response = perform(router, http.MethodPost, "/api/v1/admin/group-purchases/"+created.PurchaseID+"/refund", "", "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"refunded"`) {
		t.Fatalf("refund: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPost, "/api/v1/admin/group-purchases/"+created.PurchaseID+"/refund", "", "admin-1"); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "not_refundable") {
		t.Fatalf("second refund: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPost, "/api/v1/admin/group-purchases/missing/refund", "", "admin-1"); response.Code != http.StatusNotFound {
		t.Fatalf("refund missing: %d %s", response.Code, response.Body.String())
	}
}

func TestInsufficientBalanceAndPriceEndpoints(t *testing.T) {
	_, db := groupshopFixture(t, "groupshop-http-price")
	seedGroup(t, db, "group-expensive", "铂金组", 6000, "active")
	router := groupshopRouter(db)

	response := perform(router, http.MethodPost, "/api/v1/merchant/group-purchases", `{"group_id":"group-expensive"}`, "merchant-1")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "insufficient_balance") {
		t.Fatalf("insufficient: %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "可用余额不足") {
		t.Fatalf("missing merchant friendly message: %s", response.Body.String())
	}

	response = perform(router, http.MethodPatch, "/api/v1/admin/groups/group-expensive/price", `{"price_cents":2000}`, "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"price_cents":2000`) {
		t.Fatalf("set price: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPatch, "/api/v1/admin/groups/group-expensive/price", `{}`, "admin-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("missing price: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPatch, "/api/v1/admin/groups/missing/price", `{"price_cents":100}`, "admin-1"); response.Code != http.StatusNotFound {
		t.Fatalf("missing group price: %d %s", response.Code, response.Body.String())
	}
	response = perform(router, http.MethodGet, "/api/v1/admin/group-plans", "", "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "group-expensive") {
		t.Fatalf("admin plans: %d %s", response.Code, response.Body.String())
	}
	// A merchant principal cannot reach the administrator routes.
	if response := perform(router, http.MethodGet, "/api/v1/admin/group-purchases", "", "merchant-1"); response.Code != http.StatusForbidden {
		t.Fatalf("merchant on admin: %d %s", response.Code, response.Body.String())
	}
}

func groupshopRouter(db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router.Group("/api/v1"), db, stubSessions(), ledgerservice.New(db, nil))
	return router
}

func stubSessions() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		principalID := strings.TrimSpace(ctx.GetHeader("X-Principal"))
		if principalID == "" {
			principalID = "admin-1"
		}
		principalType := identitymodel.PrincipalAdministrator
		if !strings.HasPrefix(principalID, "admin-") {
			principalType = identitymodel.PrincipalMerchant
		}
		ctx.Set(principalContextKey, identitymodel.Session{PrincipalType: principalType, PrincipalID: principalID})
		ctx.Next()
	}
}

func perform(router http.Handler, method, path, body, principal string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if principal != "" {
		request.Header.Set("X-Principal", principal)
	}
	router.ServeHTTP(response, request)
	return response
}
