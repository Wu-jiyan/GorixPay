package migrate

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

func TestSplitStatementsRespectsQuotesAndComments(t *testing.T) {
	content := `-- 000001: header comment; with a semicolon
CREATE TABLE alpha (id INTEGER PRIMARY KEY, label TEXT DEFAULT 'semi;colon');

/* block comment; still a comment */
CREATE TABLE beta (
    id INTEGER PRIMARY KEY,
    note TEXT
);

CREATE INDEX idx_alpha_label ON alpha(label);
`
	statements := splitStatements(content)
	if len(statements) != 3 {
		t.Fatalf("statements = %d, want 3: %#v", len(statements), statements)
	}
	if !strings.HasPrefix(statements[0], "CREATE TABLE alpha") || !strings.Contains(statements[0], "'semi;colon'") {
		t.Fatalf("first statement = %q", statements[0])
	}
	if !strings.HasPrefix(statements[1], "CREATE TABLE beta") {
		t.Fatalf("second statement = %q", statements[1])
	}
	if !strings.HasPrefix(statements[2], "CREATE INDEX idx_alpha_label") {
		t.Fatalf("third statement = %q", statements[2])
	}
	for _, statement := range statements {
		if strings.Contains(statement, "--") || strings.Contains(statement, "/*") {
			t.Fatalf("comment leaked into statement %q", statement)
		}
	}
}

// TestRunnerAppliesMultiStatementFile covers the shape MySQL could not handle:
// one file, several statements, and a driver that refuses multi statement Exec.
func TestRunnerAppliesMultiStatementFile(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite", DSN: "file:migrate-multi?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{DB: db, FS: fstest.MapFS{
		"000001_multi.sql": {Data: []byte("CREATE TABLE multi_one (id INTEGER PRIMARY KEY);\nCREATE TABLE multi_two (id INTEGER PRIMARY KEY);\n")},
	}}
	if err := runner.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable("multi_one") || !db.Migrator().HasTable("multi_two") {
		t.Fatal("multi statement migration did not create both tables")
	}
}

// TestRunnerUpIsSafeWhenTwoInstancesStartTogether reproduces the crash the audit
// found: two runners on the same database raced on the schema_migrations primary
// key and one instance exited. Both must now succeed.
func TestRunnerUpIsSafeWhenTwoInstancesStartTogether(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "concurrent.db"))
	open := func() *gorm.DB {
		db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: dsn})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pool.Close() })
		return db
	}
	first, second := open(), open()
	files := fstest.MapFS{
		"000001_race.sql": {Data: []byte("CREATE TABLE race_items (id INTEGER PRIMARY KEY, name TEXT);\n")},
		"000002_race.sql": {Data: []byte("CREATE INDEX idx_race_name ON race_items(name);\n")},
	}
	start := make(chan struct{})
	errs := make([]error, 2)
	var wait sync.WaitGroup
	for index, db := range []*gorm.DB{first, second} {
		wait.Add(1)
		go func(index int, db *gorm.DB) {
			defer wait.Done()
			<-start
			errs[index] = (Runner{DB: db, FS: files}).Up(context.Background())
		}(index, db)
	}
	close(start)
	wait.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("runner %d failed: %v", index, err)
		}
	}
	var applied int64
	if err := first.Table("schema_migrations").Count(&applied).Error; err != nil {
		t.Fatal(err)
	}
	if applied != 2 {
		t.Fatalf("applied migrations = %d, want 2", applied)
	}
	if !first.Migrator().HasTable("race_items") {
		t.Fatal("table missing after concurrent migration")
	}
}

func TestIsDuplicateRecordRecognisesDialects(t *testing.T) {
	cases := []struct {
		message string
		want    bool
	}{
		{"UNIQUE constraint failed: schema_migrations.version", true},
		{"ERROR: duplicate key value violates unique constraint \"schema_migrations_pkey\"", true},
		{"Error 1062 (23000): Duplicate entry '000001' for key 'schema_migrations.PRIMARY'", true},
		{"UNIQUE constraint failed: payment_orders.trade_no", false},
		{"no such table: schema_migrations", false},
	}
	for _, testCase := range cases {
		if got := isDuplicateRecord(errString(testCase.message)); got != testCase.want {
			t.Fatalf("isDuplicateRecord(%q) = %v, want %v", testCase.message, got, testCase.want)
		}
	}
}

type errString string

func (value errString) Error() string { return string(value) }
