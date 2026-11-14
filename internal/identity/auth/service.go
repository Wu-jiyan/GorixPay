package auth

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
	"gorixpay/internal/identity/session"

	"gorm.io/gorm"
)

var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrMerchantExists = errors.New("merchant already exists")

// Second-factor and anti-bot failures surfaced to the login endpoint.
var (
	ErrCaptchaRequired = errors.New("captcha required")
	ErrCaptchaInvalid  = errors.New("captcha invalid")
	// ErrLockedOut is returned after too many failed attempts from the same
	// identifier inside the lockout window.
	ErrLockedOut    = errors.New("too many failed login attempts")
	ErrTOTPRequired = errors.New("totp required")
	ErrTOTPInvalid  = errors.New("totp invalid")
)

// LoginGuard is the optional hook the security service implements so repeated
// failures stop before the password hash runs.
type LoginGuard interface {
	LoginAllowed(ctx context.Context, principalType, identifier string) error
}

// Challenge is the optional login hook for captcha and TOTP. It is injected by
// the caller (internal/security) so this package stays free of that dependency.
type Challenge interface {
	CaptchaRequired(ctx context.Context) (bool, error)
	VerifyCaptcha(ctx context.Context, id, code string) error
	TOTPEnabled(ctx context.Context, administratorID string) (bool, error)
	VerifyTOTP(ctx context.Context, administratorID, code string) error
	RecordLoginAttempt(ctx context.Context, principalType, identifier, clientIP string, success bool)
}

type LoginMetadata struct {
	IPAddress string
	UserAgent string
	// CaptchaID/CaptchaCode are the anti-bot challenge answers and TOTPCode is
	// the administrator's one-time code. All three are optional: whether they are
	// required is decided by the injected Challenge.
	CaptchaID   string
	CaptchaCode string
	TOTPCode    string
}

type MerchantLogin struct {
	Token    string
	Merchant model.Merchant
	Session  model.Session
}

type AdministratorLogin struct {
	Token         string
	Administrator model.Administrator
	Session       model.Session
}

type Service struct {
	db          *gorm.DB
	sessions    *session.Service
	challenge   Challenge
	inviteCodes InviteCodeConsumer
}

func NewService(db *gorm.DB, sessions *session.Service) *Service {
	return &Service{db: db, sessions: sessions}
}

// WithChallenge returns a copy of the service with the captcha and TOTP hook
// attached. The original service keeps its challenge-free behaviour.
func (service *Service) WithChallenge(challenge Challenge) *Service {
	copied := *service
	copied.challenge = challenge
	return &copied
}

func (service *Service) LoginMerchant(ctx context.Context, identifier, secret string, metadata LoginMetadata) (MerchantLogin, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" || secret == "" {
		return MerchantLogin{}, ErrInvalidCredentials
	}
	if err := service.guardLogin(ctx, model.PrincipalMerchant, identifier, metadata); err != nil {
		service.recordAttempt(ctx, string(model.PrincipalMerchant), identifier, metadata, false)
		return MerchantLogin{}, err
	}
	var merchant model.Merchant
	query := service.db.WithContext(ctx).Where("email = ? OR phone = ?", identifier, identifier)
	if uid, err := strconv.ParseInt(identifier, 10, 64); err == nil {
		query = service.db.WithContext(ctx).Where("legacy_uid = ? OR email = ? OR phone = ?", uid, identifier, identifier)
	}
	if err := query.First(&merchant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return MerchantLogin{}, ErrInvalidCredentials
		}
		return MerchantLogin{}, err
	}
	if merchant.Status != model.StatusActive || merchant.PasswordHash == nil {
		return MerchantLogin{}, ErrInvalidCredentials
	}
	salt := ""
	if merchant.LegacyUID != nil {
		salt = strconv.FormatInt(*merchant.LegacyUID, 10)
	}
	matched, upgrade, err := password.VerifyWithSalt(secret, *merchant.PasswordHash, salt)
	if err != nil || !matched {
		service.recordAttempt(ctx, string(model.PrincipalMerchant), identifier, metadata, false)
		return MerchantLogin{}, ErrInvalidCredentials
	}
	service.recordAttempt(ctx, string(model.PrincipalMerchant), identifier, metadata, true)
	if upgrade {
		upgraded, err := password.Hash(secret)
		if err != nil {
			return MerchantLogin{}, err
		}
		if err := service.db.WithContext(ctx).Model(&model.Merchant{}).
			Where("id = ? AND password_hash = ?", merchant.ID, *merchant.PasswordHash).
			Update("password_hash", upgraded).Error; err != nil {
			return MerchantLogin{}, err
		}
		merchant.PasswordHash = &upgraded
	}
	token, created, err := service.sessions.Create(ctx, model.PrincipalMerchant, merchant.ID, 7*24*time.Hour, metadata.IPAddress, metadata.UserAgent)
	if err != nil {
		return MerchantLogin{}, err
	}
	return MerchantLogin{Token: token, Merchant: merchant, Session: created}, nil
}

