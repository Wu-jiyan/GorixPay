package auth

import (
	"bytes"
	"context"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	epaysigning "gorixpay/internal/compat/epay/signing"
	"gorixpay/internal/identity/model"
	"gorixpay/internal/merchant/credential"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestMiddlewareAcceptsOptionalSignTypeAndRSA(t *testing.T) {
	db, credentials, key := middlewareFixture(t, "epay-auth-signtype")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/mapi.php", Middleware(db, credentials), func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"merchant_id": MustMerchant(ctx).ID})
	})

	base := url.Values{"pid": {"1001"}, "out_trade_no": {"T1"}, "money": {"1.00"}}
	params := map[string]any{"pid": "1001", "out_trade_no": "T1", "money": "1.00"}

	// sign_type omitted defaults to MD5.
	unsigned := cloneValues(base)
	unsigned.Set("sign", epaysigning.SignMD5(params, key))
	if response := performPost(router, unsigned); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "merchant-1") {
		t.Fatalf("default sign type: %d %s", response.Code, response.Body.String())
	}

	// RSA uses the merchant public key.
	resolved, err := credentials.ResolveRSA(context.Background(), "merchant-1")
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := epaysigning.ParseRSAPublicKeyPEM(resolved.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	rsaParams := map[string]any{"pid": "1001", "out_trade_no": "T1", "money": "1.00", "sign_type": "RSA"}
	signature, err := epaysigning.SignRSA(rsaParams, testPrivateKey(t, credentials))
	if err != nil {
		t.Fatal(err)
	}
	if !epaysigning.VerifyRSA(rsaParams, publicKey, signature) {
		t.Fatal("fixture signature does not verify")
	}
	rsaForm := cloneValues(base)
	rsaForm.Set("sign_type", "RSA")
	rsaForm.Set("sign", signature)
	if response := performPost(router, rsaForm); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "merchant-1") {
		t.Fatalf("rsa sign type: %d %s", response.Code, response.Body.String())
	}

	// Tampered RSA payload is rejected.
	tampered := cloneValues(rsaForm)
	tampered.Set("money", "2.00")
	if response := performPost(router, tampered); response.Code != http.StatusOK {
		t.Fatalf("tampered rsa: %d %s", response.Code, response.Body.String())
	}

	// Unknown sign types are rejected.
	unknown := cloneValues(base)
	unknown.Set("sign_type", "ECC")
	unknown.Set("sign", "whatever")
	if response := performPost(router, unknown); response.Code != http.StatusOK {
		t.Fatalf("unknown sign type: %d %s", response.Code, response.Body.String())
	}
}

func middlewareFixture(t *testing.T, name string) (db *gorm.DB, credentials *credential.Service, key string) {
	t.Helper()
	handle, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.AutoMigrate(&model.Merchant{}, &model.MerchantCredential{}); err != nil {
		t.Fatal(err)
	}
	uid := int64(1001)
	merchant := model.Merchant{ID: "merchant-1", LegacyUID: &uid, DisplayName: "merchant", Status: model.StatusActive}
	if err := handle.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x19}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	credentials = credential.NewService(handle, cipher, time.Now)
	_, key, err = credentials.CreateMD5(context.Background(), merchant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := credentials.CreateRSA(context.Background(), merchant.ID); err != nil {
		t.Fatal(err)
	}
	return handle, credentials, key
}

func testPrivateKey(t *testing.T, credentials *credential.Service) *rsa.PrivateKey {
	t.Helper()
	resolved, err := credentials.ResolveRSA(context.Background(), "merchant-1")
	if err != nil {
		t.Fatal(err)
	}
	key, err := epaysigning.ParseRSAPrivateKeyPEM(resolved.Secret)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func cloneValues(source url.Values) url.Values {
	clone := url.Values{}
	for name, values := range source {
		for _, value := range values {
			clone.Add(name, value)
		}
	}
	return clone
}

func performPost(router http.Handler, form url.Values) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mapi.php", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(response, request)
	return response
}
