package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	financemodel "gorixpay/internal/finance/model"
	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidRequest      = errors.New("invalid finance request")
	ErrNotFound            = errors.New("finance order not found")
	ErrPaymentNotPaid      = errors.New("payment order is not paid")
	ErrRefundAmountExceeded = errors.New("refund amount exceeds payment amount")
	ErrStateConflict       = errors.New("finance order state conflict")
	ErrIdempotencyConflict = errors.New("finance idempotency conflict")
)

type CreateRefundRequest struct {
	MerchantID      string
	PaymentOrderID  string
	OutRefundNo     string
	Amount          int64
	Reason          string
}

type CreateSettlementRequest struct {
	MerchantID       string
	OutSettlementNo  string
	Amount           int64
	Currency         string
	Destination      string
}

type ProcessRequest struct {
	ID              string
	Action          string
	ProcessedBy     string
	RejectionReason string
}

type Filter struct {
	MerchantID string
	Status     financemodel.Status
	Keyword    string
}

type Page struct {
	Number int
	Size   int
}

type Service struct {
	db     *gorm.DB
	ledger *ledgerservice.Service
	now    func() time.Time
}

func New(db *gorm.DB, ledger *ledgerservice.Service, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, ledger: ledger, now: now}
}

func (service *Service) CreateRefund(ctx context.Context, request CreateRefundRequest) (financemodel.Refund, bool, error) {
	request.MerchantID = strings.TrimSpace(request.MerchantID)
	request.PaymentOrderID = strings.TrimSpace(request.PaymentOrderID)
	request.OutRefundNo = strings.TrimSpace(request.OutRefundNo)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.MerchantID == "" || request.PaymentOrderID == "" || request.OutRefundNo == "" || len(request.OutRefundNo) > 64 || request.Amount <= 0 || request.Reason == "" || len(request.Reason) > 255 {
		return financemodel.Refund{}, false, ErrInvalidRequest
	}
	var refund financemodel.Refund
	created := false
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("merchant_id = ? AND out_refund_no = ?", request.MerchantID, request.OutRefundNo).First(&refund).Error; err == nil {
			if refund.PaymentOrderID != request.PaymentOrderID || refund.Amount != request.Amount || refund.Reason != request.Reason {
				return ErrIdempotencyConflict
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var order paymentmodel.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND merchant_id = ?", request.PaymentOrderID, request.MerchantID).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if order.Status != paymentmodel.StatusPaid {
			return ErrPaymentNotPaid
		}
		var reserved int64
		if err := tx.Model(&financemodel.Refund{}).Where("payment_order_id = ? AND status <> ?", order.ID, financemodel.StatusRejected).Select("COALESCE(SUM(amount), 0)").Scan(&reserved).Error; err != nil {
			return err
		}
		if reserved+request.Amount > order.Amount {
			return ErrRefundAmountExceeded
		}
		now := service.now().UTC()
		refund = financemodel.Refund{ID: uuid.NewString(), RefundNo: businessNo("RF", now), MerchantID: request.MerchantID, PaymentOrderID: order.ID, OutRefundNo: request.OutRefundNo, Amount: request.Amount, Currency: order.Currency, Reason: request.Reason, Status: financemodel.StatusPending, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&refund).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return refund, created, err
}

func (service *Service) CreateSettlement(ctx context.Context, request CreateSettlementRequest) (financemodel.Settlement, bool, error) {
	request.MerchantID = strings.TrimSpace(request.MerchantID)
	request.OutSettlementNo = strings.TrimSpace(request.OutSettlementNo)
	request.Currency = strings.ToUpper(strings.TrimSpace(request.Currency))
	request.Destination = strings.TrimSpace(request.Destination)
	if request.MerchantID == "" || request.OutSettlementNo == "" || len(request.OutSettlementNo) > 64 || request.Amount <= 0 || len(request.Currency) != 3 || request.Destination == "" || len(request.Destination) > 255 {
		return financemodel.Settlement{}, false, ErrInvalidRequest
	}
	var settlement financemodel.Settlement
	created := false
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("merchant_id = ? AND out_settlement_no = ?", request.MerchantID, request.OutSettlementNo).First(&settlement).Error; err == nil {
			if settlement.Amount != request.Amount || settlement.Currency != request.Currency || settlement.Destination != request.Destination {
				return ErrIdempotencyConflict
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := service.now().UTC()
		settlement = financemodel.Settlement{ID: uuid.NewString(), SettlementNo: businessNo("ST", now), MerchantID: request.MerchantID, OutSettlementNo: request.OutSettlementNo, Amount: request.Amount, Currency: request.Currency, Destination: request.Destination, Status: financemodel.StatusPending, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&settlement).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return settlement, created, err
}

func (service *Service) ProcessRefund(ctx context.Context, request ProcessRequest) (financemodel.Refund, error) {
	if err := validateProcess(&request); err != nil {
		return financemodel.Refund{}, err
	}
	var refund financemodel.Refund
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&refund, "id = ?", request.ID).Error; err != nil {
			return notFoundError(err)
		}
		if refund.Status != financemodel.StatusPending {
			if (request.Action == "approve" && refund.Status == financemodel.StatusApproved) || (request.Action == "reject" && refund.Status == financemodel.StatusRejected) {
				return nil
			}
			return ErrStateConflict
		}
		return service.finishRefund(tx, &refund, request)
	})
	return refund, err
}

