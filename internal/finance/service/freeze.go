package service

import (
	"context"
	"errors"

	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	paymentmodel "gorixpay/internal/payment/model"

	"gorm.io/gorm"
)

// ErrOrderNotFreezable means the order is not in a state that can hold funds.
var ErrOrderNotFreezable = errors.New("order cannot be frozen")

// freezeEntry is what one order credited the merchant, read back from the ledger
// so a freeze never guesses an amount: whatever was actually credited to the
// available bucket is what moves to the frozen bucket.
type freezeEntry struct {
	AccountID string
	Amount    int64
}

// SetOrderFreeze moves the money an order credited from the merchant's available
// bucket to the frozen bucket (freeze) or back (unfreeze).
//
// Upstream Epay does the same thing from its order list (lib\Order::freeze): a
// disputed order stops counting towards the merchant's spendable balance until an
// operator unfreezes it. The order itself stays paid — freezing is an accounting
// action, not a state transition, so channel callbacks and refunds keep working.
func (service *Service) SetOrderFreeze(ctx context.Context, orderID string, freeze bool) (paymentmodel.Order, error) {
	var result paymentmodel.Order
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order paymentmodel.Order
		if err := tx.First(&order, "id = ?", orderID).Error; err != nil {
			return err
		}
		if order.Status != paymentmodel.StatusPaid {
			return ErrOrderNotFreezable
		}
		alreadyFrozen := order.FrozenAt != nil
		if alreadyFrozen == freeze {
			result = order
			return nil
		}
		if service.ledger == nil {
			return ErrOrderNotFreezable
		}
		merchant, _, err := service.accounts(tx, order.MerchantID, order.Currency)
		if err != nil {
			return err
		}
		credited, err := service.orderCredit(tx, order.ID, merchant.ID)
		if err != nil {
			return err
		}
		if credited > 0 {
			postings := []ledgerservice.Posting{
				{AccountID: merchant.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: credited},
				{AccountID: merchant.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketFrozen, Amount: credited},
			}
			key := "order-freeze:" + order.ID
			description := "freeze order " + order.TradeNo
			if !freeze {
				postings = []ledgerservice.Posting{
					{AccountID: merchant.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketFrozen, Amount: credited},
					{AccountID: merchant.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: credited},
				}
				key = "order-unfreeze:" + order.ID
				description = "unfreeze order " + order.TradeNo
			}
			if _, err := service.ledger.PostInTransaction(tx, ledgerservice.TransactionRequest{
				IdempotencyKey: key,
				ReferenceType:  "order_freeze",
				ReferenceID:    order.ID,
				Description:    description,
				Postings:       postings,
			}); err != nil {
				return err
			}
		}
		now := service.now().UTC()
		updates := map[string]any{"updated_at": now, "frozen_at": now}
		if !freeze {
			updates["frozen_at"] = nil
		}
		if err := tx.Model(&paymentmodel.Order{}).Where("id = ?", order.ID).Updates(updates).Error; err != nil {
			return err
		}
		order.UpdatedAt = now
		if freeze {
			order.FrozenAt = &now
		} else {
			order.FrozenAt = nil
		}
		result = order
		return nil
	})
	return result, err
}

// orderCredit sums what the order credited to the merchant's available bucket.
func (service *Service) orderCredit(tx *gorm.DB, orderID, accountID string) (int64, error) {
	var credited int64
	err := tx.Model(&ledgermodel.Entry{}).
		Joins("JOIN ledger_transactions ON ledger_transactions.id = ledger_entries.transaction_id").
		Where("ledger_transactions.reference_type = ? AND ledger_transactions.reference_id = ?", "payment", orderID).
		Where("ledger_entries.account_id = ? AND ledger_entries.direction = ? AND ledger_entries.bucket = ?",
			accountID, ledgermodel.DirectionCredit, ledgermodel.BucketAvailable).
		Select("COALESCE(SUM(ledger_entries.amount), 0)").
		Scan(&credited).Error
	return credited, err
}
