package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
		dialector = sqlite.Open(sqliteDSN(cfg.DSN))
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
		// The DSN pragmas apply to every new connection, so the guarantee holds
		// after the pool recycles a connection. This check only proves the first
		// connection got them; failing loudly beats silently losing the foreign
		// keys 30 minutes into a deployment.
		var foreignKeys int
		if err := db.WithContext(ctx).Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
			_ = sqlDB.Close()
			return nil, nil, err
		}
		if foreignKeys != 1 {
			_ = sqlDB.Close()
			return nil, nil, errors.New("sqlite foreign_keys pragma is not enabled")
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

// sqliteDSN appends the connection pragmas the driver applies to every new
// connection (glebarez/go-sqlite parses _pragma from the DSN). A plain
// connection-level Exec only covers the one connection it ran on, which is how
// foreign key enforcement used to disappear after the pool recycled a handle.
func sqliteDSN(dsn string) string {
	clauses := make([]string, 0, 2)
	lower := strings.ToLower(dsn)
	if !strings.Contains(lower, "foreign_keys") {
		clauses = append(clauses, "_pragma=foreign_keys(1)")
	}
	if !strings.Contains(lower, "busy_timeout") {
		clauses = append(clauses, "_pragma=busy_timeout(5000)")
	}
	if len(clauses) == 0 {
		return dsn
	}
	separator := "?"
	switch {
	case strings.Contains(dsn, "?"):
		separator = "&"
	case strings.HasSuffix(dsn, "&"), strings.HasSuffix(dsn, "?"):
		separator = ""
	}
	return dsn + separator + strings.Join(clauses, "&")
}
