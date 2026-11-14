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

type testClock struct{ at time.Time }

func (clock *testClock) now() time.Time { return clock.at }

type fixture struct {
	service  *Service
	db       *gorm.DB
	sessions *session.Service
	now      time.Time
	clock    *testClock
}

// advance moves the fixture clock forward, which is what a TOTP test needs to
// reach the next time step.
func (fixture *fixture) advance(duration time.Duration) {
	fixture.clock.at = fixture.clock.at.Add(duration)
	fixture.now = fixture.clock.at
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
	clock := &testClock{at: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)}
	now := clock.at
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
	service.now = clock.now
	return &fixture{
		service:  service,
		db:       db,
		sessions: session.NewService(db, clock.now),
		now:      now,
		clock:    clock,
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
