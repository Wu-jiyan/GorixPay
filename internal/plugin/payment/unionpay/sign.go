package unionpay

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
)

// canonical renders the UnionPay signature string: parameters sorted by name,
// joined as "k=v&", with a trailing '&' (the format the 5.1.0 SDK produces).
// The sign field itself and empty values are skipped.
func canonical(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for name, value := range values {
		if name == "sign" || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, name := range keys {
		builder.WriteString(name)
		builder.WriteString("=")
		builder.WriteString(values[name])
		builder.WriteString("&")
	}
	return builder.String()
}

// signValues produces the base64 SHA256withRSA signature of the canonical string.
func signValues(privateKey *rsa.PrivateKey, values map[string]string) (string, error) {
	if privateKey == nil {
		return "", errors.New("missing unionpay private key")
	}
	digest := sha256.Sum256([]byte(canonical(values)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

// verifyValues checks values["sign"] against the UnionPay public key.
func verifyValues(publicKey *rsa.PublicKey, values map[string]string) bool {
	if publicKey == nil {
		return false
	}
	signature := strings.TrimSpace(values["sign"])
	if signature == "" {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	digest := sha256.Sum256([]byte(canonical(values)))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], decoded) == nil
}
