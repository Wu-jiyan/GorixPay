package merchantgroup

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// httpFixture wires the real session middleware so the tests exercise the same
// auth chain the application uses.
func httpFixture(t *testing.T, name string) (*gin.Engine, *gorm.DB, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    "file:" + name + "?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&Group{}, &merchantRow{}, &identitymodel.Session{}); err != nil {
		t.Fatalf("migrate fixture: %v", err)
	}
	sessions := session.NewService(db, time.Now)
	token, _, err := sessions.Create(context.Background(), identitymodel.PrincipalAdministrator, "admin-1", time.Hour, "", "")
	if err != nil {
		t.Fatalf("create administrator session: %v", err)
	}
	router := gin.New()
	Register(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions))
	return router, db, token
}

func merchantToken(t *testing.T, db *gorm.DB) string {
	t.Helper()
	sessions := session.NewService(db, time.Now)
	token, _, err := sessions.Create(context.Background(), identitymodel.PrincipalMerchant, "merchant-1", time.Hour, "", "")
	if err != nil {
		t.Fatalf("create merchant session: %v", err)
	}
	return token
}

func call(t *testing.T, router *gin.Engine, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(payload)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decode(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return payload
}

func TestRoutesRequireAdministrator(t *testing.T) {
	router, db, _ := httpFixture(t, "group-http-auth")

	if recorder := call(t, router, http.MethodGet, "/api/v1/admin/groups", "", nil); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET = %d, want 401", recorder.Code)
	}
	token := merchantToken(t, db)
	if recorder := call(t, router, http.MethodGet, "/api/v1/admin/groups", token, nil); recorder.Code != http.StatusForbidden {
		t.Fatalf("merchant GET = %d, want 403", recorder.Code)
	}
}

