// Package seed creates the first administrator account.
//
// The repository used to ship only scripts/create_admin.go, which hardcodes
// ./gorixpay.db and admin/admin123 and links the CGO SQLite driver. That is
// useless on a server running a statically built binary against MySQL, so a
// deployment could not create its own operator account. This is the version that
// takes the same database configuration the server does.
package seed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// errInvalidInput reports a missing or too short field.
var ErrInvalidInput = errors.New("invalid administrator input")

// now is a package variable so a test can pin the timestamp.
var now = time.Now

// MinPasswordLength is the smallest password the seed accepts. It matches what the
// reset flow enforces, so an account created here can be kept, not just used once.
const MinPasswordLength = 8

// Administrator is the account to create or update.
type Administrator struct {
	Username    string
	Password    string
	DisplayName string
}

// EnsureAdministrator creates the account, or updates the password and display
// name when the username already exists. It never prints or returns the password,
// and it never touches a different administrator.
func EnsureAdministrator(ctx context.Context, db *gorm.DB, input Administrator) (bool, error) {
	if db == nil {
		return false, ErrInvalidInput
	}
	username := strings.TrimSpace(input.Username)
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = username
	}
	if username == "" || len(username) > 100 || len(displayName) > 128 || len(input.Password) < MinPasswordLength {
		return false, ErrInvalidInput
	}
	hash, err := password.Hash(input.Password)
	if err != nil {
		return false, err
	}
	var existing identitymodel.Administrator
	err = db.WithContext(ctx).Where("username = ?", username).First(&existing).Error
	switch {
	case err == nil:
		if updateErr := db.WithContext(ctx).Model(&identitymodel.Administrator{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{
				"password_hash": hash,
				"display_name":  displayName,
				"status":        identitymodel.StatusActive,
			}).Error; updateErr != nil {
			return false, updateErr
		}
		return false, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		createdAt := now()
		account := identitymodel.Administrator{
			ID:           uuid.NewString(),
			Username:     username,
			PasswordHash: hash,
			DisplayName:  displayName,
			Status:       identitymodel.StatusActive,
			CreatedAt:    createdAt,
			UpdatedAt:    createdAt,
		}
		if createErr := db.WithContext(ctx).Create(&account).Error; createErr != nil {
			return false, fmt.Errorf("create administrator: %w", createErr)
		}
		return true, nil
	default:
		return false, err
	}
}
