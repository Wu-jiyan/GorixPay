package rbac

import (
	"context"
	"testing"
	"time"

	"gorixpay/internal/identity/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
)

func TestAuthorizeFromPrincipalRolePermission(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:rbac?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Role{}, &model.Permission{}, &model.RolePermission{}, &model.PrincipalRole{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	role := model.Role{ID: "role-1", Code: "merchant-operator", Name: "operator", DataScope: "self", CreatedAt: now, UpdatedAt: now}
	permission := model.Permission{ID: "permission-1", Code: "payment.order.read", Name: "read orders", CreatedAt: now}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&permission).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.RolePermission{RoleID: role.ID, PermissionID: permission.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.PrincipalRole{PrincipalType: model.PrincipalMerchant, PrincipalID: "merchant-1", RoleID: role.ID}).Error; err != nil {
		t.Fatal(err)
	}
	service := NewService(db)
	allowed, err := service.Authorize(context.Background(), model.PrincipalMerchant, "merchant-1", "payment.order.read")
	if err != nil || !allowed {
		t.Fatalf("expected permission: allowed=%v err=%v", allowed, err)
	}
	allowed, err = service.Authorize(context.Background(), model.PrincipalMerchant, "merchant-1", "payment.order.refund")
	if err != nil || allowed {
		t.Fatalf("unexpected permission: allowed=%v err=%v", allowed, err)
	}
	allowed, err = service.Authorize(context.Background(), model.PrincipalMerchant, "merchant-2", "payment.order.read")
	if err != nil || allowed {
		t.Fatalf("permission leaked to other principal: allowed=%v err=%v", allowed, err)
	}
}

func TestDataScopesReturnsDistinctScopes(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:rbac-scopes?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Role{}, &model.PrincipalRole{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	roles := []model.Role{
		{ID: "r1", Code: "one", Name: "one", DataScope: "self", CreatedAt: now, UpdatedAt: now},
		{ID: "r2", Code: "two", Name: "two", DataScope: "merchant", CreatedAt: now, UpdatedAt: now},
		{ID: "r3", Code: "three", Name: "three", DataScope: "self", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&roles).Error; err != nil {
		t.Fatal(err)
	}
	for _, role := range roles {
		if err := db.Create(&model.PrincipalRole{PrincipalType: model.PrincipalAdministrator, PrincipalID: "admin-1", RoleID: role.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	scopes, err := NewService(db).DataScopes(context.Background(), model.PrincipalAdministrator, "admin-1")
	if err != nil || len(scopes) != 2 || scopes[0] != "merchant" || scopes[1] != "self" {
		t.Fatalf("scopes=%v err=%v", scopes, err)
	}
}
