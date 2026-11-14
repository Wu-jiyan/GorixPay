package profitsharing

import (
	"context"
	"errors"
	"strings"
	"time"

	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// basisPoints is the denominator of every rate in this package (10000 = 100%).
const basisPoints int64 = 10000

// receiverOwnerType is the ledger owner type used for internal receivers. The
// account is a platform liability, keyed by the receiver's external account id.
const receiverOwnerType = "receiver"

// channelPendingReason explains why a channel receiver has no money yet.
const channelPendingReason = "channel profit sharing is not implemented by any plugin yet"

// FeeResolver reports the platform fee rate of a merchant in basis points. The
// merchant group service (M1) satisfies it; a nil resolver means no fee.
type FeeResolver interface {
	RateBp(ctx context.Context, merchantID string) (int, bool)
}

// Service implements receiver management and the split execution.
type Service struct {
	db     *gorm.DB
	ledger *ledgerservice.Service
	now    func() time.Time
	fees   FeeResolver
}

func New(db *gorm.DB, ledger *ledgerservice.Service, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, ledger: ledger, now: now}
}

// WithFeeResolver returns a copy wired to a platform fee source.
func (service *Service) WithFeeResolver(resolver FeeResolver) *Service {
	copied := *service
	copied.fees = resolver
	return &copied
}

// ReceiverRequest creates a receiver. MerchantID is filled by the handler from
// the session for merchant calls and from the body for admin calls.
type ReceiverRequest struct {
	MerchantID   string
	ReceiverType ReceiverType
	Account      string
	Name         string
	RateBp       int
	Status       ReceiverStatus
}

// ReceiverUpdate patches a receiver; nil fields stay untouched.
type ReceiverUpdate struct {
	ReceiverType *ReceiverType
	Account      *string
	Name         *string
	RateBp       *int
	Status       *ReceiverStatus
}

// CreateReceiver validates and stores a receiver. The active rates of a
// merchant may not exceed 100%, otherwise the split would owe more than the
// order is worth.
func (service *Service) CreateReceiver(ctx context.Context, request ReceiverRequest) (Receiver, error) {
	request.MerchantID = strings.TrimSpace(request.MerchantID)
	request.Account = strings.TrimSpace(request.Account)
	request.Name = strings.TrimSpace(request.Name)
	if request.ReceiverType == "" {
		request.ReceiverType = ReceiverInternal
	}
	if request.Status == "" {
		request.Status = ReceiverActive
	}
	if err := validateReceiver(request.MerchantID, request.ReceiverType, request.Account, request.Name, request.RateBp, request.Status); err != nil {
		return Receiver{}, err
	}
	var existing Receiver
	err := service.db.WithContext(ctx).
		Where("merchant_id = ? AND account = ?", request.MerchantID, request.Account).
		First(&existing).Error
	if err == nil {
		return Receiver{}, ErrInvalidRequest
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return Receiver{}, err
	}
	if err := service.guardRate(ctx, request.MerchantID, "", request.RateBp, request.Status); err != nil {
		return Receiver{}, err
	}
	now := service.now().UTC()
	receiver := Receiver{
		ID: uuid.NewString(), MerchantID: request.MerchantID, ReceiverType: request.ReceiverType,
		Account: request.Account, Name: request.Name, RateBp: request.RateBp,
		Status: request.Status, CreatedAt: now, UpdatedAt: now,
	}
	if err := service.db.WithContext(ctx).Create(&receiver).Error; err != nil {
		return Receiver{}, err
	}
	return receiver, nil
}