func (service *Service) ProcessSettlement(ctx context.Context, request ProcessRequest) (financemodel.Settlement, error) {
	if err := validateProcess(&request); err != nil {
		return financemodel.Settlement{}, err
	}
	var settlement financemodel.Settlement
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settlement, "id = ?", request.ID).Error; err != nil {
			return notFoundError(err)
		}
		if settlement.Status != financemodel.StatusPending {
			if (request.Action == "approve" && settlement.Status == financemodel.StatusApproved) || (request.Action == "reject" && settlement.Status == financemodel.StatusRejected) {
				return nil
			}
			return ErrStateConflict
		}
		return service.finishSettlement(tx, &settlement, request)
	})
	return settlement, err
}

func (service *Service) finishRefund(tx *gorm.DB, refund *financemodel.Refund, request ProcessRequest) error {
	if request.Action == "reject" {
		return service.updateRefund(tx, refund, financemodel.StatusRejected, request, nil)
	}
	merchant, platform, err := service.accounts(tx, refund.MerchantID, refund.Currency)
	if err != nil {
		return err
	}
	result, err := service.ledger.PostInTransaction(tx, ledgerservice.TransactionRequest{IdempotencyKey: "refund:" + refund.ID, ReferenceType: "refund", ReferenceID: refund.ID, Description: "merchant refund", Postings: []ledgerservice.Posting{{AccountID: merchant.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: refund.Amount}, {AccountID: platform.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: refund.Amount}}})
	if err != nil {
		return err
	}
	return service.updateRefund(tx, refund, financemodel.StatusApproved, request, &result.Transaction.ID)
}

func (service *Service) finishSettlement(tx *gorm.DB, settlement *financemodel.Settlement, request ProcessRequest) error {
	if request.Action == "reject" {
		return service.updateSettlement(tx, settlement, financemodel.StatusRejected, request, nil)
	}
	merchant, platform, err := service.accounts(tx, settlement.MerchantID, settlement.Currency)
	if err != nil {
		return err
	}
	result, err := service.ledger.PostInTransaction(tx, ledgerservice.TransactionRequest{IdempotencyKey: "settlement:" + settlement.ID, ReferenceType: "settlement", ReferenceID: settlement.ID, Description: "merchant settlement", Postings: []ledgerservice.Posting{{AccountID: merchant.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: settlement.Amount}, {AccountID: platform.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: settlement.Amount}}})
	if err != nil {
		return err
	}
	return service.updateSettlement(tx, settlement, financemodel.StatusApproved, request, &result.Transaction.ID)
}

func (service *Service) accounts(tx *gorm.DB, merchantID, currency string) (ledgermodel.Account, ledgermodel.Account, error) {
	merchant, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{OwnerType: "merchant", OwnerID: merchantID, Purpose: "balance", Category: ledgermodel.CategoryLiability, Currency: currency})
	if err != nil {
		return ledgermodel.Account{}, ledgermodel.Account{}, err
	}
	platform, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{OwnerType: "platform", OwnerID: "platform", Purpose: "cash", Category: ledgermodel.CategoryAsset, Currency: currency})
	return merchant, platform, err
}

