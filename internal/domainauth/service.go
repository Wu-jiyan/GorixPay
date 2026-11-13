package domainauth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	paymentservice "gorixpay/internal/payment/service"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SettingWhitelistRequired toggles the pre-create domain whitelist check.
const SettingWhitelistRequired = "payment.domain_whitelist_required"

// RejectionReason is returned when a notify/return host is not authorized.
const RejectionReason = "当前支付域名未授权"

var domainPattern = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)

// Service manages merchant payment domains and their review workflow.
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
	Status     Status
}

type Page struct {
	Number int
	Size   int
}

// Submit registers a domain for review. Wildcards such as *.example.com are
// accepted; submissions are deduplicated per merchant.
func (service *Service) Submit(ctx context.Context, merchantID, value string) (Domain, error) {
	merchantID = strings.TrimSpace(merchantID)
	normalized, ok := Normalize(value)
	if merchantID == "" {
		return Domain{}, ErrInvalidRequest
	}
	if !ok {
		return Domain{}, ErrInvalidDomain
	}
	var existing Domain
	err := service.db.WithContext(ctx).Where("merchant_id = ? AND domain = ?", merchantID, normalized).First(&existing).Error
	if err == nil {
		return Domain{}, ErrDuplicate
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return Domain{}, err
	}
	now := service.now().UTC()
	domain := Domain{
		ID: uuid.NewString(), MerchantID: merchantID, Domain: normalized,
		Status: StatusPending, CreatedAt: now,
	}
	if err := service.db.WithContext(ctx).Create(&domain).Error; err != nil {
		return Domain{}, ErrDuplicate
	}
	return domain, nil
}

// Delete removes a domain that is pending or approved.
func (service *Service) Delete(ctx context.Context, merchantID, id string) error {
	result := service.db.WithContext(ctx).
		Where("id = ? AND merchant_id = ? AND status IN ?", strings.TrimSpace(id), strings.TrimSpace(merchantID), []Status{StatusPending, StatusApproved}).
		Delete(&Domain{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns the merchant domains ordered by creation time.
func (service *Service) List(ctx context.Context, merchantID string) ([]Domain, error) {
	var domains []Domain
	err := service.db.WithContext(ctx).Where("merchant_id = ?", strings.TrimSpace(merchantID)).Order("created_at DESC").Find(&domains).Error
	return domains, err
}

// ListAll returns domains for the admin console.
func (service *Service) ListAll(ctx context.Context, filter Filter, page Page) ([]Domain, int64, error) {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 || page.Size > 100 {
		page.Size = 20
	}
	query := service.db.WithContext(ctx).Model(&Domain{})
	if filter.MerchantID != "" {
		query = query.Where("merchant_id = ?", filter.MerchantID)
	}
	if filter.Status != "" {
		if !ValidStatus(filter.Status) {
			return nil, 0, ErrInvalidRequest
		}
		query = query.Where("status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var domains []Domain
	err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&domains).Error
	return domains, total, err
}

// Review approves or rejects a pending domain.
func (service *Service) Review(ctx context.Context, id, reviewerID, action, reason string) (Domain, error) {
	id = strings.TrimSpace(id)
	reviewerID = strings.TrimSpace(reviewerID)
	action = strings.ToLower(strings.TrimSpace(action))
	reason = strings.TrimSpace(reason)
	if id == "" || reviewerID == "" || len(reason) > 255 || (action != "approve" && action != "reject") {
		return Domain{}, ErrInvalidRequest
	}
	if action == "reject" && reason == "" {
		return Domain{}, ErrInvalidRequest
	}
	var domain Domain
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&domain, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if domain.Status != StatusPending {
			if (action == "approve" && domain.Status == StatusApproved) || (action == "reject" && domain.Status == StatusRejected) {
				return nil
			}
			return ErrStateConflict
		}
		now := service.now().UTC()
		status := StatusApproved
		updates := map[string]any{"status": status, "reviewer_id": reviewerID, "reviewed_at": now, "rejection_reason": nil}
		domain.Status = status
		if action == "reject" {
			status = StatusRejected
			updates["status"] = status
			updates["rejection_reason"] = reason
			domain.Status = status
			domain.RejectionReason = &reason
		}
		domain.ReviewerID = &reviewerID
		domain.ReviewedAt = &now
		return tx.Model(&Domain{}).Where("id = ? AND status = ?", domain.ID, StatusPending).Updates(updates).Error
	})
	return domain, err
}

// ApprovedDomains returns the approved domain patterns of one merchant.
func (service *Service) ApprovedDomains(ctx context.Context, merchantID string) ([]string, error) {
	var domains []Domain
	if err := service.db.WithContext(ctx).
		Where("merchant_id = ? AND status = ?", strings.TrimSpace(merchantID), StatusApproved).
		Find(&domains).Error; err != nil {
		return nil, err
	}
	patterns := make([]string, 0, len(domains))
	for _, domain := range domains {
		patterns = append(patterns, domain.Domain)
	}
	return patterns, nil
}

// Required reports whether the whitelist policy is enabled.
func (service *Service) Required(ctx context.Context) bool {
	raw, ok := service.setting(ctx, SettingWhitelistRequired)
	if !ok {
		return false
	}
	var value bool
	if err := json.Unmarshal([]byte(raw), &value); err == nil {
		return value
	}
	return strings.EqualFold(strings.Trim(strings.TrimSpace(raw), `"`), "true")
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

// Normalize lowercases and validates a submitted domain, returning the canonical
// pattern used for matching.
func Normalize(value string) (string, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	trimmed = strings.TrimSuffix(trimmed, ".")
	if trimmed == "" || len(trimmed) > 191 {
		return "", false
	}
	if strings.Contains(trimmed, "://") || strings.ContainsAny(trimmed, "/?#") {
		return "", false
	}
	if domainPattern.MatchString(trimmed) {
		return trimmed, true
	}
	if net.ParseIP(trimmed) != nil {
		return trimmed, true
	}
	return "", false
}

// Match reports whether host is authorized by pattern (exact or *.parent).
func Match(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if pattern == "" || host == "" {
		return false
	}
	if strings.HasPrefix(pattern, "*.") {
		base := pattern[2:]
		if base == "" || host == base {
			return false
		}
		return strings.HasSuffix(host, "."+base)
	}
	return pattern == host
}

// Host extracts the lowercase host from an absolute http(s) URL.
func Host(value string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", false
	}
	return host, true
}

// Guard validates notify_url and return_url hosts against the whitelist.
type Guard struct {
	service *Service
}

// Guard returns the payment guard for the domain whitelist policy.
func (service *Service) Guard() paymentservice.Guard {
	return Guard{service: service}
}

func (guard Guard) Check(ctx context.Context, command paymentservice.CreateCommand) error {
	if guard.service == nil || !guard.service.Required(ctx) {
		return nil
	}
	patterns, err := guard.service.ApprovedDomains(ctx, command.MerchantID)
	if err != nil {
		return paymentservice.Rejection{Reason: RejectionReason}
	}
	if !authorized(patterns, command.NotifyURL) {
		return paymentservice.Rejection{Reason: RejectionReason}
	}
	if strings.TrimSpace(command.ReturnURL) != "" && !authorized(patterns, command.ReturnURL) {
		return paymentservice.Rejection{Reason: RejectionReason}
	}
	return nil
}

func authorized(patterns []string, target string) bool {
	host, ok := Host(target)
	if !ok {
		return false
	}
	for _, pattern := range patterns {
		if Match(pattern, host) {
			return true
		}
	}
	return false
}
