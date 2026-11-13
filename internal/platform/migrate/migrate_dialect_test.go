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
