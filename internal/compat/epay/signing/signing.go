package signing

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

func Canonical(params map[string]any) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if key == "sign" || key == "sign_type" || excludedValue(value) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+phpString(params[key]))
	}
	return strings.Join(parts, "&")
}

func SignMD5(params map[string]any, key string) string {
	sum := md5.Sum([]byte(Canonical(params) + key))
	return hex.EncodeToString(sum[:])
}

func VerifyMD5(params map[string]any, key, signature string) bool {
	expected := strings.ToLower(SignMD5(params, key))
	actual := strings.ToLower(signature)
	return len(expected) == len(actual) && subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func SignRSA(params map[string]any, privateKey *rsa.PrivateKey) (string, error) {
	digest := sha256.Sum256([]byte(Canonical(params)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func VerifyRSA(params map[string]any, publicKey *rsa.PublicKey, signature string) bool {
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	digest := sha256.Sum256([]byte(Canonical(params)))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], decoded) == nil
}

func excludedValue(value any) bool {
	if value == nil {
		return true
	}
	kind := reflect.TypeOf(value).Kind()
	if kind == reflect.Array || kind == reflect.Slice || kind == reflect.Map {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}

func phpString(value any) string {
	if boolean, ok := value.(bool); ok {
		if boolean {
			return "1"
		}
		return ""
	}
	return fmt.Sprint(value)
}
