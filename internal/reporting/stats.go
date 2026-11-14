package reporting

import (
	"context"
	"strings"
	"time"

	financemodel "gorixpay/internal/finance/model"
	paymentmodel "gorixpay/internal/payment/model"

	"gorm.io/gorm"
)

type buyerStat struct {
	Buyer       string     `json:"buyer"`
	OrderCount  int64      `json:"order_count"`
	PaidAmount  int64      `json:"paid_amount"`
	FirstPaidAt *time.Time `json:"first_paid_at"`
	LastPaidAt  *time.Time `json:"last_paid_at"`
}

type merchantStat struct {
	MerchantID   string `json:"merchant_id"`
	DisplayName  string `json:"display_name"`
	OrderCount   int64  `json:"order_count"`
	PaidAmount   int64  `json:"paid_amount"`
	RefundAmount int64  `json:"refund_amount"`
	FeeAmount    int64  `json:"fee_amount"`
}

type merchantDailyStat struct {
	Date       string `json:"date"`
	OrderCount int64  `json:"order_count"`
	PaidAmount int64  `json:"paid_amount"`
}

type buyerAggregate struct {
	BuyerKey    string  `gorm:"column:buyer_key"`
	OrderCount  int64   `gorm:"column:order_count"`
	PaidAmount  int64   `gorm:"column:paid_amount"`
	FirstPaidAt *string `gorm:"column:first_paid_at"`
	LastPaidAt  *string `gorm:"column:last_paid_at"`
}

type merchantAggregate struct {
	MerchantID  string `gorm:"column:merchant_id"`
	DisplayName string `gorm:"column:display_name"`
	OrderCount  int64  `gorm:"column:order_count"`
	PaidAmount  int64  `gorm:"column:paid_amount"`
}

type refundAggregate struct {
	MerchantID   string `gorm:"column:merchant_id"`
	RefundAmount int64  `gorm:"column:refund_amount"`
}

