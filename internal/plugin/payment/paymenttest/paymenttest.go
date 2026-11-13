// Package paymenttest holds the fixtures shared by the payment plugin tests:
// deterministic RSA key pairs (a merchant key and an upstream platform key) and
// small helpers to build signed notifications. It is only imported from tests.
package paymenttest

import (
	"crypto/rsa"
	"encoding/json"
	"sync"
	"testing"

	"gorixpay/internal/compat/epay/signing"
)

// Pair bundles a private key with its PEM renderings.
type Pair struct {
	Private    *rsa.PrivateKey
	PrivatePEM string
	PublicPEM  string
}

var (
	merchantOnce  sync.Once
	merchantPair  Pair
	merchantError error

	platformOnce  sync.Once
	platformPair  Pair
	platformError error
)

// MerchantKeys returns the credential pair used as the channel's own private key.
func MerchantKeys() (Pair, error) {
	merchantOnce.Do(func() { merchantPair, merchantError = generate() })
	return merchantPair, merchantError
}

// PlatformKeys returns a second, independent pair standing in for the upstream
// platform key (Alipay public key, WeChat Pay platform certificate, ...).
func PlatformKeys() (Pair, error) {
	platformOnce.Do(func() { platformPair, platformError = generate() })
	return platformPair, platformError
}

// MustMerchantKeys is the test-friendly wrapper that fails fast.
func MustMerchantKeys(t *testing.T) Pair {
	t.Helper()
	pair, err := MerchantKeys()
	if err != nil {
		t.Fatalf("generate merchant keys: %v", err)
	}
	return pair
}

// MustPlatformKeys is the test-friendly wrapper that fails fast.
func MustPlatformKeys(t *testing.T) Pair {
	t.Helper()
	pair, err := PlatformKeys()
	if err != nil {
		t.Fatalf("generate platform keys: %v", err)
	}
	return pair
}

func generate() (Pair, error) {
	privateKey, err := signing.GenerateRSAKey()
	if err != nil {
		return Pair{}, err
	}
	privatePEM, err := signing.EncodeRSAPrivateKeyPEM(privateKey)
	if err != nil {
		return Pair{}, err
	}
	publicPEM, err := signing.EncodeRSAPublicKeyPEM(&privateKey.PublicKey)
	if err != nil {
		return Pair{}, err
	}
	return Pair{Private: privateKey, PrivatePEM: privatePEM, PublicPEM: publicPEM}, nil
}

// JSONBody renders a value as a compact JSON string.
func JSONBody(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode json: %v", err)
	}
	return string(encoded)
}
