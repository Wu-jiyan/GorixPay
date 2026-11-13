package certification

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// Settings keys read from system_settings; missing values fall back to defaults.
const (
	SettingRequired = "certification.required"
	SettingFeeCents = "certification.fee_cents"
)

func (service *Service) Required(ctx context.Context) bool {
	return service.boolSetting(ctx, SettingRequired, false)
}

func (service *Service) Fee(ctx context.Context) int64 {
	return service.intSetting(ctx, SettingFeeCents, 0)
}

func (service *Service) boolSetting(ctx context.Context, key string, fallback bool) bool {
	raw, ok := readSetting(ctx, service.db, key)
	if !ok {
		return fallback
	}
	var value bool
	if err := json.Unmarshal([]byte(raw), &value); err == nil {
		return value
	}
	return strings.EqualFold(strings.Trim(strings.TrimSpace(raw), `"`), "true")
}

func (service *Service) intSetting(ctx context.Context, key string, fallback int64) int64 {
	raw, ok := readSetting(ctx, service.db, key)
	if !ok {
		return fallback
	}
	return parseSettingInt(raw, fallback)
}

// feeInTx reads the configured fee through an open transaction. SQLite runs with
// a single connection, so a nested query on service.db would deadlock.
func (service *Service) feeInTx(ctx context.Context, tx *gorm.DB) int64 {
	raw, ok := readSetting(ctx, tx, SettingFeeCents)
	if !ok {
		return 0
	}
	return parseSettingInt(raw, 0)
}

func parseSettingInt(raw string, fallback int64) int64 {
	var value int64
	if err := json.Unmarshal([]byte(raw), &value); err == nil {
		return value
	}
	var text string
	if err := json.Unmarshal([]byte(raw), &text); err == nil {
		if parsed, convErr := parseInt(text); convErr == nil {
			return parsed
		}
	}
	return fallback
}

func readSetting(ctx context.Context, db *gorm.DB, key string) (string, bool) {
	type systemSetting struct {
		Key   string `gorm:"column:setting_key"`
		Value string `gorm:"column:setting_value"`
	}
	var record systemSetting
	err := db.WithContext(ctx).Table("system_settings").Where("setting_key = ?", key).First(&record).Error
	if err != nil {
		return "", false
	}
	return record.Value, true
}

func parseInt(value string) (int64, error) {
	var result int64
	var negative bool
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errors.New("empty number")
	}
	for index, char := range trimmed {
		if index == 0 && char == '-' {
			negative = true
			continue
		}
		if char < '0' || char > '9' {
			return 0, errors.New("invalid number")
		}
		result = result*10 + int64(char-'0')
	}
	if negative {
		result = -result
	}
	return result, nil
}
