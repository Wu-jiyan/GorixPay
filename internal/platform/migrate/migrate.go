package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// Locking constants. The advisory lock exists so two instances that start at the
// same time do not both try to apply 000001: the loser used to fail with a
// duplicate key on schema_migrations and exit.
const (
	postgresAdvisoryLockKey = int64(0x676f726978) // "gorix"
	mysqlLockName           = "gorixpay_schema_migrate"
	mysqlLockTimeoutSeconds = 120
)

// errAlreadyApplied reports that another instance recorded the same version
// first. It is not an error for the caller: the schema is in the desired state.
var errAlreadyApplied = errors.New("migration already applied by another instance")

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
	names, err := r.migrationNames()
	if err != nil {
		return err
	}
	dialect := r.DB.Dialector.Name()
	// The lock comes before the bookkeeping table: two instances that start
	// together used to race on creating schema_migrations itself.
	release, err := r.acquireLock(ctx, dialect)
	if err != nil {
		return err
	}
	defer release()
	if err := r.ensureRecordsTable(ctx); err != nil {
		return err
	}
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
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := r.apply(ctx, dialect, name, checksum, content); err != nil {
			if errors.Is(err, errAlreadyApplied) {
				continue
			}
			// A concurrent instance may have finished this version while we were
			// failing. Its row proves the schema is at that version, so adopt it
			// instead of taking the process down.
			var applied migrationRecord
			if lookupErr := r.DB.WithContext(ctx).Where("version = ?", name).First(&applied).Error; lookupErr == nil {
				continue
			}
			return err
		}
	}
	return nil
}

// ensureRecordsTable creates schema_migrations. Two instances starting together
// can both see a missing table; the loser's "already exists" is not an error
// because the table is there either way.
func (r Runner) ensureRecordsTable(ctx context.Context) error {
	if err := r.DB.WithContext(ctx).AutoMigrate(&migrationRecord{}); err != nil {
		if r.DB.Migrator().HasTable(&migrationRecord{}) {
			return nil
		}
		return err
	}
	return nil
}

func (r Runner) migrationNames() ([]string, error) {
	entries, err := fs.ReadDir(r.FS, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && path.Ext(entry.Name()) == ".sql" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// apply runs one migration file and records its version.
//
// PostgreSQL and SQLite have transactional DDL, so the statements and the
// bookkeeping row commit together and a failure leaves nothing behind. MySQL
// commits DDL implicitly: rolling back is impossible, so the statements run one
// at a time and statement level "already exists" errors are tolerated, which is
// what makes a retry after a half applied migration converge instead of failing
// with "Duplicate column name" forever.
func (r Runner) apply(ctx context.Context, dialect, name, checksum string, content []byte) error {
	statements := splitStatements(prepareMigration(dialect, string(content)))
	if len(statements) == 0 {
		return fmt.Errorf("apply migration %s: no statements", name)
	}
	record := migrationRecord{Version: name, Checksum: checksum, AppliedAt: time.Now().UTC()}
	if dialect == "mysql" {
		for index, statement := range statements {
			if err := r.DB.WithContext(ctx).Exec(statement).Error; err != nil {
				if isAlreadyAppliedStatement(err) {
					continue
				}
				return fmt.Errorf("apply migration %s statement %d: %w", name, index+1, err)
			}
		}
		if err := r.DB.WithContext(ctx).Create(&record).Error; err != nil {
			if isDuplicateRecord(err) {
				return errAlreadyApplied
			}
			return err
		}
		return nil
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for index, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("apply migration %s statement %d: %w", name, index+1, err)
			}
		}
		if err := tx.Create(&record).Error; err != nil {
			if isDuplicateRecord(err) {
				return errAlreadyApplied
			}
			return err
		}
		return nil
	})
}

