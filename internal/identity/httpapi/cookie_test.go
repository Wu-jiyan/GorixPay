package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/identity/auth"
	"gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"github.com/gin-gonic/gin"
)

// TestSessionCookieSecureFollowsThePublicScheme pins the reverse proxy case: the
// inbound hop is plain HTTP, so only the configured public scheme can make the
// cookie Secure.
func TestSessionCookieSecureFollowsThePublicScheme(t *testing.T) {
	cases := []struct {
		name         string
		secureOption bool
		wantSecure   bool
	}{
		{name: "public-https", secureOption: true, wantSecure: true},
		{name: "public-http", secureOption: false, wantSecure: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			db, _, err := database.Open(context.Background(), config.DatabaseConfig{
				Driver: "sqlite", DSN: "file:http-cookie-" + testCase.name + "?mode=memory&cache=shared",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&model.Merchant{}, &model.Session{}); err != nil {
				t.Fatal(err)
			}
			hash, err := password.Hash("SecretPass1")
			if err != nil {
				t.Fatal(err)
			}
			email := "cookie@example.test"
			merchant := model.Merchant{ID: "merchant-cookie", Email: &email, PasswordHash: &hash, DisplayName: "merchant", Status: model.StatusActive}
			if err := db.Create(&merchant).Error; err != nil {
				t.Fatal(err)
			}
			sessions := session.NewService(db, time.Now)
			router := gin.New()
			Register(router.Group("/api/v1"), auth.NewService(db, sessions), sessions, WithSecureCookies(testCase.secureOption))

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/merchant/login",
				strings.NewReader(`{"identifier":"cookie@example.test","password":"SecretPass1"}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("login: %d %s", recorder.Code, recorder.Body.String())
			}
			header := recorder.Header().Get("Set-Cookie")
			if !strings.Contains(header, "HttpOnly") || !strings.Contains(header, "SameSite=Lax") || !strings.Contains(header, "Path=/") {
				t.Fatalf("session cookie flags missing: %q", header)
			}
			if gotSecure := strings.Contains(header, "Secure"); gotSecure != testCase.wantSecure {
				t.Fatalf("cookie Secure = %v, want %v (%q)", gotSecure, testCase.wantSecure, header)
			}
		})
	}
}
