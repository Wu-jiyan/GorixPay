package security

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// rfcSecret is the ASCII secret from RFC 6238 appendix B, base32 encoded.
func rfcSecret() string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
}

func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	secret := rfcSecret()
	vectors := []struct {
		unix int64
		code string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, vector := range vectors {
		code, err := TOTPCode(secret, time.Unix(vector.unix, 0).UTC())
		if err != nil {
			t.Fatalf("unix=%d err=%v", vector.unix, err)
		}
		if code != vector.code {
			t.Fatalf("unix=%d code=%q want %q", vector.unix, code, vector.code)
		}
	}
}

func TestTOTPWindowTolerance(t *testing.T) {
	secret := rfcSecret()
	code, err := TOTPCode(secret, time.Unix(59, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	// The code belongs to the step starting at 30; verification accepts one step
	// either side, so 0 (one step ahead) passes and -1 (two steps behind) does not.
	cases := map[int64]bool{
		59:    true,
		89:    true,
		29:    true,
		0:     true,
		119:   false,
		-31:   false,
		60000: false,
	}
	for unix, expected := range cases {
		if accepted := VerifyTOTPCode(secret, code, time.Unix(unix, 0).UTC()); accepted != expected {
			t.Fatalf("unix=%d accepted=%v want %v", unix, accepted, expected)
		}
	}
}

func TestTOTPRejectsMalformedInput(t *testing.T) {
	secret := rfcSecret()
	if VerifyTOTPCode(secret, "", time.Now()) {
		t.Fatal("empty code accepted")
	}
	if VerifyTOTPCode(secret, "12345", time.Now()) {
		t.Fatal("short code accepted")
	}
	if VerifyTOTPCode(secret, "12345a", time.Now()) {
		t.Fatal("non numeric code accepted")
	}
	if VerifyTOTPCode("not-base32!!", "123456", time.Now()) {
		t.Fatal("invalid secret accepted")
	}
	if _, err := TOTPCode("not-base32!!", time.Now()); err == nil {
		t.Fatal("invalid secret produced a code")
	}
}

func TestGenerateTOTPSecretShape(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 32 {
		t.Fatalf("secret=%q length=%d want 32", secret, len(secret))
	}
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(raw) != 20 {
		t.Fatalf("decoded length=%d err=%v", len(raw), err)
	}
	other, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if other == secret {
		t.Fatal("two generated secrets are identical")
	}
}

func TestTOTPAuthURLCarriesParameters(t *testing.T) {
	uri := TOTPAuthURL("GorixPay", "admin", rfcSecret())
	for _, fragment := range []string{
		"otpauth://totp/", "secret=" + rfcSecret(), "issuer=GorixPay",
		"algorithm=SHA1", "digits=6", "period=30",
	} {
		if !strings.Contains(uri, fragment) {
			t.Fatalf("uri %q missing %q", uri, fragment)
		}
	}
}
