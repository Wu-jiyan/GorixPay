package risk

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"
	paymentservice "gorixpay/internal/payment/service"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Settings keys with their defaults (0 = unlimited / disabled).
const (
	SettingDailyAmountLimitCents = "risk.daily_amount_limit_cents"
	SettingDailyOrderLimit       = "risk.daily_order_limit"
	SettingIPDailyLimit          = "risk.ip_daily_limit"
)

// Generic rejection keeps blacklist hits opaque to the caller.
const blacklistRejection = "订单提交失败，请稍后重试"

// Service implements blacklist management, risk events and the payment guard.
type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func New(db *gorm.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}

type Filter struct {
	MerchantID string
	RuleCode   string
	EntryType  EntryType
}

type Page struct {
	Number int
	Size   int
}

// AddBlacklist creates a blacklist entry, rejecting duplicates.
func (service *Service) AddBlacklist(ctx context.Context, entryType, value, remark string) (BlacklistEntry, error) {
	kind := EntryType(strings.ToLower(strings.TrimSpace(entryType)))
	value = strings.TrimSpace(value)
	remark = strings.TrimSpace(remark)
	if !ValidEntryType(kind) || value == "" || len(value) > 191 || len(remark) > 191 {
		return BlacklistEntry{}, ErrInvalidRequest
	}
	if kind == EntryTypeIP && strings.ContainsAny(value, " /") {
		return BlacklistEntry{}, ErrInvalidRequest
	}
	entry := BlacklistEntry{ID: uuid.NewString(), EntryType: kind, Value: value, Remark: optional(remark), CreatedAt: service.now().UTC()}
	if err := service.db.WithContext(ctx).Create(&entry).Error; err != nil {
		var existing BlacklistEntry
		if lookupErr := service.db.WithContext(ctx).Where("entry_type = ? AND value = ?", kind, value).First(&existing).Error; lookupErr == nil {
			return BlacklistEntry{}, ErrDuplicate
		}
		return BlacklistEntry{}, err
	}
	return entry, nil
}

