// Package security implements administrator TOTP, login attempt auditing and
// the SVG login captcha. TOTP follows RFC 6238 with HMAC-SHA1, a 30 second time
// step and six digits, without pulling in a third-party library.
package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpDigits     = 6
	totpPeriod     = 30 * time.Second
	totpSecretSize = 20
	totpWindow     = 1
	totpModulus    = 1_000_000
)

var ErrInvalidTOTPSecret = errors.New("invalid totp secret")

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret returns a new 20 byte secret in base32 without padding,
// the shape authenticator apps expect.
func GenerateTOTPSecret() (string, error) {
	buffer := make([]byte, totpSecretSize)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base32NoPadding.EncodeToString(buffer), nil
}

// TOTPCode computes the six digit code for secret at the given instant.
func TOTPCode(secret string, at time.Time) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	return hotpCode(key, uint64(at.Unix()/int64(totpPeriod/time.Second))), nil
}

// VerifyTOTPCode accepts the code of the current step or one step either side,
// which absorbs clock skew between the server and the phone.
func VerifyTOTPCode(secret, code string, at time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits || !numeric(code) {
		return false
	}
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return false
	}
	step := at.Unix() / int64(totpPeriod/time.Second)
	for offset := -int64(totpWindow); offset <= int64(totpWindow); offset++ {
		expected := hotpCode(key, uint64(step+offset))
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// TOTPAuthURL builds the otpauth:// URI an authenticator app scans.
func TOTPAuthURL(issuer, account, secret string) string {
	issuer = strings.TrimSpace(issuer)
	account = strings.TrimSpace(account)
	label := url.PathEscape(issuer + ":" + account)
	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", issuer)
	params.Set("algorithm", "SHA1")
	params.Set("digits", fmt.Sprintf("%d", totpDigits))
	params.Set("period", fmt.Sprintf("%d", int(totpPeriod/time.Second)))
	return "otpauth://totp/" + label + "?" + params.Encode()
}

func hotpCode(key []byte, step uint64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], step)
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])
	return fmt.Sprintf("%0*d", totpDigits, value%totpModulus)
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	normalized := strings.ToUpper(strings.TrimSpace(secret))
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.TrimRight(normalized, "=")
	key, err := base32NoPadding.DecodeString(normalized)
	if err != nil || len(key) == 0 {
		return nil, ErrInvalidTOTPSecret
	}
	return key, nil
}

func numeric(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
