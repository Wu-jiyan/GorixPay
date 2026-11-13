package transfer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
)

const sessionKey = "identity_principal"

func TestMerchantAndAdminTransferEndpoints(t *testing.T) {
	service, _, merchantID := transferFixture(t, "transfer-http")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	merchantGroup := router.Group("/api/v1/merchant", stubSession(identitymodel.PrincipalMerchant, merchantID))
	RegisterMerchant(merchantGroup, service)
	adminGroup := router.Group("/api/v1/admin", stubSession(identitymodel.PrincipalAdministrator, "admin-1"))
	RegisterAdmin(adminGroup, service)

	body := `{"out_transfer_no":"HTTP-1","amount":1000,"account_name":"张三","account_no":"6222000000000009","bank_name":"ICBC"}`
	response := performJSON(router, http.MethodPost, "/api/v1/merchant/transfers", body)
	if response.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["status"] != string(StatusPending) || created["amount"] != float64(1000) {
		t.Fatalf("created: %v", created)
	}
	id, _ := created["id"].(string)

	response = performJSON(router, http.MethodGet, "/api/v1/merchant/transfers", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("list: %d %s", response.Code, response.Body.String())
	}
	response = performJSON(router, http.MethodGet, "/api/v1/merchant/transfers/"+id, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "HTTP-1") {
		t.Fatalf("get: %d %s", response.Code, response.Body.String())
	}
	response = performJSON(router, http.MethodPost, "/api/v1/admin/transfers/"+id+"/process", `{"action":"approve"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"approved"`) {
		t.Fatalf("approve: %d %s", response.Code, response.Body.String())
	}
	response = performJSON(router, http.MethodGet, "/api/v1/admin/transfers", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("admin list: %d %s", response.Code, response.Body.String())
	}
}

func TestTransferEndpointsValidatePayloadAndScope(t *testing.T) {
	service, _, merchantID := transferFixture(t, "transfer-http-errors")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterMerchant(router.Group("/api/v1/merchant", stubSession(identitymodel.PrincipalMerchant, merchantID)), service)
	RegisterAdmin(router.Group("/api/v1/admin", stubSession(identitymodel.PrincipalAdministrator, "admin-1")), service)

	if response := performJSON(router, http.MethodPost, "/api/v1/merchant/transfers", `{"amount":0}`); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid amount: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(router, http.MethodPost, "/api/v1/merchant/transfers", `{"out_transfer_no":"BIG","amount":900000,"account_no":"A"}`); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "insufficient_balance") {
		t.Fatalf("insufficient: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(router, http.MethodGet, "/api/v1/merchant/transfers/unknown", ""); response.Code != http.StatusNotFound {
		t.Fatalf("unknown get: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(router, http.MethodGet, "/api/v1/merchant/transfers?status=nope", ""); response.Code != http.StatusBadRequest {
		t.Fatalf("bad status: %d %s", response.Code, response.Body.String())
	}
}

func stubSession(principalType identitymodel.PrincipalType, principalID string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Set(sessionKey, identitymodel.Session{PrincipalType: principalType, PrincipalID: principalID})
		ctx.Next()
	}
}

func performJSON(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(response, request)
	return response
}
