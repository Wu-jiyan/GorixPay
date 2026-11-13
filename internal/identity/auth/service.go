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

type LoginMetadata struct {
	IPAddress string
	UserAgent string
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
	db       *gorm.DB
	sessions *session.Service
}

func NewService(db *gorm.DB, sessions *session.Service) *Service {
	return &Service{db: db, sessions: sessions}
}

func (service *Service) LoginMerchant(ctx context.Context, identifier, secret string, metadata LoginMetadata) (MerchantLogin, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" || secret == "" {
		return MerchantLogin{}, ErrInvalidCredentials
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
		return MerchantLogin{}, ErrInvalidCredentials
	}
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

func (service *Service) LoginAdministrator(ctx context.Context, username, secret string, metadata LoginMetadata) (AdministratorLogin, error) {
	username = strings.TrimSpace(username)
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
	token, created, err := service.sessions.Create(ctx, model.PrincipalAdministrator, administrator.ID, 12*time.Hour, metadata.IPAddress, metadata.UserAgent)
	if err != nil {
		return AdministratorLogin{}, err
	}
	return AdministratorLogin{Token: token, Administrator: administrator, Session: created}, nil
}
