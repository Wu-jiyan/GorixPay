package service

import (
	"context"
	"errors"
	"time"

	paymentmodel "gorixpay/internal/payment/model"

	"gorm.io/gorm"
)

var (
	ErrOrderNotFound    = errors.New("order not found")
	ErrInvalidQuery     = errors.New("trade_no or out_trade_no is required")
	ErrOrderNotClosable = errors.New("order cannot be closed")
)

type PublicOrder struct {
	TradeNo     string
	Name        string
	Amount      int64
	Currency    string
	PaymentType string
	Status      paymentmodel.Status
	ExpiresAt   time.Time
}

func (service *Service) Find(ctx context.Context, merchantID, tradeNo, outTradeNo string) (paymentmodel.Order, error) {
	if merchantID == "" || (tradeNo == "" && outTradeNo == "") {
		return paymentmodel.Order{}, ErrInvalidQuery
	}
	query := service.db.WithContext(ctx).Where("merchant_id = ?", merchantID)
	if tradeNo != "" {
		query = query.Where("trade_no = ?", tradeNo)
	} else {
		query = query.Where("out_trade_no = ?", outTradeNo)
	}
	var found paymentmodel.Order
	if err := query.First(&found).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return paymentmodel.Order{}, ErrOrderNotFound
		}
		return paymentmodel.Order{}, err
	}
	return found, nil
}

func (service *Service) Close(ctx context.Context, merchantID, tradeNo, outTradeNo string) (paymentmodel.Order, bool, error) {
	found, err := service.Find(ctx, merchantID, tradeNo, outTradeNo)
	if err != nil {
		return paymentmodel.Order{}, false, err
	}
	if found.Status == paymentmodel.StatusClosed {
		return found, false, nil
	}
	if found.Status != paymentmodel.StatusPending && found.Status != paymentmodel.StatusProcessing {
		return paymentmodel.Order{}, false, ErrOrderNotClosable
	}
	now := service.now().UTC()
	result := service.db.WithContext(ctx).Model(&paymentmodel.Order{}).
		Where("id = ? AND status IN ?", found.ID, []paymentmodel.Status{paymentmodel.StatusPending, paymentmodel.StatusProcessing}).
		Updates(map[string]any{"status": paymentmodel.StatusClosed, "updated_at": now})
	if result.Error != nil {
		return paymentmodel.Order{}, false, result.Error
	}
	if result.RowsAffected == 0 {
		latest, err := service.Find(ctx, merchantID, tradeNo, outTradeNo)
		if err != nil {
			return paymentmodel.Order{}, false, err
		}
		if latest.Status == paymentmodel.StatusClosed {
			return latest, false, nil
		}
		return paymentmodel.Order{}, false, ErrOrderNotClosable
	}
	found.Status = paymentmodel.StatusClosed
	found.UpdatedAt = now
	return found, true, nil
}

func (service *Service) FindPublic(ctx context.Context, tradeNo string) (PublicOrder, error) {
	if tradeNo == "" {
		return PublicOrder{}, ErrInvalidQuery
	}
	var found paymentmodel.Order
	if err := service.db.WithContext(ctx).Where("trade_no = ?", tradeNo).First(&found).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PublicOrder{}, ErrOrderNotFound
		}
		return PublicOrder{}, err
	}
	return PublicOrder{
		TradeNo: found.TradeNo, Name: found.Name, Amount: found.Amount, Currency: found.Currency,
		PaymentType: found.PaymentType, Status: found.Status, ExpiresAt: found.ExpiresAt,
	}, nil
}
