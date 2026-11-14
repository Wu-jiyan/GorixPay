package complaint

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type RuleStatus string

const (
	RuleActive   RuleStatus = "active"
	RuleDisabled RuleStatus = "disabled"

	// RuleWildcard matches channels that have no dedicated rule.
	RuleWildcard = "*"

	// AutoCloseReason is written as a system message when a complaint is closed
	// by the timeout rule.
	AutoCloseReason = "超时自动关闭"
	// SystemOperator marks messages written by the automation, never by a person.
	SystemOperator = "system"
)

// AutoRule applies an automatic reply (and optional timeout close) to complaints
// of one channel. Channel "*" is the fallback for channels without a rule.
type AutoRule struct {
	ID              string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	Channel         string     `gorm:"type:varchar(20);not null;index" json:"channel"`
	ReplyText       string     `gorm:"type:text;not null" json:"reply_text"`
	CloseAfterHours int        `gorm:"not null;default:0" json:"close_after_hours"`
	Status          RuleStatus `gorm:"type:varchar(20);not null;index" json:"status"`
	CreatedAt       time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"not null" json:"updated_at"`
}

func (AutoRule) TableName() string { return "complaint_auto_rules" }

func ValidRuleStatus(status RuleStatus) bool {
	return status == RuleActive || status == RuleDisabled
}

// AutoRuleInput carries the writable fields of an auto reply rule.
type AutoRuleInput struct {
	Channel         string
	ReplyText       string
	CloseAfterHours int
	Status          string
}

func (service *Service) ListAutoRules(ctx context.Context) ([]AutoRule, error) {
	var rules []AutoRule
	err := service.db.WithContext(ctx).Order("channel ASC, created_at ASC").Find(&rules).Error
	return rules, err
}

func (service *Service) CreateAutoRule(ctx context.Context, input AutoRuleInput) (AutoRule, error) {
	normalized, err := normalizeAutoRule(input)
	if err != nil {
		return AutoRule{}, err
	}
	now := service.now().UTC()
	rule := AutoRule{
		ID: uuid.NewString(), Channel: normalized.Channel, ReplyText: normalized.ReplyText,
		CloseAfterHours: normalized.CloseAfterHours, Status: normalized.Status,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := service.db.WithContext(ctx).Create(&rule).Error; err != nil {
		return AutoRule{}, err
	}
	return rule, nil
}

func (service *Service) UpdateAutoRule(ctx context.Context, id string, input AutoRuleInput) (AutoRule, error) {
	var rule AutoRule
	if err := service.db.WithContext(ctx).First(&rule, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AutoRule{}, ErrNotFound
		}
		return AutoRule{}, err
	}
	normalized, err := normalizeAutoRule(input)
	if err != nil {
		return AutoRule{}, err
	}
	now := service.now().UTC()
	if err := service.db.WithContext(ctx).Model(&AutoRule{}).Where("id = ?", rule.ID).Updates(map[string]any{
		"channel":           normalized.Channel,
		"reply_text":        normalized.ReplyText,
		"close_after_hours": normalized.CloseAfterHours,
		"status":            normalized.Status,
		"updated_at":        now,
	}).Error; err != nil {
		return AutoRule{}, err
	}
	if err := service.db.WithContext(ctx).First(&rule, "id = ?", rule.ID).Error; err != nil {
		return AutoRule{}, err
	}
	return rule, nil
}

func (service *Service) DeleteAutoRule(ctx context.Context, id string) error {
	result := service.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id)).Delete(&AutoRule{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// maxAutoRulesBatch caps how many complaints one rule run touches. Without it a
// backlog of overdue complaints is loaded in full every minute, and channels
// without a rule keep the same rows pending forever.
const maxAutoRulesBatch = 100

// channelPlan describes which complaint channels a rule run may touch. It keeps
// the historical rule that a disabled channel rule blocks the wildcard fallback
// for that channel.
type channelPlan struct {
	all     bool
	include []string
	exclude []string
}

func (plan channelPlan) empty() bool { return !plan.all && len(plan.include) == 0 }

func (plan channelPlan) apply(query *gorm.DB) *gorm.DB {
	if plan.all {
		if len(plan.exclude) > 0 {
			return query.Where("channel NOT IN ?", plan.exclude)
		}
		return query
	}
	return query.Where("channel IN ?", plan.include)
}

func planChannels(rules map[string]AutoRule, usable func(AutoRule) bool) channelPlan {
	if wildcard, ok := rules[RuleWildcard]; ok && usable(wildcard) {
		plan := channelPlan{all: true}
		for channel, rule := range rules {
			if channel == RuleWildcard || usable(rule) {
				continue
			}
			plan.exclude = append(plan.exclude, channel)
		}
		sort.Strings(plan.exclude)
		return plan
	}
	plan := channelPlan{}
	for channel, rule := range rules {
		if channel == RuleWildcard || !usable(rule) {
			continue
		}
		plan.include = append(plan.include, channel)
	}
	sort.Strings(plan.include)
	return plan
}

// RunAutoRules applies the active rules: pending complaints past their deadline
// get one system reply, replied complaints past deadline + close_after_hours are
// closed. Both steps are guarded by the source status, so a second run cannot
// reply or close the same complaint twice. A disabled channel rule blocks the
// wildcard fallback on purpose.
func (service *Service) RunAutoRules(ctx context.Context, now time.Time) (int, int, error) {
	var rules []AutoRule
	if err := service.db.WithContext(ctx).Find(&rules).Error; err != nil {
		return 0, 0, err
	}
	byChannel := make(map[string]AutoRule, len(rules))
	active := 0
	for _, rule := range rules {
		byChannel[rule.Channel] = rule
		if rule.Status == RuleActive {
			active++
		}
	}
	if active == 0 {
		// Nothing can move: skip the scan instead of re-reading the backlog.
		return 0, 0, nil
	}
	replyPlan := planChannels(byChannel, func(rule AutoRule) bool { return rule.Status == RuleActive })
	closePlan := planChannels(byChannel, func(rule AutoRule) bool {
		return rule.Status == RuleActive && rule.CloseAfterHours > 0
	})

	replied := 0
	if !replyPlan.empty() {
		pending := make([]Complaint, 0, maxAutoRulesBatch)
		query := replyPlan.apply(service.db.WithContext(ctx).
			Where("status = ? AND deadline_at IS NOT NULL AND deadline_at < ?", StatusPending, now.UTC()))
		if err := query.Order("deadline_at ASC, id ASC").Limit(maxAutoRulesBatch).Find(&pending).Error; err != nil {
			return 0, 0, err
		}
		for _, complaint := range pending {
			rule, ok := byChannel[strings.ToLower(strings.TrimSpace(complaint.Channel))]
			if !ok {
				rule, ok = byChannel[RuleWildcard]
			}
			if !ok || rule.Status != RuleActive {
				continue
			}
			applied, err := service.autoReply(ctx, complaint, rule.ReplyText, now)
			if err != nil {
				return replied, 0, err
			}
			if applied {
				replied++
			}
		}
	}

	closed := 0
	if !closePlan.empty() {
		repliedComplaints := make([]Complaint, 0, maxAutoRulesBatch)
		query := closePlan.apply(service.db.WithContext(ctx).
			Where("status = ? AND deadline_at IS NOT NULL AND deadline_at < ?", StatusReplied, now.UTC()))
		if err := query.Order("deadline_at ASC, id ASC").Limit(maxAutoRulesBatch).Find(&repliedComplaints).Error; err != nil {
			return replied, 0, err
		}
		for _, complaint := range repliedComplaints {
			rule, ok := byChannel[strings.ToLower(strings.TrimSpace(complaint.Channel))]
			if !ok {
				rule, ok = byChannel[RuleWildcard]
			}
			if !ok || rule.Status != RuleActive || rule.CloseAfterHours <= 0 {
				continue
			}
			closeAt := complaint.DeadlineAt.UTC().Add(time.Duration(rule.CloseAfterHours) * time.Hour)
			if now.UTC().Before(closeAt) {
				continue
			}
			applied, err := service.autoClose(ctx, complaint, now)
			if err != nil {
				return replied, closed, err
			}
			if applied {
				closed++
			}
		}
	}
	return replied, closed, nil
}

func (service *Service) autoReply(ctx context.Context, complaint Complaint, text string, now time.Time) (bool, error) {
	applied := false
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Complaint{}).Where("id = ? AND status = ?", complaint.ID, StatusPending).
			Updates(map[string]any{"status": StatusReplied, "updated_at": now.UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		message := Message{
			ID: uuid.NewString(), ComplaintID: complaint.ID, Direction: DirectionOut,
			Content: text, Operator: optional(SystemOperator), CreatedAt: now.UTC(),
		}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, err
}

func (service *Service) autoClose(ctx context.Context, complaint Complaint, now time.Time) (bool, error) {
	applied := false
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		closedAt := now.UTC()
		result := tx.Model(&Complaint{}).Where("id = ? AND status = ?", complaint.ID, StatusReplied).
			Updates(map[string]any{"status": StatusClosed, "closed_at": closedAt, "updated_at": closedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		message := Message{
			ID: uuid.NewString(), ComplaintID: complaint.ID, Direction: DirectionOut,
			Content: AutoCloseReason, Operator: optional(SystemOperator), CreatedAt: closedAt,
		}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, err
}

func normalizeAutoRule(input AutoRuleInput) (AutoRule, error) {
	channel := strings.ToLower(strings.TrimSpace(input.Channel))
	replyText := strings.TrimSpace(input.ReplyText)
	if channel == "" || len(channel) > 20 || replyText == "" || len([]rune(replyText)) > 2000 ||
		input.CloseAfterHours < 0 || input.CloseAfterHours > 8760 {
		return AutoRule{}, ErrInvalidRequest
	}
	status := RuleStatus(strings.ToLower(strings.TrimSpace(input.Status)))
	if status == "" {
		status = RuleActive
	}
	if !ValidRuleStatus(status) {
		return AutoRule{}, ErrInvalidRequest
	}
	return AutoRule{Channel: channel, ReplyText: replyText, CloseAfterHours: input.CloseAfterHours, Status: status}, nil
}
