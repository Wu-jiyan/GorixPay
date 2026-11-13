package credential

import (
	"context"
	"strings"

	"gorixpay/internal/compat/epay/signing"
	"gorixpay/internal/identity/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CreateRSA generates a fresh 2048 bit merchant key pair. The PKCS#8 private key
// is stored encrypted and returned once in plain text; the public key is stored
// as PEM and can be read later with ResolveRSA.
func (service *Service) CreateRSA(ctx context.Context, merchantID string) (model.MerchantCredential, string, error) {
	return service.createRSA(ctx, service.db, merchantID)
}

// RotateRSA revokes every active RSA credential of the merchant, then issues a
// new key pair.
func (service *Service) RotateRSA(ctx context.Context, merchantID string) (model.MerchantCredential, string, error) {
	var created model.MerchantCredential
	var privatePEM string
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := service.now().UTC()
		if err := tx.Model(&model.MerchantCredential{}).
			Where("merchant_id = ? AND credential_type = ? AND status = ?", merchantID, model.CredentialRSA, model.CredentialActive).
			Updates(map[string]any{"status": model.CredentialRevoked, "revoked_at": now}).Error; err != nil {
			return err
		}
		var err error
		created, privatePEM, err = service.createRSA(ctx, tx, merchantID)
		return err
	})
	return created, privatePEM, err
}

// ResolveRSA returns the active RSA credential including its public key PEM.
func (service *Service) ResolveRSA(ctx context.Context, merchantID string) (Resolved, error) {
	resolved, err := service.ResolveActive(ctx, merchantID, model.CredentialRSA)
	if err != nil {
		return Resolved{}, err
	}
	if resolved.Credential.PublicKey == nil || strings.TrimSpace(*resolved.Credential.PublicKey) == "" {
		return Resolved{}, ErrCredentialNotFound
	}
	resolved.PublicKey = *resolved.Credential.PublicKey
	return resolved, nil
}

func (service *Service) createRSA(ctx context.Context, db *gorm.DB, merchantID string) (model.MerchantCredential, string, error) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return model.MerchantCredential{}, "", ErrCredentialNotFound
	}
	key, err := signing.GenerateRSAKey()
	if err != nil {
		return model.MerchantCredential{}, "", err
	}
	privatePEM, err := signing.EncodeRSAPrivateKeyPEM(key)
	if err != nil {
		return model.MerchantCredential{}, "", err
	}
	publicPEM, err := signing.EncodeRSAPublicKeyPEM(&key.PublicKey)
	if err != nil {
		return model.MerchantCredential{}, "", err
	}
	encrypted, err := service.cipher.Encrypt([]byte(privatePEM), []byte(merchantID+":"+string(model.CredentialRSA)))
	if err != nil {
		return model.MerchantCredential{}, "", err
	}
	keyID := uuid.NewString()
	created := model.MerchantCredential{
		ID:              uuid.NewString(),
		MerchantID:      merchantID,
		CredentialType:  model.CredentialRSA,
		KeyID:           &keyID,
		SecretEncrypted: encrypted,
		PublicKey:       &publicPEM,
		Status:          model.CredentialActive,
		CreatedAt:       service.now().UTC(),
	}
	if err := db.WithContext(ctx).Create(&created).Error; err != nil {
		return model.MerchantCredential{}, "", err
	}
	return created, privatePEM, nil
}
