package password

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argon2Time    = 1
	argon2Memory  = 64 * 1024
	argon2Threads = 4
	argon2KeyLen  = 32
	saltLength    = 16
	legacySalt    = "1277180438"
)

var ErrEmptyPassword = errors.New("password is required")

func Hash(password string) (string, error) {
	if password == "" {
		return "", ErrEmptyPassword
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argon2Memory, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func Verify(password, encoded string) (bool, bool, error) {
	if password == "" || encoded == "" {
		return false, false, nil
	}
	if IsArgon2id(encoded) {
		ok, err := verifyArgon2id(password, encoded)
		return ok, false, err
	}
	if IsLegacyMD5(encoded) {
		return false, false, errors.New("legacy merchant password requires salt")
	}
	return false, false, nil
}

func VerifyLegacyMerchant(password, encoded, uid string) (bool, bool, error) {
	if password == "" || encoded == "" {
		return false, false, nil
	}
	if IsArgon2id(encoded) {
		ok, err := verifyArgon2id(password, encoded)
		return ok, false, err
	}
	if !IsLegacyMD5(encoded) {
		return false, false, nil
	}
	expected := LegacyMerchantHash(password, uid)
	ok := subtle.ConstantTimeCompare([]byte(strings.ToLower(encoded)), []byte(strings.ToLower(expected))) == 1
	return ok, ok, nil
}

func VerifyWithSalt(password, encoded, salt string) (bool, bool, error) {
	if IsArgon2id(encoded) {
		ok, err := verifyArgon2id(password, encoded)
		return ok, false, err
	}
	if IsLegacyMD5(encoded) {
		return VerifyLegacyMerchant(password, encoded, salt)
	}
	return false, false, nil
}

func IsArgon2id(encoded string) bool {
	return strings.HasPrefix(encoded, "$argon2id$")
}

func IsLegacyMD5(encoded string) bool {
	if len(encoded) != 32 {
		return false
	}
	for _, ch := range encoded {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

func LegacyMerchantHash(password, uid string) string {
	first := md5Hex(password)
	second := md5Hex(legacySalt + uid)
	return md5Hex(first + second)
}

func verifyArgon2id(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[2] != "v=19" {
		return false, errors.New("invalid argon2id hash")
	}
	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, err
	}
	if memory < 8*1024 || memory > 128*1024 || time < 1 || time > 5 || threads < 1 || threads > 8 {
		return false, errors.New("unsafe argon2id parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	if len(salt) < 16 || len(salt) > 64 || len(expected) < 16 || len(expected) > 64 {
		return false, errors.New("invalid argon2id salt or key length")
	}
	actual := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}