// acquireLock serialises migration runs across processes. SQLite needs no lock:
// the database file is opened with a single connection and the per-file
// transaction plus the schema_migrations unique key already serialise writers,
// which is exactly what isDuplicateRecord handles.
func (r Runner) acquireLock(ctx context.Context, dialect string) (func(), error) {
	if dialect != "postgres" && dialect != "mysql" {
		return func() {}, nil
	}
	sqlDB, err := r.DB.DB()
	if err != nil {
		return func() {}, err
	}
	// The lock is session scoped, so it must be taken and released on the same
	// connection: a pooled Exec could release a lock the other connection holds.
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return func() {}, err
	}
	switch dialect {
	case "postgres":
		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", postgresAdvisoryLockKey); err != nil {
			_ = conn.Close()
			return func() {}, fmt.Errorf("acquire migration lock: %w", err)
		}
		return func() {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", postgresAdvisoryLockKey)
			_ = conn.Close()
		}, nil
	default:
		var acquired sql.NullInt64
		if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", mysqlLockName, mysqlLockTimeoutSeconds).Scan(&acquired); err != nil {
			_ = conn.Close()
			return func() {}, fmt.Errorf("acquire migration lock: %w", err)
		}
		if !acquired.Valid || acquired.Int64 != 1 {
			_ = conn.Close()
			return func() {}, errors.New("acquire migration lock: timed out waiting for another instance")
		}
		return func() {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK(?)", mysqlLockName)
			_ = conn.Close()
		}, nil
	}
}

// isDuplicateRecord recognises a schema_migrations primary key collision, which
// means a concurrent instance got there first.
func isDuplicateRecord(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return true
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "schema_migrations") {
		return false
	}
	return strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "duplicate entry")
}

// isAlreadyAppliedStatement recognises the MySQL errors raised by a statement
// that was already applied by an earlier, partially failed run.
func isAlreadyAppliedStatement(err error) bool {
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	switch mysqlErr.Number {
	case 1050, // table already exists
		1060, // duplicate column name
		1061, // duplicate key name
		1091, // can't drop: doesn't exist
		1826: // duplicate foreign key constraint name
		return true
	}
	return false
}

var mysqlIndexGuard = regexp.MustCompile(`(?i)CREATE\s+(UNIQUE\s+)?INDEX\s+IF\s+NOT\s+EXISTS`)

// prepareMigration rewrites statements MySQL cannot parse.
//
// MySQL 5.7 rejects a leading UTF-8 BOM with a syntax error ("check the manual
// that corresponds to your MySQL server version"), while a file written by an
// editor that adds one silently works on SQLite. The BOM is stripped for every
// dialect so a stray one can never block a deployment again.
func prepareMigration(dialect, content string) string {
	content = strings.TrimPrefix(content, "\ufeff")
	if dialect != "mysql" {
		return content
	}
	return mysqlIndexGuard.ReplaceAllString(content, "CREATE ${1}INDEX")
}

// splitStatements splits a migration file into single statements so each one can
// be executed on its own. MySQL rejects multi statement Exec by default
// (multiStatements=false), which made the very first migration fail there, and
// per-statement execution is also what lets MySQL recover from a half applied
// file. Quoted text, line comments and block comments are respected.
func splitStatements(content string) []string {
	var statements []string
	var current strings.Builder
	var quote rune
	runes := []rune(content)
	for index := 0; index < len(runes); index++ {
		char := runes[index]
		if quote != 0 {
			current.WriteRune(char)
			if char == '\\' && index+1 < len(runes) {
				// Keep the escaped character out of the quote state machine.
				index++
				current.WriteRune(runes[index])
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		switch {
		case char == '\'' || char == '"' || char == '`':
			quote = char
			current.WriteRune(char)
		case char == '-' && index+2 < len(runes) && runes[index+1] == '-' && (runes[index+2] == ' ' || runes[index+2] == '\t' || runes[index+2] == '\n' || runes[index+2] == '-'):
			// Line comment: skip to the end of the line.
			for index < len(runes) && runes[index] != '\n' {
				index++
			}
			current.WriteRune('\n')
		case char == '/' && index+1 < len(runes) && runes[index+1] == '*':
			index += 2
			for index+1 < len(runes) && !(runes[index] == '*' && runes[index+1] == '/') {
				index++
			}
			index++
		case char == ';':
			appendStatement(&statements, current.String())
			current.Reset()
		default:
			current.WriteRune(char)
		}
	}
	appendStatement(&statements, current.String())
	return statements
}

func appendStatement(statements *[]string, raw string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return
	}
	*statements = append(*statements, trimmed)
}
