package complaint

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestAdminComplaintLifecycleOverHTTP(t *testing.T) {
	_, db := complaintFixture(t, "complaint-http-flow")
	router := complaintRouter(t, db)

	createBody := `{"merchant_id":"merchant-2","channel":"alipay","complaint_no":"H-1","reason":"买家投诉未到账","amount_cents":1200}`
	created := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaints", createBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var complaint complaintResponse
	if err := json.Unmarshal(created.Body.Bytes(), &complaint); err != nil {
		t.Fatal(err)
	}
	if complaint.Status != StatusPending || complaint.AmountCents != 1200 || complaint.Channel != "alipay" {
		t.Fatalf("complaint=%+v", complaint)
	}

	duplicate := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaints", createBody)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	listed := performComplaint(t, router, http.MethodGet, "/api/v1/admin/complaints?channel=alipay&status=pending&merchant_id=merchant-2", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), complaint.ID) || !strings.Contains(listed.Body.String(), `"total":1`) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}

	replied := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaints/"+complaint.ID+"/messages", `{"content":"已联系渠道处理"}`)
	if replied.Code != http.StatusOK || !strings.Contains(replied.Body.String(), `"status":"replied"`) {
		t.Fatalf("reply status=%d body=%s", replied.Code, replied.Body.String())
	}
	detail := performComplaint(t, router, http.MethodGet, "/api/v1/admin/complaints/"+complaint.ID, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "已联系渠道处理") || !strings.Contains(detail.Body.String(), `"direction":"out"`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	closed := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaints/"+complaint.ID+"/close", `{"reason":"渠道已退款"}`)
	if closed.Code != http.StatusOK || !strings.Contains(closed.Body.String(), `"status":"closed"`) {
		t.Fatalf("close status=%d body=%s", closed.Code, closed.Body.String())
	}
	if response := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaints/"+complaint.ID+"/messages", `{"content":"再回复"}`); response.Code != http.StatusConflict {
		t.Fatalf("reply after close status=%d body=%s", response.Code, response.Body.String())
	}
	if response := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaints/missing/close", `{"reason":"x"}`); response.Code != http.StatusNotFound {
		t.Fatalf("close missing status=%d", response.Code)
	}
}

func TestMerchantScopeAndAdminGuardOverHTTP(t *testing.T) {
	_, db := complaintFixture(t, "complaint-http-scope")
	router := complaintRouter(t, db)

	other := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaints",
		`{"merchant_id":"merchant-2","channel":"alipay","complaint_no":"S-1","reason":"别人的投诉"}`)
	if other.Code != http.StatusCreated {
		t.Fatalf("create other status=%d body=%s", other.Code, other.Body.String())
	}
	var otherComplaint complaintResponse
	if err := json.Unmarshal(other.Body.Bytes(), &otherComplaint); err != nil {
		t.Fatal(err)
	}
	list := performComplaint(t, router, http.MethodGet, "/api/v1/merchant/complaints", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"total":0`) {
		t.Fatalf("merchant list status=%d body=%s", list.Code, list.Body.String())
	}
	if response := performComplaint(t, router, http.MethodGet, "/api/v1/merchant/complaints/"+otherComplaint.ID, ""); response.Code != http.StatusNotFound {
		t.Fatalf("cross merchant detail status=%d body=%s", response.Code, response.Body.String())
	}

	own := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaints",
		`{"merchant_id":"merchant-1","channel":"wxpay","complaint_no":"S-2","reason":"我的投诉"}`)
	if own.Code != http.StatusCreated {
		t.Fatalf("create own status=%d body=%s", own.Code, own.Body.String())
	}
	var ownComplaint complaintResponse
	if err := json.Unmarshal(own.Body.Bytes(), &ownComplaint); err != nil {
		t.Fatal(err)
	}
	mine := performComplaint(t, router, http.MethodGet, "/api/v1/merchant/complaints", "")
	if mine.Code != http.StatusOK || !strings.Contains(mine.Body.String(), ownComplaint.ID) || !strings.Contains(mine.Body.String(), `"total":1`) {
		t.Fatalf("merchant own list status=%d body=%s", mine.Code, mine.Body.String())
	}
	if response := performComplaint(t, router, http.MethodGet, "/api/v1/merchant/complaints/"+ownComplaint.ID, ""); response.Code != http.StatusOK {
		t.Fatalf("merchant detail status=%d", response.Code)
	}

	merchantOnly := gin.New()
	Register(merchantOnly.Group("/api/v1"), db, merchantSessions())
	if response := performComplaint(t, merchantOnly, http.MethodGet, "/api/v1/admin/complaints", ""); response.Code != http.StatusForbidden {
		t.Fatalf("merchant hitting admin status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOverdueDerivedOverHTTPAndValidation(t *testing.T) {
	_, db := complaintFixture(t, "complaint-http-overdue")
	router := complaintRouter(t, db)
	past := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	created := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaints",
		`{"merchant_id":"merchant-1","channel":"alipay","complaint_no":"O-1","reason":"超时","deadline_at":"`+past+`"}`)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"status":"overdue"`) {
		t.Fatalf("create overdue status=%d body=%s", created.Code, created.Body.String())
	}
	overdueList := performComplaint(t, router, http.MethodGet, "/api/v1/admin/complaints?status=overdue", "")
	if overdueList.Code != http.StatusOK || !strings.Contains(overdueList.Body.String(), `"total":1`) {
		t.Fatalf("overdue list status=%d body=%s", overdueList.Code, overdueList.Body.String())
	}
	for _, body := range []string{
		`{"merchant_id":"merchant-1","channel":"alipay","complaint_no":"O-2","reason":""}`,
		`{"merchant_id":"missing","channel":"alipay","complaint_no":"O-3","reason":"x"}`,
		`{"merchant_id":"merchant-1","channel":"alipay","complaint_no":"O-4","reason":"x","deadline_at":"not-a-time"}`,
	} {
		if response := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaints", body); response.Code != http.StatusBadRequest {
			t.Fatalf("body %s status=%d", body, response.Code)
		}
	}
	if response := performComplaint(t, router, http.MethodGet, "/api/v1/admin/complaints?status=bogus", ""); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid status filter status=%d", response.Code)
	}
	if response := performComplaint(t, router, http.MethodGet, "/api/v1/admin/complaints?page=0", ""); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid page status=%d", response.Code)
	}
}

func complaintRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router.Group("/api/v1"), db, pathSessions())
	return router
}

func pathSessions() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if strings.Contains(ctx.Request.URL.Path, "/admin/") {
			ctx.Set("identity_principal", identitymodel.Session{PrincipalType: identitymodel.PrincipalAdministrator, PrincipalID: "admin-1"})
		} else {
			ctx.Set("identity_principal", identitymodel.Session{PrincipalType: identitymodel.PrincipalMerchant, PrincipalID: "merchant-1"})
		}
		ctx.Next()
	}
}

func merchantSessions() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Set("identity_principal", identitymodel.Session{PrincipalType: identitymodel.PrincipalMerchant, PrincipalID: "merchant-1"})
		ctx.Next()
	}
}

func performComplaint(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(response, request)
	return response
}
