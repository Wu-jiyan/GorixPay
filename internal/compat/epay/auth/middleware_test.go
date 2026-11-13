package auth

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/compat/epay/signing"
	"gorixpay/internal/identity/model"
	"gorixpay/internal/merchant/credential"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"

	"github.com/gin-gonic/gin"
)

func TestMiddlewareAuthenticatesLegacyMD5Request(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:epay-auth?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Merchant{}, &model.MerchantCredential{}); err != nil {
		t.Fatal(err)
	}
	uid := int64(1001)
	merchant := model.Merchant{ID: "merchant-1", LegacyUID: &uid, DisplayName: "merchant", Status: model.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x42}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	credentials := credential.NewService(db, cipher, time.Now)
	_, key, err := credentials.CreateMD5(context.Background(), merchant.ID)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"pid": "1001", "out_trade_no": "T1", "money": "1.00"}
	form := url.Values{"pid": {"1001"}, "out_trade_no": {"T1"}, "money": {"1.00"}, "sign_type": {"MD5"}}
	form.Set("sign", signing.SignMD5(params, key))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/mapi.php", Middleware(db, credentials), func(ctx *gin.Context) {
		resolved := MustMerchant(ctx)
		ctx.JSON(http.StatusOK, gin.H{"merchant_id": resolved.ID})
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mapi.php", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "merchant-1") {
		t.Fatalf("response: %d %s", response.Code, response.Body.String())
	}
}

func TestMiddlewareRejectsInvalidSignatureAndDisabledMerchant(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:epay-auth-invalid?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Merchant{}, &model.MerchantCredential{}); err != nil {
		t.Fatal(err)
	}
	uid := int64(1001)
	merchant := model.Merchant{ID: "merchant-1", LegacyUID: &uid, DisplayName: "merchant", Status: model.StatusDisabled}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x42}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	credentials := credential.NewService(db, cipher, time.Now)
	if _, _, err := credentials.CreateMD5(context.Background(), merchant.ID); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/mapi.php", Middleware(db, credentials), func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
	for _, form := range []url.Values{
		{"pid": {"1001"}, "sign_type": {"MD5"}, "sign": {"invalid"}},
		{"pid": {"9999"}, "sign_type": {"MD5"}, "sign": {"invalid"}},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/mapi.php", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"code":-3`) {
			t.Fatalf("response: %d %s", response.Code, response.Body.String())
		}
	}
}
