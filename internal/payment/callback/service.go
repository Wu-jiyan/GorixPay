package callback

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
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
	db     *gorm.DB
	now    func() time.Time
	ledger *ledgerservice.Service
	hooks  []PostPaymentHook
}

// PostPaymentHook runs inside the payment-completion transaction after the
// order is marked paid. It is used for invite rebates and deposit accounting.
type PostPaymentHook interface {
	OnPayment(ctx context.Context, tx *gorm.DB, order paymentmodel.Order) error
}

func New(db *gorm.DB, now func() time.Time) *Service {
	return NewWithLedger(db, now, nil)
}

// NewWithLedger wires the double-entry ledger so a successful payment credits
// the merchant balance in the same transaction that flips the order to paid.
func NewWithLedger(db *gorm.DB, now func() time.Time, ledger *ledgerservice.Service) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now, ledger: ledger}
}

// WithHooks returns a copy of the service with post-payment hooks attached.
func (service *Service) WithHooks(hooks ...PostPaymentHook) *Service {
	copied := *service
	copied.hooks = append([]PostPaymentHook{}, hooks...)
	return &copied
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
		if result.IsSale() {
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
		}
		if result.IsSale() || result.Kind == paymentmodel.KindRecharge {
			if err := service.postIncome(tx, result); err != nil {
				return err
			}
		}
		for _, hook := range service.hooks {
			if hook == nil {
				continue
			}
			if err := hook.OnPayment(ctx, tx, result); err != nil {
				return err
			}
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

// postIncome credits the merchant's available balance and debits the platform
// clearing account in the same transaction. The idempotency key is derived from
// the order id, so a replayed callback can never double-credit.
func (service *Service) postIncome(tx *gorm.DB, order paymentmodel.Order) error {
	if service.ledger == nil || order.Amount <= 0 {
		return nil
	}
	merchantAccount, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: order.MerchantID, Purpose: "balance",
		Category: ledgermodel.CategoryLiability, Currency: order.Currency,
	})
	if err != nil {
		return err
	}
	platformAccount, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
		OwnerType: "platform", OwnerID: "platform", Purpose: "cash",
		Category: ledgermodel.CategoryAsset, Currency: order.Currency,
	})
	if err != nil {
		return err
	}
	_, err = service.ledger.PostInTransaction(tx, ledgerservice.TransactionRequest{
		IdempotencyKey: "payment:" + order.ID,
		ReferenceType:  "payment",
		ReferenceID:    order.ID,
		Description:    "merchant payment income",
		Postings: []ledgerservice.Posting{
			{AccountID: merchantAccount.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: order.Amount},
			{AccountID: platformAccount.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: order.Amount},
		},
	})
	return err
}
