package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
)

func TestMerchantLoginUpgradesLegacyPasswordAndCreatesSession(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:merchant-auth?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Merchant{}, &model.Session{}); err != nil {
		t.Fatal(err)
	}
	uid := int64(1000)
	email := "merchant@example.test"
	legacy := password.LegacyMerchantHash("SecretPass1", "1000")
	merchant := model.Merchant{ID: "merchant-1", LegacyUID: &uid, Email: &email, PasswordHash: &legacy, DisplayName: "merchant", Status: model.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	service := NewService(db, session.NewService(db, func() time.Time { return now }))
	result, err := service.LoginMerchant(context.Background(), email, "SecretPass1", LoginMetadata{IPAddress: "127.0.0.1", UserAgent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Token == "" || result.Merchant.ID != merchant.ID {
		t.Fatalf("invalid login result: %+v", result)
	}
	var updated model.Merchant
	if err := db.First(&updated, "id = ?", merchant.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.PasswordHash == nil || !password.IsArgon2id(*updated.PasswordHash) {
		t.Fatal("legacy password was not upgraded")
	}
	if _, upgrade, err := password.Verify("SecretPass1", *updated.PasswordHash); err != nil || upgrade {
		t.Fatalf("upgraded password invalid: upgrade=%v err=%v", upgrade, err)
	}
}

func TestMerchantLoginUsesUniformInvalidCredentialsError(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:merchant-auth-invalid?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Merchant{}, &model.Session{}); err != nil {
		t.Fatal(err)
	}
	hash, err := password.Hash("SecretPass1")
	if err != nil {
		t.Fatal(err)
	}
	email := "disabled@example.test"
	merchant := model.Merchant{ID: "merchant-2", Email: &email, PasswordHash: &hash, DisplayName: "disabled", Status: model.StatusDisabled}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	service := NewService(db, session.NewService(db, time.Now))
	for _, attempt := range []struct{ identifier, secret string }{
		{email, "wrong"},
		{email, "SecretPass1"},
		{"missing@example.test", "SecretPass1"},
	} {
		if _, err := service.LoginMerchant(context.Background(), attempt.identifier, attempt.secret, LoginMetadata{}); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("identifier %s returned %v", attempt.identifier, err)
		}
	}
}
