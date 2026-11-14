package invitecode

import (
	"context"
	"testing"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/migrate"
	"gorixpay/migrations"
)

// TestMigrationsProvideInviteAndNotificationTables keeps migration 000027 honest.
func TestMigrationsProvideInviteAndNotificationTables(t *testing.T) {
	ctx := context.Background()
	db, _, err := database.Open(ctx, config.DatabaseConfig{Driver: "sqlite", DSN: "file:invitecode-migrations?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := (migrate.Runner{DB: db, FS: migrations.Files}).Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"invite_codes", "merchant_notification_prefs"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("table %s is missing from the migrated schema", table)
		}
	}
	if !db.Migrator().HasColumn("invite_codes", "used_count") {
		t.Fatal("invite_codes.used_count is missing")
	}
	if !db.Migrator().HasColumn("merchant_notification_prefs", "notify_channel") {
		t.Fatal("merchant_notification_prefs.notify_channel is missing")
	}
}
