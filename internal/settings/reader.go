package settings

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// Reader reads system_settings rows for anything that is not the admin console
// itself: registration gate, homepage mode, notify worker knobs. It deliberately
// keeps no cache — a console change has to take effect on the next request, which
// is the whole point of these knobs.
type Reader struct {
	db *gorm.DB
}

// NewReader builds a reader. A nil database answers every lookup with the
// caller's fallback, so wiring stays optional in tests.
func NewReader(db *gorm.DB) *Reader {
	return &Reader{db: db}
}

// Raw returns the stored value verbatim (JSON string included).
func (reader *Reader) Raw(ctx context.Context, key string) string {
	if reader == nil || reader.db == nil {
		return ""
	}
	var value string
	err := reader.db.WithContext(ctx).Table("system_settings").
		Select("setting_value").Where("setting_key = ?", strings.TrimSpace(key)).
		Limit(1).Scan(&value).Error
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// Text returns the string a key holds, with the JSON wrapper removed.
func (reader *Reader) Text(ctx context.Context, key string) string {
	return Text(reader.Raw(ctx, key))
}

// TextOr answers fallback when the key is empty.
func (reader *Reader) TextOr(ctx context.Context, key, fallback string) string {
	if value := reader.Text(ctx, key); value != "" {
		return value
	}
	return fallback
}

// Bool reads a boolean, accepting both a JSON boolean and the strings "true"/"1".
func (reader *Reader) Bool(ctx context.Context, key string, fallback bool) bool {
	raw := reader.Raw(ctx, key)
	if raw == "" {
		return fallback
	}
	var flag bool
	if err := json.Unmarshal([]byte(raw), &flag); err == nil {
		return flag
	}
	text := strings.ToLower(Text(raw))
	switch text {
	case "true", "1", "on", "yes":
		return true
	case "false", "0", "off", "no":
		return false
	}
	return fallback
}

// Int reads an integer, accepting the JSON number and the quoted form the console
// writes when a field is typed as text. A missing or unparsable value answers
// fallback so a typo never silently changes behaviour to zero.
func (reader *Reader) Int(ctx context.Context, key string, fallback int) int {
	raw := reader.Raw(ctx, key)
	if raw == "" {
		return fallback
	}
	if value, err := strconv.Atoi(Text(raw)); err == nil {
		return value
	}
	var number float64
	if err := json.Unmarshal([]byte(raw), &number); err == nil {
		return int(number)
	}
	return fallback
}
