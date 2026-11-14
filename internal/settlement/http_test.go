package settlement

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	financemodel "gorixpay/internal/finance/model"
	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
)

func TestAdminRuleCRUDAndManualRun(t *testing.T) {
	env := newSettlementEnv(t, "settle-http-crud")
	router := adminRouter(t, env)
	env.seedBalance(t, env.merchant.ID, 10000)

	response := performSettlement(t, router, http.MethodPost, "/api/v1/admin/settle-rules",
		`{"name":"daily","min_amount_cents":1000,"fee_bp":200,"period":"daily"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var rule Rule
	if err := json.Unmarshal(response.Body.Bytes(), &rule); err != nil {
		t.Fatal(err)
	}
	if rule.ID == "" || rule.Status != RuleActive || rule.Period != PeriodDaily {
		t.Fatalf("rule=%+v", rule)
	}

	listed := performSettlement(t, router, http.MethodGet, "/api/v1/admin/settle-rules?page=1&page_size=10", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), rule.ID) || !strings.Contains(listed.Body.String(), `"total":1`) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}

	updated := performSettlement(t, router, http.MethodPut, "/api/v1/admin/settle-rules/"+rule.ID,
		`{"name":"renamed","min_amount_cents":0,"fee_bp":300,"period":"daily","status":"active"}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), "renamed") {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}

	run := performSettlement(t, router, http.MethodPost, "/api/v1/admin/settle-rules/"+rule.ID+"/run", "")
	if run.Code != http.StatusOK || !strings.Contains(run.Body.String(), `"generated":1`) {
		t.Fatalf("run status=%d body=%s", run.Code, run.Body.String())
	}
	settlement := env.settlement(t, env.merchant.ID)
	if settlement.Amount != 10000 || settlement.FeeCents != 300 {
		t.Fatalf("settlement=%+v", settlement)
	}

	if response := performSettlement(t, router, http.MethodPost, "/api/v1/admin/settle-rules/missing/run", ""); response.Code != http.StatusNotFound {
		t.Fatalf("run missing status=%d", response.Code)
	}
	if response := performSettlement(t, router, http.MethodDelete, "/api/v1/admin/settle-rules/"+rule.ID, ""); response.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", response.Code)
	}
	if response := performSettlement(t, router, http.MethodDelete, "/api/v1/admin/settle-rules/"+rule.ID, ""); response.Code != http.StatusNotFound {
		t.Fatalf("delete missing status=%d", response.Code)
	}
}

func TestBatchProcessEndpointApprovesAndTransfers(t *testing.T) {
	env := newSettlementEnv(t, "settle-http-batch")
	router := adminRouter(t, env)
	env.seedBalance(t, env.merchant.ID, 10000)
	created := performSettlement(t, router, http.MethodPost, "/api/v1/admin/settle-rules",
		`{"name":"auto","min_amount_cents":0,"auto_transfer":true}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var rule Rule
	if err := json.Unmarshal(created.Body.Bytes(), &rule); err != nil {
		t.Fatal(err)
	}
	if run := performSettlement(t, router, http.MethodPost, "/api/v1/admin/settle-rules/"+rule.ID+"/run", ""); run.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", run.Code, run.Body.String())
	}
	settlement := env.settlement(t, env.merchant.ID)
	env.seedBalance(t, env.merchant.ID, 10000)

	body := `{"ids":["` + settlement.ID + `"],"action":"approve"}`
	response := performSettlement(t, router, http.MethodPost, "/api/v1/admin/settlements/batch-process", body)
	if response.Code != http.StatusOK {
		t.Fatalf("batch status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []BatchResult `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Status != string(financemodel.StatusApproved) || payload.Items[0].TransferStatus != TransferDone {
		t.Fatalf("items=%+v", payload.Items)
	}
	stored := env.settlement(t, env.merchant.ID)
	if stored.TransferStatus != TransferDone || stored.TransferAt == nil {
		t.Fatalf("stored=%+v", stored)
	}

	transfer := performSettlement(t, router, http.MethodPost, "/api/v1/admin/settlements/"+settlement.ID+"/auto-transfer", "")
	if transfer.Code != http.StatusOK || !strings.Contains(transfer.Body.String(), `"skipped":true`) {
		t.Fatalf("auto transfer status=%d body=%s", transfer.Code, transfer.Body.String())
	}
	if response := performSettlement(t, router, http.MethodPost, "/api/v1/admin/settlements/missing/auto-transfer", ""); response.Code != http.StatusNotFound {
		t.Fatalf("auto transfer missing status=%d", response.Code)
	}
	if response := performSettlement(t, router, http.MethodPost, "/api/v1/admin/settlements/batch-process", `{"ids":[],"action":"approve"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("empty batch status=%d", response.Code)
	}
}

func TestAdminGuardAndRuleValidationOverHTTP(t *testing.T) {
	env := newSettlementEnv(t, "settle-http-guard")
	router := gin.New()
	group := router.Group("/api/v1")
	Register(group, env.db, stubSession(identitymodel.PrincipalMerchant), env.finance, env.transfers, nil)
	response := performSettlement(t, router, http.MethodGet, "/api/v1/admin/settle-rules", "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("merchant session status=%d body=%s", response.Code, response.Body.String())
	}

	admin := adminRouter(t, env)
	if response := performSettlement(t, admin, http.MethodPost, "/api/v1/admin/settle-rules", `{"name":"","min_amount_cents":0}`); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid rule status=%d", response.Code)
	}
	if response := performSettlement(t, admin, http.MethodPost, "/api/v1/admin/settle-rules", `{"name":"x","fee_bp":20000}`); response.Code != http.StatusBadRequest {
		t.Fatalf("fee out of range status=%d", response.Code)
	}
	if response := performSettlement(t, admin, http.MethodPost, "/api/v1/admin/settle-rules", `{"name":"x","merchant_id":"missing"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown merchant status=%d", response.Code)
	}
}

func TestRulesCoexistWithExistingSettlementDetailRoute(t *testing.T) {
	env := newSettlementEnv(t, "settle-http-routes")
	router := gin.New()
	group := router.Group("/api/v1")
	// Mirrors the route internal/admin/httpapi already owns; registering it here
	// proves the batch and auto-transfer paths do not collide with it.
	group.GET("/admin/settlements/:id", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"existing": ctx.Param("id")})
	})
	Register(group, env.db, stubSession(identitymodel.PrincipalAdministrator), env.finance, env.transfers, nil)
	response := performSettlement(t, router, http.MethodGet, "/api/v1/admin/settlements/existing-1", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "existing-1") {
		t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
	}
}

func adminRouter(t *testing.T, env *settlementEnv) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router.Group("/api/v1"), env.db, stubSession(identitymodel.PrincipalAdministrator), env.finance, env.transfers, func(context.Context, string) (int, bool) { return 250, true })
	return router
}

func stubSession(principalType identitymodel.PrincipalType) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Set("identity_principal", identitymodel.Session{PrincipalType: principalType, PrincipalID: "admin-1"})
		ctx.Next()
	}
}

func performSettlement(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(response, request)
	return response
}
