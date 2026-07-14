package query

import (
	"context"
	"errors"
	"strings"
	"time"

	paymentmodel "gorixpay/internal/payment/model"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("payment order not found")

type Page struct {
	Number int
	Size   int
}

type OrderFilter struct {
	Status      paymentmodel.Status
	PaymentType string
	Keyword     string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

type Statistics struct {
	OrderCount     int64                         `json:"order_count"`
	TotalAmount    int64                         `json:"total_amount"`
	PaidOrderCount int64                         `json:"paid_order_count"`
	PaidAmount     int64                         `json:"paid_amount"`
	StatusCounts   map[paymentmodel.Status]int64 `json:"status_counts"`
}

type Service struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (service *Service) Orders(ctx context.Context, merchantID string, filter OrderFilter, page Page) ([]paymentmodel.Order, int64, error) {
	query := service.filtered(ctx, merchantID, filter)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orders []paymentmodel.Order
	if err := query.Order("created_at DESC, id DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

func (service *Service) Order(ctx context.Context, merchantID, id string) (paymentmodel.Order, error) {
	var order paymentmodel.Order
	if err := service.db.WithContext(ctx).Where("merchant_id = ? AND id = ?", merchantID, id).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return paymentmodel.Order{}, ErrNotFound
		}
		return paymentmodel.Order{}, err
	}
	return order, nil
}

func (service *Service) Statistics(ctx context.Context, merchantID string, filter OrderFilter) (Statistics, error) {
	type statusAggregate struct {
		Status paymentmodel.Status
		Count  int64
		Amount int64
	}
	var aggregates []statusAggregate
	if err := service.filtered(ctx, merchantID, filter).Select("status, COUNT(*) AS count, COALESCE(SUM(amount), 0) AS amount").Group("status").Scan(&aggregates).Error; err != nil {
		return Statistics{}, err
	}
	result := Statistics{StatusCounts: make(map[paymentmodel.Status]int64, len(aggregates))}
	for _, aggregate := range aggregates {
		result.OrderCount += aggregate.Count
		result.TotalAmount += aggregate.Amount
		result.StatusCounts[aggregate.Status] = aggregate.Count
		if aggregate.Status == paymentmodel.StatusPaid {
			result.PaidOrderCount = aggregate.Count
			result.PaidAmount = aggregate.Amount
		}
	}
	return result, nil
}

func (service *Service) filtered(ctx context.Context, merchantID string, filter OrderFilter) *gorm.DB {
	query := service.db.WithContext(ctx).Model(&paymentmodel.Order{}).Where("merchant_id = ?", merchantID)
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.PaymentType != "" {
		query = query.Where("payment_type = ?", filter.PaymentType)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("trade_no LIKE ? OR out_trade_no LIKE ? OR name LIKE ?", like, like, like)
	}
	if filter.CreatedFrom != nil {
		query = query.Where("created_at >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query = query.Where("created_at < ?", *filter.CreatedTo)
	}
	return query
}
