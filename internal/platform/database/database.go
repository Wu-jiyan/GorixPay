package database

import (
	"context"
	"fmt"
	"time"

	"gorixpay/internal/platform/config"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(ctx context.Context, cfg config.DatabaseConfig) (*gorm.DB, Dialect, error) {
	dialect, err := NewDialect(cfg.Driver)
	if err != nil {
		return nil, nil, err
	}
	var dialector gorm.Dialector
	switch cfg.Driver {
	case "sqlite":
		dialector = sqlite.Open(cfg.DSN)
	case "mysql":
		dialector = mysql.Open(cfg.DSN)
	case "postgres":
		dialector = postgres.Open(cfg.DSN)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, nil, fmt.Errorf("open %s database: %w", cfg.Driver, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, err
	}
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	if cfg.Driver == "sqlite" {
		sqlDB.SetMaxOpenConns(1)
		if err := db.WithContext(ctx).Exec("PRAGMA foreign_keys = ON").Error; err != nil {
			_ = sqlDB.Close()
			return nil, nil, err
		}
		if err := db.WithContext(ctx).Exec("PRAGMA busy_timeout = 5000").Error; err != nil {
			_ = sqlDB.Close()
			return nil, nil, err
		}
	} else {
		sqlDB.SetMaxOpenConns(25)
		sqlDB.SetMaxIdleConns(5)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("ping %s database: %w", cfg.Driver, err)
	}
	return db, dialect, nil
}
