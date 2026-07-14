package session

import (
	"context"
	"errors"
	"time"

	"gorixpay/internal/identity/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrInvalidSession = errors.New("invalid session")

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func NewService(db *gorm.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}

func (service *Service) Create(ctx context.Context, principalType model.PrincipalType, principalID string, ttl time.Duration, ipAddress, userAgent string) (string, model.Session, error) {
	plain, hash, err := GenerateToken()
	if err != nil {
		return "", model.Session{}, err
	}
	now := service.now().UTC()
	created := model.Session{
		ID:            uuid.NewString(),
		PrincipalType: principalType,
		PrincipalID:   principalID,
		TokenHash:     hash,
		IPAddress:     optionalString(ipAddress),
		UserAgent:     optionalString(userAgent),
		ExpiresAt:     now.Add(ttl),
		CreatedAt:     now,
	}
	if err := service.db.WithContext(ctx).Create(&created).Error; err != nil {
		return "", model.Session{}, err
	}
	return plain, created, nil
}

func (service *Service) Authenticate(ctx context.Context, token string) (model.Session, error) {
	if token == "" {
		return model.Session{}, ErrInvalidSession
	}
	var found model.Session
	if err := service.db.WithContext(ctx).Where("token_hash = ?", HashToken(token)).First(&found).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Session{}, ErrInvalidSession
		}
		return model.Session{}, err
	}
	if !found.Active(service.now().UTC()) {
		return model.Session{}, ErrInvalidSession
	}
	return found, nil
}

func (service *Service) Revoke(ctx context.Context, id string) error {
	now := service.now().UTC()
	return service.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", now).Error
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
