package rbac

import (
	"context"
	"sort"

	"gorixpay/internal/identity/model"

	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (service *Service) Authorize(ctx context.Context, principalType model.PrincipalType, principalID, permissionCode string) (bool, error) {
	var count int64
	err := service.db.WithContext(ctx).
		Table("principal_roles AS pr").
		Joins("JOIN role_permissions AS rp ON rp.role_id = pr.role_id").
		Joins("JOIN permissions AS p ON p.id = rp.permission_id").
		Where("pr.principal_type = ? AND pr.principal_id = ? AND p.code = ?", principalType, principalID, permissionCode).
		Count(&count).Error
	return count > 0, err
}

func (service *Service) DataScopes(ctx context.Context, principalType model.PrincipalType, principalID string) ([]string, error) {
	var scopes []string
	err := service.db.WithContext(ctx).
		Table("principal_roles AS pr").
		Select("DISTINCT r.data_scope").
		Joins("JOIN roles AS r ON r.id = pr.role_id").
		Where("pr.principal_type = ? AND pr.principal_id = ?", principalType, principalID).
		Pluck("r.data_scope", &scopes).Error
	sort.Strings(scopes)
	return scopes, err
}
