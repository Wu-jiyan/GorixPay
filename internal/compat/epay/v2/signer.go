package v2

import (
	"context"
	"crypto/rsa"
	"errors"
	"strings"
	"sync"
	"time"

	"gorixpay/internal/compat/epay/signing"
	"gorixpay/internal/platform/secret"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// SettingPlatformPrivateKey keeps the AES-GCM encrypted PKCS#8 platform key.
	SettingPlatformPrivateKey = "epay.v2.platform_private_key"
	// SettingPlatformPublicKey keeps the PEM encoded platform public key.
	SettingPlatformPublicKey = "epay.v2.platform_public_key"

	platformKeyContext = "system:" + SettingPlatformPrivateKey
)

var (
	ErrSignerUnavailable = errors.New("epay v2 platform signer is unavailable")
	ErrMissingCredential = errors.New("missing platform signing key")
)

type PlatformSetting struct {
	Key       string    `gorm:"column:setting_key;primaryKey"`
	Value     string    `gorm:"column:setting_value;type:text;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (PlatformSetting) TableName() string { return "system_settings" }

// Signer holds the platform RSA key pair used to sign every V2 response and
// every V2 merchant notification.
type Signer struct {
	private   *rsa.PrivateKey
	publicPEM string
	mu        sync.RWMutex
}

// LoadSigner reads the platform key pair from system_settings and generates a
// 2048 bit pair on first use. The private key is stored encrypted with the
// platform secret.Cipher.
func LoadSigner(ctx context.Context, db *gorm.DB, cipher *secret.Cipher) (*Signer, error) {
	if db == nil {
		return nil, ErrSignerUnavailable
	}
	if cipher == nil {
		return nil, ErrSignerUnavailable
	}
	privatePEM, ok, err := readSetting(ctx, db, SettingPlatformPrivateKey)
	if err != nil {
		return nil, err
	}
	publicPEM, hasPublic, err := readSetting(ctx, db, SettingPlatformPublicKey)
	if err != nil {
		return nil, err
	}
	if ok {
		plain, _, decryptErr := cipher.Decrypt(privatePEM, []byte(platformKeyContext))
		if decryptErr != nil {
			return nil, ErrSignerUnavailable
		}
		key, parseErr := signing.ParseRSAPrivateKeyPEM(string(plain))
		if parseErr != nil {
			return nil, ErrSignerUnavailable
		}
		if !hasPublic {
			encoded, encodeErr := signing.EncodeRSAPublicKeyPEM(&key.PublicKey)
			if encodeErr != nil {
				return nil, encodeErr
			}
			publicPEM = encoded
		}
		return &Signer{private: key, publicPEM: publicPEM}, nil
	}
	key, err := signing.GenerateRSAKey()
	if err != nil {
		return nil, err
	}
	encodedPrivate, err := signing.EncodeRSAPrivateKeyPEM(key)
	if err != nil {
		return nil, err
	}
	encodedPublic, err := signing.EncodeRSAPublicKeyPEM(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	encrypted, err := cipher.Encrypt([]byte(encodedPrivate), []byte(platformKeyContext))
	if err != nil {
		return nil, err
	}
	if err := writeSetting(ctx, db, SettingPlatformPrivateKey, encrypted); err != nil {
		return nil, err
	}
	if err := writeSetting(ctx, db, SettingPlatformPublicKey, encodedPublic); err != nil {
		return nil, err
	}
	// A concurrent process may have won the race; prefer the persisted key.
	if stored, found, readErr := readSetting(ctx, db, SettingPlatformPrivateKey); readErr == nil && found && stored != encrypted {
		if plain, _, decryptErr := cipher.Decrypt(stored, []byte(platformKeyContext)); decryptErr == nil {
			if winner, parseErr := signing.ParseRSAPrivateKeyPEM(string(plain)); parseErr == nil {
				winnerPublic, _, _ := readSetting(ctx, db, SettingPlatformPublicKey)
				return &Signer{private: winner, publicPEM: winnerPublic}, nil
			}
		}
	}
	return &Signer{private: key, publicPEM: encodedPublic}, nil
}

// SignValues signs the canonical form of a string valued parameter set.
func (signer *Signer) SignValues(values map[string]string) (string, error) {
	if signer == nil || signer.private == nil {
		return "", ErrSignerUnavailable
	}
	return signing.SignRSA(toStringMap(values), signer.private)
}

// SignParams signs a mixed value parameter set. Arrays and empty values are
// excluded from the canonical content, matching the reference implementation.
func (signer *Signer) SignParams(values map[string]any) (string, error) {
	if signer == nil || signer.private == nil {
		return "", ErrSignerUnavailable
	}
	return signing.SignRSA(values, signer.private)
}

// PublicKeyPEM returns the PEM encoded platform public key.
func (signer *Signer) PublicKeyPEM() string {
	if signer == nil {
		return ""
	}
	signer.mu.RLock()
	defer signer.mu.RUnlock()
	return signer.publicPEM
}

// PrivateKey exposes the platform key for tests that need to verify signatures.
func (signer *Signer) PrivateKey() *rsa.PrivateKey {
	if signer == nil {
		return nil
	}
	return signer.private
}

func readSetting(ctx context.Context, db *gorm.DB, key string) (string, bool, error) {
	var record PlatformSetting
	err := db.WithContext(ctx).Where("setting_key = ?", key).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return record.Value, strings.TrimSpace(record.Value) != "", nil
}

func writeSetting(ctx context.Context, db *gorm.DB, key, value string) error {
	record := PlatformSetting{Key: key, Value: value, UpdatedAt: time.Now().UTC()}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "setting_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"setting_value", "updated_at"}),
	}).Create(&record).Error
}
