package migrate

import (
	"context"
	"testing"
	"testing/fstest"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
)

func TestRunnerAppliesMigrationsIdempotently(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file::memory:?cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{DB: db, FS: fstest.MapFS{
		"000001_test.sql": {Data: []byte("CREATE TABLE test_items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")},
	}}
	if err := runner.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Table("schema_migrations").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 || !db.Migrator().HasTable("test_items") {
		t.Fatalf("migration count = %d, table exists = %v", count, db.Migrator().HasTable("test_items"))
	}
}

func TestRunnerRejectsChecksumDrift(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:drift?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{DB: db, FS: fstest.MapFS{
		"000001_test.sql": {Data: []byte("CREATE TABLE drift_items (id INTEGER PRIMARY KEY);")},
	}}
	if err := runner.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.FS = fstest.MapFS{
		"000001_test.sql": {Data: []byte("CREATE TABLE drift_items (id INTEGER PRIMARY KEY, changed TEXT);")},
	}
	if err := runner.Up(context.Background()); err == nil {
		t.Fatal("checksum drift accepted")
	}
}
