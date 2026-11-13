package credential

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/compat/epay/signing"
	"gorixpay/internal/identity/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"

	"gorm.io/gorm"
)

func TestCreateRSAStoresEncryptedKeyAndResolvesPublicKey(t *testing.T) {
	db, service := rsaFixture(t, "credential-rsa-create")
	created, privatePEM, err := service.CreateRSA(context.Background(), "merchant-1")
	if err != nil {
		t.Fatal(err)
	}
	if created.CredentialType != model.CredentialRSA || created.PublicKey == nil || created.KeyID == nil {
		t.Fatalf("credential: %+v", created)
	}
	if strings.Contains(created.SecretEncrypted, "PRIVATE KEY") || created.SecretEncrypted == privatePEM {
		t.Fatal("private key stored in plaintext")
	}
	parsed, err := signing.ParseRSAPrivateKeyPEM(privatePEM)
	if err != nil || parsed.N.BitLen() != 2048 {
		t.Fatalf("private key: %v err=%v", parsed, err)
	}
	resolved, err := service.ResolveRSA(context.Background(), "merchant-1")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.PublicKey != *created.PublicKey {
		t.Fatalf("resolved public key mismatch")
	}
	decrypted, err := signing.ParseRSAPrivateKeyPEM(resolved.Secret)
	if err != nil || decrypted.N.Cmp(parsed.N) != 0 {
		t.Fatalf("resolved secret does not match created key")
	}
	var stored model.MerchantCredential
	if err := db.First(&stored, "id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.CredentialActive {
		t.Fatalf("status: %s", stored.Status)
	}
}

func TestRotateRSARevokesPreviousCredential(t *testing.T) {
	_, service := rsaFixture(t, "credential-rsa-rotate")
	first, firstPEM, err := service.CreateRSA(context.Background(), "merchant-1")
	if err != nil {
		t.Fatal(err)
	}
	second, secondPEM, err := service.RotateRSA(context.Background(), "merchant-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || firstPEM == secondPEM {
		t.Fatal("rotation reused key material")
	}
	resolved, err := service.ResolveRSA(context.Background(), "merchant-1")
	if err != nil || resolved.Credential.ID != second.ID {
		t.Fatalf("resolve after rotate: %+v err=%v", resolved, err)
	}
	active, err := service.ResolveActive(context.Background(), "merchant-1", model.CredentialRSA)
	if err != nil || active.Credential.ID != second.ID {
		t.Fatalf("active credential: %+v err=%v", active, err)
	}
	if _, err := service.ResolveActive(context.Background(), "merchant-1", model.CredentialRSA); err != nil {
		t.Fatal(err)
	}
	var previous model.MerchantCredential
	if err := service.db.First(&previous, "id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if previous.Status != model.CredentialRevoked || previous.RevokedAt == nil {
		t.Fatalf("previous credential still active: %+v", previous)
	}
}

func TestResolveRSAMissingCredential(t *testing.T) {
	_, service := rsaFixture(t, "credential-rsa-missing")
	if _, err := service.ResolveRSA(context.Background(), "merchant-1"); err != ErrCredentialNotFound {
		t.Fatalf("err = %v", err)
	}
}

func rsaFixture(t *testing.T, name string) (*gorm.DB, *Service) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Merchant{}, &model.MerchantCredential{}); err != nil {
		t.Fatal(err)
	}
	merchant := model.Merchant{ID: "merchant-1", DisplayName: "merchant", Status: model.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x61}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	return db, NewService(db, cipher, time.Now)
}
