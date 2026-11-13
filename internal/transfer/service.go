package transfer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MaxAmountCents caps a single payout at 100000.00 CNY, matching the V2 amount rule.
const MaxAmountCents int64 = 100000 * 100

var businessNoPattern = regexp.MustCompile(`^[a-zA-Z0-9._\-|]+$`)

// Request is a merchant payout application.
type Request struct {
	MerchantID    string
	OutTransferNo string
	Amount        int64
	Currency      string
	AccountName   string
	AccountNo     string
	BankName      string
	BranchName    string
}

// Filter selects payouts for listings.
type Filter struct {
	MerchantID string
	Status     Status
}

// Page is a simple offset pagination request.
type Page struct {
	Number int
	Size   int
}

// Balance reports the merchant ledger buckets used by payouts.
type Balance struct {
	Available int64
	Frozen    int64
}

// Service implements payout applications, approvals and balance lookups.
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

// Create registers a pending payout and freezes the merchant balance. It is
// idempotent on (merchant_id, out_transfer_no).
func (service *Service) Create(ctx context.Context, request Request) (Order, bool, error) {
	request.MerchantID = strings.TrimSpace(request.MerchantID)
	request.OutTransferNo = strings.TrimSpace(request.OutTransferNo)
	request.Currency = strings.ToUpper(strings.TrimSpace(request.Currency))
	request.AccountName = strings.TrimSpace(request.AccountName)
	request.AccountNo = strings.TrimSpace(request.AccountNo)
	request.BankName = strings.TrimSpace(request.BankName)
	request.BranchName = strings.TrimSpace(request.BranchName)
	if request.Currency == "" {
		request.Currency = "CNY"
	}
	if request.MerchantID == "" || request.OutTransferNo == "" || len(request.OutTransferNo) > 64 ||
		!businessNoPattern.MatchString(request.OutTransferNo) || request.Amount <= 0 || request.Amount > MaxAmountCents ||
		len(request.Currency) != 3 || request.AccountNo == "" || len(request.AccountNo) > 128 || len(request.AccountName) > 128 ||
		len(request.BankName) > 128 || len(request.BranchName) > 191 {
		return Order{}, false, ErrInvalidRequest
	}
	existing, err := service.findByOutNo(ctx, request.MerchantID, request.OutTransferNo)
	if err == nil {
		return existing, false, compare(existing, request)
	}
	if !errors.Is(err, ErrNotFound) {
		return Order{}, false, err
	}
	if service.ledger == nil {
		return Order{}, false, ErrInvalidRequest
	}
	account, err := service.ledger.CreateAccount(ctx, ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: request.MerchantID, Purpose: "balance",
		Category: ledgermodel.CategoryLiability, Currency: request.Currency,
	})
	if err != nil {
		return Order{}, false, err
	}
	now := service.now().UTC()
	order := Order{
		ID:            uuid.NewString(),
		TransferNo:    businessNo("TR", now),
		MerchantID:    request.MerchantID,
		OutTransferNo: request.OutTransferNo,
		Amount:        request.Amount,
		Currency:      request.Currency,
		AccountName:   request.AccountName,
		AccountNo:     request.AccountNo,
		BankName:      request.BankName,
		BranchName:    request.BranchName,
		Status:        StatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := service.db.WithContext(ctx).Create(&order).Error; err != nil {
		if conflict, lookupErr := service.findByOutNo(ctx, request.MerchantID, request.OutTransferNo); lookupErr == nil {
			return conflict, false, compare(conflict, request)
		}
		return Order{}, false, err
	}
	if _, _, err := service.ledger.Freeze(ctx, ledgerservice.FreezeRequest{
		BusinessKey:   "transfer:" + order.ID,
		AccountID:     account.ID,
		Amount:        order.Amount,
		ReferenceType: "transfer",
		ReferenceID:   order.ID,
		Description:   "merchant payout freeze " + order.TransferNo,
	}); err != nil {
		_ = service.db.WithContext(ctx).Delete(&Order{}, "id = ? AND status = ?", order.ID, StatusPending).Error
		if errors.Is(err, ledgerservice.ErrInsufficientBalance) {
			return Order{}, false, ErrInsufficientBalance
		}
		return Order{}, false, err
	}
	return order, true, nil
}

