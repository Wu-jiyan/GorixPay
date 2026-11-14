package notify

import (
	"context"
	"testing"
	"time"

	"gorixpay/internal/identity/password"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/migrate"
	"gorixpay/migrations"
)

// TestMigrationAddsNotificationSchema runs the real migration runner over the
// real migration files, so a dialect mistake in 000021 fails here instead of at
// startup, and proves the package works against the production schema.
func TestMigrationAddsNotificationSchema(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    "file:notify-migration?mode=memory&cache=shared",
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

	expected := map[string][]string{
		"notification_channels": {"id", "kind", "provider", "config_encrypted", "status", "created_at", "updated_at"},
		"verify_codes":          {"id", "scene", "target", "code_hash", "expires_at", "attempts", "consumed_at", "created_at"},
		"password_resets":       {"id", "principal_type", "principal_id", "token_hash", "expires_at", "consumed_at", "created_at"},
	}
	for table, columns := range expected {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("table %q was not created", table)
		}
		for _, column := range columns {
			if !db.Migrator().HasColumn(table, column) {
				t.Fatalf("%s is missing column %q", table, column)
			}
		}
	}
	for _, index := range []string{"idx_verify_codes_scene_target", "idx_password_resets_token_hash", "idx_password_resets_principal"} {
		if !db.Migrator().HasIndex(indexTable(index), index) {
			// HasIndex needs the table; the helper maps the known names below.
			t.Fatalf("index %q missing", index)
		}
	}

	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	hash, err := password.Hash("old-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db.Exec(`INSERT INTO merchants (id, email, password_hash, display_name, status, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)`, "merchant-1", "shop@example.com", hash, "迁移测试商户", "active", now, now).Error; err != nil {
		t.Fatalf("seed merchant against the real schema: %v", err)
	}
	if err := db.Exec(`INSERT INTO auth_sessions (id, principal_type, principal_id, token_hash, expires_at, created_at)
        VALUES (?, ?, ?, ?, ?, ?)`, "session-1", principalTypeMerchant, "merchant-1", "token-hash-1", now.Add(time.Hour), now).Error; err != nil {
		t.Fatalf("seed session against the real schema: %v", err)
	}

	sender := &captureSender{}
	source := &clock{at: now}
	service, err := NewService(Config{DB: db, Cipher: testCipher(t), Sender: sender, Now: source.Now})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	if _, err := service.SendVerifyCode(ctx, SceneResetPassword, "shop@example.com"); err != nil {
		t.Fatalf("send code against the real schema: %v", err)
	}
	if len(sender.mail) != 1 {
		t.Fatalf("mail = %+v", sender.mail)
	}
	if err := service.VerifyCode(ctx, SceneResetPassword, "shop@example.com", codeFromMail(t, sender.mail[0].Body)); err != nil {
		t.Fatalf("verify code against the real schema: %v", err)
	}
	channel, err := service.CreateChannel(ctx, ChannelInput{Kind: KindMail, Provider: "smtp", Config: map[string]string{"host": "smtp.example.com"}})
	if err != nil {
		t.Fatalf("create channel against the real schema: %v", err)
	}
	if _, err := service.TestChannel(ctx, channel.ID); err != nil {
		t.Fatalf("test channel against the real schema: %v", err)
	}
	if _, err := service.RequestPasswordReset(ctx, "shop@example.com"); err != nil {
		t.Fatalf("request reset against the real schema: %v", err)
	}
	if live := liveSessions(t, db, "merchant-1"); live != 1 {
		t.Fatalf("live sessions = %d, want 1 before the reset completes", live)
	}

	// Re-running must be a no-op (checksum match, no duplicate index errors).
	if err := runner.Up(context.Background()); err != nil {
		t.Fatalf("second migration run: %v", err)
	}
}

// indexTable maps the index names created by 000021 onto their table.
func indexTable(index string) string {
	switch index {
	case "idx_verify_codes_scene_target":
		return "verify_codes"
	default:
		return "password_resets"
	}
}