type buyerFilter struct {
	MerchantID  string
	Keyword     string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

type merchantFilter struct {
	Keyword     string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

// buyerKeyExpression collapses the payer identity: the channel buyer field when
// present, then the client IP that V1 recorded for every order.
const buyerKeyExpression = "COALESCE(NULLIF(buyer, ''), NULLIF(client_ip, ''), '(unknown)')"

func buyerQuery(db *gorm.DB, filter buyerFilter) *gorm.DB {
	query := db.Model(&paymentmodel.Order{}).Where("status = ?", paymentmodel.StatusPaid)
	if filter.MerchantID != "" {
		query = query.Where("merchant_id = ?", filter.MerchantID)
	}
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		query = query.Where("buyer LIKE ? OR client_ip LIKE ?", like, like)
	}
	if filter.CreatedFrom != nil {
		query = query.Where("paid_at >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query = query.Where("paid_at < ?", *filter.CreatedTo)
	}
	return query
}

func loadBuyerStats(db *gorm.DB, filter buyerFilter, page Page) ([]buyerStat, int64, error) {
	total := int64(0)
	if err := buyerQuery(db, filter).
		Select("COUNT(DISTINCT " + buyerKeyExpression + ")").
		Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	var aggregates []buyerAggregate
	if err := buyerQuery(db, filter).
		Select(buyerAggregateSelect).
		Group("buyer_key").
		Order("paid_amount DESC, buyer_key ASC").
		Offset((page.Number - 1) * page.Size).Limit(page.Size).
		Scan(&aggregates).Error; err != nil {
		return nil, 0, err
	}
	items := make([]buyerStat, 0, len(aggregates))
	for _, aggregate := range aggregates {
		items = append(items, buyerStat{
			Buyer:       aggregate.BuyerKey,
			OrderCount:  aggregate.OrderCount,
			PaidAmount:  aggregate.PaidAmount,
			FirstPaidAt: parseDatabaseTime(aggregate.FirstPaidAt),
			LastPaidAt:  parseDatabaseTime(aggregate.LastPaidAt),
		})
	}
	return items, total, nil
}

const buyerAggregateSelect = buyerKeyExpression + " AS buyer_key, COUNT(*) AS order_count, " +
	"COALESCE(SUM(amount), 0) AS paid_amount, MIN(paid_at) AS first_paid_at, MAX(paid_at) AS last_paid_at"

// loadBuyerExport reuses the statistics aggregation without pagination, capped
// by the export row limit.
func loadBuyerExport(db *gorm.DB, filter buyerFilter, limit int) ([]buyerStat, bool, error) {
	var aggregates []buyerAggregate
	if err := buyerQuery(db, filter).
		Select(buyerAggregateSelect).
		Group("buyer_key").
		Order("paid_amount DESC, buyer_key ASC").
		Limit(limit + 1).
		Scan(&aggregates).Error; err != nil {
		return nil, false, err
	}
	aggregates, truncated := trimExport(aggregates)
	items := make([]buyerStat, 0, len(aggregates))
	for _, aggregate := range aggregates {
		items = append(items, buyerStat{
			Buyer:       aggregate.BuyerKey,
			OrderCount:  aggregate.OrderCount,
			PaidAmount:  aggregate.PaidAmount,
			FirstPaidAt: parseDatabaseTime(aggregate.FirstPaidAt),
			LastPaidAt:  parseDatabaseTime(aggregate.LastPaidAt),
		})
	}
	return items, truncated, nil
}

func merchantQuery(db *gorm.DB, filter merchantFilter) *gorm.DB {
	query := db.Table("payment_orders AS o").
		Joins("LEFT JOIN merchants AS m ON m.id = o.merchant_id")
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		query = query.Where("o.merchant_id LIKE ? OR m.display_name LIKE ? OR m.email LIKE ?", like, like, like)
	}
	if filter.CreatedFrom != nil {
		query = query.Where("o.created_at >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query = query.Where("o.created_at < ?", *filter.CreatedTo)
	}
	return query
}

// loadMerchantStats aggregates order volume per merchant, merges approved
// refunds for the current page only, and estimates the fee per merchant. The
// explicit override wins when present; otherwise the resolver (merchant group
// rate) decides, and an unknown rate means no fee.
func loadMerchantStats(ctx context.Context, db *gorm.DB, filter merchantFilter, resolver FeeResolver, override *int, page Page) ([]merchantStat, int64, error) {
	total := int64(0)
	if err := merchantQuery(db, filter).
		Select("COUNT(DISTINCT o.merchant_id)").
		Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	var aggregates []merchantAggregate
	if err := merchantQuery(db, filter).
		Select("o.merchant_id AS merchant_id, COALESCE(m.display_name, '') AS display_name, "+
			"COUNT(*) AS order_count, "+
			"COALESCE(SUM(CASE WHEN o.status = ? THEN o.amount ELSE 0 END), 0) AS paid_amount",
			paymentmodel.StatusPaid).
		Group("o.merchant_id, m.display_name").
		Order("paid_amount DESC, o.merchant_id ASC").
		Offset((page.Number - 1) * page.Size).Limit(page.Size).
		Scan(&aggregates).Error; err != nil {
		return nil, 0, err
	}
	merchantIDs := make([]string, 0, len(aggregates))
	for _, aggregate := range aggregates {
		merchantIDs = append(merchantIDs, aggregate.MerchantID)
	}
	refundByMerchant := make(map[string]int64, len(merchantIDs))
	if len(merchantIDs) > 0 {
		var refunds []refundAggregate
		query := db.Table("refund_orders").
			Select("merchant_id AS merchant_id, COALESCE(SUM(amount), 0) AS refund_amount").
			Where("merchant_id IN ?", merchantIDs).
			Where("status = ?", financemodel.StatusApproved)
		if filter.CreatedFrom != nil {
			query = query.Where("created_at >= ?", *filter.CreatedFrom)
		}
		if filter.CreatedTo != nil {
			query = query.Where("created_at < ?", *filter.CreatedTo)
		}
		if err := query.Group("merchant_id").Scan(&refunds).Error; err != nil {
			return nil, 0, err
		}
		for _, refund := range refunds {
			refundByMerchant[refund.MerchantID] = refund.RefundAmount
		}
	}
	items := make([]merchantStat, 0, len(aggregates))
	for _, aggregate := range aggregates {
		rateBp := merchantRateBp(ctx, resolver, override, aggregate.MerchantID)
		items = append(items, merchantStat{
			MerchantID:   aggregate.MerchantID,
			DisplayName:  aggregate.DisplayName,
			OrderCount:   aggregate.OrderCount,
			PaidAmount:   aggregate.PaidAmount,
			RefundAmount: refundByMerchant[aggregate.MerchantID],
			FeeAmount:    aggregate.PaidAmount * int64(rateBp) / 10000,
		})
	}
	return items, total, nil
}

// merchantRateBp resolves the fee rate for one merchant. A nil resolver and an
// unknown merchant both mean zero; the override always wins when provided.
func merchantRateBp(ctx context.Context, resolver FeeResolver, override *int, merchantID string) int {
	if override != nil {
		return *override
	}
	if resolver == nil {
		return 0
	}
	rate, ok := resolver(ctx, merchantID)
	if !ok || rate < 0 {
		return 0
	}
	return rate
}

// loadMerchantDaily builds one bucket per day. Per-day queries keep the SQL
// dialect neutral (no date()/DATE() differences between SQLite, MySQL and
// PostgreSQL) and the window is capped at 90 days.
func loadMerchantDaily(db *gorm.DB, merchantID string, days int, now time.Time) ([]merchantDailyStat, error) {
	today := startOfDay(now)
	items := make([]merchantDailyStat, 0, days)
	for offset := days - 1; offset >= 0; offset-- {
		start := today.AddDate(0, 0, -offset)
		end := start.AddDate(0, 0, 1)
		item := merchantDailyStat{Date: start.Format("2006-01-02")}
		if err := db.Model(&paymentmodel.Order{}).
			Where("merchant_id = ? AND created_at >= ? AND created_at < ?", merchantID, start, end).
			Count(&item.OrderCount).Error; err != nil {
			return nil, err
		}
		if err := db.Model(&paymentmodel.Order{}).
			Select("COALESCE(SUM(amount), 0)").
			Where("merchant_id = ? AND status = ? AND paid_at >= ? AND paid_at < ?", merchantID, paymentmodel.StatusPaid, start, end).
			Scan(&item.PaidAmount).Error; err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func startOfDay(at time.Time) time.Time {
	utc := at.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

// parseDatabaseTime normalizes the string form some drivers return for SQL
// aggregate timestamps (MIN/MAX lose the column type on SQLite). Unparseable
// values degrade to nil instead of failing the whole report.
func parseDatabaseTime(raw *string) *time.Time {
	if raw == nil {
		return nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			utc := parsed.UTC()
			return &utc
		}
	}
	return nil
}
