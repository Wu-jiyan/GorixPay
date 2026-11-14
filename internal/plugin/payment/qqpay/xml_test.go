package qqpay

import (
	"strings"
	"testing"
)

// verifySignature is constant time and case insensitive: a lower-case or padded
// signature from the gateway verifies, a truncated one does not.
func TestVerifySignatureNormalizesCase(t *testing.T) {
	values := map[string]string{"appid": "wx-app", "mch_id": "M1", "out_trade_no": "T1", "total_fee": "100"}
	expected := signParams(values, "qq-key")
	signed := func(signature string) map[string]string {
		copied := make(map[string]string, len(values)+1)
		for name, value := range values {
			copied[name] = value
		}
		copied["sign"] = signature
		return copied
	}
	if !verifySignature(signed(expected), "qq-key") {
		t.Fatal("canonical signature rejected")
	}
	if !verifySignature(signed(strings.ToLower(expected)), "qq-key") {
		t.Fatal("lower-case signature rejected")
	}
	if !verifySignature(signed(" "+expected+" "), "qq-key") {
		t.Fatal("padded signature rejected")
	}
	if verifySignature(signed(expected[:len(expected)-1]), "qq-key") {
		t.Fatal("truncated signature accepted")
	}
	if verifySignature(signed(""), "qq-key") {
		t.Fatal("empty signature accepted")
	}
	if verifySignature(signed(expected), "other-key") {
		t.Fatal("signature verified with the wrong key")
	}
}
