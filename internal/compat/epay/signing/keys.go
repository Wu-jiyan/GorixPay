package signing

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

var (
	ErrInvalidPrivateKey = errors.New("invalid rsa private key")
	ErrInvalidPublicKey  = errors.New("invalid rsa public key")
	ErrWeakRSAKey        = errors.New("rsa key must be at least 2048 bits")
)

// GenerateRSAKey creates a fresh 2048 bit RSA key pair.
func GenerateRSAKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
}

// EncodeRSAPrivateKeyPEM renders the key as a PKCS#8 PEM block, the format used by
// epay platform credentials and merchant V2 credentials.
func EncodeRSAPrivateKeyPEM(key *rsa.PrivateKey) (string, error) {
	if key == nil {
		return "", ErrInvalidPrivateKey
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

// EncodeRSAPublicKeyPEM renders the public key as a PKIX PEM block.
func EncodeRSAPublicKeyPEM(key *rsa.PublicKey) (string, error) {
	if key == nil {
		return "", ErrInvalidPublicKey
	}
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// ParseRSAPrivateKeyPEM accepts PKCS#8 ("PRIVATE KEY") and PKCS#1 ("RSA PRIVATE KEY")
// PEM blocks, matching the formats produced by openssl and the epay platform.
func ParseRSAPrivateKeyPEM(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, ErrInvalidPrivateKey
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		parsed, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, ErrInvalidPrivateKey
		}
		return parsed, nil
	}
	parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, ErrInvalidPrivateKey
	}
	return parsed, nil
}

// ParseRSAPublicKeyPEM accepts PKIX ("PUBLIC KEY") and PKCS#1 ("RSA PUBLIC KEY") blocks.
func ParseRSAPublicKeyPEM(value string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, ErrInvalidPublicKey
	}
	if parsed, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		key, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, ErrInvalidPublicKey
		}
		return key, nil
	}
	parsed, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, ErrInvalidPublicKey
	}
	return parsed, nil
}

// EnsureKeySize rejects keys below the documented 2048 bit floor.
func EnsureKeySize(key *rsa.PrivateKey) error {
	if key == nil {
		return ErrInvalidPrivateKey
	}
	if key.N.BitLen() < 2048 {
		return fmt.Errorf("%w: %d bits", ErrWeakRSAKey, key.N.BitLen())
	}
	return nil
}
