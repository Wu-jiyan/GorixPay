package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorixpay/internal/identity/httpapi"
	"gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/merchant/credential"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"

	"github.com/gin-gonic/gin"
)

func TestCredentialEndpointsCreateListAndRotateWithoutExposingStoredSecret(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:credential-http?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Merchant{}, &model.Session{}, &model.MerchantCredential{}); err != nil {
		t.Fatal(err)
	}
	merchant := model.Merchant{ID: "merchant-1", DisplayName: "merchant", Status: model.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	sessions := session.NewService(db, time.Now)
	token, _, err := sessions.Create(context.Background(), model.PrincipalMerchant, merchant.ID, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x42}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	credentials := credential.NewService(db, cipher, time.Now)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router.Group("/api/v1/merchant"), sessions, credentials)

	first := perform(t, router, http.MethodPost, "/api/v1/merchant/credentials/md5/rotate", token)
	if first.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	var firstBody map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil || firstBody["secret"] == "" {
		t.Fatalf("create response: %s err=%v", first.Body.String(), err)
	}

	listed := perform(t, router, http.MethodGet, "/api/v1/merchant/credentials", token)
	if listed.Code != http.StatusOK || bytes.Contains(listed.Body.Bytes(), []byte(firstBody["secret"].(string))) || bytes.Contains(listed.Body.Bytes(), []byte("secret_encrypted")) {
		t.Fatalf("unsafe list response: %d %s", listed.Code, listed.Body.String())
	}

	second := perform(t, router, http.MethodPost, "/api/v1/merchant/credentials/md5/rotate", token)
	if second.Code != http.StatusCreated || second.Body.String() == first.Body.String() {
		t.Fatalf("rotate: %d %s", second.Code, second.Body.String())
	}
}

func TestCredentialEndpointsRequireMerchantSession(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:credential-http-auth?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Session{}, &model.MerchantCredential{}); err != nil {
		t.Fatal(err)
	}
	sessions := session.NewService(db, time.Now)
	adminToken, _, err := sessions.Create(context.Background(), model.PrincipalAdministrator, "admin-1", time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x42}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	Register(router.Group("/api/v1/merchant"), sessions, credential.NewService(db, cipher, time.Now))
	for _, token := range []string{"", adminToken} {
		response := perform(t, router, http.MethodGet, "/api/v1/merchant/credentials", token)
		if response.Code != http.StatusUnauthorized && response.Code != http.StatusForbidden {
			t.Fatalf("token %q status = %d", token, response.Code)
		}
	}
}

func perform(t *testing.T, router http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(response, request)
	return response
}

var _ = httpapi.MustSession
