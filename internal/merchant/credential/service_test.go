package credential

import (
	"bytes"
	"context"
	"testing"
	"time"

	"gorixpay/internal/identity/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"
)

func TestCreateMD5CredentialStoresCiphertextAndReturnsSecretOnce(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:credential?mode=memory&cache=shared"})
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
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x42}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(db, cipher, func() time.Time { return time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC) })
	created, plain, err := service.CreateMD5(context.Background(), merchant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 32 || created.SecretEncrypted == plain || created.KeyID == nil || *created.KeyID == "" {
		t.Fatalf("invalid credential: %+v plain=%q", created, plain)
	}
	resolved, err := service.ResolveActive(context.Background(), merchant.ID, model.CredentialMD5)
	if err != nil || resolved.Secret != plain || resolved.Credential.ID != created.ID {
		t.Fatalf("resolve: %+v err=%v", resolved, err)
	}
}

func TestRotateRevokesPreviousCredential(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:credential-rotate?mode=memory&cache=shared"})
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
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x42}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(db, cipher, time.Now)
	first, firstPlain, err := service.CreateMD5(context.Background(), merchant.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, secondPlain, err := service.RotateMD5(context.Background(), merchant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstPlain == secondPlain || first.ID == second.ID {
		t.Fatal("rotation reused credential")
	}
	var previous model.MerchantCredential
	if err := db.First(&previous, "id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if previous.Status != model.CredentialRevoked || previous.RevokedAt == nil {
		t.Fatalf("previous credential remains active: %+v", previous)
	}
}
