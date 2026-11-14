package notifyprefs

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

	"github.com/gin-gonic/gin"
)

func newRouter(t *testing.T, fixture *fixture) (http.Handler, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	sessions := session.NewService(fixture.db, time.Now)
	token, _, err := sessions.Create(context.Background(), identitymodel.PrincipalMerchant, "merchant-1", time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterMerchant(router.Group("/api/v1/merchant", identityhttp.Authenticate(sessions)), fixture.service)
	return router, token
}

func perform(t *testing.T, router http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
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

func TestNotificationPrefsEndpoints(t *testing.T) {
	fixture := newFixture(t, "notifyprefs-http")
	router, token := newRouter(t, fixture)

	defaults := perform(t, router, http.MethodGet, "/api/v1/merchant/notification-prefs", token, "")
	if defaults.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", defaults.Code, defaults.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(defaults.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["notify_payment"] != true || body["notify_channel"] != ChannelMail {
		t.Fatalf("defaults=%v", body)
	}
	updated := perform(t, router, http.MethodPut, "/api/v1/merchant/notification-prefs", token, `{"notify_complaint":false,"notify_channel":"sms"}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"notify_channel":"sms"`) {
		t.Fatalf("put status=%d body=%s", updated.Code, updated.Body.String())
	}
	if invalid := perform(t, router, http.MethodPut, "/api/v1/merchant/notification-prefs", token, `{"notify_channel":"pigeon"}`); invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid channel status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	if anonymous := perform(t, router, http.MethodGet, "/api/v1/merchant/notification-prefs", "", ""); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", anonymous.Code)
	}
	if broken := perform(t, router, http.MethodPut, "/api/v1/merchant/notification-prefs", token, `not-json`); broken.Code != http.StatusBadRequest {
		t.Fatalf("bad json status=%d", broken.Code)
	}
}
