package invite

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	"gorixpay/internal/money"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Settings keys with their defaults.
const (
	SettingEnabled = "invite.enabled"
	SettingRateBP  = "invite.rate_bp"
)

// Service implements invite statistics and the rebate post-payment hook.
type Service struct {
	db      *gorm.DB
	ledger  *ledgerservice.Service
	siteURL string
	now     func() time.Time
}

func New(db *gorm.DB, ledger *ledgerservice.Service, siteURL string, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, ledger: ledger, siteURL: strings.TrimRight(siteURL, "/"), now: now}
}

// Enabled reports whether the invite rebate feature is on.
func (service *Service) Enabled(ctx context.Context) bool {
	return service.boolSetting(ctx, SettingEnabled, false)
}

// RateBP returns the configured rebate rate in basis points (100 = 1%).
func (service *Service) RateBP(ctx context.Context) int64 {
	return service.intSetting(ctx, SettingRateBP, 0)
}

// Stats aggregates the merchant invite dashboard.
func (service *Service) Stats(ctx context.Context, merchantID string) (Stats, error) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return Stats{}, ErrInvalidRequest
	}
	var merchant identitymodel.Merchant
	if err := service.db.WithContext(ctx).First(&merchant, "id = ?", merchantID).Error; err != nil {
		return Stats{}, ErrNotFound
	}
	stats := Stats{
		Enabled:    service.Enabled(ctx),
		RateBP:     service.RateBP(ctx),
		InviteCode: inviteCodeOf(merchant),
	}
	if stats.InviteCode != "" {
		stats.InviteURL = service.siteURL + "/register?invite=" + stats.InviteCode
	}
	db := service.db.WithContext(ctx)
	if err := db.Model(&identitymodel.Merchant{}).Where("invited_by = ?", merchantID).Count(&stats.InvitedCount).Error; err != nil {
		return Stats{}, err
	}
	if err := db.Model(&Rebate{}).Where("inviter_id = ?", merchantID).Select("COALESCE(SUM(rebate_amount), 0)").Scan(&stats.TotalIncome).Error; err != nil {
		return Stats{}, err
	}
	start := startOfDay(service.now())
	if err := db.Model(&Rebate{}).Where("inviter_id = ? AND created_at >= ?", merchantID, start).Select("COALESCE(SUM(rebate_amount), 0)").Scan(&stats.TodayIncome).Error; err != nil {
		return Stats{}, err
	}
	return stats, nil
}

// Records lists rebates for one inviter.
func (service *Service) Records(ctx context.Context, inviterID string, page Page) ([]RecordView, int64, error) {
	return service.records(ctx, strings.TrimSpace(inviterID), page)
}

// AdminRecords lists rebates across inviters, optionally filtered.
func (service *Service) AdminRecords(ctx context.Context, inviterID string, page Page) ([]RecordView, int64, error) {
	return service.records(ctx, strings.TrimSpace(inviterID), page)
}

