package signing

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

func TestCanonicalAndMD5MatchEpayRules(t *testing.T) {
	params := map[string]any{
		"pid":           1001,
		"name":          "test",
		"money":         "1.00",
		"empty":         " ",
		"zero":          0,
		"false":         false,
		"sign":          "ignored",
		"sign_type":     "MD5",
		"response_list": []string{"ignored"},
	}
	canonical := Canonical(params)
	if canonical != "false=&money=1.00&name=test&pid=1001&zero=0" {
		t.Fatalf("canonical = %q", canonical)
	}
	signature := SignMD5(params, "test-key")
	if !VerifyMD5(params, "test-key", signature) || !VerifyMD5(params, "test-key", upper(signature)) {
		t.Fatal("MD5 verification failed")
	}
	if VerifyMD5(params, "wrong", signature) {
		t.Fatal("wrong MD5 key accepted")
	}
}

func TestRSASHA256SignAndVerify(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"pid": 1001, "out_trade_no": "T1", "money": "1.00"}
	signature, err := SignRSA(params, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyRSA(params, &privateKey.PublicKey, signature) {
		t.Fatal("RSA verification failed")
	}
	params["money"] = "2.00"
	if VerifyRSA(params, &privateKey.PublicKey, signature) {
		t.Fatal("modified request accepted")
	}
}

func upper(value string) string {
	bytes := []byte(value)
	for index, current := range bytes {
		if current >= 'a' && current <= 'f' {
			bytes[index] = current - 32
		}
	}
	return string(bytes)
}
