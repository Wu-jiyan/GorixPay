package oauth

import (
	"context"
	"testing"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/migrate"
	"gorixpay/migrations"
)

// TestMigrationsProvideOAuthTables keeps migration 000025 honest.
func TestMigrationsProvideOAuthTables(t *testing.T) {
	ctx := context.Background()
	db, _, err := database.Open(ctx, config.DatabaseConfig{Driver: "sqlite", DSN: "file:oauth-migrations?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := (migrate.Runner{DB: db, FS: migrations.Files}).Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"oauth_bindings", "qr_login_sessions", "oauth_states"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("table %s is missing from the migrated schema", table)
		}
	}
	if !db.Migrator().HasColumn("oauth_bindings", "union_id") {
		t.Fatal("oauth_bindings.union_id is missing")
	}
	if !db.Migrator().HasIndex("qr_login_sessions", "idx_qr_login_sessions_expires") {
		t.Fatal("qr_login_sessions expiry index is missing")
	}
}
