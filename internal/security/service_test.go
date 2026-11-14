package security

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCaptchaLifecycleIsSingleUse(t *testing.T) {
	fixture := newFixture(t, "security-captcha")
	ctx := context.Background()
	generated, err := NewCaptcha()
	if err != nil {
		t.Fatal(err)
	}
	id := "captcha-1"
	if err := fixture.db.Create(&CaptchaChallenge{
		ID: id, AnswerHash: captchaHash(id, generated.Code),
		ExpiresAt: fixture.now.Add(captchaTTL), CreatedAt: fixture.now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.VerifyCaptcha(ctx, id, "!!!!"); !errors.Is(err, ErrCaptchaInvalid) {
		t.Fatalf("wrong answer err=%v", err)
	}
	if err := fixture.service.VerifyCaptcha(ctx, id, strings.ToLower(generated.Code)); err != nil {
		t.Fatalf("lowercase answer should be accepted: %v", err)
	}
	if err := fixture.service.VerifyCaptcha(ctx, id, generated.Code); !errors.Is(err, ErrCaptchaUsed) {
		t.Fatalf("replay err=%v", err)
	}
	if err := fixture.service.VerifyCaptcha(ctx, "missing", generated.Code); !errors.Is(err, ErrCaptchaInvalid) {
		t.Fatalf("unknown id err=%v", err)
	}
}

func TestCaptchaExpires(t *testing.T) {
	fixture := newFixture(t, "security-captcha-expiry")
	ctx := context.Background()
	generated := Captcha{Code: "ABCD"}
	id := "captcha-expired"
	if err := fixture.db.Create(&CaptchaChallenge{
		ID: id, AnswerHash: captchaHash(id, generated.Code),
		ExpiresAt: fixture.now.Add(-time.Second), CreatedAt: fixture.now.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.VerifyCaptcha(ctx, id, generated.Code); !errors.Is(err, ErrCaptchaExpired) {
		t.Fatalf("expired err=%v", err)
	}
}

func TestCreateCaptchaPersistsOnlyTheHash(t *testing.T) {
	fixture := newFixture(t, "security-captcha-create")
	ctx := context.Background()
	generated, err := fixture.service.CreateCaptcha(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if generated.ID == "" || !strings.Contains(generated.SVG, "<svg") || len(generated.Code) != captchaLength {
		t.Fatalf("generated=%+v", generated)
	}
	var stored CaptchaChallenge
	if err := fixture.db.First(&stored, "id = ?", generated.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.AnswerHash == generated.Code || len(stored.AnswerHash) != 64 {
		t.Fatalf("answer hash=%q", stored.AnswerHash)
	}
	if err := fixture.service.VerifyCaptcha(ctx, generated.ID, generated.Code); err != nil {
		t.Fatalf("verify freshly created captcha: %v", err)
	}
}

func TestCaptchaRequiredReadsSetting(t *testing.T) {
	fixture := newFixture(t, "security-captcha-setting")
	ctx := context.Background()
	required, err := fixture.service.CaptchaRequired(ctx)
	if err != nil || required {
		t.Fatalf("default required=%v err=%v", required, err)
	}
	fixture.setCaptchaRequired(t, true)
	required, err = fixture.service.CaptchaRequired(ctx)
	if err != nil || !required {
		t.Fatalf("enabled required=%v err=%v", required, err)
	}
}

func TestTOTPEnrollmentFlow(t *testing.T) {
	fixture := newFixture(t, "security-totp-flow")
	ctx := context.Background()
	service := fixture.service

	if _, err := service.EnableTOTP(ctx, testAdministratorID, "123456"); !errors.Is(err, ErrTOTPNotSetup) {
		t.Fatalf("enable without setup err=%v", err)
	}
	status, err := service.TOTPStatus(ctx, testAdministratorID)
	if err != nil || status.Enabled {
		t.Fatalf("initial status=%+v err=%v", status, err)
	}
	secretValue, otpauthURL, err := service.SetupTOTP(ctx, testAdministratorID, testUsername)
	if err != nil {
		t.Fatal(err)
	}
	if len(secretValue) != 32 || !strings.HasPrefix(otpauthURL, "otpauth://totp/") {
		t.Fatalf("secret=%q url=%q", secretValue, otpauthURL)
	}
	var stored AdminTOTP
	if err := fixture.db.First(&stored, "administrator_id = ?", testAdministratorID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SecretEncrypted == secretValue || stored.EnabledAt != nil {
		t.Fatalf("stored=%+v", stored)
	}
	if _, err := service.EnableTOTP(ctx, testAdministratorID, "000000"); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("wrong code err=%v", err)
	}
	code, err := TOTPCode(secretValue, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	status, err = service.EnableTOTP(ctx, testAdministratorID, code)
	if err != nil || !status.Enabled || status.EnabledAt == nil {
		t.Fatalf("enable status=%+v err=%v", status, err)
	}
	if _, _, err := service.SetupTOTP(ctx, testAdministratorID, testUsername); !errors.Is(err, ErrTOTPAlreadyEnabled) {
		t.Fatalf("re-setup while enabled err=%v", err)
	}
	if enabled, err := service.TOTPEnabled(ctx, testAdministratorID); err != nil || !enabled {
		t.Fatalf("TOTPEnabled=%v err=%v", enabled, err)
	}
	if err := service.VerifyTOTP(ctx, testAdministratorID, code); err != nil {
		t.Fatalf("verify current code: %v", err)
	}
	if err := service.VerifyTOTP(ctx, testAdministratorID, "000000"); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("verify wrong code err=%v", err)
	}
	if err := service.DisableTOTP(ctx, testAdministratorID, "000000"); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("disable wrong code err=%v", err)
	}
	if err := service.DisableTOTP(ctx, testAdministratorID, code); err != nil {
		t.Fatal(err)
	}
	status, err = service.TOTPStatus(ctx, testAdministratorID)
	if err != nil || status.Enabled {
		t.Fatalf("status after disable=%+v err=%v", status, err)
	}
	if err := service.VerifyTOTP(ctx, testAdministratorID, code); !errors.Is(err, ErrTOTPNotSetup) {
		t.Fatalf("verify after disable err=%v", err)
	}
}

func TestTOTPSetupNeedsCipher(t *testing.T) {
	fixture := newFixture(t, "security-totp-cipher")
	service := New(fixture.db, nil)
	if _, _, err := service.SetupTOTP(context.Background(), testAdministratorID, testUsername); !errors.Is(err, ErrCipherMissing) {
		t.Fatalf("err=%v", err)
	}
}

func TestListLoginAttemptsFilters(t *testing.T) {
	fixture := newFixture(t, "security-attempts-list")
	ctx := context.Background()
	service := fixture.service
	service.RecordLoginAttempt(ctx, loginPrincipalAdmin, "admin", "10.0.0.1", true)
	service.RecordLoginAttempt(ctx, loginPrincipalAdmin, "admin", "10.0.0.1", false)
	service.RecordLoginAttempt(ctx, loginPrincipalAdmin, "root", "10.0.0.2", false)
	if err := fixture.db.Model(&LoginAttempt{}).Where("identifier = ?", "admin").Update("created_at", fixture.now.Add(time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	items, total, err := service.ListLoginAttempts(ctx, LoginAttemptFilter{}, Page{Number: 1, Size: 2})
	if err != nil || total != 3 || len(items) != 2 {
		t.Fatalf("items=%d total=%d err=%v", len(items), total, err)
	}
	failure := false
	items, total, err = service.ListLoginAttempts(ctx, LoginAttemptFilter{Identifier: "admin", Success: &failure}, Page{Number: 1, Size: 10})
	if err != nil || total != 1 || items[0].Identifier != "admin" || items[0].Success {
		t.Fatalf("filtered items=%+v total=%d err=%v", items, total, err)
	}
	if items[0].ClientIP == nil || *items[0].ClientIP != "10.0.0.1" {
		t.Fatalf("client ip=%v", items[0].ClientIP)
	}
}

func TestAdministratorName(t *testing.T) {
	fixture := newFixture(t, "security-admin-name")
	name, err := fixture.service.AdministratorName(context.Background(), testAdministratorID)
	if err != nil || name != testUsername {
		t.Fatalf("name=%q err=%v", name, err)
	}
	if _, err := fixture.service.AdministratorName(context.Background(), "missing"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing err=%v", err)
	}
}
