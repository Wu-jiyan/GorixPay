package credential

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"gorixpay/internal/identity/model"
	"gorixpay/internal/platform/secret"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrCredentialNotFound = errors.New("active credential not found")

type Resolved struct {
	Credential model.MerchantCredential
	Secret     string
	PublicKey  string
}

type Service struct {
	db     *gorm.DB
	cipher *secret.Cipher
	now    func() time.Time
}

func NewService(db *gorm.DB, cipher *secret.Cipher, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, cipher: cipher, now: now}
}

func (service *Service) CreateMD5(ctx context.Context, merchantID string) (model.MerchantCredential, string, error) {
	return service.createMD5(ctx, service.db, merchantID)
}

func (service *Service) RotateMD5(ctx context.Context, merchantID string) (model.MerchantCredential, string, error) {
	var created model.MerchantCredential
	var plain string
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := service.now().UTC()
		if err := tx.Model(&model.MerchantCredential{}).
			Where("merchant_id = ? AND credential_type = ? AND status = ?", merchantID, model.CredentialMD5, model.CredentialActive).
			Updates(map[string]any{"status": model.CredentialRevoked, "revoked_at": now}).Error; err != nil {
			return err
		}
		var err error
		created, plain, err = service.createMD5(ctx, tx, merchantID)
		return err
	})
	return created, plain, err
}

func (service *Service) ResolveActive(ctx context.Context, merchantID string, credentialType model.CredentialType) (Resolved, error) {
	var found model.MerchantCredential
	err := service.db.WithContext(ctx).
		Where("merchant_id = ? AND credential_type = ? AND status = ?", merchantID, credentialType, model.CredentialActive).
		Order("created_at DESC").First(&found).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Resolved{}, ErrCredentialNotFound
	}
	if err != nil {
		return Resolved{}, err
	}
	plain, _, err := service.cipher.Decrypt(found.SecretEncrypted, []byte(found.MerchantID+":"+string(found.CredentialType)))
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{Credential: found, Secret: string(plain)}, nil
}

func (service *Service) List(ctx context.Context, merchantID string, result *[]model.MerchantCredential) error {
	return service.db.WithContext(ctx).
		Where("merchant_id = ?", merchantID).
		Order("created_at DESC").
		Find(result).Error
}

func (service *Service) createMD5(ctx context.Context, db *gorm.DB, merchantID string) (model.MerchantCredential, string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return model.MerchantCredential{}, "", err
	}
	plain := hex.EncodeToString(bytes)
	encrypted, err := service.cipher.Encrypt([]byte(plain), []byte(merchantID+":"+string(model.CredentialMD5)))
	if err != nil {
		return model.MerchantCredential{}, "", err
	}
	keyID := uuid.NewString()
	created := model.MerchantCredential{
		ID:              uuid.NewString(),
		MerchantID:      merchantID,
		CredentialType:  model.CredentialMD5,
		KeyID:           &keyID,
		SecretEncrypted: encrypted,
		Status:          model.CredentialActive,
		CreatedAt:       service.now().UTC(),
	}
	if err := db.WithContext(ctx).Create(&created).Error; err != nil {
		return model.MerchantCredential{}, "", err
	}
	return created, plain, nil
}
