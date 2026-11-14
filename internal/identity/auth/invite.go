package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// inviteAlphabet excludes look-alike characters so invite codes are easy to read.
const inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// ErrInviteCodeInvalid reports a registration invite code that is unknown,
// disabled, expired or used up. The caller must not reveal which one.
var ErrInviteCodeInvalid = errors.New("invite code invalid")

// InviteCodeConsumer validates and consumes one use of a registration invite
// code. It is injected by the invitecode package so identity stays free of that
// dependency.
type InviteCodeConsumer interface {
	ConsumeRegistrationCode(ctx context.Context, code string) error
}

// InviteCodeReleaser optionally refunds a consumed use when registration fails
// afterwards (for example a duplicate identifier).
type InviteCodeReleaser interface {
	ReleaseRegistrationCode(ctx context.Context, code string) error
}

// WithInviteCodes attaches the registration invite code consumer.
func (service *Service) WithInviteCodes(consumer InviteCodeConsumer) *Service {
	copied := *service
	copied.inviteCodes = consumer
	return &copied
}

// RegisterMerchant keeps the original signature and delegates to
// RegisterMerchantWithInvite without an invite code.
func (service *Service) RegisterMerchant(ctx context.Context, identifier, secret string, metadata LoginMetadata) (MerchantLogin, error) {
	return service.RegisterMerchantWithInvite(ctx, identifier, secret, "", metadata)
}

// RegisterMerchantWithCodes validates a registration invite code first, then
// delegates to the regular invite-aware registration. An empty code keeps the
// behaviour identical to RegisterMerchantWithInvite.
func (service *Service) RegisterMerchantWithCodes(ctx context.Context, identifier, secret, invite, inviteCode string, metadata LoginMetadata) (MerchantLogin, error) {
	inviteCode = strings.ToUpper(strings.TrimSpace(inviteCode))
	if inviteCode != "" && service.inviteCodes != nil {
		if err := service.inviteCodes.ConsumeRegistrationCode(ctx, inviteCode); err != nil {
			return MerchantLogin{}, fmt.Errorf("%w: %v", ErrInviteCodeInvalid, err)
		}
		result, err := service.RegisterMerchantWithInvite(ctx, identifier, secret, invite, metadata)
		if err != nil {
			if releaser, ok := service.inviteCodes.(InviteCodeReleaser); ok {
				_ = releaser.ReleaseRegistrationCode(ctx, inviteCode)
			}
			return MerchantLogin{}, err
		}
		return result, nil
	}
	return service.RegisterMerchantWithInvite(ctx, identifier, secret, invite, metadata)
}

// RegisterMerchantWithInvite creates a merchant, assigns a unique 8 character
// invite code and links the inviter when the submitted code exists. Unknown
// invite codes are ignored on purpose so registration never fails on them.
func (service *Service) RegisterMerchantWithInvite(ctx context.Context, identifier, secret, invite string, metadata LoginMetadata) (MerchantLogin, error) {
	identifier = strings.TrimSpace(identifier)
	invite = strings.ToUpper(strings.TrimSpace(invite))
	if identifier == "" || secret == "" {
		return MerchantLogin{}, errors.New("identifier and password are required")
	}

	var count int64
	if err := service.db.WithContext(ctx).Model(&model.Merchant{}).
		Where("email = ? OR phone = ?", identifier, identifier).
		Count(&count).Error; err != nil {
		return MerchantLogin{}, err
	}
	if count > 0 {
		return MerchantLogin{}, ErrMerchantExists
	}

	hash, err := password.Hash(secret)
	if err != nil {
		return MerchantLogin{}, err
	}

	var email *string
	var phone *string
	if strings.Contains(identifier, "@") {
		email = &identifier
	} else {
		phone = &identifier
	}

	now := time.Now().UTC()
	merchant := model.Merchant{
		ID:           uuid.NewString(),
		Email:        email,
		Phone:        phone,
		PasswordHash: &hash,
		DisplayName:  identifier,
		Status:       model.StatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if invite != "" {
		if inviter, lookupErr := service.merchantByInviteCode(ctx, invite); lookupErr == nil {
			merchant.InvitedBy = &inviter.ID
		}
	}
	if err := service.createMerchant(ctx, &merchant); err != nil {
		return MerchantLogin{}, err
	}

	token, created, err := service.sessions.Create(ctx, model.PrincipalMerchant, merchant.ID, 7*24*time.Hour, metadata.IPAddress, metadata.UserAgent)
	if err != nil {
		return MerchantLogin{}, err
	}
	return MerchantLogin{Token: token, Merchant: merchant, Session: created}, nil
}

func (service *Service) merchantByInviteCode(ctx context.Context, code string) (model.Merchant, error) {
	var merchant model.Merchant
	if err := service.db.WithContext(ctx).Where("invite_code = ?", code).First(&merchant).Error; err != nil {
		return model.Merchant{}, err
	}
	return merchant, nil
}

// createMerchant assigns a unique legacy_uid (epay merchant number) and a unique
// invite code, retrying on collision.
func (service *Service) createMerchant(ctx context.Context, merchant *model.Merchant) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		uid, err := randomLegacyUID()
		if err != nil {
			return err
		}
		code, err := randomInviteCode(service.db, ctx)
		if err != nil {
			return err
		}
		merchant.LegacyUID = &uid
		merchant.InviteCode = &code
		if err := service.db.WithContext(ctx).Create(merchant).Error; err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func randomInviteCode(db *gorm.DB, ctx context.Context) (string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		code, err := randomCode(8)
		if err != nil {
			return "", err
		}
		var count int64
		if err := db.WithContext(ctx).Model(&model.Merchant{}).Where("invite_code = ?", code).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return code, nil
		}
	}
	return "", errors.New("could not allocate a unique invite code")
}

func randomCode(length int) (string, error) {
	var builder strings.Builder
	for index := 0; index < length; index++ {
		position, err := rand.Int(rand.Reader, big.NewInt(int64(len(inviteAlphabet))))
		if err != nil {
			return "", err
		}
		builder.WriteByte(inviteAlphabet[position.Int64()])
	}
	return builder.String(), nil
}

// InviteCodeOf returns the merchant invite code or an empty string.
func InviteCodeOf(merchant model.Merchant) string {
	if merchant.InviteCode == nil {
		return ""
	}
	return *merchant.InviteCode
}
