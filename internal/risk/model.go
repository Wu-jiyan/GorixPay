package risk

import (
	"errors"
	"time"
)

type EntryType string

const (
	EntryTypeIP      EntryType = "ip"
	EntryTypeDomain  EntryType = "domain"
	EntryTypeKeyword EntryType = "keyword"
	EntryTypeEmail   EntryType = "email"
)

var (
	ErrInvalidRequest = errors.New("invalid risk request")
	ErrNotFound       = errors.New("risk entry not found")
	ErrDuplicate      = errors.New("risk entry already exists")
)

// BlacklistEntry is one blocked value (ip, domain, keyword or email).
type BlacklistEntry struct {
	ID        string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	EntryType EntryType `gorm:"type:varchar(20);not null;uniqueIndex:idx_blacklist_type_value" json:"entry_type"`
	Value     string    `gorm:"type:varchar(191);not null;uniqueIndex:idx_blacklist_type_value" json:"value"`
	Remark    *string   `gorm:"type:varchar(191)" json:"remark"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
}

func (BlacklistEntry) TableName() string { return "blacklist_entries" }

// Event is a risk control hit recorded for the admin console.
type Event struct {
	ID         string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	MerchantID *string   `gorm:"type:varchar(36);index" json:"merchant_id"`
	RuleCode   string    `gorm:"type:varchar(50);not null;index" json:"rule_code"`
	Detail     string    `gorm:"type:varchar(500);not null" json:"detail"`
	ClientIP   *string   `gorm:"type:varchar(45)" json:"client_ip"`
	CreatedAt  time.Time `gorm:"not null" json:"created_at"`
}

func (Event) TableName() string { return "risk_events" }

func ValidEntryType(value EntryType) bool {
	switch value {
	case EntryTypeIP, EntryTypeDomain, EntryTypeKeyword, EntryTypeEmail:
		return true
	}
	return false
}
