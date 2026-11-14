package profitsharing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	ledgerservice "gorixpay/internal/ledger/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const principalContextKey = "identity_principal"

func TestMerchantProfitSharingEndpoints(t *testing.T) {
	_, db, _ := profitsharingFixture(t, "ps-http-merchant")
	router := profitsharingRouter(t, db)

	response := perform(router, http.MethodPost, "/api/v1/merchant/profit-sharing/receivers",
		`{"account":"acc-http","name":"渠道HTTP","rate_bp":2500}`, "merchant-1")
	if response.Code != http.StatusCreated {
		t.Fatalf("create receiver: %d %s", response.Code, response.Body.String())
	}
	var receiver Receiver
	if err := json.Unmarshal(response.Body.Bytes(), &receiver); err != nil {
		t.Fatal(err)
	}
	if receiver.MerchantID != "merchant-1" || receiver.RateBp != 2500 || receiver.Status != ReceiverActive {
		t.Fatalf("receiver=%+v", receiver)
	}

	response = perform(router, http.MethodGet, "/api/v1/merchant/profit-sharing/receivers", "", "merchant-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"acc-http"`) || strings.Contains(response.Body.String(), `"total"`) {
		t.Fatalf("list receivers: %d %s", response.Code, response.Body.String())
	}
	response = perform(router, http.MethodPatch, "/api/v1/merchant/profit-sharing/receivers/"+receiver.ID,
		`{"rate_bp":1500}`, "merchant-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"rate_bp":1500`) {
		t.Fatalf("patch receiver: %d %s", response.Code, response.Body.String())
	}
	response = perform(router, http.MethodGet, "/api/v1/merchant/profit-sharing/orders", "", "merchant-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":0`) {
		t.Fatalf("list orders: %d %s", response.Code, response.Body.String())
	}

	if response := perform(router, http.MethodPost, "/api/v1/merchant/profit-sharing/receivers", `{"name":"x"}`, "merchant-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("missing rate: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodGet, "/api/v1/merchant/profit-sharing/orders?status=nope", "", "merchant-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("bad status: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPatch, "/api/v1/merchant/profit-sharing/receivers/missing", `{"name":"x"}`, "merchant-1"); response.Code != http.StatusNotFound {
		t.Fatalf("missing receiver: %d %s", response.Code, response.Body.String())
	}
	// Enable/disable uses the same PATCH endpoint with a single field.
	response = perform(router, http.MethodPatch, "/api/v1/merchant/profit-sharing/receivers/"+receiver.ID, `{"status":"disabled"}`, "merchant-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"disabled"`) {
		t.Fatalf("disable receiver: %d %s", response.Code, response.Body.String())
	}
	response = perform(router, http.MethodPatch, "/api/v1/merchant/profit-sharing/receivers/"+receiver.ID, `{"status":"active"}`, "merchant-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"active"`) {
		t.Fatalf("enable receiver: %d %s", response.Code, response.Body.String())
	}
	// A receiver without splits can be deleted.
	response = perform(router, http.MethodPost, "/api/v1/merchant/profit-sharing/receivers", `{"account":"acc-del","name":"待删","rate_bp":100}`, "merchant-1")
	if response.Code != http.StatusCreated {
		t.Fatalf("create deletable receiver: %d %s", response.Code, response.Body.String())
	}
	var deletable Receiver
	if err := json.Unmarshal(response.Body.Bytes(), &deletable); err != nil {
		t.Fatal(err)
	}
	if response := perform(router, http.MethodDelete, "/api/v1/merchant/profit-sharing/receivers/"+deletable.ID, "", "merchant-1"); response.Code != http.StatusNoContent {
		t.Fatalf("delete receiver: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodDelete, "/api/v1/merchant/profit-sharing/receivers/"+deletable.ID, "", "merchant-1"); response.Code != http.StatusNotFound {
		t.Fatalf("delete twice: %d %s", response.Code, response.Body.String())
	}
	// The merchant scope wins over any merchant_id in the payload.
	if response := perform(router, http.MethodPost, "/api/v1/merchant/profit-sharing/receivers",
		`{"merchant_id":"other","account":"acc-scope","name":"x","rate_bp":100}`, "merchant-1"); response.Code != http.StatusCreated {
		t.Fatalf("scoped create: %d %s", response.Code, response.Body.String())
	}
	var scoped Receiver
	if err := db.First(&scoped, "account = ?", "acc-scope").Error; err != nil || scoped.MerchantID != "merchant-1" {
		t.Fatalf("scoped receiver=%+v err=%v", scoped, err)
	}
}

func TestAdminProfitSharingEndpoints(t *testing.T) {
	service, db, payment := profitsharingFixture(t, "ps-http-admin")
	router := profitsharingRouter(t, db)

	response := perform(router, http.MethodPost, "/api/v1/admin/profit-sharing/receivers",
		`{"merchant_id":"merchant-1","account":"acc-admin","name":"管理员建","rate_bp":1000,"receiver_type":"channel"}`, "admin-1")
	if response.Code != http.StatusCreated {
		t.Fatalf("admin create receiver: %d %s", response.Code, response.Body.String())
	}
	var receiver Receiver
	if err := json.Unmarshal(response.Body.Bytes(), &receiver); err != nil {
		t.Fatal(err)
	}
	if receiver.MerchantID != "merchant-1" || receiver.ReceiverType != ReceiverChannel {
		t.Fatalf("receiver=%+v", receiver)
	}
	response = perform(router, http.MethodGet, "/api/v1/admin/profit-sharing/receivers?merchant_id=merchant-1", "", "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"acc-admin"`) {
		t.Fatalf("admin list receivers: %d %s", response.Code, response.Body.String())
	}

	rows, err := service.Execute(context.Background(), payment.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != OrderPending {
		t.Fatalf("execute rows=%+v err=%v", rows, err)
	}
	response = perform(router, http.MethodGet, "/api/v1/admin/profit-sharing/orders?status=pending", "", "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("admin list orders: %d %s", response.Code, response.Body.String())
	}
	response = perform(router, http.MethodPost, "/api/v1/admin/profit-sharing/orders/"+rows[0].ID+"/retry", "", "admin-1")
	if response.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", response.Code, response.Body.String())
	}
	var retried Order
	if err := json.Unmarshal(response.Body.Bytes(), &retried); err != nil {
		t.Fatal(err)
	}
	if retried.Status != OrderPending || retried.Error == nil {
		t.Fatalf("retried=%+v", retried)
	}
	// A receiver that already produced a split cannot be deleted.
	if response := perform(router, http.MethodDelete, "/api/v1/admin/profit-sharing/receivers/"+receiver.ID, "", "admin-1"); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "receiver_in_use") {
		t.Fatalf("admin delete in-use receiver: %d %s", response.Code, response.Body.String())
	}
	// An unused receiver can be deleted by the administrator.
	response = perform(router, http.MethodPost, "/api/v1/admin/profit-sharing/receivers",
		`{"merchant_id":"merchant-1","account":"acc-unused","name":"未使用","rate_bp":100}`, "admin-1")
	if response.Code != http.StatusCreated {
		t.Fatalf("admin create unused receiver: %d %s", response.Code, response.Body.String())
	}
	var unused Receiver
	if err := json.Unmarshal(response.Body.Bytes(), &unused); err != nil {
		t.Fatal(err)
	}
	if response := perform(router, http.MethodDelete, "/api/v1/admin/profit-sharing/receivers/"+unused.ID, "", "admin-1"); response.Code != http.StatusNoContent {
		t.Fatalf("admin delete unused receiver: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPost, "/api/v1/admin/profit-sharing/orders/missing/retry", "", "admin-1"); response.Code != http.StatusNotFound {
		t.Fatalf("retry missing: %d %s", response.Code, response.Body.String())
	}
	// A merchant principal cannot reach the admin group.
	if response := perform(router, http.MethodGet, "/api/v1/admin/profit-sharing/orders", "", "merchant-1"); response.Code != http.StatusForbidden {
		t.Fatalf("merchant on admin route: %d %s", response.Code, response.Body.String())
	}
}

func TestRateExceededReturnsConflict(t *testing.T) {
	_, db, _ := profitsharingFixture(t, "ps-http-rate")
	router := profitsharingRouter(t, db)
	if response := perform(router, http.MethodPost, "/api/v1/merchant/profit-sharing/receivers",
		`{"account":"acc-1","name":"A","rate_bp":6000}`, "merchant-1"); response.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", response.Code, response.Body.String())
	}
	response := perform(router, http.MethodPost, "/api/v1/merchant/profit-sharing/receivers",
		`{"account":"acc-2","name":"B","rate_bp":6000}`, "merchant-1")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "rate_exceeded") {
		t.Fatalf("second: %d %s", response.Code, response.Body.String())
	}
}

func profitsharingRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ledger := ledgerservice.New(db, time.Now)
	router := gin.New()
	Register(router.Group("/api/v1"), db, stubSessions(), ledger)
	return router
}

// stubSessions reads the principal from a header so one router can serve both
// the merchant and the admin group.
func stubSessions() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		principalID := strings.TrimSpace(ctx.GetHeader("X-Principal"))
		if principalID == "" {
			principalID = "merchant-1"
		}
		principalType := identitymodel.PrincipalMerchant
		if strings.HasPrefix(principalID, "admin-") {
			principalType = identitymodel.PrincipalAdministrator
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

func TestMountUsesTheProvidedService(t *testing.T) {
	service, db, payment := profitsharingFixture(t, "ps-http-mount")
	service = service.WithFeeResolver(staticFee(100))
	if _, err := service.CreateReceiver(context.Background(), ReceiverRequest{MerchantID: "merchant-1", Account: "acc-mount", Name: "挂载", RateBp: 1000}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(context.Background(), payment.ID); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Mount(router.Group("/api/v1"), stubSessions(), service)
	response := perform(router, http.MethodGet, "/api/v1/merchant/profit-sharing/orders", "", "merchant-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("mounted orders: %d %s", response.Code, response.Body.String())
	}
	// Missing dependencies are ignored instead of panicking.
	Mount(nil, nil, nil)
	Register(nil, db, nil, nil)
}
