package epay

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixturePath resolves tests/fixtures/epay/<name> from this package directory.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "tests", "fixtures", "epay", name)
}

// newSourceFixture materialises the Epay fixture in a temporary SQLite database
// and returns its DSN (a forward-slash path, which is the portable DSN form).
func newSourceFixture(t *testing.T) string {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "epay-source.db"))
	content, err := os.ReadFile(fixturePath(t, "epay_fixture.sql"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	execScript(t, path, string(content))
	return path
}

func execScript(t *testing.T, dsn, script string) {
	t.Helper()
	db := mustOpenDB(t, dsn)
	defer db.Close()
	for _, statement := range splitSQL(script) {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("执行 fixture 语句失败: %v\n%s", err, statement)
		}
	}
}

// mustOpenDB opens a database/sql handle with the sqlite driver registered by
// source.go (glebarez/go-sqlite), which is also the driver GORM uses.
func mustOpenDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("打开 %s 失败: %v", dsn, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("连接 %s 失败: %v", dsn, err)
	}
	return db
}

// splitSQL splits the fixture on ";\n". The fixture never contains a semicolon
// inside a string literal, so this is enough and keeps the helper readable.
func splitSQL(script string) []string {
	statements := []string{}
	for _, chunk := range strings.Split(script, ";\n") {
		if strings.TrimSpace(stripSQLComments(chunk)) == "" {
			continue
		}
		statements = append(statements, chunk)
	}
	return statements
}

func stripSQLComments(chunk string) string {
	var builder strings.Builder
	for _, line := range strings.Split(chunk, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	return builder.String()
}

func execOn(t *testing.T, dsn string, statements ...string) {
	t.Helper()
	db := mustOpenDB(t, dsn)
	defer db.Close()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("执行 %q 失败: %v", statement, err)
		}
	}
}

func queryInt(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var value sql.NullInt64
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("查询 %q 失败: %v", query, err)
	}
	return intValue(value)
}

func queryString(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	var value sql.NullString
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("查询 %q 失败: %v", query, err)
	}
	return value.String
}

func countRows(t *testing.T, db *sql.DB, table string, where string, args ...any) int64 {
	t.Helper()
	query := "SELECT COUNT(*) FROM " + table
	if where != "" {
		query += " WHERE " + where
	}
	return queryInt(t, db, query, args...)
}
