package security

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
	testAdministratorID = "admin-1"
	testUsername        = "admin"
	testPassword        = "SecretPass1"
)

type fixture struct {
	service  *Service
	db       *gorm.DB
	sessions *session.Service
	now      time.Time
}

func newFixture(t *testing.T, name string) *fixture {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&identitymodel.Administrator{}, &identitymodel.Session{},
		&AdminTOTP{}, &LoginAttempt{}, &CaptchaChallenge{}, &systemSetting{},
	); err != nil {
		t.Fatal(err)
	}
	hash, err := password.Hash(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	administrator := identitymodel.Administrator{
		ID: testAdministratorID, Username: testUsername, DisplayName: "管理员",
		PasswordHash: hash, Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&administrator).Error; err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x33}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	service := New(db, cipher)
	service.now = func() time.Time { return now }
	return &fixture{
		service:  service,
		db:       db,
		sessions: session.NewService(db, func() time.Time { return now }),
		now:      now,
	}
}

func (fixture *fixture) setCaptchaRequired(t *testing.T, required bool) {
	t.Helper()
	value := "false"
	if required {
		value = "true"
	}
	record := systemSetting{Key: settingCaptchaRequired, Value: value}
	if err := fixture.db.Where("setting_key = ?", settingCaptchaRequired).Delete(&systemSetting{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
}

func (fixture *fixture) loginAttempts(t *testing.T) []LoginAttempt {
	t.Helper()
	var attempts []LoginAttempt
	if err := fixture.db.Order("created_at ASC, id ASC").Find(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	return attempts
}
