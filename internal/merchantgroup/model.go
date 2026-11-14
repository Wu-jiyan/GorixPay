package merchantgroup

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Status is the administrator visible switch of a group. It controls whether the
// group can be picked for new merchants in the console; it does not change the
// fee that already assigned merchants pay, because RateBp has to stay stable for
// settlement runs that started before the switch was flipped.
type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// SettleMode decides how a merchant is paid out.
//
//	normal — money is collected by the platform and settled on request
//	direct — the channel settles straight to the merchant, the platform never
//	         holds the funds
type SettleMode string

const (
	SettleModeNormal SettleMode = "normal"
	SettleModeDirect SettleMode = "direct"
)

var (
	ErrNotFound         = errors.New("merchant group not found")
	ErrMerchantNotFound = errors.New("merchant not found")
	ErrInvalidRequest   = errors.New("invalid merchant group request")
	ErrDuplicateName    = errors.New("merchant group name already exists")
	ErrGroupInUse       = errors.New("merchant group still has merchants")
	ErrGroupDisabled    = errors.New("merchant group is disabled")
)

// Group is a fee bucket. Every merchant optionally belongs to one group; the
// group rate is what settlement charges, and the group settle mode is copied onto
// the merchant row when the assignment changes.
type Group struct {
	ID         string     `gorm:"type:varchar(36);primaryKey"`
	Name       string     `gorm:"type:varchar(64);not null;uniqueIndex"`
	RateBp     int        `gorm:"not null;default:0"`
	PayTypes   PayTypes   `gorm:"type:text"`
	SettleMode SettleMode `gorm:"type:varchar(20);not null;default:'normal'"`
	Status     Status     `gorm:"type:varchar(20);not null;default:'active'"`
	CreatedAt  time.Time  `gorm:"not null"`
	UpdatedAt  time.Time  `gorm:"not null"`
}

func (Group) TableName() string { return "merchant_groups" }

// PayTypes is the list of payment type codes a group may use. It is stored as a
// JSON array in the TEXT column so the value stays readable in every dialect and
// needs no join table for what is a short, administrator curated list.
type PayTypes []string

// Value implements driver.Valuer.
func (types PayTypes) Value() (driver.Value, error) {
	if len(types) == 0 {
		return "[]", nil
	}
	encoded, err := json.Marshal([]string(types))
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

// Scan implements sql.Scanner.
func (types *PayTypes) Scan(value any) error {
	if types == nil {
		return errors.New("merchantgroup: PayTypes receiver is nil")
	}
	var raw []byte
	switch typed := value.(type) {
	case nil:
		*types = nil
		return nil
	case []byte:
		raw = typed
	case string:
		raw = []byte(typed)
	default:
		return fmt.Errorf("merchantgroup: cannot scan %T into PayTypes", value)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		*types = nil
		return nil
	}
	var decoded []string
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return fmt.Errorf("merchantgroup: decode pay_types: %w", err)
	}
	*types = decoded
	return nil
}

// merchantRow is the slice of `merchants` this package owns: which group the
// merchant belongs to and the effective settle mode copied from that group.
// Writing it as a narrow model keeps gorm from touching unrelated columns.
type merchantRow struct {
	ID         string  `gorm:"type:varchar(36);primaryKey"`
	GroupID    *string `gorm:"type:varchar(36);index"`
	SettleMode string  `gorm:"type:varchar(20);not null;default:'normal'"`
}

func (merchantRow) TableName() string { return "merchants" }

// ValidStatus reports whether value is a known group status.
func ValidStatus(value Status) bool {
	return value == StatusActive || value == StatusDisabled
}

// ValidSettleMode reports whether value is a known settle mode.
func ValidSettleMode(value SettleMode) bool {
	return value == SettleModeNormal || value == SettleModeDirect
}
