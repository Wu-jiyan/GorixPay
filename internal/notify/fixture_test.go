package notify

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"

	"gorm.io/gorm"
)

// captureSender records what the service tried to deliver.
type captureSender struct {
	sms   []sent
	mail  []sent
	fail  error
	calls int
}

type sent struct {
	To      string
	Subject string
	Body    string
}

func (sender *captureSender) SendSMS(_ context.Context, to, text string) error {
	sender.calls++
	if sender.fail != nil {
		return sender.fail
	}
	sender.sms = append(sender.sms, sent{To: to, Body: text})
	return nil
}

func (sender *captureSender) SendMail(_ context.Context, to, subject, body string) error {
	sender.calls++
	if sender.fail != nil {
		return sender.fail
	}
	sender.mail = append(sender.mail, sent{To: to, Subject: subject, Body: body})
	return nil
}

// clock is a controllable time source.
type clock struct {
	at time.Time
}

func (source *clock) Now() time.Time             { return source.at }
func (source *clock) Advance(step time.Duration) { source.at = source.at.Add(step) }

var fixtureStart = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

func newTestCipher(t *testing.T, fill byte) *secret.Cipher {
	t.Helper()
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{fill}, 32), "v1")
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}
	return cipher
}

// openFixtureDB opens a private in-memory SQLite database and returns its closer.
func openFixtureDB(name string) (*gorm.DB, func(), error) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    "file:" + name + "?mode=memory&cache=shared",
	})
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	return db, cleanup, nil
}

func testCipher(t *testing.T) *secret.Cipher {
	t.Helper()
	return newTestCipher(t, 0x2a)
}

// newFixture builds the service over a real SQLite database.
func newFixture(t *testing.T, name string) (*Service, *gorm.DB, *captureSender, *clock) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    "file:" + name + "?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&Channel{}, &VerifyCode{}, &PasswordReset{}, &merchantRecord{}, &identitymodel.Session{}); err != nil {
		t.Fatalf("migrate fixture: %v", err)
	}
	sender := &captureSender{}
	source := &clock{at: fixtureStart}
	service, err := NewService(Config{
		DB:     db,
		Cipher: testCipher(t),
		Sender: sender,
		Now:    source.Now,
		Logf:   func(string, ...any) {},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service, db, sender, source
}

type merchantSeed struct {
	ID       string
	Email    string
	Phone    string
	Password string
}

func seedMerchant(t *testing.T, db *gorm.DB, seed merchantSeed) {
	t.Helper()
	hash, err := password.Hash(seed.Password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	var email, phone any
	if seed.Email != "" {
		email = strings.ToLower(seed.Email)
	}
	if seed.Phone != "" {
		phone = seed.Phone
	}
	if err := db.Exec(`INSERT INTO merchants (id, legacy_uid, email, phone, password_hash, display_name, status)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		seed.ID, nil, email, phone, hash, seed.ID, "active").Error; err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
}

// seedSession inserts a live session row so revocation can be asserted.
func seedSession(t *testing.T, db *gorm.DB, id, merchantID string, now time.Time) {
	t.Helper()
	if err := db.Exec(`INSERT INTO auth_sessions (id, principal_type, principal_id, token_hash, expires_at, created_at)
        VALUES (?, ?, ?, ?, ?, ?)`, id, principalTypeMerchant, merchantID, "hash-"+id, now.Add(time.Hour), now).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func liveSessions(t *testing.T, db *gorm.DB, merchantID string) int64 {
	t.Helper()
	var total int64
	if err := db.Table("auth_sessions").
		Where("principal_type = ? AND principal_id = ? AND revoked_at IS NULL", principalTypeMerchant, merchantID).
		Count(&total).Error; err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return total
}

func storedPasswordHash(t *testing.T, db *gorm.DB, merchantID string) string {
	t.Helper()
	var hash string
	if err := db.Raw("SELECT password_hash FROM merchants WHERE id = ?", merchantID).Scan(&hash).Error; err != nil {
		t.Fatalf("read password hash: %v", err)
	}
	return hash
}

// extractToken pulls the reset token out of the development log line.
func extractToken(t *testing.T, logs []string) string {
	t.Helper()
	for _, line := range logs {
		index := strings.Index(line, "token=")
		if index < 0 {
			continue
		}
		rest := line[index+len("token="):]
		if space := strings.IndexAny(rest, " \t\n"); space >= 0 {
			rest = rest[:space]
		}
		if len(rest) >= 32 {
			return rest
		}
	}
	t.Fatalf("no reset token in logs: %v", logs)
	return ""
}

var errDeliveryStub = errors.New("delivery stub")