// Approve releases the freeze and books the payout against the platform cash account.
func (service *Service) Approve(ctx context.Context, id, processedBy string) (Order, error) {
	return service.process(ctx, id, processedBy, "approve", "")
}

// Reject releases the freeze and closes the payout as rejected.
func (service *Service) Reject(ctx context.Context, id, processedBy, reason string) (Order, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 255 {
		return Order{}, ErrInvalidRequest
	}
	return service.process(ctx, id, processedBy, "reject", reason)
}

func (service *Service) process(ctx context.Context, id, processedBy, action, reason string) (Order, error) {
	id = strings.TrimSpace(id)
	processedBy = strings.TrimSpace(processedBy)
	if id == "" || processedBy == "" || (action != "approve" && action != "reject") {
		return Order{}, ErrInvalidRequest
	}
	order, err := service.getByID(ctx, id)
	if err != nil {
		return Order{}, err
	}
	if order.Status == StatusApproved || order.Status == StatusRejected {
		if (action == "approve" && order.Status == StatusApproved) || (action == "reject" && order.Status == StatusRejected) {
			return order, nil
		}
		return Order{}, ErrStateConflict
	}
	merchant, platform, err := service.accounts(ctx, order.MerchantID, order.Currency)
	if err != nil {
		return Order{}, err
	}
	if _, err := service.ledger.Release(ctx, "transfer:"+order.ID); err != nil && !errors.Is(err, ledgerservice.ErrFreezeNotFound) {
		return Order{}, err
	}
	now := service.now().UTC()
	updates := map[string]any{"processed_by": processedBy, "processed_at": now, "updated_at": now}
	if action == "reject" {
		updates["status"] = StatusRejected
		updates["rejection_reason"] = reason
	} else {
		result, postErr := service.ledger.Post(ctx, ledgerservice.TransactionRequest{
			IdempotencyKey: "transfer-settle:" + order.ID,
			ReferenceType:  "transfer",
			ReferenceID:    order.ID,
			Description:    "merchant payout " + order.TransferNo,
			Postings: []ledgerservice.Posting{
				{AccountID: merchant.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: order.Amount},
				{AccountID: platform.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: order.Amount},
			},
		})
		if postErr != nil {
			return Order{}, postErr
		}
		updates["status"] = StatusApproved
		updates["ledger_transaction_id"] = result.Transaction.ID
		order.LedgerTransactionID = &result.Transaction.ID
	}
	update := service.db.WithContext(ctx).Model(&Order{}).Where("id = ? AND status = ?", order.ID, StatusPending).Updates(updates)
	if update.Error != nil {
		return Order{}, update.Error
	}
	if update.RowsAffected != 1 {
		latest, lookupErr := service.getByID(ctx, order.ID)
		if lookupErr == nil && latest.Status == Status(updates["status"].(Status)) {
			return latest, nil
		}
		return Order{}, ErrStateConflict
	}
	order.Status = updates["status"].(Status)
	order.ProcessedBy = &processedBy
	order.ProcessedAt = &now
	order.UpdatedAt = now
	if action == "reject" {
		order.RejectionReason = &reason
	}
	return order, nil
}

// Get returns one payout scoped to a merchant; an empty merchantID means admin access.
func (service *Service) Get(ctx context.Context, merchantID, id string) (Order, error) {
	order, err := service.getByID(ctx, id)
	if err != nil {
		return Order{}, err
	}
	if merchantID != "" && order.MerchantID != merchantID {
		return Order{}, ErrNotFound
	}
	return order, nil
}

