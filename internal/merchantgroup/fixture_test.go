package merchantgroup

import (
	"context"
	"testing"
	"time"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

// fixedNow keeps timestamps deterministic so assertions can compare them.
var fixedNow = time.Date(2026, 11, 14, 9, 30, 0, 0, time.UTC)

// groupFixture builds the service on a real SQLite database with only the
// columns this package owns. The migration test covers the full schema.
func groupFixture(t *testing.T, name string) (*Service, *gorm.DB) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    "file:" + name + "?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&Group{}, &merchantRow{}); err != nil {
		t.Fatalf("migrate fixture: %v", err)
	}
	return New(db, func() time.Time { return fixedNow }), db
}

func seedMerchant(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	if err := db.Exec("INSERT INTO merchants (id, settle_mode) VALUES (?, ?)", id, string(SettleModeNormal)).Error; err != nil {
		t.Fatalf("seed merchant %s: %v", id, err)
	}
}

func merchantSettleMode(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var mode string
	if err := db.Raw("SELECT settle_mode FROM merchants WHERE id = ?", id).Scan(&mode).Error; err != nil {
		t.Fatalf("read settle_mode of %s: %v", id, err)
	}
	return mode
}

func merchantGroupID(t *testing.T, db *gorm.DB, id string) *string {
	t.Helper()
	var value *string
	if err := db.Raw("SELECT group_id FROM merchants WHERE id = ?", id).Scan(&value).Error; err != nil {
		t.Fatalf("read group_id of %s: %v", id, err)
	}
	return value
}
