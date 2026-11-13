package v2

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/compat/epay/signing"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"
)

func TestCanonicalDropsSignatureNoiseAndSortsKeys(t *testing.T) {
	values := map[string]string{
		"z": "last", "sign": "ignored", "sign_type": "RSA", "empty": "", "spaces": "   ",
		"A": "upper", "a": "lower", "amount": "1.00", "zero": "0", "space": " x ",
	}
	want := "A=upper&a=lower&amount=1.00&space= x &z=last&zero=0"
	if got := Canonical(values); got != want {
		t.Fatalf("canonical = %q, want %q", got, want)
	}
}

func TestMD5MatchesReferenceVector(t *testing.T) {
	// Reference vector from Epay/includes/lib/Payment.php: md5(signContent + key)
	// with an empty sign content and merchant key "123".
	if got := SignMD5(map[string]string{"sign": "ignored", "empty": "", "blank": "  "}, "123"); got != "202cb962ac59075b964b07152d234b70" {
		t.Fatalf("md5 = %q", got)
	}
	digest := md5.Sum([]byte(Canonical(map[string]string{"a": "1", "b": "2"}) + "key"))
	if got, want := SignMD5(map[string]string{"a": "1", "b": "2"}, "key"), hex.EncodeToString(digest[:]); got != want {
		t.Fatalf("md5 = %q, want %q", got, want)
	}
	if !VerifyMD5(map[string]string{"a": "1", "b": "2"}, "key", strings.ToUpper(hex.EncodeToString(digest[:]))) {
		t.Fatal("uppercase md5 signature should verify")
	}
	if VerifyMD5(map[string]string{"a": "1", "b": "3"}, "key", hex.EncodeToString(digest[:])) {
		t.Fatal("tampered value verified")
	}
}

func TestRSASignAndVerifyRoundTrip(t *testing.T) {
	key, err := signing.GenerateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"pid": "1001", "money": "0.01", "timestamp": "1700000000", "sign_type": "RSA"}
	signature, err := SignValuesRSA(values, key)
	if err != nil || signature == "" {
		t.Fatalf("sign: %q err=%v", signature, err)
	}
	if !VerifyValuesRSA(values, &key.PublicKey, signature) {
		t.Fatal("signature did not verify")
	}
	tampered := map[string]string{"pid": "1001", "money": "0.02", "timestamp": "1700000000", "sign_type": "RSA"}
	if VerifyValuesRSA(tampered, &key.PublicKey, signature) {
		t.Fatal("tampered values verified")
	}
}

func TestLoadSignerPersistsEncryptedPlatformKeyOnce(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:signer-load?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&PlatformSetting{}); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x37}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	first, err := LoadSigner(context.Background(), db, cipher)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadSigner(context.Background(), db, cipher)
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicKeyPEM() == "" || first.PublicKeyPEM() != second.PublicKeyPEM() {
		t.Fatal("platform key was not reused")
	}
	publicKey, err := signing.ParseRSAPublicKeyPEM(first.PublicKeyPEM())
	if err != nil || publicKey.N.BitLen() != 2048 {
		t.Fatalf("public key: %v bits err=%v", publicKey.N.BitLen(), err)
	}
	stored, found, err := readSetting(context.Background(), db, SettingPlatformPrivateKey)
	if err != nil || !found {
		t.Fatalf("stored key: found=%v err=%v", found, err)
	}
	if !strings.HasPrefix(stored, "v1.") || strings.Contains(stored, "PRIVATE KEY") {
		t.Fatalf("private key stored in plaintext: %q", stored)
	}
}

func TestSignerSignsMixedPayloadAndSkipsArrays(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:signer-payload?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&PlatformSetting{}); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x11}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := LoadSigner(context.Background(), db, cipher)
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"code": 0, "money": "1.00", "items": []string{"ignored"}, "empty": "", "sign_type": "RSA"}
	signature, err := signer.SignParams(payload)
	if err != nil || signature == "" {
		t.Fatalf("sign: %q err=%v", signature, err)
	}
	publicKey, err := signing.ParseRSAPublicKeyPEM(signer.PublicKeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	if !signing.VerifyRSA(payload, publicKey, signature) {
		t.Fatal("payload signature did not verify")
	}
	payload["money"] = "2.00"
	if signing.VerifyRSA(payload, publicKey, signature) {
		t.Fatal("tampered payload verified")
	}
}

func TestParseAmountRejectsZeroNegativeAndPrecision(t *testing.T) {
	valid := map[string]int64{"0.01": 1, "1": 100, "1.5": 150, "1.50": 150, "100000.00": MaxAmountCents}
	for input, want := range valid {
		got, err := ParseAmount(input)
		if err != nil || got != want {
			t.Fatalf("%q => %d err=%v", input, got, err)
		}
	}
	for _, input := range []string{"", "0", "0.00", "-1", "0.001", "1.001", "1e3", "100000.01", "abc", "1,00"} {
		if _, err := ParseAmount(input); err == nil {
			t.Fatalf("invalid amount accepted: %q", input)
		}
	}
	if got := FormatAmount(1); got != "0.01" {
		t.Fatalf("format = %q", got)
	}
}

func TestReplayStoreAcceptsOneSignature(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:replay-once?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&RequestNonce{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	store := NewDBReplayStore(db, func() time.Time { return now })
	key := ReplayKey("merchant-1", "pay/close", "signature-value")
	accepted, err := store.Once(context.Background(), key, NonceTTL)
	if err != nil || !accepted {
		t.Fatalf("first once: accepted=%v err=%v", accepted, err)
	}
	accepted, err = store.Once(context.Background(), key, NonceTTL)
	if err != nil || accepted {
		t.Fatalf("second once: accepted=%v err=%v", accepted, err)
	}
}