func (service *Service) updateRefund(tx *gorm.DB, refund *financemodel.Refund, status financemodel.Status, request ProcessRequest, transactionID *string) error {
	now := service.now().UTC()
	updates := map[string]any{"status": status, "processed_by": request.ProcessedBy, "processed_at": now, "updated_at": now}
	if transactionID != nil {
		updates["ledger_transaction_id"] = *transactionID
		refund.LedgerTransactionID = transactionID
	} else {
		updates["rejection_reason"] = request.RejectionReason
		refund.RejectionReason = &request.RejectionReason
	}
	result := tx.Model(&financemodel.Refund{}).Where("id = ? AND status = ?", refund.ID, financemodel.StatusPending).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrStateConflict
	}
	refund.Status, refund.ProcessedBy, refund.ProcessedAt, refund.UpdatedAt = status, &request.ProcessedBy, &now, now
	return nil
}

func (service *Service) updateSettlement(tx *gorm.DB, settlement *financemodel.Settlement, status financemodel.Status, request ProcessRequest, transactionID *string) error {
	now := service.now().UTC()
	updates := map[string]any{"status": status, "processed_by": request.ProcessedBy, "processed_at": now, "updated_at": now}
	if transactionID != nil {
		updates["ledger_transaction_id"] = *transactionID
		settlement.LedgerTransactionID = transactionID
	} else {
		updates["rejection_reason"] = request.RejectionReason
		settlement.RejectionReason = &request.RejectionReason
	}
	result := tx.Model(&financemodel.Settlement{}).Where("id = ? AND status = ?", settlement.ID, financemodel.StatusPending).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrStateConflict
	}
	settlement.Status, settlement.ProcessedBy, settlement.ProcessedAt, settlement.UpdatedAt = status, &request.ProcessedBy, &now, now
	return nil
}

func (service *Service) Refund(ctx context.Context, merchantID, id string) (financemodel.Refund, error) {
	var item financemodel.Refund
	query := service.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id))
	if merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	if err := query.First(&item).Error; err != nil {
		return item, notFoundError(err)
	}
	return item, nil
}

func (service *Service) Settlement(ctx context.Context, merchantID, id string) (financemodel.Settlement, error) {
	var item financemodel.Settlement
	query := service.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id))
	if merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	if err := query.First(&item).Error; err != nil {
		return item, notFoundError(err)
	}
	return item, nil
}

func (service *Service) Refunds(ctx context.Context, filter Filter, page Page) ([]financemodel.Refund, int64, error) {
	var items []financemodel.Refund
	query := service.refundQuery(service.db.WithContext(ctx).Model(&financemodel.Refund{}), filter)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&items).Error
	return items, total, err
}

func (service *Service) Settlements(ctx context.Context, filter Filter, page Page) ([]financemodel.Settlement, int64, error) {
	var items []financemodel.Settlement
	query := service.settlementQuery(service.db.WithContext(ctx).Model(&financemodel.Settlement{}), filter)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&items).Error
	return items, total, err
}

func (service *Service) refundQuery(query *gorm.DB, filter Filter) *gorm.DB {
	if filter.MerchantID != "" {
		query = query.Where("merchant_id = ?", filter.MerchantID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("refund_no LIKE ? OR out_refund_no LIKE ?", like, like)
	}
	return query
}

func (service *Service) settlementQuery(query *gorm.DB, filter Filter) *gorm.DB {
	if filter.MerchantID != "" {
		query = query.Where("merchant_id = ?", filter.MerchantID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("settlement_no LIKE ? OR out_settlement_no LIKE ?", like, like)
	}
	return query
}

func validateProcess(request *ProcessRequest) error {
	request.ID = strings.TrimSpace(request.ID)
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	request.ProcessedBy = strings.TrimSpace(request.ProcessedBy)
	request.RejectionReason = strings.TrimSpace(request.RejectionReason)
	if request.ID == "" || request.ProcessedBy == "" || (request.Action != "approve" && request.Action != "reject") || (request.Action == "reject" && request.RejectionReason == "") || len(request.RejectionReason) > 255 {
		return ErrInvalidRequest
	}
	return nil
}

func notFoundError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

func businessNo(prefix string, now time.Time) string {
	return fmt.Sprintf("%s%s%s", prefix, now.Format("20060102150405"), strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
}
