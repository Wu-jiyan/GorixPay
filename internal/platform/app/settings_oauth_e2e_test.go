package app

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
)

// TestAdminStoredOAuthCredentialReachesTheProvider covers the whole credential
// path end to end: the admin API encrypts the secret at rest, the settings list
// never returns it, and the OAuth authorise endpoint can build a provider from it.
// Before this fix the secret was stored in the clear and the read path expected
// ciphertext, so a configured provider answered "not configured" forever.
func TestAdminStoredOAuthCredentialReachesTheProvider(t *testing.T) {
	client, application := newE2EApplicationNamed(t, "app-settings-oauth")
	now := time.Now().UTC()
	hash, err := password.Hash("AdminPass12345")
	if err != nil {
		t.Fatal(err)
	}
	administrator := identitymodel.Administrator{
		ID: "00000000-0000-0000-0000-00000000oadm", Username: "oauthadmin", PasswordHash: hash,
		DisplayName: "Admin", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := application.DB().Create(&administrator).Error; err != nil {
		t.Fatal(err)
	}
	status, body := client.json(http.MethodPost, "/api/v1/auth/administrator/login", map[string]string{
		"identifier": "oauthadmin", "password": "AdminPass12345",
	})
	if status != http.StatusOK {
		t.Fatalf("admin login: %d %v", status, body)
	}
	client.token = jsonString(t, body["token"])

	const appID = "wx-e2e-app-id"
	const appSecret = "wx-e2e-app-secret"
	if status, body := client.json(http.MethodPut, "/api/v1/admin/settings/oauth.wechat.app_id", map[string]string{"value": appID}); status != http.StatusOK {
		t.Fatalf("store app_id: %d %v", status, body)
	}
	status, body = client.json(http.MethodPut, "/api/v1/admin/settings/oauth.wechat.app_secret", map[string]string{"value": appSecret})
	if status != http.StatusOK {
		t.Fatalf("store app_secret: %d %v", status, body)
	}
	if body["secret"] != true || body["configured"] != true {
		t.Fatalf("secret write body = %v", body)
	}

	// The stored value must be ciphertext, not the plaintext.
	var stored struct {
		Value string
	}
	if err := application.DB().Table("system_settings").
		Select("setting_value as value").
		Where("setting_key = ?", "oauth.wechat.app_secret").Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Value == "" || strings.Contains(stored.Value, appSecret) {
		t.Fatalf("stored secret = %q, want ciphertext", stored.Value)
	}

	// The list must not echo it back.
	status, listing := client.json(http.MethodGet, "/api/v1/admin/settings", nil)
	if status != http.StatusOK {
		t.Fatalf("list settings: %d", status)
	}
	encoded := strings.TrimSpace(strings.Join(stringSlice(listing["items"]), " "))
	if strings.Contains(encoded, appSecret) {
		t.Fatal("settings list leaked the credential")
	}

	// The authorise endpoint builds the provider from the stored credential: a 302
	// means credentials() decrypted it. Redirects are stopped so the test never
	// talks to the real open platform.
	httpClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       5 * time.Second,
	}
	response, err := httpClient.Get(client.base + "/api/v1/auth/oauth/wechat/authorize?redirect=/merchant")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want 302 (credential not usable)", response.StatusCode)
	}
	location := response.Header.Get("Location")
	if !strings.Contains(location, "open.weixin.qq.com") || !strings.Contains(location, "appid="+appID) {
		t.Fatalf("authorize location = %q", location)
	}

	// An empty write keeps the stored credential: the console never sees it, so a
	// blank form field cannot wipe a working provider.
	if status, body := client.json(http.MethodPut, "/api/v1/admin/settings/oauth.wechat.app_secret", map[string]string{"value": ""}); status != http.StatusOK || body["configured"] != true {
		t.Fatalf("empty write: %d %v", status, body)
	}
	retry, err := httpClient.Get(client.base + "/api/v1/auth/oauth/wechat/authorize?redirect=/merchant")
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Body.Close()
	_, _ = io.Copy(io.Discard, retry.Body)
	if retry.StatusCode != http.StatusFound {
		t.Fatalf("authorize after an empty write = %d, want 302", retry.StatusCode)
	}
}

// stringSlice renders whatever the JSON decoder produced for a list so the test
// can search it for a leaked value without depending on map key order.
func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, renderValue(item))
	}
	return out
}

func renderValue(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		builder := strings.Builder{}
		for key, inner := range typed {
			builder.WriteString(key)
			builder.WriteString("=")
			builder.WriteString(renderValue(inner))
			builder.WriteString(" ")
		}
		return builder.String()
	case string:
		return typed
	case nil:
		return ""
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return "value"
	}
}