// RemoveBlacklist deletes one entry.
func (service *Service) RemoveBlacklist(ctx context.Context, id string) error {
	result := service.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id)).Delete(&BlacklistEntry{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ListBlacklist returns entries, optionally filtered by type.
func (service *Service) ListBlacklist(ctx context.Context, entryType string) ([]BlacklistEntry, error) {
	query := service.db.WithContext(ctx).Model(&BlacklistEntry{})
	if entryType = strings.TrimSpace(entryType); entryType != "" {
		if !ValidEntryType(EntryType(entryType)) {
			return nil, ErrInvalidRequest
		}
		query = query.Where("entry_type = ?", entryType)
	}
	var entries []BlacklistEntry
	err := query.Order("created_at DESC").Limit(500).Find(&entries).Error
	return entries, err
}

// RecordEvent stores a risk event; failures are returned so callers can log them.
func (service *Service) RecordEvent(ctx context.Context, event Event) error {
	if strings.TrimSpace(event.RuleCode) == "" || strings.TrimSpace(event.Detail) == "" {
		return ErrInvalidRequest
	}
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = service.now().UTC()
	}
	if len(event.Detail) > 500 {
		event.Detail = event.Detail[:500]
	}
	return service.db.WithContext(ctx).Create(&event).Error
}

// ListEvents returns risk events for the admin console.
func (service *Service) ListEvents(ctx context.Context, filter Filter, page Page) ([]Event, int64, error) {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 || page.Size > 100 {
		page.Size = 20
	}
	query := service.db.WithContext(ctx).Model(&Event{})
	if filter.MerchantID != "" {
		query = query.Where("merchant_id = ?", filter.MerchantID)
	}
	if filter.RuleCode != "" {
		query = query.Where("rule_code = ?", filter.RuleCode)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var events []Event
	err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&events).Error
	return events, total, err
}

// Guard applies the blacklist checks followed by the daily limits.
type Guard struct {
	service *Service
}

// Guard returns the payment guard for risk control.
func (service *Service) Guard() paymentservice.Guard {
	return Guard{service: service}
}

func (guard Guard) Check(ctx context.Context, command paymentservice.CreateCommand) error {
	if guard.service == nil {
		return nil
	}
	if err := guard.service.checkBlacklist(ctx, command); err != nil {
		return err
	}
	return guard.service.checkLimits(ctx, command)
}

func (service *Service) checkBlacklist(ctx context.Context, command paymentservice.CreateCommand) error {
	var entries []BlacklistEntry
	if err := service.db.WithContext(ctx).Find(&entries).Error; err != nil || len(entries) == 0 {
		return nil
	}
	host := hostOf(command.NotifyURL)
	var email string
	var merchant identitymodel.Merchant
	if err := service.db.WithContext(ctx).First(&merchant, "id = ?", command.MerchantID).Error; err == nil && merchant.Email != nil {
		email = strings.ToLower(*merchant.Email)
	}
	name := strings.ToLower(command.Name)
	for _, entry := range entries {
		value := strings.ToLower(strings.TrimSpace(entry.Value))
		if value == "" {
			continue
		}
		hit := false
		switch entry.EntryType {
		case EntryTypeIP:
			hit = strings.EqualFold(command.ClientIP, value)
		case EntryTypeEmail:
			hit = email != "" && email == value
		case EntryTypeDomain:
			hit = matchDomain(value, host)
		case EntryTypeKeyword:
			hit = strings.Contains(name, value)
		}
		if !hit {
			continue
		}
		service.record(ctx, command.MerchantID, "blacklist."+string(entry.EntryType), "blacklist hit", command.ClientIP)
		return paymentservice.Rejection{Reason: blacklistRejection}
	}
	return nil
}

func (service *Service) checkLimits(ctx context.Context, command paymentservice.CreateCommand) error {
	start := startOfDay(service.now())
	dailyAmount := service.intSetting(ctx, SettingDailyAmountLimitCents)
	if dailyAmount > 0 {
		var paid int64
		query := service.db.WithContext(ctx).Model(&paymentmodel.Order{}).
			Where("merchant_id = ? AND status = ? AND paid_at >= ?", command.MerchantID, paymentmodel.StatusPaid, start)
		if err := query.Select("COALESCE(SUM(amount), 0)").Scan(&paid).Error; err == nil && paid+command.Amount > dailyAmount {
			service.record(ctx, command.MerchantID, "risk.daily_amount_limit", "daily paid amount limit exceeded", command.ClientIP)
			return paymentservice.Rejection{Reason: "当前商户今日收款已达到限额，无法发起支付"}
		}
	}
	dailyOrders := service.intSetting(ctx, SettingDailyOrderLimit)
	if dailyOrders > 0 {
		var count int64
		query := service.db.WithContext(ctx).Model(&paymentmodel.Order{}).
			Where("merchant_id = ? AND status = ? AND paid_at >= ?", command.MerchantID, paymentmodel.StatusPaid, start)
		if err := query.Count(&count).Error; err == nil && count >= dailyOrders {
			service.record(ctx, command.MerchantID, "risk.daily_order_limit", "daily paid order limit exceeded", command.ClientIP)
			return paymentservice.Rejection{Reason: "当前商户今日收款已达到限额，无法发起支付"}
		}
	}
	ipLimit := service.intSetting(ctx, SettingIPDailyLimit)
	if ipLimit > 0 && strings.TrimSpace(command.ClientIP) != "" {
		var count int64
		query := service.db.WithContext(ctx).Model(&paymentmodel.Order{}).
			Where("client_ip = ? AND created_at >= ?", command.ClientIP, start)
		if err := query.Count(&count).Error; err == nil && count >= ipLimit {
			service.record(ctx, command.MerchantID, "risk.ip_daily_limit", "daily ip order limit exceeded", command.ClientIP)
			return paymentservice.Rejection{Reason: "你今天已无法再发起支付，请明天再试"}
		}
	}
	return nil
}

func (service *Service) record(ctx context.Context, merchantID, ruleCode, detail, clientIP string) {
	merchant := merchantID
	event := Event{ID: uuid.NewString(), MerchantID: &merchant, RuleCode: ruleCode, Detail: detail, ClientIP: optional(clientIP), CreatedAt: service.now().UTC()}
	_ = service.db.WithContext(ctx).Create(&event).Error
}

func (service *Service) intSetting(ctx context.Context, key string) int64 {
	raw, ok := service.setting(ctx, key)
	if !ok {
		return 0
	}
	var value int64
	if err := json.Unmarshal([]byte(raw), &value); err == nil {
		return value
	}
	var text string
	if err := json.Unmarshal([]byte(raw), &text); err == nil {
		if parsed, convErr := strconv.ParseInt(strings.TrimSpace(text), 10, 64); convErr == nil {
			return parsed
		}
	}
	return 0
}

func (service *Service) setting(ctx context.Context, key string) (string, bool) {
	type systemSetting struct {
		Value string `gorm:"column:setting_value"`
	}
	var record systemSetting
	if err := service.db.WithContext(ctx).Table("system_settings").Where("setting_key = ?", key).First(&record).Error; err != nil {
		return "", false
	}
	return record.Value, true
}

func startOfDay(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func hostOf(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func matchDomain(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	host = strings.ToLower(strings.TrimSpace(host))
	if pattern == "" || host == "" {
		return false
	}
	if strings.HasPrefix(pattern, "*.") {
		base := pattern[2:]
		return base != "" && host != base && strings.HasSuffix(host, "."+base)
	}
	return pattern == host
}

func optional(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}
