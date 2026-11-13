package epay

import (
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// legacyTimeLayouts are the timestamp shapes Epay writes. TIMESTAMP/DATETIME
// columns arrive either as "YYYY-MM-DD HH:MM:SS" (MySQL without parseTime,
// SQLite TEXT) or as RFC3339 when a fork stored ISO strings.
var legacyTimeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02",
}

// deterministicID derives a stable UUID for a legacy key so a retried run writes
// the same primary keys instead of inventing new ones.
func deterministicID(kind, key string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("gorixpay:epay:"+kind+":"+key)).String()
}

// nullString turns an empty legacy column into SQL NULL, which matters for the
// UNIQUE email/phone columns: two empty strings would collide, NULL does not.
func nullString(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func pointerString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func stringValue(nullable sql.NullString) string {
	if !nullable.Valid {
		return ""
	}
	return nullable.String
}

func intValue(nullable sql.NullInt64) int64 {
	if !nullable.Valid {
		return 0
	}
	return nullable.Int64
}

// trimUTF8 truncates on a rune boundary so a shortened value never becomes
// invalid UTF-8 when it hits a VARCHAR limit.
func trimUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	var builder strings.Builder
	for _, symbol := range value {
		if builder.Len()+utf8.RuneLen(symbol) > limit {
			break
		}
		builder.WriteRune(symbol)
	}
	return builder.String()
}

func parseLegacyTime(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	for _, layout := range legacyTimeLayouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

// timeOr parses a legacy timestamp and falls back when the source row has no
// usable value (NULL addtime, a fork-specific format).
func timeOr(value string, fallback time.Time) time.Time {
	if parsed, ok := parseLegacyTime(value); ok {
		return parsed
	}
	return fallback.UTC()
}

// laterOf returns the later of two timestamps, keeping updated_at >= created_at.
func laterOf(primary, secondary time.Time) time.Time {
	if secondary.After(primary) {
		return secondary
	}
	return primary
}

// reasonForError classifies a driver error into a stable report reason code.
func reasonForError(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "unique"), strings.Contains(message, "duplicate"):
		return "unique_conflict"
	case strings.Contains(message, "foreign key"), strings.Contains(message, "constraint failed: foreign"):
		return "foreign_key_violation"
	case strings.Contains(message, "checksum"), strings.Contains(message, "check constraint"):
		return "check_constraint"
	default:
		return "insert_failed"
	}
}

// counters accumulates per-table outcome counts plus the first few failure
// messages, which is all an operator needs to triage a run.
type counters struct {
	inserted    int64
	skipped     int64
	failed      int64
	skipReasons map[string]int64
	failReasons map[string]int64
	failureKeys map[string]bool
	samples     []string
}

const failureSampleLimit = 5

func newCounters() *counters {
	return &counters{skipReasons: map[string]int64{}, failReasons: map[string]int64{}, failureKeys: map[string]bool{}}
}

func (counts *counters) skip(reason string) {
	counts.skipped++
	counts.skipReasons[reason]++
}

func (counts *counters) fail(reason, message string) {
	counts.failed++
	counts.failReasons[reason]++
	if len(counts.samples) < failureSampleLimit && strings.TrimSpace(message) != "" {
		counts.samples = append(counts.samples, reason+": "+trimUTF8(message, 200))
	}
}

// failKey records a row level write failure so the caller can exclude that row
// from the amount reconciliation instead of counting it as migrated.
func (counts *counters) failKey(key, reason, message string) {
	if key != "" {
		counts.failureKeys[key] = true
	}
	counts.fail(reason, message)
}
