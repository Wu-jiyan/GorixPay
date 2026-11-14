package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gorixpay/internal/platform/config"
)

func TestSQLiteDSNAddsPragmas(t *testing.T) {
	cases := []struct {
		dsn  string
		want []string
	}{
		{"gorixpay.db", []string{"?_pragma=foreign_keys(1)", "&_pragma=busy_timeout(5000)"}},
		{"file:gorixpay.db?mode=memory&cache=shared", []string{"&_pragma=foreign_keys(1)", "&_pragma=busy_timeout(5000)"}},
		{"file:x.db?_pragma=foreign_keys(1)", []string{"&_pragma=busy_timeout(5000)"}},
		{"file:x.db?_pragma=busy_timeout(9000)", []string{"&_pragma=foreign_keys(1)"}},
		{"file:x.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", nil},
		{"file:x.db?", []string{"_pragma=foreign_keys(1)"}},
	}
	for _, item := range cases {
		got := sqliteDSN(item.dsn)
		if len(item.want) == 0 {
			if got != item.dsn {
				t.Fatalf("dsn %q should be unchanged, got %q", item.dsn, got)
			}
			continue
		}
		for _, fragment := range item.want {
			if !strings.Contains(got, fragment) {
				t.Fatalf("dsn %q -> %q missing %q", item.dsn, got, fragment)
			}
		}
		if strings.Count(got, "foreign_keys") != 1 || strings.Count(got, "busy_timeout") != 1 {
			t.Fatalf("dsn %q -> %q duplicated a pragma", item.dsn, got)
		}
	}
}

// TestSQLiteForeignKeysSurviveConnectionRecycle reproduces the audit finding:
// the pragma used to be set on one connection only, so after the pool replaced
// that connection foreign_keys dropped back to 0 and orphan rows were accepted.
func TestSQLiteForeignKeysSurviveConnectionRecycle(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "pragma.db")
	db, _, err := Open(ctx, config.DatabaseConfig{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.Exec("CREATE TABLE parents (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE children (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL, FOREIGN KEY (parent_id) REFERENCES parents(id))").Error; err != nil {
		t.Fatal(err)
	}
	// Force the pool to drop the connection that opened the database: with no
	// idle connections, every following query runs on a brand new connection.
	sqlDB.SetMaxIdleConns(0)
	for attempt := 0; attempt < 3; attempt++ {
		var foreignKeys int
		if err := db.WithContext(ctx).Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 {
			t.Fatalf("attempt %d: foreign_keys=%d after connection recycle", attempt, foreignKeys)
		}
	}
	var busyTimeout int
	if err := db.WithContext(ctx).Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout=%d want 5000", busyTimeout)
	}
	if err := db.Exec("INSERT INTO children (parent_id) VALUES (42)").Error; err == nil {
		t.Fatal("orphan row was accepted, foreign keys are not enforced on the recycled connection")
	}
	if err := db.Exec("INSERT INTO parents (id) VALUES (42)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO children (parent_id) VALUES (42)").Error; err != nil {
		t.Fatalf("valid child rejected: %v", err)
	}
}
