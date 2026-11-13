package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"time"

	"gorm.io/gorm"
)

type Runner struct {
	DB *gorm.DB
	FS fs.FS
}

type migrationRecord struct {
	Version   string    `gorm:"primaryKey;size:255"`
	Checksum  string    `gorm:"size:64;not null"`
	AppliedAt time.Time `gorm:"not null"`
}

func (migrationRecord) TableName() string {
	return "schema_migrations"
}

func (r Runner) Up(ctx context.Context) error {
	if r.DB == nil || r.FS == nil {
		return fmt.Errorf("database and migration filesystem are required")
	}
	if err := r.DB.WithContext(ctx).AutoMigrate(&migrationRecord{}); err != nil {
		return err
	}
	entries, err := fs.ReadDir(r.FS, ".")
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && path.Ext(entry.Name()) == ".sql" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		content, err := fs.ReadFile(r.FS, name)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(content)
		checksum := hex.EncodeToString(digest[:])
		var existing migrationRecord
		err = r.DB.WithContext(ctx).Where("version = ?", name).First(&existing).Error
		if err == nil {
			if existing.Checksum != checksum {
				return fmt.Errorf("migration %s checksum drift", name)
			}
			continue
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		if err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(prepareMigration(tx.Dialector.Name(), string(content))).Error; err != nil {
				return fmt.Errorf("apply migration %s: %w", name, err)
			}
			return tx.Create(&migrationRecord{Version: name, Checksum: checksum, AppliedAt: time.Now().UTC()}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

var mysqlIndexGuard = regexp.MustCompile(`(?i)CREATE\s+(UNIQUE\s+)?INDEX\s+IF\s+NOT\s+EXISTS`)

// prepareMigration rewrites statements MySQL cannot parse. MySQL has no
// CREATE INDEX IF NOT EXISTS, and migrations run once per version, so the guard
// can be dropped safely for that dialect only.
func prepareMigration(dialect, content string) string {
	if dialect != "mysql" {
		return content
	}
	return mysqlIndexGuard.ReplaceAllString(content, "CREATE ${1}INDEX")
}
