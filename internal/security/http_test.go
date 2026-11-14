package security

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
)

func newHTTPRouter(t *testing.T, fixture *fixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	identityhttp.Register(router.Group("/api/v1"), newAuthService(fixture), fixture.sessions)
	Register(router.Group("/api/v1"), identityhttp.Authenticate(fixture.sessions), fixture.service)
	return router
}

func (fixture *fixture) adminToken(t *testing.T) string {
	t.Helper()
	token, _, err := fixture.sessions.Create(context.Background(), identitymodel.PrincipalAdministrator, testAdministratorID, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (fixture *fixture) merchantToken(t *testing.T) string {
	t.Helper()
	token, _, err := fixture.sessions.Create(context.Background(), identitymodel.PrincipalMerchant, "merchant-1", time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func request(t *testing.T, router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
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
	httpRequest := httptest.NewRequest(method, path, reader)
	httpRequest.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(response, httpRequest)
	return response
}

func TestCaptchaEndpointReturnsSVGWithoutAnswer(t *testing.T) {
	fixture := newFixture(t, "security-http-captcha")
	router := newHTTPRouter(t, fixture)
	response := request(t, router, http.MethodGet, "/api/v1/auth/captcha", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	id, _ := body["id"].(string)
	svg, _ := body["image_svg"].(string)
	if id == "" || !strings.HasPrefix(svg, "<svg") {
		t.Fatalf("body=%+v", body)
	}
	if _, leaked := body["code"]; leaked {
		t.Fatalf("captcha answer leaked: %+v", body)
	}
	if response.Header().Get("X-Captcha-Id") != id {
		t.Fatalf("x-captcha-id=%q", response.Header().Get("X-Captcha-Id"))
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control=%q", response.Header().Get("Cache-Control"))
	}
}

func TestSecurityRoutesRequireAdministrator(t *testing.T) {
	fixture := newFixture(t, "security-http-auth")
	router := newHTTPRouter(t, fixture)
	merchantToken := fixture.merchantToken(t)
	for _, path := range []string{"/api/v1/admin/security/totp", "/api/v1/admin/security/login-attempts"} {
		if response := request(t, router, http.MethodGet, path, "", nil); response.Code != http.StatusUnauthorized {
			t.Fatalf("%s anonymous status=%d", path, response.Code)
		}
		if response := request(t, router, http.MethodGet, path, merchantToken, nil); response.Code != http.StatusForbidden {
			t.Fatalf("%s merchant status=%d", path, response.Code)
		}
	}
}

func TestTOTPHTTPFlow(t *testing.T) {
	fixture := newFixture(t, "security-http-totp")
	router := newHTTPRouter(t, fixture)
	token := fixture.adminToken(t)

	status := request(t, router, http.MethodGet, "/api/v1/admin/security/totp", token, nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"enabled":false`) {
		t.Fatalf("status=%d body=%s", status.Code, status.Body.String())
	}
	setup := request(t, router, http.MethodPost, "/api/v1/admin/security/totp/setup", token, nil)
	if setup.Code != http.StatusOK {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}
	var setupBody struct {
		Secret     string `json:"secret"`
		OtpauthURL string `json:"otpauth_url"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &setupBody); err != nil {
		t.Fatal(err)
	}
	if len(setupBody.Secret) != 32 || !strings.HasPrefix(setupBody.OtpauthURL, "otpauth://totp/") {
		t.Fatalf("setup=%+v", setupBody)
	}
	code, err := TOTPCode(setupBody.Secret, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	wrong := request(t, router, http.MethodPost, "/api/v1/admin/security/totp/enable", token, map[string]string{"code": "000000"})
	if wrong.Code != http.StatusBadRequest || !strings.Contains(wrong.Body.String(), "totp_invalid") {
		t.Fatalf("wrong enable status=%d body=%s", wrong.Code, wrong.Body.String())
	}
	enable := request(t, router, http.MethodPost, "/api/v1/admin/security/totp/enable", token, map[string]string{"code": code})
	if enable.Code != http.StatusOK || !strings.Contains(enable.Body.String(), `"enabled":true`) {
		t.Fatalf("enable status=%d body=%s", enable.Code, enable.Body.String())
	}
	enableAgain := request(t, router, http.MethodPost, "/api/v1/admin/security/totp/setup", token, nil)
	if enableAgain.Code != http.StatusConflict || !strings.Contains(enableAgain.Body.String(), "totp_already_enabled") {
		t.Fatalf("re-setup status=%d body=%s", enableAgain.Code, enableAgain.Body.String())
	}
	disableWrong := request(t, router, http.MethodPost, "/api/v1/admin/security/totp/disable", token, map[string]string{"code": "000000"})
	if disableWrong.Code != http.StatusBadRequest {
		t.Fatalf("disable wrong status=%d", disableWrong.Code)
	}
	// The enrollment code is spent; disabling needs the next step's code.
	reused := request(t, router, http.MethodPost, "/api/v1/admin/security/totp/disable", token, map[string]string{"code": code})
	if reused.Code != http.StatusBadRequest || !strings.Contains(reused.Body.String(), "totp_reused") {
		t.Fatalf("disable with a spent code status=%d body=%s", reused.Code, reused.Body.String())
	}
	fixture.advance(30 * time.Second)
	disableCode, err := TOTPCode(setupBody.Secret, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	disable := request(t, router, http.MethodPost, "/api/v1/admin/security/totp/disable", token, map[string]string{"code": disableCode})
	if disable.Code != http.StatusOK || !strings.Contains(disable.Body.String(), `"enabled":false`) {
		t.Fatalf("disable status=%d body=%s", disable.Code, disable.Body.String())
	}
}

func TestLoginAttemptsEndpointFilters(t *testing.T) {
	fixture := newFixture(t, "security-http-attempts")
	router := newHTTPRouter(t, fixture)
	token := fixture.adminToken(t)
	ctx := context.Background()
	fixture.service.RecordLoginAttempt(ctx, loginPrincipalAdmin, "admin", "10.0.0.1", true)
	fixture.service.RecordLoginAttempt(ctx, loginPrincipalAdmin, "admin", "10.0.0.1", false)
	fixture.service.RecordLoginAttempt(ctx, loginPrincipalAdmin, "root", "10.0.0.2", false)

	all := request(t, router, http.MethodGet, "/api/v1/admin/security/login-attempts?page=1&page_size=2", token, nil)
	if all.Code != http.StatusOK || !strings.Contains(all.Body.String(), `"total":3`) {
		t.Fatalf("all status=%d body=%s", all.Code, all.Body.String())
	}
	failed := request(t, router, http.MethodGet, "/api/v1/admin/security/login-attempts?success=false&identifier=admin", token, nil)
	if failed.Code != http.StatusOK || !strings.Contains(failed.Body.String(), `"total":1`) {
		t.Fatalf("failed status=%d body=%s", failed.Code, failed.Body.String())
	}
	if bad := request(t, router, http.MethodGet, "/api/v1/admin/security/login-attempts?success=maybe", token, nil); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid filter status=%d", bad.Code)
	}
}

func TestLoginHTTPCodesForCaptchaAndTOTP(t *testing.T) {
	fixture := newFixture(t, "security-http-login")
	router := newHTTPRouter(t, fixture)
	fixture.setCaptchaRequired(t, true)

	missing := request(t, router, http.MethodPost, "/api/v1/auth/administrator/login", "", map[string]string{
		"identifier": testUsername, "password": testPassword,
	})
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "captcha_required") {
		t.Fatalf("missing captcha status=%d body=%s", missing.Code, missing.Body.String())
	}
	captcha, err := fixture.service.CreateCaptcha(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	withCaptcha := request(t, router, http.MethodPost, "/api/v1/auth/administrator/login", "", map[string]string{
		"identifier": testUsername, "password": testPassword,
		"captcha_id": captcha.ID, "captcha_code": captcha.Code,
	})
	if withCaptcha.Code != http.StatusOK || !strings.Contains(withCaptcha.Body.String(), "token") {
		t.Fatalf("valid captcha status=%d body=%s", withCaptcha.Code, withCaptcha.Body.String())
	}

	secretValue, _, err := fixture.service.SetupTOTP(context.Background(), testAdministratorID, testUsername)
	if err != nil {
		t.Fatal(err)
	}
	code, err := TOTPCode(secretValue, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.EnableTOTP(context.Background(), testAdministratorID, code); err != nil {
		t.Fatal(err)
	}
	fixture.setCaptchaRequired(t, false)

	missingTOTP := request(t, router, http.MethodPost, "/api/v1/auth/administrator/login", "", map[string]string{
		"identifier": testUsername, "password": testPassword,
	})
	if missingTOTP.Code != http.StatusUnauthorized || !strings.Contains(missingTOTP.Body.String(), "totp_required") {
		t.Fatalf("missing totp status=%d body=%s", missingTOTP.Code, missingTOTP.Body.String())
	}
	wrongTOTP := request(t, router, http.MethodPost, "/api/v1/auth/administrator/login", "", map[string]string{
		"identifier": testUsername, "password": testPassword, "totp_code": "000000",
	})
	if wrongTOTP.Code != http.StatusUnauthorized || !strings.Contains(wrongTOTP.Body.String(), "totp_invalid") {
		t.Fatalf("wrong totp status=%d body=%s", wrongTOTP.Code, wrongTOTP.Body.String())
	}
	validTOTP := request(t, router, http.MethodPost, "/api/v1/auth/administrator/login", "", map[string]string{
		"identifier": testUsername, "password": testPassword, "totp_code": code,
	})
	if validTOTP.Code != http.StatusUnauthorized || !strings.Contains(validTOTP.Body.String(), "totp_invalid") {
		t.Fatalf("spent totp status=%d body=%s, want totp_invalid", validTOTP.Code, validTOTP.Body.String())
	}
	// A replay of the enrollment code is refused above; the next step's code
	// logs in normally.
	fixture.advance(30 * time.Second)
	loginCode, err := TOTPCode(secretValue, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	validTOTP = request(t, router, http.MethodPost, "/api/v1/auth/administrator/login", "", map[string]string{
		"identifier": testUsername, "password": testPassword, "totp_code": loginCode,
	})
	if validTOTP.Code != http.StatusOK {
		t.Fatalf("valid totp status=%d body=%s", validTOTP.Code, validTOTP.Body.String())
	}
	if !strings.Contains(validTOTP.Body.String(), `"code"`) && !strings.Contains(validTOTP.Body.String(), "token") {
		t.Fatalf("unexpected login body=%s", validTOTP.Body.String())
	}
}