// UpdateReceiver patches a receiver. An empty merchantID means the caller is an
// administrator and the row is addressed by id alone.
func (service *Service) UpdateReceiver(ctx context.Context, merchantID, id string, update ReceiverUpdate) (Receiver, error) {
	var receiver Receiver
	query := service.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id))
	if scoped := strings.TrimSpace(merchantID); scoped != "" {
		query = query.Where("merchant_id = ?", scoped)
	}
	if err := query.First(&receiver).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Receiver{}, ErrNotFound
		}
		return Receiver{}, err
	}
	receiverType := receiver.ReceiverType
	if update.ReceiverType != nil {
		receiverType = *update.ReceiverType
	}
	account := receiver.Account
	if update.Account != nil {
		account = strings.TrimSpace(*update.Account)
	}
	name := receiver.Name
	if update.Name != nil {
		name = strings.TrimSpace(*update.Name)
	}
	rate := receiver.RateBp
	if update.RateBp != nil {
		rate = *update.RateBp
	}
	status := receiver.Status
	if update.Status != nil {
		status = *update.Status
	}
	if err := validateReceiver(receiver.MerchantID, receiverType, account, name, rate, status); err != nil {
		return Receiver{}, err
	}
	if account != receiver.Account {
		var conflict int64
		if err := service.db.WithContext(ctx).Model(&Receiver{}).
			Where("merchant_id = ? AND account = ? AND id <> ?", receiver.MerchantID, account, receiver.ID).
			Count(&conflict).Error; err != nil {
			return Receiver{}, err
		}
		if conflict > 0 {
			return Receiver{}, ErrInvalidRequest
		}
	}
	if err := service.guardRate(ctx, receiver.MerchantID, receiver.ID, rate, status); err != nil {
		return Receiver{}, err
	}
	now := service.now().UTC()
	updates := map[string]any{
		"receiver_type": receiverType, "account": account, "name": name,
		"rate_bp": rate, "status": status, "updated_at": now,
	}
	if err := service.db.WithContext(ctx).Model(&Receiver{}).Where("id = ?", receiver.ID).Updates(updates).Error; err != nil {
		return Receiver{}, err
	}
	receiver.ReceiverType, receiver.Account, receiver.Name = receiverType, account, name
	receiver.RateBp, receiver.Status, receiver.UpdatedAt = rate, status, now
	return receiver, nil
}

// Receivers lists receivers for one merchant (empty = all merchants). The list
// is small enough to stay unpaginated, which is what the console expects.
func (service *Service) Receivers(ctx context.Context, filter ReceiverFilter) ([]Receiver, error) {
	if filter.Status != "" && !validReceiverStatus(filter.Status) {
		return nil, ErrInvalidRequest
	}
	query := service.db.WithContext(ctx).Model(&Receiver{})
	if merchantID := strings.TrimSpace(filter.MerchantID); merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var items []Receiver
	if err := query.Order("created_at ASC, id ASC").Limit(500).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// DeleteReceiver removes a receiver that has never produced a split. Receivers
// with history stay for auditing and must be disabled instead.
func (service *Service) DeleteReceiver(ctx context.Context, merchantID, id string) error {
	var receiver Receiver
	query := service.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id))
	if scoped := strings.TrimSpace(merchantID); scoped != "" {
		query = query.Where("merchant_id = ?", scoped)
	}
	if err := query.First(&receiver).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	var used int64
	if err := service.db.WithContext(ctx).Model(&Order{}).Where("receiver_id = ?", receiver.ID).Count(&used).Error; err != nil {
		return err
	}
	if used > 0 {
		return ErrReceiverInUse
	}
	return service.db.WithContext(ctx).Delete(&Receiver{}, "id = ?", receiver.ID).Error
}

