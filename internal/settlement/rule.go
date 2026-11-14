package settlement

import (
	"context"
	"errors"
	"strings"

	identitymodel "gorixpay/internal/identity/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Page is a simple offset pagination request shared by the rule listing.
type Page struct {
	Number int
	Size   int
}

// RuleInput carries the writable fields of a settle rule.
type RuleInput struct {
	Name           string
	MerchantID     *string
	MinAmountCents int64
	FeeBP          int
	Period         string
	AutoTransfer   bool
	Status         string
}

func (service *Service) ListRules(ctx context.Context, merchantID, status string, page Page) ([]Rule, int64, error) {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 || page.Size > 100 {
		page.Size = 20
	}
	query := service.db.WithContext(ctx).Model(&Rule{})
	if merchantID = strings.TrimSpace(merchantID); merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	if status = strings.TrimSpace(status); status != "" {
		if !ValidRuleStatus(RuleStatus(status)) {
			return nil, 0, ErrInvalidRequest
		}
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rules []Rule
	err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&rules).Error
	return rules, total, err
}

func (service *Service) GetRule(ctx context.Context, id string) (Rule, error) {
	var rule Rule
	if err := service.db.WithContext(ctx).First(&rule, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Rule{}, ErrRuleNotFound
		}
		return Rule{}, err
	}
	return rule, nil
}

func (service *Service) CreateRule(ctx context.Context, input RuleInput) (Rule, error) {
	normalized, err := service.normalizeRule(ctx, input)
	if err != nil {
		return Rule{}, err
	}
	now := service.now().UTC()
	rule := Rule{
		ID:             uuid.NewString(),
		Name:           normalized.Name,
		MerchantID:     normalized.MerchantID,
		MinAmountCents: normalized.MinAmountCents,
		FeeBP:          normalized.FeeBP,
		Period:         normalized.Period,
		AutoTransfer:   normalized.AutoTransfer,
		Status:         normalized.Status,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := service.db.WithContext(ctx).Create(&rule).Error; err != nil {
		return Rule{}, err
	}
	return rule, nil
}

func (service *Service) UpdateRule(ctx context.Context, id string, input RuleInput) (Rule, error) {
	existing, err := service.GetRule(ctx, id)
	if err != nil {
		return Rule{}, err
	}
	normalized, err := service.normalizeRule(ctx, input)
	if err != nil {
		return Rule{}, err
	}
	now := service.now().UTC()
	updates := map[string]any{
		"name":             normalized.Name,
		"merchant_id":      normalized.MerchantID,
		"min_amount_cents": normalized.MinAmountCents,
		"fee_bp":           normalized.FeeBP,
		"period":           normalized.Period,
		"auto_transfer":    normalized.AutoTransfer,
		"status":           normalized.Status,
		"updated_at":       now,
	}
	if err := service.db.WithContext(ctx).Model(&Rule{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return Rule{}, err
	}
	return service.GetRule(ctx, existing.ID)
}

func (service *Service) DeleteRule(ctx context.Context, id string) error {
	result := service.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id)).Delete(&Rule{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRuleNotFound
	}
	return nil
}

func (service *Service) normalizeRule(ctx context.Context, input RuleInput) (Rule, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 100 || input.MinAmountCents < 0 || input.FeeBP < 0 || input.FeeBP > 10000 {
		return Rule{}, ErrInvalidRequest
	}
	period := Period(strings.ToLower(strings.TrimSpace(input.Period)))
	if period == "" {
		period = PeriodDaily
	}
	if !ValidPeriod(period) {
		return Rule{}, ErrInvalidRequest
	}
	status := RuleStatus(strings.ToLower(strings.TrimSpace(input.Status)))
	if status == "" {
		status = RuleActive
	}
	if !ValidRuleStatus(status) {
		return Rule{}, ErrInvalidRequest
	}
	var merchantID *string
	if input.MerchantID != nil {
		trimmed := strings.TrimSpace(*input.MerchantID)
		if trimmed != "" {
			var merchant identitymodel.Merchant
			if err := service.db.WithContext(ctx).First(&merchant, "id = ?", trimmed).Error; err != nil {
				return Rule{}, ErrInvalidRequest
			}
			merchantID = &trimmed
		}
	}
	return Rule{
		Name:           name,
		MerchantID:     merchantID,
		MinAmountCents: input.MinAmountCents,
		FeeBP:          input.FeeBP,
		Period:         period,
		AutoTransfer:   input.AutoTransfer,
		Status:         status,
	}, nil
}
