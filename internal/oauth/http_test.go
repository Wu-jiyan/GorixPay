package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
)

func newHTTPRouter(t *testing.T, fixture *fixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterWithProviders(router.Group("/api/v1"), fixture.db, fixture.sessions, fixture.cipher, "https://pay.example", fixture.provider)
	return router
}

func (fixture *fixture) merchantToken(t *testing.T) string {
	t.Helper()
	token, _, err := fixture.sessions.Create(context.Background(), identitymodel.PrincipalMerchant, testMerchantID, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (fixture *fixture) adminToken(t *testing.T) string {
	t.Helper()
	token, _, err := fixture.sessions.Create(context.Background(), identitymodel.PrincipalAdministrator, "admin-1", time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func perform(t *testing.T, router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(response, request)
	return response
}

func stateFromRedirect(t *testing.T, location string) string {
	t.Helper()
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatalf("location %q has no state", location)
	}
	return state
}

func TestAuthorizeRedirectsToProviderWithState(t *testing.T) {
	fixture := newFixture(t, "oauth-http-authorize")
	router := newHTTPRouter(t, fixture)
	response := perform(t, router, http.MethodGet, "/api/v1/auth/oauth/wechat/authorize?redirect=/merchant", "", nil)
	if response.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "https://fake.example/authorize?state=") {
		t.Fatalf("location=%q", location)
	}
	state := stateFromRedirect(t, location)
	var stored OAuthState
	if err := fixture.db.First(&stored, "state_hash = ?", hashToken(state)).Error; err != nil {
		t.Fatalf("state not persisted: %v", err)
	}
	if stored.Provider != "wechat" || stored.RedirectPath != "/merchant" {
		t.Fatalf("state=%+v", stored)
	}
	if response := perform(t, router, http.MethodGet, "/api/v1/auth/oauth/github/authorize", "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("unknown provider status=%d", response.Code)
	}
	if response := perform(t, router, http.MethodGet, "/api/v1/auth/oauth/qq/authorize", "", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("unconfigured provider status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCallbackBindFlowOverHTTP(t *testing.T) {
	fixture := newFixture(t, "oauth-http-bind")
	router := newHTTPRouter(t, fixture)
	authorize := perform(t, router, http.MethodGet, "/api/v1/auth/oauth/wechat/authorize", "", nil)
	state := stateFromRedirect(t, authorize.Header().Get("Location"))
	callback := perform(t, router, http.MethodGet, "/api/v1/auth/oauth/wechat/callback?code=code-1&state="+state, "", nil)
	if callback.Code != http.StatusConflict || !strings.Contains(callback.Body.String(), "not_bound") {
		t.Fatalf("callback status=%d body=%s", callback.Code, callback.Body.String())
	}
	var notBound struct {
		BindTicket string `json:"bind_ticket"`
	}
	if err := json.Unmarshal(callback.Body.Bytes(), &notBound); err != nil || notBound.BindTicket == "" {
		t.Fatalf("bind ticket missing: %s", callback.Body.String())
	}
	wrong := perform(t, router, http.MethodPost, "/api/v1/auth/oauth/bind", "", map[string]string{
		"bind_ticket": notBound.BindTicket, "identifier": testEmail, "password": "wrong-password",
	})
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status=%d body=%s", wrong.Code, wrong.Body.String())
	}
	bound := perform(t, router, http.MethodPost, "/api/v1/auth/oauth/bind", "", map[string]string{
		"bind_ticket": notBound.BindTicket, "identifier": testEmail, "password": testPassword,
	})
	if bound.Code != http.StatusOK || !strings.Contains(bound.Body.String(), "token") {
		t.Fatalf("bind status=%d body=%s", bound.Code, bound.Body.String())
	}
	// After binding, a fresh authorize+callback signs the merchant straight in.
	again := perform(t, router, http.MethodGet, "/api/v1/auth/oauth/wechat/authorize", "", nil)
	callbackAgain := perform(t, router, http.MethodGet,
		"/api/v1/auth/oauth/wechat/callback?code=code-2&state="+stateFromRedirect(t, again.Header().Get("Location")), "", nil)
	if callbackAgain.Code != http.StatusOK {
		t.Fatalf("bound callback status=%d body=%s", callbackAgain.Code, callbackAgain.Body.String())
	}
	var login LoginResult
	if err := json.Unmarshal(callbackAgain.Body.Bytes(), &login); err != nil || login.Token == "" || login.PrincipalType != "merchant" {
		t.Fatalf("login=%+v err=%v", login, err)
	}
	replayed := perform(t, router, http.MethodGet, "/api/v1/auth/oauth/wechat/callback?code=code-3&state="+state, "", nil)
	if replayed.Code != http.StatusBadRequest || !strings.Contains(replayed.Body.String(), "invalid_state") {
		t.Fatalf("replayed state status=%d body=%s", replayed.Code, replayed.Body.String())
	}
}

func TestMerchantBindingsEndpoints(t *testing.T) {
	fixture := newFixture(t, "oauth-http-bindings")
	router := newHTTPRouter(t, fixture)
	token := fixture.merchantToken(t)
	binding := Binding{
		ID: "binding-1", PrincipalType: "merchant", PrincipalID: testMerchantID,
		Provider: "wechat", OpenID: "open-1", Nickname: stringPointer("昵称"),
		CreatedAt: fixture.clock.value, UpdatedAt: fixture.clock.value,
	}
	if err := fixture.db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	listed := perform(t, router, http.MethodGet, "/api/v1/merchant/oauth/bindings", token, nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "open-1") {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	if response := perform(t, router, http.MethodGet, "/api/v1/merchant/oauth/bindings", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list status=%d", response.Code)
	}
	removed := perform(t, router, http.MethodDelete, "/api/v1/merchant/oauth/bindings/wechat", token, nil)
	if removed.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", removed.Code, removed.Body.String())
	}
	if response := perform(t, router, http.MethodDelete, "/api/v1/merchant/oauth/bindings/qq", token, nil); response.Code != http.StatusNotFound {
		t.Fatalf("delete missing status=%d", response.Code)
	}
}

func TestQRLoginOverHTTP(t *testing.T) {
	fixture := newFixture(t, "oauth-http-qr")
	router := newHTTPRouter(t, fixture)
	merchantToken := fixture.merchantToken(t)
	created := perform(t, router, http.MethodPost, "/api/v1/auth/qr/ticket", "", nil)
	if created.Code != http.StatusOK {
		t.Fatalf("ticket status=%d body=%s", created.Code, created.Body.String())
	}
	var ticket QRTicket
	if err := json.Unmarshal(created.Body.Bytes(), &ticket); err != nil || ticket.Ticket == "" || !strings.Contains(ticket.SVG, "<svg") {
		t.Fatalf("ticket=%+v err=%v", ticket, err)
	}
	poll := perform(t, router, http.MethodGet, "/api/v1/auth/qr/poll?ticket="+ticket.Ticket, "", nil)
	if poll.Code != http.StatusOK || !strings.Contains(poll.Body.String(), `"status":"pending"`) {
		t.Fatalf("poll status=%d body=%s", poll.Code, poll.Body.String())
	}
	scan := perform(t, router, http.MethodPost, "/api/v1/auth/qr/scan?ticket="+ticket.Ticket, "", nil)
	if scan.Code != http.StatusOK || !strings.Contains(scan.Body.String(), "scanned") {
		t.Fatalf("scan status=%d body=%s", scan.Code, scan.Body.String())
	}
	if response := perform(t, router, http.MethodPost, "/api/v1/merchant/qr/confirm", "", map[string]string{"ticket": ticket.Ticket}); response.Code != http.StatusUnauthorized {
		t.Fatalf("confirm anonymous status=%d", response.Code)
	}
	if response := perform(t, router, http.MethodPost, "/api/v1/merchant/qr/confirm", fixture.adminToken(t), map[string]string{"ticket": ticket.Ticket}); response.Code != http.StatusForbidden {
		t.Fatalf("confirm admin status=%d", response.Code)
	}
	confirm := perform(t, router, http.MethodPost, "/api/v1/merchant/qr/confirm", merchantToken, map[string]string{"ticket": ticket.Ticket})
	if confirm.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", confirm.Code, confirm.Body.String())
	}
	confirmed := perform(t, router, http.MethodGet, "/api/v1/auth/qr/poll?ticket="+ticket.Ticket, "", nil)
	if confirmed.Code != http.StatusOK || !strings.Contains(confirmed.Body.String(), `"status":"confirmed"`) || !strings.Contains(confirmed.Body.String(), `"token"`) {
		t.Fatalf("confirmed poll status=%d body=%s", confirmed.Code, confirmed.Body.String())
	}
}

func stringPointer(value string) *string { return &value }