// LoginAdministrator verifies the optional captcha first, then the password,
// then the TOTP code when the administrator enrolled one. Every outcome is
// reported to the challenge hook so failed logins are auditable.
func (service *Service) LoginAdministrator(ctx context.Context, username, secret string, metadata LoginMetadata) (AdministratorLogin, error) {
	username = strings.TrimSpace(username)
	if service.challenge != nil {
		if err := service.guardLogin(ctx, model.PrincipalAdministrator, username, metadata); err != nil {
			service.recordAttempt(ctx, string(model.PrincipalAdministrator), username, metadata, false)
			return AdministratorLogin{}, err
		}
	}
	result, err := service.loginAdministrator(ctx, username, secret, metadata)
	service.recordAttempt(ctx, string(model.PrincipalAdministrator), username, metadata, err == nil)
	if err != nil {
		return AdministratorLogin{}, err
	}
	return result, nil
}

func (service *Service) loginAdministrator(ctx context.Context, username, secret string, metadata LoginMetadata) (AdministratorLogin, error) {
	if username == "" || secret == "" {
		return AdministratorLogin{}, ErrInvalidCredentials
	}
	var administrator model.Administrator
	if err := service.db.WithContext(ctx).Where("username = ?", username).First(&administrator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AdministratorLogin{}, ErrInvalidCredentials
		}
		return AdministratorLogin{}, err
	}
	if administrator.Status != model.StatusActive {
		return AdministratorLogin{}, ErrInvalidCredentials
	}
	matched, upgrade, err := password.Verify(secret, administrator.PasswordHash)
	if err != nil || !matched || upgrade {
		return AdministratorLogin{}, ErrInvalidCredentials
	}
	if service.challenge != nil {
		enabled, err := service.challenge.TOTPEnabled(ctx, administrator.ID)
		if err != nil {
			return AdministratorLogin{}, err
		}
		if enabled {
			code := strings.TrimSpace(metadata.TOTPCode)
			if code == "" {
				return AdministratorLogin{}, ErrTOTPRequired
			}
			if err := service.challenge.VerifyTOTP(ctx, administrator.ID, code); err != nil {
				return AdministratorLogin{}, ErrTOTPInvalid
			}
		}
	}
	token, created, err := service.sessions.Create(ctx, model.PrincipalAdministrator, administrator.ID, 12*time.Hour, metadata.IPAddress, metadata.UserAgent)
	if err != nil {
		return AdministratorLogin{}, err
	}
	return AdministratorLogin{Token: token, Administrator: administrator, Session: created}, nil
}

func (service *Service) checkCaptcha(ctx context.Context, metadata LoginMetadata) error {
	required, err := service.challenge.CaptchaRequired(ctx)
	if err != nil {
		return err
	}
	if !required {
		return nil
	}
	id := strings.TrimSpace(metadata.CaptchaID)
	code := strings.TrimSpace(metadata.CaptchaCode)
	if id == "" || code == "" {
		return ErrCaptchaRequired
	}
	if err := service.challenge.VerifyCaptcha(ctx, id, code); err != nil {
		return ErrCaptchaInvalid
	}
	return nil
}

func (service *Service) recordAttempt(ctx context.Context, principalType, identifier string, metadata LoginMetadata, success bool) {
	if service.challenge == nil {
		return
	}
	service.challenge.RecordLoginAttempt(ctx, principalType, identifier, metadata.IPAddress, success)
}

// guardLogin rejects a login attempt that follows too many failures, and asks for
// the captcha when the operator turned it on. It runs before the password hash so
// a brute force run cannot use the (deliberately expensive) hash as a CPU and
// memory amplifier.
func (service *Service) guardLogin(ctx context.Context, principalType model.PrincipalType, identifier string, metadata LoginMetadata) error {
	if service.challenge == nil {
		return nil
	}
	if guard, ok := service.challenge.(LoginGuard); ok {
		if err := guard.LoginAllowed(ctx, string(principalType), identifier); err != nil {
			return ErrLockedOut
		}
	}
	return service.checkCaptcha(ctx, metadata)
}
