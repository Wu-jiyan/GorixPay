package oauth

import (
	"bytes"
	"context"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"

	"gorm.io/gorm"
)

const (
	testMerchantID = "merchant-1"
	testEmail      = "merchant@example.com"
	testPassword   = "SecretPass1"
)

type clock struct {
	value time.Time
}

func (clock *clock) now() time.Time { return clock.value }

type fixture struct {
	service  *Service
	db       *gorm.DB
	sessions *session.Service
	cipher   *secret.Cipher
	provider *fakeProvider
	clock    *clock
}

func newFixture(t *testing.T, name string) *fixture {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&Binding{}, &QRLoginSession{}, &OAuthState{}, &systemSetting{},
		&identitymodel.Merchant{}, &identitymodel.Administrator{}, &identitymodel.Session{},
	); err != nil {
		t.Fatal(err)
	}
	hash, err := password.Hash(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	email := testEmail
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	merchant := identitymodel.Merchant{
		ID: testMerchantID, Email: &email, PasswordHash: &hash, DisplayName: "商户",
		Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x55}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	shared := &clock{value: now}
	sessions := session.NewService(db, shared.now)
	provider := newFakeProvider("wechat")
	service := New(db, sessions, cipher, "https://pay.example").WithProviders(provider)
	service.now = shared.now
	return &fixture{service: service, db: db, sessions: sessions, cipher: cipher, provider: provider, clock: shared}
}

func (fixture *fixture) advance(duration time.Duration) {
	fixture.clock.value = fixture.clock.value.Add(duration)
}

func (fixture *fixture) createMerchant(t *testing.T, id, email string) {
	t.Helper()
	hash, err := password.Hash(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	now := fixture.clock.value
	merchant := identitymodel.Merchant{
		ID: id, Email: &email, PasswordHash: &hash, DisplayName: id,
		Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := fixture.db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
}