// Orders lists split rows. An empty merchantID means the caller is an
// administrator reading across merchants.
func (service *Service) Orders(ctx context.Context, filter OrderFilter, page Page) ([]Order, int64, error) {
	if filter.Status != "" && !validOrderStatus(filter.Status) {
		return nil, 0, ErrInvalidRequest
	}
	query := service.db.WithContext(ctx).Model(&Order{})
	if merchantID := strings.TrimSpace(filter.MerchantID); merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	if orderID := strings.TrimSpace(filter.OrderID); orderID != "" {
		query = query.Where("order_id = ?", orderID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []Order
	if err := query.Order("created_at DESC, id ASC").Offset(page.Offset()).Limit(page.Limit()).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Execute re-runs the split for one paid order outside the payment transaction.
// It backs the admin retry endpoint and any repair job.
func (service *Service) Execute(ctx context.Context, orderID string) ([]Order, error) {
	var payment paymentmodel.Order
	if err := service.db.WithContext(ctx).First(&payment, "id = ?", strings.TrimSpace(orderID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if payment.Status != paymentmodel.StatusPaid {
		return nil, ErrOrderNotPaid
	}
	var results []Order
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		results, txErr = service.executeInTransaction(ctx, tx, payment)
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// Hook runs the split inside the payment completion transaction so the ledger
// posts atomically with the payment. Invite-style structural match: it satisfies
// paymentcallback.PostPaymentHook without importing that package.
func (service *Service) Hook() *Hook { return &Hook{service: service} }

// Hook adapts the service to the payment post-payment hook contract.
type Hook struct {
	service *Service
}

// OnPayment splits a paid sale order. Funding orders never take part.
func (hook *Hook) OnPayment(ctx context.Context, tx *gorm.DB, order paymentmodel.Order) error {
	if hook == nil || hook.service == nil {
		return nil
	}
	// The hook runs after the status update inside the payment transaction, so
	// the in-memory order may still carry the pre-payment status. Kind and amount
	// are enough to decide.
	if !order.IsSale() || order.Amount <= 0 {
		return nil
	}
	if tx == nil {
		_, err := hook.service.Execute(ctx, order.ID)
		return err
	}
	_, err := hook.service.executeInTransaction(ctx, tx, order)
	return err
}

// Retry re-attempts one split row: pending channel splits keep waiting, failed
// ledger splits are posted again (the ledger idempotency key makes it safe).
func (service *Service) Retry(ctx context.Context, id string) (Order, error) {
	var row Order
	if err := service.db.WithContext(ctx).First(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	if row.Status == OrderDone {
		return row, nil
	}
	var receiver Receiver
	if err := service.db.WithContext(ctx).First(&receiver, "id = ?", row.ReceiverID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	var payment paymentmodel.Order
	if err := service.db.WithContext(ctx).First(&payment, "id = ?", row.OrderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	if payment.Status != paymentmodel.StatusPaid {
		return Order{}, ErrOrderNotPaid
	}
	now := service.now().UTC()
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if receiver.ReceiverType != ReceiverInternal {
			row.Status = OrderPending
			row.Error = stringPointer(channelPendingReason)
			row.UpdatedAt = now
			return tx.Model(&Order{}).Where("id = ?", row.ID).
				Updates(map[string]any{"status": row.Status, "error": channelPendingReason, "updated_at": now}).Error
		}
		transactionID, err := service.postSplit(ctx, tx, payment, receiver, row.Amount)
		if err != nil {
			row.Status = OrderFailed
			row.Error = stringPointer(truncate(err.Error(), 255))
			row.UpdatedAt = now
			return tx.Model(&Order{}).Where("id = ?", row.ID).
				Updates(map[string]any{"status": row.Status, "error": *row.Error, "updated_at": now}).Error
		}
		row.Status = OrderDone
		row.Error = nil
		row.LedgerTransactionID = &transactionID
		row.UpdatedAt = now
		return tx.Model(&Order{}).Where("id = ?", row.ID).Updates(map[string]any{
			"status": row.Status, "ledger_transaction_id": transactionID, "error": nil, "updated_at": now,
		}).Error
	})
	if err != nil {
		return Order{}, err
	}
	return row, nil
}

// executeInTransaction is the shared split body. Per-receiver problems are
// recorded on the row instead of failing the payment; only database failures
// bubble up.
func (service *Service) executeInTransaction(ctx context.Context, tx *gorm.DB, payment paymentmodel.Order) ([]Order, error) {
	if tx == nil || !payment.IsSale() || payment.Amount <= 0 {
		return nil, nil
	}
	var receivers []Receiver
	if err := tx.WithContext(ctx).
		Where("merchant_id = ? AND status = ?", payment.MerchantID, ReceiverActive).
		Order("created_at ASC, id ASC").Find(&receivers).Error; err != nil {
		return nil, err
	}
	if len(receivers) == 0 {
		return nil, nil
	}
	feeRate := 0
	if service.fees != nil {
		if rate, ok := service.fees.RateBp(ctx, payment.MerchantID); ok {
			feeRate = rate
		}
	}
	base := payment.Amount - share(payment.Amount, feeRate)
	now := service.now().UTC()
	results := make([]Order, 0, len(receivers))
	for _, receiver := range receivers {
		amount := share(base, receiver.RateBp)
		if amount <= 0 {
			continue
		}
		var existing Order
		err := tx.WithContext(ctx).Where("order_id = ? AND receiver_id = ?", payment.ID, receiver.ID).First(&existing).Error
		if err == nil {
			results = append(results, existing)
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		row := Order{
			ID: uuid.NewString(), OrderID: payment.ID, MerchantID: payment.MerchantID,
			ReceiverID: receiver.ID, Amount: amount, Status: OrderPending, CreatedAt: now, UpdatedAt: now,
		}
		if receiver.ReceiverType == ReceiverInternal {
			transactionID, postErr := service.postSplit(ctx, tx, payment, receiver, amount)
			if postErr != nil {
				row.Status = OrderFailed
				row.Error = stringPointer(truncate(postErr.Error(), 255))
			} else {
				row.Status = OrderDone
				row.LedgerTransactionID = &transactionID
			}
		} else {
			row.Error = stringPointer(channelPendingReason)
		}
		if createErr := tx.Create(&row).Error; createErr != nil {
			// A concurrent payment completion may have written the same row.
			var raced Order
			if lookupErr := tx.WithContext(ctx).Where("order_id = ? AND receiver_id = ?", payment.ID, receiver.ID).First(&raced).Error; lookupErr == nil {
				results = append(results, raced)
				continue
			}
			return nil, createErr
		}
		results = append(results, row)
	}
	return results, nil
}

// postSplit moves the receiver share from the merchant balance to the receiver
// balance. The idempotency key is fixed per (order, receiver), so replays and
// retries return the original transaction.
func (service *Service) postSplit(ctx context.Context, tx *gorm.DB, payment paymentmodel.Order, receiver Receiver, amount int64) (string, error) {
	if amount <= 0 {
		return "", ErrInvalidRequest
	}
	merchantAccount, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: payment.MerchantID, Purpose: "balance",
		Category: ledgermodel.CategoryLiability, Currency: payment.Currency,
	})
	if err != nil {
		return "", err
	}
	receiverAccount, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
		OwnerType: receiverOwnerType, OwnerID: receiver.Account, Purpose: "balance",
		Category: ledgermodel.CategoryLiability, Currency: payment.Currency,
	})
	if err != nil {
		return "", err
	}
	result, err := service.ledger.PostInTransaction(tx, ledgerservice.TransactionRequest{
		IdempotencyKey: "split:" + payment.ID + ":" + receiver.ID,
		ReferenceType:  "profit-sharing",
		ReferenceID:    payment.ID,
		Description:    "profit sharing " + receiver.Name,
		Postings: []ledgerservice.Posting{
			{AccountID: merchantAccount.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: amount},
			{AccountID: receiverAccount.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: amount},
		},
	})
	if err != nil {
		return "", err
	}
	return result.Transaction.ID, nil
}

// share applies a basis-point rate to an amount, rounding down to the fen.
func share(amount int64, rateBp int) int64 {
	if amount <= 0 || rateBp <= 0 {
		return 0
	}
	if rateBp > 10000 {
		rateBp = 10000
	}
	return amount * int64(rateBp) / basisPoints
}

// guardRate keeps the active rates of a merchant within 100%.
func (service *Service) guardRate(ctx context.Context, merchantID, excludeID string, rateBp int, status ReceiverStatus) error {
	if status != ReceiverActive {
		return nil
	}
	var total int64
	query := service.db.WithContext(ctx).Model(&Receiver{}).
		Where("merchant_id = ? AND status = ?", merchantID, ReceiverActive)
	if excludeID != "" {
		query = query.Where("id <> ?", excludeID)
	}
	if err := query.Select("COALESCE(SUM(rate_bp), 0)").Scan(&total).Error; err != nil {
		return err
	}
	if total+int64(rateBp) > int64(basisPoints) {
		return ErrRateExceeded
	}
	return nil
}

func validateReceiver(merchantID string, receiverType ReceiverType, account, name string, rateBp int, status ReceiverStatus) error {
	if strings.TrimSpace(merchantID) == "" || !validReceiverType(receiverType) ||
		account == "" || len(account) > 191 || name == "" || len(name) > 128 ||
		rateBp < 0 || rateBp > int(basisPoints) || !validReceiverStatus(status) {
		return ErrInvalidRequest
	}
	return nil
}

// Offset and Limit translate the page into GORM values.
func (page Page) Offset() int {
	number := page.Number
	if number < 1 {
		number = 1
	}
	return (number - 1) * page.Limit()
}

// Limit clamps the page size to 100.
func (page Page) Limit() int {
	size := page.Size
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return size
}

func stringPointer(value string) *string { return &value }

func truncate(value string, size int) string {
	if len(value) <= size {
		return value
	}
	return value[:size]
}
