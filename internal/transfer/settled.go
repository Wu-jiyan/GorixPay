package transfer

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// SettledRequest describes a payout that has already been booked by whoever
// created it. A settlement approval debits the merchant balance and credits the
// platform cash account, so the payout row that follows must not touch the
// ledger again.
type SettledRequest struct {
	MerchantID          string
	OutTransferNo       string
	Amount              int64
	Currency            string
	AccountName         string
	AccountNo           string
	LedgerTransactionID string
	Remark              string
}

// CreateSettled records a payout for money that has already left the merchant
// account.
//
// It deliberately skips both the freeze and the settlement posting that create()
// performs. Approving a settlement already moved available funds out of the
// merchant ledger; freezing and posting again made one settlement leave the
// account twice, which is how a 4000 settlement paid out 8000.
func (service *Service) CreateSettled(ctx context.Context, request SettledRequest) (Order, bool, error) {
	request.MerchantID = strings.TrimSpace(request.MerchantID)
	request.OutTransferNo = strings.TrimSpace(request.OutTransferNo)
	request.Currency = strings.ToUpper(strings.TrimSpace(request.Currency))
	request.AccountName = strings.TrimSpace(request.AccountName)
	request.AccountNo = strings.TrimSpace(request.AccountNo)
	if request.Currency == "" {
		request.Currency = "CNY"
	}
	if request.MerchantID == "" || request.OutTransferNo == "" || len(request.OutTransferNo) > 64 ||
		!businessNoPattern.MatchString(request.OutTransferNo) || request.Amount <= 0 || request.Amount > MaxAmountCents ||
		len(request.Currency) != 3 || request.AccountNo == "" || len(request.AccountNo) > 128 ||
		len(request.AccountName) > 128 {
		return Order{}, false, ErrInvalidRequest
	}
	existing, err := service.findByOutNo(ctx, request.MerchantID, request.OutTransferNo)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Order{}, false, err
	}
	now := service.now().UTC()
	processedBy := "auto-transfer"
	var ledgerTransactionID *string
	if strings.TrimSpace(request.LedgerTransactionID) != "" {
		value := request.LedgerTransactionID
		ledgerTransactionID = &value
	}
	order := Order{
		ID:                  uuid.NewString(),
		TransferNo:          businessNo("TR", now),
		MerchantID:          request.MerchantID,
		OutTransferNo:       request.OutTransferNo,
		Amount:              request.Amount,
		Currency:            request.Currency,
		AccountName:         request.AccountName,
		AccountNo:           request.AccountNo,
		Status:              StatusApproved,
		Kind:                KindNormal,
		Remark:              optionalText(request.Remark),
		LedgerTransactionID: ledgerTransactionID,
		ProcessedBy:         &processedBy,
		ProcessedAt:         &now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := service.db.WithContext(ctx).Create(&order).Error; err != nil {
		if conflict, lookupErr := service.findByOutNo(ctx, request.MerchantID, request.OutTransferNo); lookupErr == nil {
			return conflict, false, nil
		}
		return Order{}, false, err
	}
	return order, true, nil
}