func TestGroupHTTPFlow(t *testing.T) {
	router, _, token := httpFixture(t, "group-http-flow")

	recorder := call(t, router, http.MethodPost, "/api/v1/admin/groups", token, map[string]any{
		"name": "标准组", "rate_bp": 60, "pay_types": []string{"alipay", "wxpay"}, "settle_mode": "normal",
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", recorder.Code, recorder.Body.String())
	}
	created := decode(t, recorder)
	groupID, _ := created["id"].(string)
	if groupID == "" || created["name"] != "标准组" || created["rate_bp"] != float64(60) {
		t.Fatalf("created = %v", created)
	}
	if created["status"] != "active" || created["settle_mode"] != "normal" || created["merchant_count"] != float64(0) {
		t.Fatalf("created defaults = %v", created)
	}
	if types, _ := created["pay_types"].([]any); len(types) != 2 || types[0] != "alipay" {
		t.Fatalf("created pay_types = %v", created["pay_types"])
	}

	recorder = call(t, router, http.MethodGet, "/api/v1/admin/groups?page=1&page_size=10", token, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list = %d %s", recorder.Code, recorder.Body.String())
	}
	listing := decode(t, recorder)
	items, _ := listing["items"].([]any)
	if len(items) != 1 || listing["total"] != float64(1) || listing["page"] != float64(1) || listing["page_size"] != float64(10) {
		t.Fatalf("listing = %v", listing)
	}

	recorder = call(t, router, http.MethodPut, "/api/v1/admin/groups/"+groupID, token, map[string]any{
		"name": "标准组", "rate_bp": 45, "pay_types": []string{"alipay"}, "settle_mode": "direct",
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("update = %d %s", recorder.Code, recorder.Body.String())
	}
	if updated := decode(t, recorder); updated["rate_bp"] != float64(45) || updated["settle_mode"] != "direct" {
		t.Fatalf("updated = %v", updated)
	}

	recorder = call(t, router, http.MethodPatch, "/api/v1/admin/groups/"+groupID+"/status", token, map[string]any{"status": "disabled"})
	if recorder.Code != http.StatusOK || decode(t, recorder)["status"] != "disabled" {
		t.Fatalf("status = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = call(t, router, http.MethodPost, "/api/v1/admin/groups", token, map[string]any{"name": "标准组"})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate create = %d %s", recorder.Code, recorder.Body.String())
	}
	if code := decode(t, recorder)["code"]; code != "duplicate_name" {
		t.Fatalf("duplicate code = %v", code)
	}

	recorder = call(t, router, http.MethodPost, "/api/v1/admin/groups", token, map[string]any{"name": "坏组", "rate_bp": 10001})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid rate = %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = call(t, router, http.MethodPut, "/api/v1/admin/groups/missing", token, map[string]any{"name": "x"})
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("update missing = %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = call(t, router, http.MethodPatch, "/api/v1/admin/groups/"+groupID+"/status", token, map[string]any{"status": "paused"})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = call(t, router, http.MethodGet, "/api/v1/admin/groups?page_size=500", token, nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("page_size=500 = %d, want 400", recorder.Code)
	}
}

func TestMerchantAssignmentHTTP(t *testing.T) {
	router, db, token := httpFixture(t, "group-http-assign")
	seedMerchant(t, db, "merchant-1")

	recorder := call(t, router, http.MethodPost, "/api/v1/admin/groups", token, map[string]any{
		"name": "直清组", "rate_bp": 25, "settle_mode": "direct",
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", recorder.Code, recorder.Body.String())
	}
	groupID := decode(t, recorder)["id"].(string)

	recorder = call(t, router, http.MethodPatch, "/api/v1/admin/merchants/merchant-1/group", token, map[string]any{"group_id": groupID})
	if recorder.Code != http.StatusOK {
		t.Fatalf("assign = %d %s", recorder.Code, recorder.Body.String())
	}
	assigned := decode(t, recorder)
	if assigned["merchant_id"] != "merchant-1" || assigned["group_id"] != groupID || assigned["settle_mode"] != "direct" {
		t.Fatalf("assignment = %v", assigned)
	}
	if mode := merchantSettleMode(t, db, "merchant-1"); mode != "direct" {
		t.Fatalf("merchant settle_mode = %q", mode)
	}
	// The group shows up in the member count.
	recorder = call(t, router, http.MethodGet, "/api/v1/admin/groups", token, nil)
	items := decode(t, recorder)["items"].([]any)
	if count := items[0].(map[string]any)["merchant_count"]; count != float64(1) {
		t.Fatalf("merchant_count = %v", count)
	}

	recorder = call(t, router, http.MethodPatch, "/api/v1/admin/merchants/merchant-1/group", token, map[string]any{})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing group_id = %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = call(t, router, http.MethodPatch, "/api/v1/admin/merchants/merchant-1/group", token, map[string]any{"group_id": nil})
	if recorder.Code != http.StatusOK {
		t.Fatalf("unassign = %d %s", recorder.Code, recorder.Body.String())
	}
	if group := decode(t, recorder)["group_id"]; group != nil {
		t.Fatalf("unassign group_id = %v, want null", group)
	}
	if mode := merchantSettleMode(t, db, "merchant-1"); mode != "normal" {
		t.Fatalf("settle_mode after unassign = %q", mode)
	}

	recorder = call(t, router, http.MethodPatch, "/api/v1/admin/merchants/missing/group", token, map[string]any{"group_id": groupID})
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown merchant = %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = call(t, router, http.MethodPatch, "/api/v1/admin/merchants/merchant-1/group", token, map[string]any{"group_id": "no-such-group"})
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown group = %d %s", recorder.Code, recorder.Body.String())
	}

	if recorder = call(t, router, http.MethodPatch, "/api/v1/admin/groups/"+groupID+"/status", token, map[string]any{"status": "disabled"}); recorder.Code != http.StatusOK {
		t.Fatalf("disable = %d", recorder.Code)
	}
	recorder = call(t, router, http.MethodPatch, "/api/v1/admin/merchants/merchant-1/group", token, map[string]any{"group_id": groupID})
	if recorder.Code != http.StatusConflict || decode(t, recorder)["code"] != "group_disabled" {
		t.Fatalf("assign to disabled group = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestDeleteGroupHTTP(t *testing.T) {
	router, db, token := httpFixture(t, "group-http-delete")
	seedMerchant(t, db, "merchant-1")

	recorder := call(t, router, http.MethodPost, "/api/v1/admin/groups", token, map[string]any{"name": "待删组", "rate_bp": 10})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create = %d", recorder.Code)
	}
	groupID := decode(t, recorder)["id"].(string)
	if recorder = call(t, router, http.MethodPatch, "/api/v1/admin/merchants/merchant-1/group", token, map[string]any{"group_id": groupID}); recorder.Code != http.StatusOK {
		t.Fatalf("assign = %d", recorder.Code)
	}

	recorder = call(t, router, http.MethodDelete, "/api/v1/admin/groups/"+groupID, token, nil)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("delete in use = %d %s", recorder.Code, recorder.Body.String())
	}
	if code := decode(t, recorder)["code"]; code != "group_in_use" {
		t.Fatalf("delete in use code = %v", code)
	}

	if recorder = call(t, router, http.MethodPatch, "/api/v1/admin/merchants/merchant-1/group", token, map[string]any{"group_id": nil}); recorder.Code != http.StatusOK {
		t.Fatalf("unassign = %d", recorder.Code)
	}
	if recorder = call(t, router, http.MethodDelete, "/api/v1/admin/groups/"+groupID, token, nil); recorder.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder = call(t, router, http.MethodDelete, "/api/v1/admin/groups/"+groupID, token, nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d", recorder.Code)
	}
}

// TestRouteTreeCoexistsWithExistingAdminRoutes guards the wiring: the assignment
// route lives under the same /admin/merchants/:id node the existing handler uses
// for status and detail, so a gin conflict here would panic at startup.
func TestRouteTreeCoexistsWithExistingAdminRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api/v1")
	noop := func(ctx *gin.Context) { ctx.Status(http.StatusOK) }
	api.PATCH("/admin/merchants/:id/status", noop)
	api.GET("/admin/merchants/:id", noop)
	api.GET("/admin/merchants", noop)

	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    "file:group-http-tree?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&Group{}, &merchantRow{}, &identitymodel.Session{}); err != nil {
		t.Fatalf("migrate fixture: %v", err)
	}
	sessions := session.NewService(db, time.Now)
	token, _, err := sessions.Create(context.Background(), identitymodel.PrincipalAdministrator, "admin-1", time.Hour, "", "")
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	// A panic here means the route tree collided with internal/admin/httpapi.
	Register(api, db, identityhttp.Authenticate(sessions))

	if recorder := call(t, router, http.MethodGet, "/api/v1/admin/groups", token, nil); recorder.Code != http.StatusOK {
		t.Fatalf("groups after coexistence registration = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := call(t, router, http.MethodPatch, "/api/v1/admin/merchants/merchant-1/group", token, map[string]any{"group_id": nil}); recorder.Code != http.StatusNotFound {
		t.Fatalf("assignment route = %d, want 404 for an unknown merchant", recorder.Code)
	}
}

// TestRegisterAcceptsAnAlreadyAdminGroup covers the second wiring shape: the Lead
// may pass the authenticated /api/v1/admin group instead of the root.
func TestRegisterAcceptsAnAlreadyAdminGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    "file:group-http-admin-group?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&Group{}, &merchantRow{}, &identitymodel.Session{}); err != nil {
		t.Fatalf("migrate fixture: %v", err)
	}
	sessions := session.NewService(db, time.Now)
	token, _, err := sessions.Create(context.Background(), identitymodel.PrincipalAdministrator, "admin-1", time.Hour, "", "")
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	router := gin.New()
	adminGroup := router.Group("/api/v1/admin", identityhttp.Authenticate(sessions), identityhttp.RequireAdministrator())
	Register(adminGroup, db, identityhttp.Authenticate(sessions))

	if recorder := call(t, router, http.MethodGet, "/api/v1/admin/groups", token, nil); recorder.Code != http.StatusOK {
		t.Fatalf("GET with admin group wiring = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := call(t, router, http.MethodGet, "/api/v1/admin/admin/groups", token, nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("routes must not nest under /admin/admin, got %d", recorder.Code)
	}
}

func TestRegisterWithoutSessionsMountsNothing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router.Group("/api/v1"), &gorm.DB{}, nil)
	if recorder := call(t, router, http.MethodGet, "/api/v1/admin/groups", "", nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("nil sessions registered routes: %d", recorder.Code)
	}
}
