package epay

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// SourceConfig points at the legacy Epay database. MySQL is the production
// shape; SQLite covers forks and the migration fixtures.
type SourceConfig struct {
	Driver string
	DSN    string
}

// TargetConfig points at the GorixPay database that receives the migrated rows.
type TargetConfig struct {
	Driver string
	DSN    string
}

// Options configure one migration run.
type Options struct {
	Source SourceConfig
	Target TargetConfig
	// DryRun performs preflight plus a read-only preview and never writes.
	DryRun bool
	// BatchSize is the number of rows inserted per transaction.
	BatchSize int
	// MasterKeyFile is validated when set so the CLI accepts the same flag set
	// as gorixpay itself. This migration writes no encrypted column.
	MasterKeyFile string
	// ReportPrefix is the file prefix for the .json and .md reports.
	ReportPrefix string
	// Now is injectable for tests.
	Now func() time.Time
	// Logf receives progress lines; nil silences them.
	Logf func(format string, arguments ...any)
}

const (
	defaultBatchSize = 500
	maxBatchSize     = 10000
)

// maxReportSamples bounds how many offending values a report lists.
const maxReportSamples = 5

func normalizeDriver(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// Validate checks the option set before any database is touched.
func (options Options) Validate() error {
	switch normalizeDriver(options.Source.Driver) {
	case "mysql", "sqlite":
	default:
		return fmt.Errorf("unsupported source driver %q (want mysql or sqlite)", options.Source.Driver)
	}
	if strings.TrimSpace(options.Source.DSN) == "" {
		return errors.New("source DSN is required (--source-dsn)")
	}
	switch normalizeDriver(options.Target.Driver) {
	case "sqlite", "mysql", "postgres":
	default:
		return fmt.Errorf("unsupported target driver %q (want sqlite, mysql or postgres)", options.Target.Driver)
	}
	if strings.TrimSpace(options.Target.DSN) == "" {
		return errors.New("target DSN is required (--target-dsn)")
	}
	if options.BatchSize < 0 {
		return fmt.Errorf("batch size must be positive, got %d", options.BatchSize)
	}
	if options.BatchSize > maxBatchSize {
		return fmt.Errorf("batch size %d exceeds the maximum of %d", options.BatchSize, maxBatchSize)
	}
	if normalizeDriver(options.Source.Driver) == normalizeDriver(options.Target.Driver) &&
		strings.TrimSpace(options.Source.DSN) == strings.TrimSpace(options.Target.DSN) {
		return errors.New("source and target point at the same database")
	}
	return nil
}

func (options Options) batchSize() int {
	if options.BatchSize <= 0 {
		return defaultBatchSize
	}
	return options.BatchSize
}

func (options Options) clock() time.Time {
	if options.Now != nil {
		return options.Now().UTC()
	}
	return time.Now().UTC()
}

// redactDSN masks credentials so a report can be shared.
func redactDSN(driver, dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	normalized := normalizeDriver(driver)
	if normalized == "mysql" {
		// user:password@tcp(host:port)/db
		at := strings.LastIndex(trimmed, "@")
		colon := strings.Index(trimmed, ":")
		if at > 0 && colon > 0 && colon < at {
			return trimmed[:colon+1] + "***" + trimmed[at:]
		}
		return trimmed
	}
	if parsed, err := url.Parse(trimmed); err == nil && parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), "***")
			return parsed.String()
		}
	}
	return trimmed
}
