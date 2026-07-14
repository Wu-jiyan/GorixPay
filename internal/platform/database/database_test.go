package database

import (
	"context"
	"testing"

	"gorixpay/internal/platform/config"
)

func TestOpenSQLiteAndDetectDialect(t *testing.T) {
	db, dialect, err := Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file::memory:?cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if dialect.Name() != "sqlite" || dialect.BindVar(3) != "?" || dialect.SupportsSkipLocked() {
		t.Fatalf("unexpected SQLite dialect: %#v", dialect)
	}
	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestDialectsExposeDatabaseDifferences(t *testing.T) {
	tests := []struct {
		name       string
		bindThird  string
		skipLocked bool
	}{
		{name: "sqlite", bindThird: "?", skipLocked: false},
		{name: "mysql", bindThird: "?", skipLocked: false},
		{name: "postgres", bindThird: "$3", skipLocked: true},
	}
	for _, test := range tests {
		dialect, err := NewDialect(test.name)
		if err != nil {
			t.Fatal(err)
		}
		if dialect.BindVar(3) != test.bindThird || dialect.SupportsSkipLocked() != test.skipLocked {
			t.Errorf("unexpected %s dialect", test.name)
		}
	}
	if _, err := NewDialect("oracle"); err == nil {
		t.Fatal("unsupported dialect accepted")
	}
}