// Find resolves a payout by transfer_no or out_transfer_no for one merchant.
func (service *Service) Find(ctx context.Context, merchantID, transferNo, outTransferNo string) (Order, error) {
	transferNo = strings.TrimSpace(transferNo)
	outTransferNo = strings.TrimSpace(outTransferNo)
	if transferNo == "" && outTransferNo == "" {
		return Order{}, ErrInvalidRequest
	}
	query := service.db.WithContext(ctx).Where("merchant_id = ?", merchantID)
	if transferNo != "" {
		query = query.Where("transfer_no = ?", transferNo)
	} else {
		query = query.Where("out_transfer_no = ?", outTransferNo)
	}
	var order Order
	if err := query.First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	return order, nil
}

// List returns payouts filtered by merchant and status.
func (service *Service) List(ctx context.Context, filter Filter, page Page) ([]Order, int64, error) {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 || page.Size > 100 {
		page.Size = 20
	}
	query := service.db.WithContext(ctx).Model(&Order{})
	if filter.MerchantID != "" {
		query = query.Where("merchant_id = ?", filter.MerchantID)
	}
	if filter.Status != "" {
		if !ValidStatus(filter.Status) {
			return nil, 0, ErrInvalidRequest
		}
		query = query.Where("status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orders []Order
	err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&orders).Error
	return orders, total, err
}

// Balance reports the merchant available and frozen ledger amounts.
func (service *Service) Balance(ctx context.Context, merchantID string) (Balance, error) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return Balance{}, ErrInvalidRequest
	}
	var account ledgermodel.Account
	err := service.db.WithContext(ctx).
		Where("owner_type = ? AND owner_id = ? AND purpose = ? AND currency = ?", "merchant", merchantID, "balance", "CNY").
		First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Balance{}, nil
	}
	if err != nil {
		return Balance{}, err
	}
	var balance ledgermodel.Balance
	if err := service.db.WithContext(ctx).First(&balance, "account_id = ?", account.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Balance{}, nil
		}
		return Balance{}, err
	}
	return Balance{Available: balance.Available, Frozen: balance.Frozen}, nil
}

func (service *Service) getByID(ctx context.Context, id string) (Order, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Order{}, ErrInvalidRequest
	}
	var order Order
	if err := service.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	return order, nil
}

func (service *Service) findByOutNo(ctx context.Context, merchantID, outTransferNo string) (Order, error) {
	var order Order
	if err := service.db.WithContext(ctx).
		Where("merchant_id = ? AND out_transfer_no = ?", merchantID, outTransferNo).
		First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	return order, nil
}

func (service *Service) accounts(ctx context.Context, merchantID, currency string) (ledgermodel.Account, ledgermodel.Account, error) {
	if service.ledger == nil {
		return ledgermodel.Account{}, ledgermodel.Account{}, ErrInvalidRequest
	}
	merchant, err := service.ledger.CreateAccount(ctx, ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: merchantID, Purpose: "balance",
		Category: ledgermodel.CategoryLiability, Currency: currency,
	})
	if err != nil {
		return ledgermodel.Account{}, ledgermodel.Account{}, err
	}
	platform, err := service.ledger.CreateAccount(ctx, ledgerservice.AccountRequest{
		OwnerType: "platform", OwnerID: "platform", Purpose: "cash",
		Category: ledgermodel.CategoryAsset, Currency: currency,
	})
	return merchant, platform, err
}

func compare(order Order, request Request) error {
	if order.Amount != request.Amount || order.Currency != request.Currency || order.AccountNo != request.AccountNo ||
		order.AccountName != request.AccountName || order.BankName != request.BankName || order.BranchName != request.BranchName {
		return ErrIdempotencyConflict
	}
	return nil
}

func businessNo(prefix string, now time.Time) string {
	return fmt.Sprintf("%s%s%s", prefix, now.Format("20060102150405"), strings.ReplaceAll(uuid.NewString(), "-", "")[:8])
}
