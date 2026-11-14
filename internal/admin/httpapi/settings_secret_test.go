package httpapi

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
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"
	"gorixpay/internal/settings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// settingsRouter mounts only the settings endpoints with a real administrator
// session, so the handler runs with the cipher the wiring layer gives it.
func settingsRouter(t *testing.T, name string, cipher *secret.Cipher) (http.Handler, *gorm.DB, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Administrator{}, &identitymodel.Session{}, &systemSetting{}, &auditEvent{}); err != nil {
		t.Fatal(err)
	}
	sessions := session.NewService(db, nil)
	token, _, err := sessions.Create(context.Background(), identitymodel.PrincipalAdministrator, "admin-1", time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	options := []Option{}
	if cipher != nil {
		options = append(options, WithCipher(cipher))
	}
	handler := &Handler{db: db}
	for _, option := range options {
		option(handler)
	}
	admin := router.Group("/api/v1/admin", identityhttp.Authenticate(sessions), identityhttp.RequireAdministrator())
	admin.GET("/settings", handler.settings)
	admin.PUT("/settings/:key", handler.updateSetting)
	return router, db, token
}

func performSetting(t *testing.T, router http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// TestSecretSettingsAreEncryptedAndNeverEchoed pins the credential handling: the
// admin API stores the value encrypted, the list endpoint reports only that it is
// configured, and an empty write keeps the stored credential.
func TestSecretSettingsAreEncryptedAndNeverEchoed(t *testing.T) {
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x27}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	router, db, token := settingsRouter(t, "admin-settings-secret", cipher)

	const plaintext = "wechat-app-secret-value"
	response := performSetting(t, router, http.MethodPut, "/api/v1/admin/settings/oauth.wechat.app_secret", token,
		`{"value":"`+plaintext+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("write status=%d body=%s", response.Code, response.Body.String())
	}
	var stored systemSetting
	if err := db.First(&stored, "setting_key = ?", "oauth.wechat.app_secret").Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.Value, plaintext) {
		t.Fatalf("stored value contains the plaintext: %s", stored.Value)
	}
	if !strings.Contains(stored.Value, "v1.") {
		t.Fatalf("stored value is not ciphertext: %s", stored.Value)
	}
	decrypted, err := settings.Decrypt(cipher, "oauth.wechat.app_secret", stored.Value)
	if err != nil || decrypted != plaintext {
		t.Fatalf("decrypt = (%q, %v), want the plaintext", decrypted, err)
	}

	// The list endpoint must not leak the value.
	list := performSetting(t, router, http.MethodGet, "/api/v1/admin/settings", token, "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d", list.Code)
	}
	if strings.Contains(list.Body.String(), plaintext) || strings.Contains(list.Body.String(), stored.Value) {
		t.Fatalf("list leaked the credential: %s", list.Body.String())
	}
	var body struct {
		Items []struct {
			Key        string `json:"key"`
			Secret     bool   `json:"secret"`
			Configured bool   `json:"configured"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range body.Items {
		if item.Key == "oauth.wechat.app_secret" {
			found = true
			if !item.Secret || !item.Configured {
				t.Fatalf("secret item = %+v", item)
			}
		}
	}
	if !found {
		t.Fatal("secret setting missing from the list")
	}

	// An empty value means "keep the stored credential", not "erase it".
	empty := performSetting(t, router, http.MethodPut, "/api/v1/admin/settings/oauth.wechat.app_secret", token, `{"value":""}`)
	if empty.Code != http.StatusOK {
		t.Fatalf("empty write status=%d body=%s", empty.Code, empty.Body.String())
	}
	var after systemSetting
	if err := db.First(&after, "setting_key = ?", "oauth.wechat.app_secret").Error; err != nil {
		t.Fatal(err)
	}
	if after.Value != stored.Value {
		t.Fatal("an empty write replaced the stored credential")
	}
}

// TestSecretSettingRequiresCipher keeps a deployment without a master key honest:
// storing a credential in the clear is refused instead of silently allowed.
func TestSecretSettingRequiresCipher(t *testing.T) {
	router, db, token := settingsRouter(t, "admin-settings-nocipher", nil)
	response := performSetting(t, router, http.MethodPut, "/api/v1/admin/settings/notify.smtp_password", token, `{"value":"hunter2"}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "cipher_missing") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var count int64
	if err := db.Model(&systemSetting{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("settings written without a cipher: count=%d err=%v", count, err)
	}
}

// TestPlainSettingStillRoundTripsAsJSON covers the ordinary, non-secret path.
func TestPlainSettingStillRoundTripsAsJSON(t *testing.T) {
	router, _, token := settingsRouter(t, "admin-settings-plain", nil)
	response := performSetting(t, router, http.MethodPut, "/api/v1/admin/settings/site.name", token, `{"value":"我的支付站"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("write status=%d body=%s", response.Code, response.Body.String())
	}
	list := performSetting(t, router, http.MethodGet, "/api/v1/admin/settings", token, "")
	if !strings.Contains(list.Body.String(), "我的支付站") {
		t.Fatalf("list = %s", list.Body.String())
	}
}
