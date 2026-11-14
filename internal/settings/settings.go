// Package settings centralises how the system_settings table is read and
// written.
//
// Two conventions live here because three different packages implement them
// independently and got them wrong in different ways:
//
//   - Values are stored as JSON (the admin API marshals whatever the form sent),
//     so a stored string arrives wrapped in quotes. Text() unwraps it.
//   - Credentials are stored encrypted with the master key. Secret() says which
//     keys those are, and Encrypt/Decrypt use a per-key AEAD context so a
//     ciphertext cannot be replayed under another key.
package settings

import (
	"encoding/json"
	"errors"
	"strings"

	"gorixpay/internal/platform/secret"
)

// ErrNotEncrypted reports a value that is expected to be ciphertext but is not.
var ErrNotEncrypted = errors.New("setting is not encrypted")

// Text returns the plain string a stored setting holds: a JSON encoded string is
// unwrapped, anything else is returned trimmed. Values written before the JSON
// convention, and values written by other tools, both keep working.
func Text(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var decoded string
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return strings.TrimSpace(decoded)
	}
	return trimmed
}

// secretSuffixes are the key endings that hold credentials.
var secretSuffixes = []string{"_secret", "_password", "_token"}

// Secret reports whether a setting key holds a credential. Such a key is
// encrypted before it is stored and never returned by the admin API.
func Secret(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, suffix := range secretSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// Context is the AEAD additional data for one key. Binding the ciphertext to its
// key means a value copied into another setting fails to decrypt instead of
// silently being used as somebody else's credential.
func Context(key string) []byte {
	return []byte("setting:" + strings.TrimSpace(key))
}

// Encrypt turns a plaintext credential into the stored form.
func Encrypt(cipher *secret.Cipher, key, plain string) (string, error) {
	if cipher == nil {
		return "", secretError()
	}
	trimmed := strings.TrimSpace(plain)
	if trimmed == "" {
		return "", nil
	}
	return cipher.Encrypt([]byte(trimmed), Context(key))
}

// Decrypt returns the plaintext credential.
//
// A legacy value that was stored before encryption existed is returned as is:
// refusing it would lock an operator out of a working deployment. A value that
// looks like ciphertext but does not decrypt returns ErrNotEncrypted rather than
// the ciphertext, so a broken credential fails instead of being sent to a
// provider as-is.
func Decrypt(cipher *secret.Cipher, key, raw string) (string, error) {
	text := Text(raw)
	if text == "" {
		return "", nil
	}
	if !IsCiphertext(text) {
		return text, nil
	}
	if cipher == nil {
		return "", secretError()
	}
	plain, _, err := cipher.Decrypt(text, Context(key))
	if err != nil {
		return "", ErrNotEncrypted
	}
	return strings.TrimSpace(string(plain)), nil
}

// IsCiphertext reports the "<version>.<payload>" shape the cipher produces, where
// version is a "v" followed by digits. The test has to be this narrow: a host
// name such as "smtp.example.com" also contains a dot, and treating a legacy
// plaintext value as ciphertext would lock an operator out of a working SMTP
// configuration.
func IsCiphertext(value string) bool {
	prefix, rest, found := strings.Cut(strings.TrimSpace(value), ".")
	if !found || rest == "" {
		return false
	}
	if len(prefix) < 2 || prefix[0] != 'v' {
		return false
	}
	for _, char := range prefix[1:] {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func secretError() error {
	return errors.New("master key is not configured")
}
