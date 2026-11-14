// Package invitecode manages registration invite codes: the operator issues a
// code, a merchant registers with it, and every successful use is deducted from
// the code budget with a conditional update so concurrent signups cannot
// oversell a limited code.
package invitecode

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrInvalidRequest = errors.New("invalid invite code request")
	ErrNotFound       = errors.New("invite code not found")
	ErrDuplicate      = errors.New("invite code already exists")
	// ErrInvalid covers unknown, disabled, expired and used-up codes. The
	// caller must not leak which of the four it was.
	ErrInvalid = errors.New("invite code is invalid")
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"

	codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	codeLength   = 8
)

type InviteCode struct {
	ID              string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	Code            string     `gorm:"type:varchar(32);not null;uniqueIndex" json:"code"`
	OwnerMerchantID *string    `gorm:"type:varchar(36)" json:"owner_merchant_id"`
	MaxUses         int        `gorm:"not null" json:"max_uses"`
	UsedCount       int        `gorm:"not null" json:"used_count"`
	ExpiresAt       *time.Time `json:"expires_at"`
	Status          Status     `gorm:"type:varchar(20);not null" json:"status"`
	CreatedAt       time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"not null" json:"updated_at"`
}

func (InviteCode) TableName() string { return "invite_codes" }

type CreateInput struct {
	Code            string
	OwnerMerchantID *string
	MaxUses         int
	ExpiresAt       *time.Time
	Status          Status
}

type UpdateInput struct {
	OwnerMerchantID *string
	MaxUses         *int
	ExpiresAt       *time.Time
	Status          Status
	ClearExpiry     bool
}

type Filter struct {
	Status  string
	Keyword string
}

type Page struct {
	Number int
	Size   int
}

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func New(db *gorm.DB) *Service {
	return &Service{db: db, now: time.Now}
}

func (service *Service) List(ctx context.Context, filter Filter, page Page) ([]InviteCode, int64, error) {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 {
		page.Size = 20
	}
	if page.Size > 100 {
		page.Size = 100
	}
	query := service.db.WithContext(ctx).Model(&InviteCode{})
	if status := strings.TrimSpace(filter.Status); status != "" {
		if !validStatus(Status(status)) {
			return nil, 0, ErrInvalidRequest
		}
		query = query.Where("status = ?", status)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		query = query.Where("code LIKE ?", "%"+strings.ToUpper(keyword)+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []InviteCode
	if err := query.Order("created_at DESC, id DESC").
		Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (service *Service) Create(ctx context.Context, input CreateInput) (InviteCode, error) {
	if input.MaxUses < 0 {
		return InviteCode{}, ErrInvalidRequest
	}
	status := input.Status
	if status == "" {
		status = StatusActive
	}
	if !validStatus(status) {
		return InviteCode{}, ErrInvalidRequest
	}
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	if code == "" {
		generated, err := service.uniqueCode(ctx)
		if err != nil {
			return InviteCode{}, err
		}
		code = generated
	} else if len(code) < 4 || len(code) > 32 {
		return InviteCode{}, ErrInvalidRequest
	}
	now := service.now().UTC()
	record := InviteCode{
		ID: uuid.NewString(), Code: code, OwnerMerchantID: input.OwnerMerchantID,
		MaxUses: input.MaxUses, Status: status, ExpiresAt: input.ExpiresAt,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := service.db.WithContext(ctx).Create(&record).Error; err != nil {
		return InviteCode{}, ErrDuplicate
	}
	return record, nil
}

func (service *Service) Update(ctx context.Context, id string, input UpdateInput) (InviteCode, error) {
	var record InviteCode
	if err := service.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return InviteCode{}, ErrNotFound
		}
		return InviteCode{}, err
	}
	updates := map[string]any{"updated_at": service.now().UTC()}
	if input.MaxUses != nil {
		if *input.MaxUses < 0 {
			return InviteCode{}, ErrInvalidRequest
		}
		updates["max_uses"] = *input.MaxUses
	}
	if input.Status != "" {
		if !validStatus(input.Status) {
			return InviteCode{}, ErrInvalidRequest
		}
		updates["status"] = input.Status
	}
	if input.OwnerMerchantID != nil {
		updates["owner_merchant_id"] = strings.TrimSpace(*input.OwnerMerchantID)
	}
	switch {
	case input.ClearExpiry:
		updates["expires_at"] = nil
	case input.ExpiresAt != nil:
		updates["expires_at"] = input.ExpiresAt.UTC()
	}
	if err := service.db.WithContext(ctx).Model(&InviteCode{}).Where("id = ?", record.ID).Updates(updates).Error; err != nil {
		return InviteCode{}, err
	}
	return service.Get(ctx, record.ID)
}

func (service *Service) Get(ctx context.Context, id string) (InviteCode, error) {
	var record InviteCode
	if err := service.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return InviteCode{}, ErrNotFound
		}
		return InviteCode{}, err
	}
	return record, nil
}

func (service *Service) Delete(ctx context.Context, id string) error {
	result := service.db.WithContext(ctx).Where("id = ?", id).Delete(&InviteCode{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ConsumeRegistrationCode burns one use of the code. The single conditional
// UPDATE is the whole concurrency story: the database decides whether the
// budget allows one more, so two simultaneous signups cannot both take the
// last slot.
func (service *Service) ConsumeRegistrationCode(ctx context.Context, code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return ErrInvalid
	}
	now := service.now().UTC()
	result := service.db.WithContext(ctx).Model(&InviteCode{}).
		Where("code = ? AND status = ?", code, StatusActive).
		Where("(expires_at IS NULL OR expires_at > ?)", now).
		Where("(max_uses = 0 OR used_count < max_uses)").
		Updates(map[string]any{"used_count": gorm.Expr("used_count + 1"), "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrInvalid
	}
	return nil
}

// ReleaseRegistrationCode refunds one use when registration fails after the
// code was consumed, so a duplicate identifier does not burn a slot.
func (service *Service) ReleaseRegistrationCode(ctx context.Context, code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return ErrInvalid
	}
	now := service.now().UTC()
	return service.db.WithContext(ctx).Model(&InviteCode{}).
		Where("code = ? AND used_count > 0", code).
		Updates(map[string]any{"used_count": gorm.Expr("used_count - 1"), "updated_at": now}).Error
}

func (service *Service) uniqueCode(ctx context.Context) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		candidate, err := randomCode()
		if err != nil {
			return "", err
		}
		var count int64
		if err := service.db.WithContext(ctx).Model(&InviteCode{}).Where("code = ?", candidate).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
	return "", errors.New("could not allocate a unique invite code")
}

func validStatus(status Status) bool {
	return status == StatusActive || status == StatusDisabled
}
