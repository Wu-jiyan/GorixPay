package epay

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/migrate"
	"gorixpay/migrations"

	"gorm.io/gorm"
)

// targetStore owns the GorixPay connection. writable is false for dry runs, in
// which case the store either stays nil (target does not exist yet) or is used
// read only to show what is already migrated.
type targetStore struct {
	db       *gorm.DB
	driver   string
	writable bool
	applied  int
	note     string
}

// openTargetStore opens the target through the platform database factory so the
// migration inherits the exact dialect handling (pragmas, pool sizing) the
// application uses.
func openTargetStore(ctx context.Context, cfg TargetConfig, writable bool) (*targetStore, error) {
	driver := normalizeDriver(cfg.Driver)
	db, _, err := database.Open(ctx, config.DatabaseConfig{Driver: driver, DSN: strings.TrimSpace(cfg.DSN)})
	if err != nil {
		return nil, fmt.Errorf("open target database: %w", err)
	}
	return &targetStore{db: db, driver: driver, writable: writable}, nil
}

func (target *targetStore) Close() error {
	if target == nil || target.db == nil {
		return nil
	}
	sqlDB, err := target.db.DB()
	if err != nil || sqlDB == nil {
		return nil
	}
	return sqlDB.Close()
}

func (target *targetStore) hasTable(name string) bool {
	if target == nil || target.db == nil {
		return false
	}
	return target.db.Migrator().HasTable(name)
}

func (target *targetStore) hasColumn(table, column string) bool {
	if target == nil || target.db == nil {
		return false
	}
	return target.db.Migrator().HasColumn(table, column)
}

func (target *targetStore) count(ctx context.Context, table string) (int64, error) {
	if target == nil || target.db == nil {
		return 0, nil
	}
	var total int64
	if err := target.db.WithContext(ctx).Table(table).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// applyMigrations runs the project's own runner so the target schema is the
// same one the application expects, then reports how many versions were new.
func (target *targetStore) applyMigrations(ctx context.Context) error {
	if target == nil || target.db == nil || !target.writable {
		return nil
	}
	before, err := target.migrationCount(ctx)
	if err != nil {
		return err
	}
	if err := (migrate.Runner{DB: target.db, FS: migrations.Files}).Up(ctx); err != nil {
		return fmt.Errorf("apply target migrations: %w", err)
	}
	after, err := target.migrationCount(ctx)
	if err != nil {
		return err
	}
	target.applied = int(after - before)
	return nil
}

func (target *targetStore) migrationCount(ctx context.Context) (int64, error) {
	var total int64
	if !target.hasTable("schema_migrations") {
		return 0, nil
	}
	if err := target.db.WithContext(ctx).Table("schema_migrations").Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// auxTables are the ecosystem tables from the plan's migration 000012. When that
// migration is present the statements below are no-ops; when it is not, the
// migration still lands the domain and blacklist rows instead of dropping them.
// The definitions match the plan so a later 000012 sees the expected shape.
var auxTables = []string{
	`CREATE TABLE IF NOT EXISTS merchant_domains (
    id VARCHAR(36) PRIMARY KEY,
    merchant_id VARCHAR(36) NOT NULL,
    domain VARCHAR(191) NOT NULL,
    status VARCHAR(20) NOT NULL,
    rejection_reason VARCHAR(255),
    reviewer_id VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    reviewed_at TIMESTAMP,
    UNIQUE (merchant_id, domain)
)`,
	`CREATE TABLE IF NOT EXISTS blacklist_entries (
    id VARCHAR(36) PRIMARY KEY,
    entry_type VARCHAR(20) NOT NULL,
    value VARCHAR(191) NOT NULL,
    remark VARCHAR(191),
    created_at TIMESTAMP NOT NULL,
    UNIQUE (entry_type, value)
)`,
}

func (target *targetStore) ensureAuxTables(ctx context.Context) error {
	if target == nil || target.db == nil || !target.writable {
		return nil
	}
	for _, statement := range auxTables {
		if err := target.db.WithContext(ctx).Exec(statement).Error; err != nil {
			return fmt.Errorf("ensure auxiliary table: %w", err)
		}
	}
	return nil
}

// probeTarget opens the target read only for a dry run. A missing SQLite file or
// an unreachable server is not an error: the report simply states that the
// target was never touched.
func probeTarget(ctx context.Context, cfg TargetConfig) (*targetStore, string) {
	driver := normalizeDriver(cfg.Driver)
	if driver == "sqlite" {
		path, isFile := sqliteFilePath(cfg.DSN)
		if !isFile {
			return nil, "目标为内存库，dry-run 未连接（不会写入）"
		}
		if _, err := os.Stat(path); err != nil {
			return nil, "目标库文件不存在，dry-run 未创建"
		}
	}
	probeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	store, err := openTargetStore(probeContext, cfg, false)
	if err != nil {
		return nil, "目标库不可访问：" + err.Error()
	}
	if !store.hasTable("merchants") {
		store.Close()
		return nil, "目标库尚未初始化（缺少 merchants 表），dry-run 未执行 migrations"
	}
	return store, ""
}
