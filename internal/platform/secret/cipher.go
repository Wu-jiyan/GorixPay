package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

type Cipher struct {
	aead    cipher.AEAD
	version string
}

func NewCipher(key []byte, version string) (*Cipher, error) {
	if len(key) != 32 {
		return nil, errors.New("master key must be 32 bytes")
	}
	if version == "" || strings.Contains(version, ".") {
		return nil, errors.New("invalid key version")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead, version: version}, nil
}

func (c *Cipher) Encrypt(plain, context []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nil, nonce, plain, context)
	payload := append(nonce, sealed...)
	return c.version + "." + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (c *Cipher) Decrypt(encoded string, context []byte) ([]byte, string, error) {
	parts := strings.SplitN(encoded, ".", 2)
	if len(parts) != 2 || parts[0] == "" {
		return nil, "", errors.New("invalid encrypted secret")
	}
	if parts[0] != c.version {
		return nil, parts[0], fmt.Errorf("unsupported key version %q", parts[0])
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != parts[1] || len(payload) < c.aead.NonceSize()+c.aead.Overhead() {
		return nil, parts[0], errors.New("invalid encrypted secret")
	}
	nonce := payload[:c.aead.NonceSize()]
	plain, err := c.aead.Open(nil, nonce, payload[c.aead.NonceSize():], context)
	if err != nil {
		return nil, parts[0], errors.New("invalid encrypted secret")
	}
	return plain, parts[0], nil
}
