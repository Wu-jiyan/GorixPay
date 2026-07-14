package secret

import (
	"bytes"
	"testing"
)

func TestCipherEncryptsWithRandomNonceAndContext(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	cipher, err := NewCipher(key, "v1")
	if err != nil {
		t.Fatal(err)
	}
	first, err := cipher.Encrypt([]byte("merchant-secret"), []byte("merchant-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := cipher.Encrypt([]byte("merchant-secret"), []byte("merchant-1"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second || bytes.Contains([]byte(first), []byte("merchant-secret")) {
		t.Fatal("ciphertext is deterministic or leaks plaintext")
	}
	plain, version, err := cipher.Decrypt(first, []byte("merchant-1"))
	if err != nil || version != "v1" || string(plain) != "merchant-secret" {
		t.Fatalf("decrypt: plain=%q version=%q err=%v", plain, version, err)
	}
	if _, _, err := cipher.Decrypt(first, []byte("merchant-2")); err == nil {
		t.Fatal("ciphertext accepted with wrong context")
	}
}

func TestCipherRejectsInvalidKeyAndTampering(t *testing.T) {
	if _, err := NewCipher([]byte("short"), "v1"); err == nil {
		t.Fatal("invalid key accepted")
	}
	cipher, err := NewCipher(bytes.Repeat([]byte{0x42}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := cipher.Encrypt([]byte("secret"), nil)
	if err != nil {
		t.Fatal(err)
	}
	tamperedBytes := []byte(encoded)
	separator := bytes.IndexByte(tamperedBytes, '.')
	if separator < 0 || separator+1 >= len(tamperedBytes) {
		t.Fatal("invalid test ciphertext")
	}
	if tamperedBytes[separator+1] == 'A' {
		tamperedBytes[separator+1] = 'B'
	} else {
		tamperedBytes[separator+1] = 'A'
	}
	tampered := string(tamperedBytes)
	if _, _, err := cipher.Decrypt(tampered, nil); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}
