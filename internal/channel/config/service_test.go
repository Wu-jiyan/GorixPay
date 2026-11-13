package config

import (
	"reflect"
	"strings"
	"testing"

	"gorixpay/internal/platform/secret"
)

func testCipher(t *testing.T) *secret.Cipher {
	t.Helper()
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func TestServiceEncryptsAndDecryptsChannelBoundConfig(t *testing.T) {
	service := NewService(testCipher(t))
	want := Config{APIURL: "https://pay.example.com", PID: "1001", Key: "merchant-secret"}
	encrypted, err := service.Encrypt("channel-1", want)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, want.Key) {
		t.Fatal("encrypted config contains plaintext key")
	}
	got, err := service.Decrypt("channel-1", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
	if _, err := service.Decrypt("channel-2", encrypted); err == nil {
		t.Fatal("config decrypted for another channel")
	}
}

func TestServiceEncryptsExtraConfigurationForNonEpayPlugins(t *testing.T) {
	service := NewService(testCipher(t))
	want := Config{Extra: map[string]string{"app_id": " 2021000000 ", "private_key": "PEM", "blank": "  "}}
	encrypted, err := service.Encrypt("channel-alipay", want)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "PEM") {
		t.Fatal("encrypted config contains plaintext private key")
	}
	got, err := service.Decrypt("channel-alipay", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIURL != "" || got.PID != "" || got.Key != "" {
		t.Fatalf("legacy fields unexpectedly populated: %+v", got)
	}
	if got.Extra["app_id"] != "2021000000" || got.Extra["private_key"] != "PEM" {
		t.Fatalf("extra=%+v", got.Extra)
	}
	if _, ok := got.Extra["blank"]; ok {
		t.Fatalf("blank extra value kept: %+v", got.Extra)
	}
}

func TestConfigValuesFlattensAndPrefersExtra(t *testing.T) {
	config := Config{APIURL: "https://pay.example.com", PID: "1001", Key: "secret", Extra: map[string]string{"key": "override", "app_id": "42", "": "ignored"}}
	values := config.Values()
	if values["api_url"] != "https://pay.example.com" || values["pid"] != "1001" {
		t.Fatalf("values=%+v", values)
	}
	if values["key"] != "override" || values["app_id"] != "42" {
		t.Fatalf("values=%+v", values)
	}
	if _, exists := values[""]; exists {
		t.Fatalf("empty key kept: %+v", values)
	}
	if empty := (Config{}).Values(); len(empty) != 0 {
		t.Fatalf("empty config values=%+v", empty)
	}
}

func TestServiceRejectsInvalidConfig(t *testing.T) {
	service := NewService(testCipher(t))
	for name, candidate := range map[string]Config{
		"relative-url":      {APIURL: "/mapi.php", PID: "1001", Key: "secret"},
		"url-userinfo":      {APIURL: "https://user:pass@pay.example.com", PID: "1001", Key: "secret"},
		"missing-pid":       {APIURL: "https://pay.example.com", Key: "secret"},
		"missing-key":       {APIURL: "https://pay.example.com", PID: "1001"},
		"missing-api-url":   {PID: "1001", Key: "secret"},
		"nothing-at-all":    {},
		"blank-extra-only":  {Extra: map[string]string{"": "  "}},
		"ftp-url-in-legacy": {APIURL: "ftp://pay.example.com", PID: "1001", Key: "secret"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Encrypt("channel-1", candidate); err == nil {
				t.Fatalf("accepted config=%+v", candidate)
			}
		})
	}
	if _, err := service.Encrypt("", Config{APIURL: "https://pay.example.com", PID: "1001", Key: "secret"}); err == nil {
		t.Fatal("accepted empty channel id")
	}
}
