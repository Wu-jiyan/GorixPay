package security

import (
	"context"
	"testing"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/migrate"
	"gorixpay/migrations"
)

// TestMigrationsProvideSecurityTables keeps migration 000022 honest: the schema
// contract this package depends on must survive future migration numbers.
func TestMigrationsProvideSecurityTables(t *testing.T) {
	ctx := context.Background()
	db, _, err := database.Open(ctx, config.DatabaseConfig{Driver: "sqlite", DSN: "file:security-migrations?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := (migrate.Runner{DB: db, FS: migrations.Files}).Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"admin_totp", "login_attempts", "captcha_challenges"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("table %s is missing from the migrated schema", table)
		}
	}
	if !db.Migrator().HasIndex("login_attempts", "idx_login_attempts_identifier_created") {
		t.Fatal("login_attempts identifier index is missing")
	}
	if !db.Migrator().HasColumn("admin_totp", "secret_encrypted") {
		t.Fatal("admin_totp.secret_encrypted is missing")
	}
}
