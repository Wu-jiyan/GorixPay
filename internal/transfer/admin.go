package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	identitymodel "gorixpay/internal/identity/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AdminRequest is a payout created by an administrator on behalf of a merchant.
// IdempotencyKey, when present, derives a stable out_transfer_no.
type AdminRequest struct {
	MerchantID     string
	Amount         int64
	Destination    string
	Remark         string
	IdempotencyKey string
}

// AdminBatchItem is one payout inside an administrator batch.
type AdminBatchItem struct {
	Amount      int64
	Destination string
}

// AdminBatchRequest creates several manual payouts at once. The whole batch is
// compensated when one item fails: already created rows are released and
// deleted, so a failed batch leaves no frozen funds behind.
type AdminBatchRequest struct {
	MerchantID     string
	Items          []AdminBatchItem
	Remark         string
	IdempotencyKey string
}

// AdminBatchResult reports the batch number and how many rows were created.
type AdminBatchResult struct {
	BatchNo string
	Created int
}

const (
	manualAccountName = "manual payout"
	manualBankName    = "manual"
	maxBatchItems     = 100
)

// CreateAdmin registers one manual payout (kind=manual) and freezes the amount.
func (service *Service) CreateAdmin(ctx context.Context, request AdminRequest) (Order, bool, error) {
	request.MerchantID = strings.TrimSpace(request.MerchantID)
	request.Destination = strings.TrimSpace(request.Destination)
	request.Remark = strings.TrimSpace(request.Remark)
	if request.MerchantID == "" || request.Destination == "" || len(request.Destination) > 128 || len(request.Remark) > 191 {
		return Order{}, false, ErrInvalidRequest
	}
	if _, err := service.merchantExists(ctx, request.MerchantID); err != nil {
		return Order{}, false, err
	}
	outTransferNo := adminOutTransferNo("admin-transfer:"+request.IdempotencyKey, request.IdempotencyKey)
	return service.create(ctx, Request{
		MerchantID:    request.MerchantID,
		OutTransferNo: outTransferNo,
		Amount:        request.Amount,
		Currency:      "CNY",
		AccountName:   manualAccountName,
		AccountNo:     request.Destination,
		BankName:      manualBankName,
	}, createOptions{kind: KindManual, remark: request.Remark})
}

// CreateAdminBatch creates several manual payouts. Items are created one by one
// because ledger freezing owns its own transaction; a failure compensates the
// rows created so far.
func (service *Service) CreateAdminBatch(ctx context.Context, request AdminBatchRequest) (AdminBatchResult, error) {
	request.MerchantID = strings.TrimSpace(request.MerchantID)
	request.Remark = strings.TrimSpace(request.Remark)
	if request.MerchantID == "" || len(request.Items) == 0 || len(request.Items) > maxBatchItems || len(request.Remark) > 191 {
		return AdminBatchResult{}, ErrInvalidRequest
	}
	if _, err := service.merchantExists(ctx, request.MerchantID); err != nil {
		return AdminBatchResult{}, err
	}
	now := service.now().UTC()
	batchNo := businessNo("BT", now)
	created := make([]Order, 0, len(request.Items))
	for index, item := range request.Items {
		destination := strings.TrimSpace(item.Destination)
		if destination == "" || len(destination) > 128 || item.Amount <= 0 || item.Amount > MaxAmountCents {
			service.compensateBatch(ctx, created)
			return AdminBatchResult{}, ErrInvalidRequest
		}
		key := fmt.Sprintf("%s:%d", request.IdempotencyKey, index)
		if request.IdempotencyKey == "" {
			key = fmt.Sprintf("%s:%d", batchNo, index)
		}
		order, isNew, err := service.create(ctx, Request{
			MerchantID:    request.MerchantID,
			OutTransferNo: adminOutTransferNo(key, key),
			Amount:        item.Amount,
			Currency:      "CNY",
			AccountName:   manualAccountName,
			AccountNo:     destination,
			BankName:      manualBankName,
		}, createOptions{kind: KindManual, remark: request.Remark, batchNo: batchNo})
		if err != nil {
			service.compensateBatch(ctx, created)
			return AdminBatchResult{}, err
		}
		if isNew {
			created = append(created, order)
		}
	}
	return AdminBatchResult{BatchNo: batchNo, Created: len(created)}, nil
}

func (service *Service) compensateBatch(ctx context.Context, orders []Order) {
	for _, order := range orders {
		_, _ = service.ledger.Release(ctx, "transfer:"+order.ID)
		_ = service.db.WithContext(ctx).Delete(&Order{}, "id = ?", order.ID).Error
	}
}

func (service *Service) merchantExists(ctx context.Context, merchantID string) (identitymodel.Merchant, error) {
	var merchant identitymodel.Merchant
	if err := service.db.WithContext(ctx).First(&merchant, "id = ?", merchantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return identitymodel.Merchant{}, ErrInvalidRequest
		}
		return identitymodel.Merchant{}, err
	}
	return merchant, nil
}

// adminOutTransferNo derives a deterministic key-friendly out_transfer_no that
// fits the VARCHAR(64) column and the [a-zA-Z0-9._-|] pattern. The caller's
// idempotency key itself is hashed, so a replay produces the same number.
func adminOutTransferNo(key, rawKey string) string {
	if strings.TrimSpace(rawKey) == "" {
		return "ADM" + randomSuffix()
	}
	digest := sha256.Sum256([]byte(key))
	return "ADM" + hex.EncodeToString(digest[:])[:24]
}

func randomSuffix() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
}

func optionalText(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}
