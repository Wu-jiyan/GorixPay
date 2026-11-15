package epay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	// sqlite is registered by the same driver the GORM sqlite dialector uses.
	// Importing modernc.org/sqlite here would call sql.Register("sqlite") a
	// second time and panic, so the fork is the only safe option in a binary
	// that also opens a GORM sqlite target.
	_ "github.com/glebarez/go-sqlite"
	_ "github.com/go-sql-driver/mysql"
)

// sourceDB reads the legacy Epay schema through database/sql. It is read only:
// the migration never issues a write against the source.
type sourceDB struct {
	db     *sql.DB
	driver string
	tables map[string]bool
	// prefix is the legacy table prefix actually used by this database: upstream
	// 彩虹易支付 ships pre_ but several forks (including the one this was written
	// against) use pay_. Every logical name in this package is written with the
	// upstream prefix and translated by physical().
	prefix string
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func openSource(ctx context.Context, config SourceConfig) (*sourceDB, error) {
	driver := normalizeDriver(config.Driver)
	if driver == "sqlite" {
		if path, isFile := sqliteFilePath(config.DSN); isFile {
			if _, err := os.Stat(path); err != nil {
				return nil, fmt.Errorf("source sqlite database %q is not readable: %w", path, err)
			}
		}
	}
	db, err := sql.Open(driver, strings.TrimSpace(config.DSN))
	if err != nil {
		return nil, fmt.Errorf("open source database: %w", err)
	}
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(4)
		db.SetMaxIdleConns(2)
	}
	pingContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := db.PingContext(pingContext); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to source database: %w", err)
	}
	source := &sourceDB{db: db, driver: driver, tables: map[string]bool{}, prefix: defaultTablePrefix}
	if err := source.loadTables(ctx); err != nil {
		db.Close()
		return nil, err
	}
	source.prefix = detectTablePrefix(source.tables)
	return source, nil
}

// defaultTablePrefix is the upstream 彩虹易支付 prefix, used when nothing better
// can be detected.
const defaultTablePrefix = "pre_"

// knownTablePrefixes are probed in order: the upstream one first, because a
// database left over from the original release should keep its name.
var knownTablePrefixes = []string{"pre_", "pay_"}

// detectTablePrefix picks the prefix whose <prefix>user table exists. Falling
// back to any *_user table covers forks with an owner-specific prefix.
func detectTablePrefix(tables map[string]bool) string {
	for _, candidate := range knownTablePrefixes {
		if tables[candidate+"user"] {
			return candidate
		}
	}
	for name := range tables {
		if strings.HasSuffix(name, "_user") {
			return strings.TrimSuffix(name, "user")
		}
	}
	return defaultTablePrefix
}

// physical translates a logical upstream table name (pre_channel) into the name
// this database actually uses (pay_channel).
func (source *sourceDB) physical(table string) string {
	if source == nil {
		return table
	}
	name := strings.ToLower(strings.TrimSpace(table))
	name = strings.TrimPrefix(name, defaultTablePrefix)
	return source.prefix + name
}

// legacyTablePattern matches a logical upstream table name inside a SQL string.
var legacyTablePattern = regexp.MustCompile(`\bpre_([a-z0-9_]+)`)

// rewrite maps the upstream table names written in the queries of this package
// onto the prefix this database uses, so a fork with pay_* tables works without
// every call site building SQL by hand.
func (source *sourceDB) rewrite(query string) string {
	if source == nil || source.prefix == defaultTablePrefix || source.prefix == "" {
		return query
	}
	return legacyTablePattern.ReplaceAllString(query, source.prefix+"$1")
}

// queryContext runs a read query with the table prefix rewritten.
func (source *sourceDB) queryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return source.db.QueryContext(ctx, source.rewrite(query), args...)
}

// queryRowContext runs a single-row read query with the table prefix rewritten.
func (source *sourceDB) queryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return source.db.QueryRowContext(ctx, source.rewrite(query), args...)
}

func (source *sourceDB) Close() error {
	if source == nil || source.db == nil {
		return nil
	}
	return source.db.Close()
}

