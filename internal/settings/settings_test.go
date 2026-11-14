package settings

import (
	"bytes"
	"errors"
	"testing"

	"gorixpay/internal/platform/secret"
)

func testCipher(t *testing.T) *secret.Cipher {
	t.Helper()
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x51}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func TestTextUnwrapsJSONStringsAndKeepsRawValues(t *testing.T) {
	cases := map[string]string{
		`"smtp.example.com"`: "smtp.example.com",
		`smtp.example.com`:   "smtp.example.com",
		`"  127.0.0.1  "`:    "127.0.0.1",
		`""`:                 "",
		`   `:                "",
		`"v1.abc"`:           "v1.abc",
	}
	for raw, want := range cases {
		if got := Text(raw); got != want {
			t.Fatalf("Text(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestSecretRecognisesCredentialKeys(t *testing.T) {
	secretKeys := []string{
		"oauth.wechat.app_secret", "notify.smtp_password", "notify.sms_token",
		"OAuth.QQ.App_Secret",
	}
	for _, key := range secretKeys {
		if !Secret(key) {
			t.Fatalf("Secret(%q) = false, want true", key)
		}
	}
	plainKeys := []string{"site.name", "oauth.wechat.app_id", "notify.smtp_host", "notify.smtp_from"}
	for _, key := range plainKeys {
		if Secret(key) {
			t.Fatalf("Secret(%q) = true, want false", key)
		}
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	cipher := testCipher(t)
	const key = "oauth.wechat.app_secret"
	encrypted, err := Encrypt(cipher, key, "plain-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if !IsCiphertext(encrypted) {
		t.Fatalf("ciphertext shape = %q", encrypted)
	}
	if bytes.Contains([]byte(encrypted), []byte("plain-secret-value")) {
		t.Fatal("ciphertext contains the plaintext")
	}
	// The stored form is JSON wrapped, exactly like the admin API writes it.
	stored := `"` + encrypted + `"`
	plain, err := Decrypt(cipher, key, stored)
	if err != nil || plain != "plain-secret-value" {
		t.Fatalf("Decrypt = (%q, %v)", plain, err)
	}
}

// TestDecryptRejectsCrossKeyReuse pins the additional-data binding: a ciphertext
// moved to another setting must not silently decrypt.
func TestDecryptRejectsCrossKeyReuse(t *testing.T) {
	cipher := testCipher(t)
	encrypted, err := Encrypt(cipher, "oauth.wechat.app_secret", "wechat-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(cipher, "notify.smtp_password", `"`+encrypted+`"`); !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("cross-key decrypt err = %v, want ErrNotEncrypted", err)
	}
}

func TestDecryptAcceptsLegacyPlaintext(t *testing.T) {
	cipher := testCipher(t)
	for _, stored := range []string{`"smtp.example.com"`, "smtp.example.com", `""`} {
		got, err := Decrypt(cipher, "notify.smtp_host", stored)
		if err != nil {
			t.Fatalf("Decrypt(%q) err = %v", stored, err)
		}
		if stored == `""` && got != "" {
			t.Fatalf("Decrypt(%q) = %q, want empty", stored, got)
		}
	}
}

func TestDecryptNeedsCipherForCiphertextAndReportsBadVersion(t *testing.T) {
	cipher := testCipher(t)
	encrypted, err := Encrypt(cipher, "notify.sms_token", "token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(nil, "notify.sms_token", encrypted); err == nil {
		t.Fatal("ciphertext accepted without a cipher")
	}
	if _, err := Decrypt(cipher, "notify.sms_token", "v9.bm90LXJlYWw"); !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("unknown version err = %v, want ErrNotEncrypted", err)
	}
}

func TestIsCiphertextShape(t *testing.T) {
	cases := map[string]bool{
		"v1.AAAA":            true,
		"v12.xyz-_":          true,
		"smtp.example.com":   false,
		"127.0.0.1":          false,
		"v.abc":              false,
		"v1":                 false,
		"v1.":                false,
		"":                   false,
		"va.bc":              false,
		"v1Xabc":             false,
		"somethingelse.here": false,
	}
	for value, want := range cases {
		if got := IsCiphertext(value); got != want {
			t.Fatalf("IsCiphertext(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestEncryptEmptyValueStaysEmpty(t *testing.T) {
	cipher := testCipher(t)
	encrypted, err := Encrypt(cipher, "notify.smtp_password", "   ")
	if err != nil || encrypted != "" {
		t.Fatalf("Encrypt(empty) = (%q, %v), want empty", encrypted, err)
	}
}
