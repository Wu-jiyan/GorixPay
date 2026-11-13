package v2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorixpay/internal/compat/epay/signing"
	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
)

func TestRegisterMerchantRoutesIssuesAndRotatesKeys(t *testing.T) {
	env := newTestEnv(t, "v2-merchant-credentials", nil)
	router := gin.New()
	group := router.Group("/api/v1/merchant", func(ctx *gin.Context) {
		ctx.Set("identity_principal", identitymodel.Session{PrincipalType: identitymodel.PrincipalMerchant, PrincipalID: "merchant-1"})
		ctx.Next()
	})
	RegisterMerchantRoutes(group, Deps{DB: env.db, Credentials: env.credentials, Cipher: env.cipher})

	response := performRequest(router, http.MethodGet, "/api/v1/merchant/credentials/v2")
	if response.Code != http.StatusOK {
		t.Fatalf("get: %d %s", response.Code, response.Body.String())
	}
	var current map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current["key_id"] == "" || current["public_key"] == "" || current["platform_public_key"] == "" {
		t.Fatalf("payload: %v", current)
	}
	if current["platform_public_key"] != env.platformPEM {
		t.Fatal("platform public key mismatch")
	}

	response = performRequest(router, http.MethodPost, "/api/v1/merchant/credentials/v2/rotate")
	if response.Code != http.StatusCreated {
		t.Fatalf("rotate: %d %s", response.Code, response.Body.String())
	}
	var rotated map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	privateKey, _ := rotated["private_key"].(string)
	if privateKey == "" || rotated["key_id"] == current["key_id"] {
		t.Fatalf("rotated: %v", rotated)
	}
	if _, err := signing.ParseRSAPrivateKeyPEM(privateKey); err != nil {
		t.Fatalf("private key: %v", err)
	}
	if _, err := signing.ParseRSAPublicKeyPEM(rotated["public_key"].(string)); err != nil {
		t.Fatalf("public key: %v", err)
	}
	response = performRequest(router, http.MethodGet, "/api/v1/merchant/credentials/v2")
	if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current["key_id"] != rotated["key_id"] {
		t.Fatalf("active key not rotated: %v", current)
	}
}

func performRequest(router http.Handler, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	return response
}
