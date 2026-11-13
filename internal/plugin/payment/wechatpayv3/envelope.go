package wechatpayv3

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"strings"
)

// decryptResource opens the AEAD_AES_256_GCM envelope of an APIv3 notification.
func decryptResource(apiV3Key, ciphertext, nonce, associatedData string) ([]byte, error) {
	key := []byte(strings.TrimSpace(apiV3Key))
	if len(key) != apiV3KeyLength {
		return nil, fmt.Errorf("%w: api_v3_key must be %d bytes", errInvalidCiphertext, apiV3KeyLength)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ciphertext))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidCiphertext, err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidCiphertext, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidCiphertext, err)
	}
	plain, err := gcm.Open(nil, []byte(nonce), raw, []byte(associatedData))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidCiphertext, err)
	}
	return plain, nil
}
