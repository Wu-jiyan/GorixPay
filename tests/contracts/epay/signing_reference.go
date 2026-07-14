package epay

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

func CanonicalContent(params map[string]any) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "sign" || key == "sign_type" {
			continue
		}
		value, include := phpScalar(params[key])
		if !include || strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "&")
}

func MD5Sign(params map[string]any, key string) string {
	sum := md5.Sum([]byte(CanonicalContent(params) + key))
	return hex.EncodeToString(sum[:])
}

func RSASign(params map[string]any, privatePEM []byte) (string, error) {
	privateKey, err := parsePrivateKey(privatePEM)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(CanonicalContent(params)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func RSAVerify(params map[string]any, signature string, publicPEM []byte) error {
	publicKey, err := parsePublicKey(publicPEM)
	if err != nil {
		return err
	}
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(CanonicalContent(params)))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], decoded)
}

func SignByType(params map[string]any, md5Key string, privatePEM []byte) string {
	if params["sign_type"] == "RSA" {
		signature, _ := RSASign(params, privatePEM)
		return signature
	}
	return MD5Sign(params, md5Key)
}

func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid private key PEM")
	}
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if key, ok := parsed.(*rsa.PrivateKey); ok {
			return key, nil
		}
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func parsePublicKey(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid public key PEM")
	}
	if parsed, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if key, ok := parsed.(*rsa.PublicKey); ok {
			return key, nil
		}
	}
	return x509.ParsePKCS1PublicKey(block.Bytes)
}

func phpScalar(value any) (string, bool) {
	if value == nil {
		return "", false
	}
	kind := reflect.TypeOf(value).Kind()
	if kind == reflect.Array || kind == reflect.Slice || kind == reflect.Map {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		if typed {
			return "1", true
		}
		return "", false
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32), true
	default:
		return fmt.Sprint(value), true
	}
}
