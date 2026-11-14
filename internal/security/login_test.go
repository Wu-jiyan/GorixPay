package security

import (
	"context"
	"errors"
	"strings"
	"testing"

	identityauth "gorixpay/internal/identity/auth"
)

func newAuthService(fixture *fixture) *identityauth.Service {
	return identityauth.NewService(fixture.db, fixture.sessions).WithChallenge(fixture.service)
}

func TestLoginRequiresCaptchaWhenSettingEnabled(t *testing.T) {
	fixture := newFixture(t, "security-login-captcha")
	ctx := context.Background()
	fixture.setCaptchaRequired(t, true)
	authService := newAuthService(fixture)

	if _, err := authService.LoginAdministrator(ctx, testUsername, testPassword, identityauth.LoginMetadata{}); !errors.Is(err, identityauth.ErrCaptchaRequired) {
		t.Fatalf("missing captcha err=%v", err)
	}
	captcha, err := fixture.service.CreateCaptcha(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authService.LoginAdministrator(ctx, testUsername, testPassword, identityauth.LoginMetadata{
		CaptchaID: captcha.ID, CaptchaCode: "ZZZZ",
	}); !errors.Is(err, identityauth.ErrCaptchaInvalid) {
		t.Fatalf("wrong captcha err=%v", err)
	}
	result, err := authService.LoginAdministrator(ctx, testUsername, testPassword, identityauth.LoginMetadata{
		CaptchaID: captcha.ID, CaptchaCode: strings.ToLower(captcha.Code),
	})
	if err != nil || result.Token == "" || result.Administrator.ID != testAdministratorID {
		t.Fatalf("valid login result=%+v err=%v", result, err)
	}
	if _, err := authService.LoginAdministrator(ctx, testUsername, testPassword, identityauth.LoginMetadata{
		CaptchaID: captcha.ID, CaptchaCode: captcha.Code,
	}); !errors.Is(err, identityauth.ErrCaptchaInvalid) {
		t.Fatalf("replayed captcha err=%v", err)
	}
	attempts := fixture.loginAttempts(t)
	if len(attempts) != 4 {
		t.Fatalf("attempts=%d want 4: %+v", len(attempts), attempts)
	}
	successes := 0
	for _, attempt := range attempts {
		if attempt.Identifier != testUsername || attempt.PrincipalType != loginPrincipalAdmin {
			t.Fatalf("attempt=%+v", attempt)
		}
		if attempt.Success {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("success rows=%d want 1", successes)
	}
}

func TestLoginRequiresTOTPWhenEnrolled(t *testing.T) {
	fixture := newFixture(t, "security-login-totp")
	ctx := context.Background()
	authService := newAuthService(fixture)
	secretValue, _, err := fixture.service.SetupTOTP(ctx, testAdministratorID, testUsername)
	if err != nil {
		t.Fatal(err)
	}
	code, err := TOTPCode(secretValue, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.EnableTOTP(ctx, testAdministratorID, code); err != nil {
		t.Fatal(err)
	}
	if _, err := authService.LoginAdministrator(ctx, testUsername, testPassword, identityauth.LoginMetadata{}); !errors.Is(err, identityauth.ErrTOTPRequired) {
		t.Fatalf("missing totp err=%v", err)
	}
	if _, err := authService.LoginAdministrator(ctx, testUsername, testPassword, identityauth.LoginMetadata{TOTPCode: "000000"}); !errors.Is(err, identityauth.ErrTOTPInvalid) {
		t.Fatalf("wrong totp err=%v", err)
	}
	result, err := authService.LoginAdministrator(ctx, testUsername, testPassword, identityauth.LoginMetadata{TOTPCode: code})
	if err != nil || result.Token == "" {
		t.Fatalf("valid totp result=%+v err=%v", result, err)
	}
	// A wrong password must still fail as invalid credentials, not as TOTP.
	if _, err := authService.LoginAdministrator(ctx, testUsername, "wrong-password", identityauth.LoginMetadata{TOTPCode: code}); !errors.Is(err, identityauth.ErrInvalidCredentials) {
		t.Fatalf("wrong password err=%v", err)
	}
	attempts := fixture.loginAttempts(t)
	successes := 0
	for _, attempt := range attempts {
		if attempt.Success {
			successes++
		}
	}
	if len(attempts) != 4 || successes != 1 {
		t.Fatalf("attempts=%d successes=%d", len(attempts), successes)
	}
}

func TestLoginRecordsCredentialFailures(t *testing.T) {
	fixture := newFixture(t, "security-login-failure")
	ctx := context.Background()
	authService := newAuthService(fixture)
	if _, err := authService.LoginAdministrator(ctx, testUsername, "nope-nope", identityauth.LoginMetadata{IPAddress: "203.0.113.9"}); !errors.Is(err, identityauth.ErrInvalidCredentials) {
		t.Fatalf("err=%v", err)
	}
	attempts := fixture.loginAttempts(t)
	if len(attempts) != 1 || attempts[0].Success {
		t.Fatalf("attempts=%+v", attempts)
	}
	if attempts[0].ClientIP == nil || *attempts[0].ClientIP != "203.0.113.9" {
		t.Fatalf("client ip=%v", attempts[0].ClientIP)
	}
}

func TestLoginWithoutChallengeKeepsWorking(t *testing.T) {
	fixture := newFixture(t, "security-login-plain")
	ctx := context.Background()
	authService := identityauth.NewService(fixture.db, fixture.sessions)
	result, err := authService.LoginAdministrator(ctx, testUsername, testPassword, identityauth.LoginMetadata{})
	if err != nil || result.Token == "" {
		t.Fatalf("plain login result=%+v err=%v", result, err)
	}
	if attempts := fixture.loginAttempts(t); len(attempts) != 0 {
		t.Fatalf("plain login must not write attempts: %+v", attempts)
	}
}
