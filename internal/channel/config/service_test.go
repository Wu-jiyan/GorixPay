package config

import (
	"strings"
	"testing"

	"gorixpay/internal/platform/secret"
)

func TestServiceEncryptsAndDecryptsChannelBoundConfig(t *testing.T) {
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(cipher)
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
	if got != want {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
	if _, err := service.Decrypt("channel-2", encrypted); err == nil {
		t.Fatal("config decrypted for another channel")
	}
}

func TestServiceRejectsInvalidConfig(t *testing.T) {
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(cipher)
	for name, candidate := range map[string]Config{
		"relative-url": {APIURL: "/mapi.php", PID: "1001", Key: "secret"},
		"url-userinfo": {APIURL: "https://user:pass@pay.example.com", PID: "1001", Key: "secret"},
		"missing-pid":  {APIURL: "https://pay.example.com", Key: "secret"},
		"missing-key":  {APIURL: "https://pay.example.com", PID: "1001"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Encrypt("channel-1", candidate); err == nil {
				t.Fatalf("accepted config=%+v", candidate)
			}
		})
	}
}
