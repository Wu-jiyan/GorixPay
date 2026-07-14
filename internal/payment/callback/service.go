package callback

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorixpay/internal/outbox"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrOrderNotFound        = errors.New("payment order not found")
	ErrAmountMismatch       = errors.New("callback amount mismatch")
	ErrChannelTradeConflict = errors.New("channel trade number conflict")
	ErrOrderNotPayable      = errors.New("payment order is not payable")
)

type Completion struct {
	OrderID        string
	ChannelID      string
	ChannelTradeNo string
	Amount         int64
	Digest         string
}

type Record struct {
	ID             string    `gorm:"type:varchar(36);primaryKey"`
	OrderID        string    `gorm:"type:varchar(36);not null;index"`
	ChannelID      string    `gorm:"type:varchar(36);not null;uniqueIndex:idx_channel_trade"`
	ChannelTradeNo string    `gorm:"type:varchar(128);not null;uniqueIndex:idx_channel_trade"`
	Digest         string    `gorm:"type:varchar(64);not null;uniqueIndex"`
	Amount         int64     `gorm:"not null"`
	CreatedAt      time.Time `gorm:"not null"`
}

func (Record) TableName() string {
	return "payment_callbacks"
}

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

func (service *Service) Complete(ctx context.Context, completion Completion) (paymentmodel.Order, bool, error) {
	if completion.OrderID == "" || completion.ChannelID == "" || completion.ChannelTradeNo == "" || completion.Digest == "" {
		return paymentmodel.Order{}, false, ErrOrderNotPayable
	}
	var result paymentmodel.Order
	applied := false
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var duplicate Record
		if err := tx.Where("digest = ?", completion.Digest).First(&duplicate).Error; err == nil {
			if duplicate.OrderID != completion.OrderID {
				return ErrChannelTradeConflict
			}
			if err := tx.First(&result, "id = ?", completion.OrderID).Error; err != nil {
				return err
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.First(&result, "id = ?", completion.OrderID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrOrderNotFound
			}
			return err
		}
		if result.Amount != completion.Amount {
			return ErrAmountMismatch
		}
		if result.Status == paymentmodel.StatusPaid {
			if result.ChannelTradeNo != nil && *result.ChannelTradeNo == completion.ChannelTradeNo {
				return nil
			}
			return ErrOrderNotPayable
		}
		if result.Status != paymentmodel.StatusPending && result.Status != paymentmodel.StatusProcessing {
			return ErrOrderNotPayable
		}
		var channelTradeCount int64
		if err := tx.Model(&Record{}).
			Where("channel_id = ? AND channel_trade_no = ?", completion.ChannelID, completion.ChannelTradeNo).
			Count(&channelTradeCount).Error; err != nil {
			return err
		}
		if channelTradeCount > 0 {
			return ErrChannelTradeConflict
		}
		now := service.now().UTC()
		record := Record{
			ID: uuid.NewString(), OrderID: completion.OrderID, ChannelID: completion.ChannelID,
			ChannelTradeNo: completion.ChannelTradeNo, Digest: completion.Digest, Amount: completion.Amount, CreatedAt: now,
		}
		if err := tx.Create(&record).Error; err != nil {
			return ErrChannelTradeConflict
		}
		update := tx.Model(&paymentmodel.Order{}).
			Where("id = ? AND status IN ?", result.ID, []paymentmodel.Status{paymentmodel.StatusPending, paymentmodel.StatusProcessing}).
			Updates(map[string]any{
				"status": paymentmodel.StatusPaid, "channel_trade_no": completion.ChannelTradeNo,
				"paid_at": now, "updated_at": now,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrOrderNotPayable
		}
		payload, err := json.Marshal(map[string]any{
			"order_id": result.ID, "merchant_id": result.MerchantID, "trade_no": result.TradeNo,
			"out_trade_no": result.OutTradeNo, "channel_trade_no": completion.ChannelTradeNo,
			"amount": result.Amount, "paid_at": now,
		})
		if err != nil {
			return err
		}
		message := outbox.Message{
			ID: uuid.NewString(), Topic: "merchant.payment.notify", AggregateID: result.ID,
			IdempotencyKey: "merchant-payment-notify:" + result.ID, Payload: string(payload),
			Status: outbox.StatusPending, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		result.Status = paymentmodel.StatusPaid
		result.ChannelTradeNo = &completion.ChannelTradeNo
		result.PaidAt = &now
		result.UpdatedAt = now
		applied = true
		return nil
	})
	return result, applied, err
}
