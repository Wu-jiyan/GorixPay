package seed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

func seedDatabase(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Administrator{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestEnsureAdministratorCreatesThenResetsThePassword(t *testing.T) {
	db := seedDatabase(t, "seed-create")
	ctx := context.Background()

	created, err := EnsureAdministrator(ctx, db, Administrator{Username: "admin", Password: "first-password"})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first call did not report a creation")
	}
	var first identitymodel.Administrator
	if err := db.First(&first, "username = ?", "admin").Error; err != nil {
		t.Fatal(err)
	}
	if first.DisplayName != "admin" || first.Status != identitymodel.StatusActive {
		t.Fatalf("administrator = %+v", first)
	}
	if first.PasswordHash == "first-password" {
		t.Fatal("password stored in the clear")
	}

	// A second call with the same username must reset, not duplicate.
	created, err = EnsureAdministrator(ctx, db, Administrator{Username: "admin", Password: "second-password", DisplayName: "运营"})
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second call reported a creation")
	}
	var count int64
	if err := db.Model(&identitymodel.Administrator{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("administrators = %d err=%v", count, err)
	}
	var second identitymodel.Administrator
	if err := db.First(&second, "username = ?", "admin").Error; err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("id changed: %s -> %s", first.ID, second.ID)
	}
	if second.PasswordHash == first.PasswordHash {
		t.Fatal("password hash was not replaced")
	}
	if second.DisplayName != "运营" {
		t.Fatalf("display name = %q", second.DisplayName)
	}
}

func TestEnsureAdministratorRejectsWeakOrMissingInput(t *testing.T) {
	db := seedDatabase(t, "seed-invalid")
	ctx := context.Background()
	cases := []Administrator{
		{Username: "", Password: "long-enough-password"},
		{Username: "admin", Password: ""},
		{Username: "admin", Password: "short"},
		{Username: strings.Repeat("a", 101), Password: "long-enough-password"},
	}
	for _, input := range cases {
		if _, err := EnsureAdministrator(ctx, db, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("input %+v err = %v, want ErrInvalidInput", input, err)
		}
	}
	var count int64
	if err := db.Model(&identitymodel.Administrator{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("administrators written for invalid input: %d err=%v", count, err)
	}
	if _, err := EnsureAdministrator(ctx, nil, Administrator{Username: "admin", Password: "long-enough-password"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil database err = %v", err)
	}
}

func TestEnsureAdministratorStampsTheClock(t *testing.T) {
	db := seedDatabase(t, "seed-clock")
	fixed := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	original := now
	now = func() time.Time { return fixed }
	defer func() { now = original }()

	if _, err := EnsureAdministrator(context.Background(), db, Administrator{Username: "clock", Password: "long-enough-password"}); err != nil {
		t.Fatal(err)
	}
	var account identitymodel.Administrator
	if err := db.First(&account, "username = ?", "clock").Error; err != nil {
		t.Fatal(err)
	}
	if !account.CreatedAt.Equal(fixed) || !account.UpdatedAt.Equal(fixed) {
		t.Fatalf("timestamps = %v / %v", account.CreatedAt, account.UpdatedAt)
	}
}
