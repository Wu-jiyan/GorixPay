package invitecode

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newRouter(t *testing.T, service *Service) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterAdmin(router.Group("/api/v1/admin"), service)
	return router
}

func perform(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	return response
}

func TestAdminInviteCodeCRUD(t *testing.T) {
	service, _ := newService(t, "invite-http")
	router := newRouter(t, service)

	created := perform(t, router, http.MethodPost, "/api/v1/admin/invite-codes", `{"max_uses":2}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var body InviteCode
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil || len(body.Code) != codeLength {
		t.Fatalf("body=%+v err=%v", body, err)
	}
	listed := perform(t, router, http.MethodGet, "/api/v1/admin/invite-codes?page=1&page_size=10", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"total":1`) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	updated := perform(t, router, http.MethodPut, "/api/v1/admin/invite-codes/"+body.ID, `{"max_uses":5,"status":"disabled"}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"max_uses":5`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	duplicate := perform(t, router, http.MethodPost, "/api/v1/admin/invite-codes", `{"code":"`+body.Code+`"}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
	if response := perform(t, router, http.MethodPost, "/api/v1/admin/invite-codes", `{"max_uses":-1}`); response.Code != http.StatusBadRequest {
		t.Fatalf("negative max_uses status=%d", response.Code)
	}
	if response := perform(t, router, http.MethodDelete, "/api/v1/admin/invite-codes/"+body.ID, ""); response.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	if response := perform(t, router, http.MethodDelete, "/api/v1/admin/invite-codes/"+body.ID, ""); response.Code != http.StatusNotFound {
		t.Fatalf("delete again status=%d", response.Code)
	}
	if response := perform(t, router, http.MethodPost, "/api/v1/admin/invite-codes", `not-json`); response.Code != http.StatusBadRequest {
		t.Fatalf("bad json status=%d", response.Code)
	}
}

var _ = bytes.NewReader
