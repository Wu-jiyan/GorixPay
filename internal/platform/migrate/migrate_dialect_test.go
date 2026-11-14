package migrate

import (
	"strings"
	"testing"
)

func TestPrepareMigrationDropsIndexGuardForMySQLOnly(t *testing.T) {
	input := "CREATE INDEX IF NOT EXISTS idx_a ON t(a);\nCREATE UNIQUE INDEX IF NOT EXISTS idx_b ON t(b);"
	mysql := prepareMigration("mysql", input)
	if strings.Contains(strings.ToUpper(mysql), "IF NOT EXISTS") {
		t.Fatalf("mysql migration still contains index guard: %s", mysql)
	}
	if mysql != "CREATE INDEX idx_a ON t(a);\nCREATE UNIQUE INDEX idx_b ON t(b);" {
		t.Fatalf("unexpected mysql rewrite: %s", mysql)
	}
	for _, dialect := range []string{"sqlite", "postgres"} {
		if prepareMigration(dialect, input) != input {
			t.Fatalf("%s migration must stay untouched", dialect)
		}
	}
}

// TestPrepareMigrationStripsUTF8BOM pins the MySQL 5.7 failure found on a real
// server: one migration file started with a BOM and 5.7 rejected it with a syntax
// error while SQLite accepted it.
func TestPrepareMigrationStripsUTF8BOM(t *testing.T) {
	input := "\ufeffCREATE TABLE bom_items (id INTEGER PRIMARY KEY);"
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		got := prepareMigration(dialect, input)
		if strings.HasPrefix(got, "\ufeff") || !strings.HasPrefix(got, "CREATE TABLE bom_items") {
			t.Fatalf("%s: %q", dialect, got)
		}
	}
	// The MySQL index rewrite still happens after the BOM is gone.
	withIndex := "\ufeffCREATE INDEX IF NOT EXISTS idx_bom ON bom_items(id);"
	if got := prepareMigration("mysql", withIndex); got != "CREATE INDEX idx_bom ON bom_items(id);" {
		t.Fatalf("mysql rewrite after BOM strip = %q", got)
	}
}
