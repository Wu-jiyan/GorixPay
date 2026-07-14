package httpapi

import (
	"context"
	"encoding/json"
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

func TestMerchantLoginMeAndLogout(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:http-auth?mode=memory&cache=shared"})
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
	email := "merchant@example.test"
	merchant := model.Merchant{ID: "merchant-1", Email: &email, PasswordHash: &hash, DisplayName: "merchant", Status: model.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	sessions := session.NewService(db, time.Now)
	router := gin.New()
	Register(router.Group("/api/v1"), auth.NewService(db, sessions), sessions)

	login := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/merchant/login", strings.NewReader(`{"identifier":"merchant@example.test","password":"SecretPass1"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(login, request)
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	var loginBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &loginBody); err != nil || loginBody.Token == "" {
		t.Fatalf("login response: %s err=%v", login.Body.String(), err)
	}

	me := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.Header.Set("Authorization", "Bearer "+loginBody.Token)
	router.ServeHTTP(me, request)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"principal_id":"merchant-1"`) {
		t.Fatalf("me: %d %s", me.Code, me.Body.String())
	}

	logout := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.Header.Set("Authorization", "Bearer "+loginBody.Token)
	router.ServeHTTP(logout, request)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", logout.Code, logout.Body.String())
	}

	me = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.Header.Set("Authorization", "Bearer "+loginBody.Token)
	router.ServeHTTP(me, request)
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d", me.Code)
	}
}

func TestAuthenticationRejectsMissingAndCookieToken(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:http-auth-cookie?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Merchant{}, &model.Session{}); err != nil {
		t.Fatal(err)
	}
	sessions := session.NewService(db, time.Now)
	router := gin.New()
	Register(router.Group("/api/v1"), auth.NewService(db, sessions), sessions)
	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d", missing.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "unknown"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("cookie token status = %d", response.Code)
	}
}
