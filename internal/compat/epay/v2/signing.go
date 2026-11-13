package v2

import (
	"crypto/rsa"

	"gorixpay/internal/compat/epay/signing"
)

// Canonical renders the epay V2 sign content: drop sign/sign_type and empty
// values, sort the remaining keys in ASCII order and join them as k=v&k=v.
func Canonical(values map[string]string) string {
	return signing.Canonical(toStringMap(values))
}

// SignMD5 produces md5(canonical + key) in lowercase hex.
func SignMD5(values map[string]string, key string) string {
	return signing.SignMD5(toStringMap(values), key)
}

// VerifyMD5 checks a lowercase MD5 signature in constant time.
func VerifyMD5(values map[string]string, key, signature string) bool {
	return signing.VerifyMD5(toStringMap(values), key, signature)
}

// SignValuesRSA signs the canonical content with SHA256withRSA and base64 encodes it.
func SignValuesRSA(values map[string]string, key *rsa.PrivateKey) (string, error) {
	return signing.SignRSA(toStringMap(values), key)
}

// VerifyValuesRSA verifies a base64 SHA256withRSA signature over the canonical content.
func VerifyValuesRSA(values map[string]string, key *rsa.PublicKey, signature string) bool {
	return signing.VerifyRSA(toStringMap(values), key, signature)
}

func toStringMap(values map[string]string) map[string]any {
	converted := make(map[string]any, len(values))
	for name, value := range values {
		converted[name] = value
	}
	return converted
}
