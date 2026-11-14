package merchantgroup

import (
	"context"
	"testing"
	"time"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/migrate"
	"gorixpay/migrations"
)

// TestMigrationAddsGroupSchema runs the real migration runner over the real
// migration files, so a dialect mistake in 000017 fails here instead of at
// startup.
func TestMigrationAddsGroupSchema(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    "file:group-migration?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	runner := migrate.Runner{DB: db, FS: migrations.Files}
	if err := runner.Up(context.Background()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	for _, column := range []string{"id", "name", "rate_bp", "pay_types", "settle_mode", "status", "created_at", "updated_at"} {
		if !db.Migrator().HasColumn("merchant_groups", column) {
			t.Fatalf("merchant_groups is missing column %q", column)
		}
	}
	for _, column := range []string{"group_id", "settle_mode"} {
		if !db.Migrator().HasColumn("merchants", column) {
			t.Fatalf("merchants is missing column %q", column)
		}
	}

	now := time.Date(2026, 11, 14, 9, 30, 0, 0, time.UTC)
	if err := db.Exec(
		`INSERT INTO merchants (id, display_name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"merchant-1", "迁移测试商户", "active", now, now).Error; err != nil {
		t.Fatalf("seed merchant against the real schema: %v", err)
	}
	service := New(db, func() time.Time { return now })
	ctx := context.Background()
	group, err := service.Create(ctx, Input{Name: "迁移验证组", RateBp: 42, PayTypes: []string{"alipay"}, SettleMode: "direct"})
	if err != nil {
		t.Fatalf("create against the real schema: %v", err)
	}
	// pay_types must survive the TEXT column round trip.
	var reread []Group
	if err := db.Find(&reread, "id = ?", group.ID).Error; err != nil {
		t.Fatalf("reread: %v", err)
	}
	if len(reread) != 1 || len(reread[0].PayTypes) != 1 || reread[0].PayTypes[0] != "alipay" {
		t.Fatalf("pay_types round trip = %#v", reread)
	}
	if _, err := service.AssignMerchant(ctx, "merchant-1", &group.ID); err != nil {
		t.Fatalf("assign against the real schema: %v", err)
	}
	if rate, ok := service.RateBp(ctx, "merchant-1"); !ok || rate != 42 {
		t.Fatalf("RateBp against the real schema = (%d,%v)", rate, ok)
	}

	// Re-running must be a no-op (checksum match, no duplicate column errors).
	if err := runner.Up(context.Background()); err != nil {
		t.Fatalf("second migration run: %v", err)
	}
}