// loadTables caches the source table names (lower case) once, so every
// existence check is a map lookup rather than a query.
func (source *sourceDB) loadTables(ctx context.Context) error {
	var (
		query string
		args  []any
	)
	switch source.driver {
	case "sqlite":
		query = `SELECT name FROM sqlite_master WHERE type = 'table'`
	case "mysql":
		query = `SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()`
	default:
		return fmt.Errorf("unsupported source driver %q", source.driver)
	}
	rows, err := source.queryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("list source tables: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan source table name: %w", err)
		}
		source.tables[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list source tables: %w", err)
	}
	return nil
}

func (source *sourceDB) hasTable(name string) bool {
	if source == nil {
		return false
	}
	return source.tables[strings.ToLower(source.physical(name))]
}

// tableNames returns the discovered source tables in a stable order.
func (source *sourceDB) tableNames() []string {
	names := make([]string, 0, len(source.tables))
	for name := range source.tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// columns returns the column names of one legacy table (lower case).
func (source *sourceDB) columns(ctx context.Context, table string) (map[string]bool, error) {
	table = source.physical(table)
	if !identifierPattern.MatchString(table) {
		return nil, fmt.Errorf("invalid table name %q", table)
	}
	result := map[string]bool{}
	switch source.driver {
	case "sqlite":
		rows, err := source.queryContext(ctx, "PRAGMA table_info("+table+")")
		if err != nil {
			return nil, fmt.Errorf("read columns of %s: %w", table, err)
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		nameIndex := indexOf(columns, "name")
		for rows.Next() {
			values := make([]any, len(columns))
			for index := range values {
				values[index] = new(sql.RawBytes)
			}
			if err := rows.Scan(values...); err != nil {
				return nil, err
			}
			if nameIndex >= 0 {
				result[strings.ToLower(string(*values[nameIndex].(*sql.RawBytes)))] = true
			}
		}
		return result, rows.Err()
	case "mysql":
		rows, err := source.queryContext(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ?`, table)
		if err != nil {
			return nil, fmt.Errorf("read columns of %s: %w", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			result[strings.ToLower(name)] = true
		}
		return result, rows.Err()
	default:
		return nil, fmt.Errorf("unsupported source driver %q", source.driver)
	}
}

// hasColumn reports whether the table exposes the column; missing optional
// columns are how older Epay releases differ from newer ones.
func (source *sourceDB) hasColumn(ctx context.Context, table, column string) (bool, error) {
	columns, err := source.columns(ctx, table)
	if err != nil {
		return false, err
	}
	return columns[strings.ToLower(column)], nil
}

// selectStatement builds a SELECT with a fixed column order. Columns that older
// Epay releases do not have are replaced by NULL so the scan code stays stable
// across forks instead of needing a variant per version.
func (source *sourceDB) selectStatement(ctx context.Context, table string, required, optional []string) string {
	parts := make([]string, 0, len(required)+len(optional))
	parts = append(parts, required...)
	for _, column := range optional {
		present, err := source.hasColumn(ctx, table, column)
		if err == nil && present {
			parts = append(parts, column)
			continue
		}
		parts = append(parts, "NULL AS "+column)
	}
	return "SELECT " + strings.Join(parts, ", ") + " FROM " + source.physical(table)
}

// countInt runs a single-value integer query.
func (source *sourceDB) countInt(ctx context.Context, query string, args ...any) (int64, error) {
	var value sql.NullInt64
	if err := source.queryRowContext(ctx, query, args...).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return intValue(value), nil
}

// count returns the row count of one legacy table.
func (source *sourceDB) count(ctx context.Context, table string) (int64, error) {
	table = source.physical(table)
	if !identifierPattern.MatchString(table) {
		return 0, fmt.Errorf("invalid table name %q", table)
	}
	return source.countInt(ctx, "SELECT COUNT(*) FROM "+table)
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if strings.EqualFold(value, target) {
			return index
		}
	}
	return -1
}

// sqliteFilePath extracts a filesystem path from a SQLite DSN. The second return
// value is false for in-memory databases, where existence checks are meaningless.
func sqliteFilePath(dsn string) (string, bool) {
	trimmed := strings.TrimSpace(dsn)
	trimmed = strings.TrimPrefix(trimmed, "file:")
	if index := strings.IndexByte(trimmed, '?'); index >= 0 {
		trimmed = trimmed[:index]
	}
	if trimmed == "" || strings.HasPrefix(trimmed, ":memory:") {
		return "", false
	}
	return trimmed, true
}