func (service *Service) records(ctx context.Context, inviterID string, page Page) ([]RecordView, int64, error) {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 || page.Size > 100 {
		page.Size = 20
	}
	query := service.db.WithContext(ctx).Model(&Rebate{})
	if inviterID != "" {
		query = query.Where("inviter_id = ?", inviterID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rebates []Rebate
	if err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&rebates).Error; err != nil {
		return nil, 0, err
	}
	views := make([]RecordView, 0, len(rebates))
	names := service.inviteeNames(ctx, rebates)
	for _, rebate := range rebates {
		views = append(views, RecordView{Rebate: rebate, InviteeName: names[rebate.InviteeID]})
	}
	return views, total, nil
}

func (service *Service) inviteeNames(ctx context.Context, rebates []Rebate) map[string]string {
	names := make(map[string]string, len(rebates))
	if len(rebates) == 0 {
		return names
	}
	ids := make([]string, 0, len(rebates))
	for _, rebate := range rebates {
		ids = append(ids, rebate.InviteeID)
	}
	var merchants []identitymodel.Merchant
	if err := service.db.WithContext(ctx).Where("id IN ?", ids).Find(&merchants).Error; err != nil {
		return names
	}
	for _, merchant := range merchants {
		names[merchant.ID] = merchant.DisplayName
	}
	return names
}

// Hook returns the post-payment hook that books invite rebates.
func (service *Service) Hook() *Hook {
	return &Hook{service: service}
}

// Hook implements paymentcallback.PostPaymentHook.
type Hook struct {
	service *Service
}

func (hook *Hook) OnPayment(ctx context.Context, tx *gorm.DB, order paymentmodel.Order) error {
	if hook == nil || hook.service == nil || tx == nil {
		return nil
	}
	if !order.IsSale() || order.Amount <= 0 {
		return nil
	}
	service := hook.service
	if !service.boolSettingTx(ctx, tx, SettingEnabled, false) {
		return nil
	}
	rate := service.intSettingTx(ctx, tx, SettingRateBP, 0)
	if rate <= 0 || rate > 10000 {
		return nil
	}
	var invitee identitymodel.Merchant
	if err := tx.WithContext(ctx).First(&invitee, "id = ?", order.MerchantID).Error; err != nil {
		return nil
	}
	if invitee.InvitedBy == nil || strings.TrimSpace(*invitee.InvitedBy) == "" {
		return nil
	}
	inviterID := *invitee.InvitedBy
	if inviterID == order.MerchantID {
		return nil
	}
	rebateAmount := money.ApplyBasisPoints(order.Amount, int(rate))
	if rebateAmount <= 0 {
		return nil
	}
	var existing Rebate
	if err := tx.WithContext(ctx).Where("order_id = ?", order.ID).First(&existing).Error; err == nil {
		return nil
	} else if err != gorm.ErrRecordNotFound {
		return err
	}
	if service.ledger != nil {
		inviter, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
			OwnerType: "merchant", OwnerID: inviterID, Purpose: "balance",
			Category: ledgermodel.CategoryLiability, Currency: order.Currency,
		})
		if err != nil {
			return err
		}
		platform, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
			OwnerType: "platform", OwnerID: "platform", Purpose: "cash",
			Category: ledgermodel.CategoryAsset, Currency: order.Currency,
		})
		if err != nil {
			return err
		}
		if _, err := service.ledger.PostInTransaction(tx, ledgerservice.TransactionRequest{
			IdempotencyKey: "rebate:" + order.ID,
			ReferenceType:  "invite_rebate",
			ReferenceID:    order.ID,
			Description:    "invite rebate for " + order.TradeNo,
			Postings: []ledgerservice.Posting{
				{AccountID: inviter.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: rebateAmount},
				{AccountID: platform.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: rebateAmount},
			},
		}); err != nil {
			return err
		}
	}
	record := Rebate{
		ID: uuid.NewString(), InviterID: inviterID, InviteeID: order.MerchantID, OrderID: order.ID,
		TradeNo: order.TradeNo, OrderAmount: order.Amount, RebateAmount: rebateAmount,
		RateBP: int(rate), CreatedAt: service.now().UTC(),
	}
	return tx.WithContext(ctx).Create(&record).Error
}

func (service *Service) boolSetting(ctx context.Context, key string, fallback bool) bool {
	raw, ok := service.setting(ctx, service.db, key)
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
	raw, ok := service.setting(ctx, service.db, key)
	if !ok {
		return fallback
	}
	return parseSettingInt(raw, fallback)
}

func (service *Service) boolSettingTx(ctx context.Context, tx *gorm.DB, key string, fallback bool) bool {
	raw, ok := service.setting(ctx, tx, key)
	if !ok {
		return fallback
	}
	var value bool
	if err := json.Unmarshal([]byte(raw), &value); err == nil {
		return value
	}
	return strings.EqualFold(strings.Trim(strings.TrimSpace(raw), `"`), "true")
}

func (service *Service) intSettingTx(ctx context.Context, tx *gorm.DB, key string, fallback int64) int64 {
	raw, ok := service.setting(ctx, tx, key)
	if !ok {
		return fallback
	}
	return parseSettingInt(raw, fallback)
}

func (service *Service) setting(ctx context.Context, db *gorm.DB, key string) (string, bool) {
	type systemSetting struct {
		Value string `gorm:"column:setting_value"`
	}
	var record systemSetting
	if err := db.WithContext(ctx).Table("system_settings").Where("setting_key = ?", key).First(&record).Error; err != nil {
		return "", false
	}
	return record.Value, true
}

func parseSettingInt(raw string, fallback int64) int64 {
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
	return fallback
}

func inviteCodeOf(merchant identitymodel.Merchant) string {
	if merchant.InviteCode == nil {
		return ""
	}
	return *merchant.InviteCode
}

func startOfDay(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
