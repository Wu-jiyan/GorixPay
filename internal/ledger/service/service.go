package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	ledgermodel "gorixpay/internal/ledger/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidRequest      = errors.New("invalid ledger request")
	ErrUnbalanced          = errors.New("ledger transaction is not balanced")
	ErrAccountNotFound     = errors.New("ledger account not found")
	ErrAccountDisabled     = errors.New("ledger account is disabled")
	ErrInsufficientBalance = errors.New("insufficient ledger balance")
	ErrIdempotencyConflict = errors.New("ledger idempotency key conflict")
	ErrOperationNotFound   = errors.New("ledger operation not found")
	ErrOperationProcessed  = errors.New("ledger operation already processed")
	ErrFreezeNotFound      = errors.New("ledger freeze not found")
)

type AccountRequest struct {
	OwnerType string
	OwnerID   string
	Purpose   string
	Category  ledgermodel.AccountCategory
	Currency  string
}

type Posting struct {
	AccountID string                `json:"account_id"`
	Direction ledgermodel.Direction `json:"direction"`
	Bucket    ledgermodel.Bucket    `json:"bucket"`
	Amount    int64                 `json:"amount"`
}

type TransactionRequest struct {
	IdempotencyKey string
	ReferenceType  string
	ReferenceID    string
	Description    string
	Metadata       string
	Postings       []Posting
}

type Result struct {
	Transaction ledgermodel.Transaction `json:"transaction"`
	Entries     []ledgermodel.Entry     `json:"entries"`
	Replayed    bool                    `json:"replayed"`
}

type OperationRequest struct {
	IdempotencyKey        string
	OperationType         ledgermodel.OperationType
	AccountID             string
	CounterpartyAccountID string
	Amount                int64
	ReferenceType         string
	ReferenceID           string
	Description           string
	Metadata              string
	CreatedBy             string
}

type ProcessOperationRequest struct {
	OperationID     string
	Action          string
	ProcessedBy     string
	RejectionReason string
}

type FreezeRequest struct {
	BusinessKey   string
	AccountID     string
	Amount        int64
	ReferenceType string
	ReferenceID   string
	Description   string
	Metadata      string
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

func (service *Service) CreateAccount(ctx context.Context, request AccountRequest) (ledgermodel.Account, error) {
	var account ledgermodel.Account
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		account, err = service.CreateAccountInTransaction(tx, request)
		return err
	})
	return account, err
}

func (service *Service) CreateAccountInTransaction(tx *gorm.DB, request AccountRequest) (ledgermodel.Account, error) {
	request.OwnerType = strings.TrimSpace(request.OwnerType)
	request.OwnerID = strings.TrimSpace(request.OwnerID)
	request.Purpose = strings.TrimSpace(request.Purpose)
	request.Currency = strings.ToUpper(strings.TrimSpace(request.Currency))
	if tx == nil || request.OwnerType == "" || request.OwnerID == "" || request.Purpose == "" || len(request.Currency) != 3 || !validCategory(request.Category) {
		return ledgermodel.Account{}, ErrInvalidRequest
	}
	var existing ledgermodel.Account
	err := tx.Where("owner_type = ? AND owner_id = ? AND purpose = ? AND currency = ?", request.OwnerType, request.OwnerID, request.Purpose, request.Currency).First(&existing).Error
	if err == nil {
		if existing.Category != request.Category {
			return ledgermodel.Account{}, ErrIdempotencyConflict
		}
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return ledgermodel.Account{}, err
	}
	now := service.now().UTC()
	account := ledgermodel.Account{ID: uuid.NewString(), OwnerType: request.OwnerType, OwnerID: request.OwnerID, Purpose: request.Purpose, Category: request.Category, Currency: request.Currency, Status: ledgermodel.AccountActive, CreatedAt: now, UpdatedAt: now}
	if err := tx.Create(&account).Error; err != nil {
		return ledgermodel.Account{}, err
	}
	if err := tx.Create(&ledgermodel.Balance{AccountID: account.ID, Version: 1, UpdatedAt: now}).Error; err != nil {
		return ledgermodel.Account{}, err
	}
	return account, nil
}

func (service *Service) Post(ctx context.Context, request TransactionRequest) (Result, error) {
	var result Result
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = service.PostInTransaction(tx, request)
		return err
	})
	return result, err
}

func (service *Service) PostInTransaction(tx *gorm.DB, request TransactionRequest) (Result, error) {
	hash, err := validateAndHash(request)
	if tx == nil {
		return Result{}, ErrInvalidRequest
	}
	if err != nil {
		return Result{}, err
	}
	var existing ledgermodel.Transaction
	err = tx.Where("idempotency_key = ?", request.IdempotencyKey).First(&existing).Error
	if err == nil {
		if existing.RequestHash != hash {
			return Result{}, ErrIdempotencyConflict
		}
		var entries []ledgermodel.Entry
		if err := tx.Where("transaction_id = ?", existing.ID).Order("sequence ASC").Find(&entries).Error; err != nil {
			return Result{}, err
		}
		return Result{Transaction: existing, Entries: entries, Replayed: true}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return Result{}, err
	}
	accountIDs := uniqueAccountIDs(request.Postings)
	var accounts []ledgermodel.Account
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", accountIDs).Order("id ASC").Find(&accounts).Error; err != nil {
		return Result{}, err
	}
	if len(accounts) != len(accountIDs) {
		return Result{}, ErrAccountNotFound
	}
	accountByID := make(map[string]ledgermodel.Account, len(accounts))
	for _, account := range accounts {
		if account.Status != ledgermodel.AccountActive {
			return Result{}, ErrAccountDisabled
		}
		accountByID[account.ID] = account
	}
	var balances []ledgermodel.Balance
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id IN ?", accountIDs).Order("account_id ASC").Find(&balances).Error; err != nil {
		return Result{}, err
	}
	if len(balances) != len(accountIDs) {
		return Result{}, ErrAccountNotFound
	}
	balanceByID := make(map[string]*ledgermodel.Balance, len(balances))
	for index := range balances {
		balanceByID[balances[index].AccountID] = &balances[index]
	}
	now := service.now().UTC()
	transaction := ledgermodel.Transaction{ID: uuid.NewString(), IdempotencyKey: request.IdempotencyKey, RequestHash: hash, ReferenceType: request.ReferenceType, ReferenceID: request.ReferenceID, Description: request.Description, Metadata: normalizeMetadata(request.Metadata), CreatedAt: now}
	if err := tx.Create(&transaction).Error; err != nil {
		return Result{}, err
	}
	entries := make([]ledgermodel.Entry, 0, len(request.Postings))
	for index, posting := range request.Postings {
		balance := balanceByID[posting.AccountID]
		delta := naturalDelta(accountByID[posting.AccountID].Category, posting.Direction, posting.Amount)
		var balanceAfter int64
		if posting.Bucket == ledgermodel.BucketAvailable {
			balance.Available += delta
			balanceAfter = balance.Available
		} else {
			balance.Frozen += delta
			balanceAfter = balance.Frozen
		}
		if balanceAfter < 0 {
			return Result{}, ErrInsufficientBalance
		}
		entries = append(entries, ledgermodel.Entry{ID: uuid.NewString(), TransactionID: transaction.ID, AccountID: posting.AccountID, Sequence: index + 1, Direction: posting.Direction, Bucket: posting.Bucket, Amount: posting.Amount, BalanceAfter: balanceAfter, CreatedAt: now})
	}
	if err := tx.Create(&entries).Error; err != nil {
		return Result{}, err
	}
	for _, balance := range balances {
		balance.Version++
		balance.UpdatedAt = now
		update := tx.Model(&ledgermodel.Balance{}).Where("account_id = ? AND version = ?", balance.AccountID, balance.Version-1).Updates(map[string]any{"available": balance.Available, "frozen": balance.Frozen, "version": balance.Version, "updated_at": now})
		if update.Error != nil {
			return Result{}, update.Error
		}
		if update.RowsAffected != 1 {
			return Result{}, ErrIdempotencyConflict
		}
	}
	return Result{Transaction: transaction, Entries: entries}, nil
}

func (service *Service) CreateOperation(ctx context.Context, request OperationRequest) (ledgermodel.Operation, bool, error) {
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.CounterpartyAccountID = strings.TrimSpace(request.CounterpartyAccountID)
	request.ReferenceType = strings.TrimSpace(request.ReferenceType)
	request.ReferenceID = strings.TrimSpace(request.ReferenceID)
	request.CreatedBy = strings.TrimSpace(request.CreatedBy)
	request.Metadata = normalizeMetadata(request.Metadata)
	if request.IdempotencyKey == "" || len(request.IdempotencyKey) > 191 || request.AccountID == "" || request.CounterpartyAccountID == "" || request.AccountID == request.CounterpartyAccountID || request.Amount <= 0 || request.ReferenceType == "" || request.ReferenceID == "" || request.CreatedBy == "" || !validOperationType(request.OperationType) || !json.Valid([]byte(request.Metadata)) {
		return ledgermodel.Operation{}, false, ErrInvalidRequest
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return ledgermodel.Operation{}, false, err
	}
	digest := sha256.Sum256(payload)
	hash := hex.EncodeToString(digest[:])
	var operation ledgermodel.Operation
	created := false
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ledgermodel.Operation
		err := tx.Where("idempotency_key = ?", request.IdempotencyKey).First(&existing).Error
		if err == nil {
			if existing.RequestHash != hash {
				return ErrIdempotencyConflict
			}
			operation = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var accountCount int64
		if err := tx.Model(&ledgermodel.Account{}).Where("id IN ? AND status = ?", []string{request.AccountID, request.CounterpartyAccountID}, ledgermodel.AccountActive).Count(&accountCount).Error; err != nil {
			return err
		}
		if accountCount != 2 {
			return ErrAccountNotFound
		}
		now := service.now().UTC()
		operation = ledgermodel.Operation{ID: uuid.NewString(), IdempotencyKey: request.IdempotencyKey, RequestHash: hash, OperationType: request.OperationType, Status: ledgermodel.OperationPending, AccountID: request.AccountID, CounterpartyAccountID: request.CounterpartyAccountID, Amount: request.Amount, ReferenceType: request.ReferenceType, ReferenceID: request.ReferenceID, Description: strings.TrimSpace(request.Description), Metadata: request.Metadata, CreatedBy: request.CreatedBy, CreatedAt: now}
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return operation, created, err
}

func (service *Service) ProcessOperation(ctx context.Context, request ProcessOperationRequest) (ledgermodel.Operation, error) {
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	request.ProcessedBy = strings.TrimSpace(request.ProcessedBy)
	request.RejectionReason = strings.TrimSpace(request.RejectionReason)
	if request.OperationID == "" || request.ProcessedBy == "" || (request.Action != "post" && request.Action != "reject") || (request.Action == "reject" && request.RejectionReason == "") || len(request.RejectionReason) > 255 {
		return ledgermodel.Operation{}, ErrInvalidRequest
	}
	var operation ledgermodel.Operation
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&operation, "id = ?", request.OperationID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrOperationNotFound
			}
			return err
		}
		if operation.Status != ledgermodel.OperationPending {
			return nil
		}
		now := service.now().UTC()
		updates := map[string]any{"processed_by": request.ProcessedBy, "processed_at": now}
		if request.Action == "reject" {
			updates["status"] = ledgermodel.OperationRejected
			updates["rejection_reason"] = request.RejectionReason
			operation.Status = ledgermodel.OperationRejected
			operation.RejectionReason = &request.RejectionReason
		} else {
			var accounts []ledgermodel.Account
			if err := tx.Where("id IN ?", []string{operation.AccountID, operation.CounterpartyAccountID}).Find(&accounts).Error; err != nil {
				return err
			}
			if len(accounts) != 2 {
				return ErrAccountNotFound
			}
			categories := make(map[string]ledgermodel.AccountCategory, 2)
			var category ledgermodel.AccountCategory
			var currency string
			for _, account := range accounts {
				if category != "" && (account.Category != category || account.Currency != currency) {
					return ErrInvalidRequest
				}
				category = account.Category
				currency = account.Currency
				categories[account.ID] = account.Category
			}
			accountPositive := operation.OperationType == ledgermodel.OperationCredit
			result, err := service.PostInTransaction(tx, TransactionRequest{IdempotencyKey: "ledger-operation:" + operation.ID, ReferenceType: operation.ReferenceType, ReferenceID: operation.ReferenceID, Description: operation.Description, Metadata: operation.Metadata, Postings: []Posting{{AccountID: operation.AccountID, Direction: directionForDelta(categories[operation.AccountID], accountPositive), Bucket: ledgermodel.BucketAvailable, Amount: operation.Amount}, {AccountID: operation.CounterpartyAccountID, Direction: directionForDelta(categories[operation.CounterpartyAccountID], !accountPositive), Bucket: ledgermodel.BucketAvailable, Amount: operation.Amount}}})
			if err != nil {
				return err
			}
			updates["status"] = ledgermodel.OperationPosted
			updates["transaction_id"] = result.Transaction.ID
			operation.Status = ledgermodel.OperationPosted
			operation.TransactionID = &result.Transaction.ID
		}
		update := tx.Model(&ledgermodel.Operation{}).Where("id = ? AND status = ?", operation.ID, ledgermodel.OperationPending).Updates(updates)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrOperationProcessed
		}
		operation.ProcessedBy = &request.ProcessedBy
		operation.ProcessedAt = &now
		return nil
	})
	return operation, err
}

func (service *Service) Freeze(ctx context.Context, request FreezeRequest) (ledgermodel.Freeze, bool, error) {
	request.BusinessKey = strings.TrimSpace(request.BusinessKey)
	if request.BusinessKey == "" || len(request.BusinessKey) > 191 || request.AccountID == "" || request.Amount <= 0 || request.ReferenceType == "" || request.ReferenceID == "" {
		return ledgermodel.Freeze{}, false, ErrInvalidRequest
	}
	var freeze ledgermodel.Freeze
	created := false
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("business_key = ?", request.BusinessKey).First(&freeze).Error; err == nil {
			if freeze.AccountID != request.AccountID || freeze.Amount != request.Amount {
				return ErrIdempotencyConflict
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var account ledgermodel.Account
		if err := tx.First(&account, "id = ?", request.AccountID).Error; err != nil {
			return ErrAccountNotFound
		}
		result, err := service.PostInTransaction(tx, TransactionRequest{IdempotencyKey: "freeze:" + request.BusinessKey, ReferenceType: request.ReferenceType, ReferenceID: request.ReferenceID, Description: request.Description, Metadata: request.Metadata, Postings: []Posting{{AccountID: request.AccountID, Direction: directionForDelta(account.Category, false), Bucket: ledgermodel.BucketAvailable, Amount: request.Amount}, {AccountID: request.AccountID, Direction: directionForDelta(account.Category, true), Bucket: ledgermodel.BucketFrozen, Amount: request.Amount}}})
		if err != nil {
			return err
		}
		now := service.now().UTC()
		freeze = ledgermodel.Freeze{ID: uuid.NewString(), AccountID: request.AccountID, BusinessKey: request.BusinessKey, Amount: request.Amount, Status: ledgermodel.FreezeActive, FreezeTransactionID: result.Transaction.ID, CreatedAt: now}
		if err := tx.Create(&freeze).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return freeze, created, err
}

func (service *Service) Release(ctx context.Context, businessKey string) (ledgermodel.Freeze, error) {
	var freeze ledgermodel.Freeze
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("business_key = ?", strings.TrimSpace(businessKey)).First(&freeze).Error; err != nil {
			return ErrFreezeNotFound
		}
		if freeze.Status == ledgermodel.FreezeReleased {
			return nil
		}
		var account ledgermodel.Account
		if err := tx.First(&account, "id = ?", freeze.AccountID).Error; err != nil {
			return ErrAccountNotFound
		}
		result, err := service.PostInTransaction(tx, TransactionRequest{IdempotencyKey: "release:" + freeze.BusinessKey, ReferenceType: "freeze", ReferenceID: freeze.ID, Description: "release frozen funds", Postings: []Posting{{AccountID: freeze.AccountID, Direction: directionForDelta(account.Category, false), Bucket: ledgermodel.BucketFrozen, Amount: freeze.Amount}, {AccountID: freeze.AccountID, Direction: directionForDelta(account.Category, true), Bucket: ledgermodel.BucketAvailable, Amount: freeze.Amount}}})
		if err != nil {
			return err
		}
		now := service.now().UTC()
		freeze.Status = ledgermodel.FreezeReleased
		freeze.ReleaseTransactionID = &result.Transaction.ID
		freeze.ReleasedAt = &now
		return tx.Model(&ledgermodel.Freeze{}).Where("id = ? AND status = ?", freeze.ID, ledgermodel.FreezeActive).Updates(map[string]any{"status": freeze.Status, "release_transaction_id": result.Transaction.ID, "released_at": now}).Error
	})
	return freeze, err
}

func validateAndHash(request TransactionRequest) (string, error) {
	if strings.TrimSpace(request.IdempotencyKey) == "" || len(request.IdempotencyKey) > 191 || strings.TrimSpace(request.ReferenceType) == "" || strings.TrimSpace(request.ReferenceID) == "" || len(request.Postings) < 2 {
		return "", ErrInvalidRequest
	}
	if request.Metadata != "" && !json.Valid([]byte(request.Metadata)) {
		return "", ErrInvalidRequest
	}
	var debit, credit int64
	for _, posting := range request.Postings {
		if posting.AccountID == "" || posting.Amount <= 0 || (posting.Direction != ledgermodel.DirectionDebit && posting.Direction != ledgermodel.DirectionCredit) || (posting.Bucket != ledgermodel.BucketAvailable && posting.Bucket != ledgermodel.BucketFrozen) {
			return "", ErrInvalidRequest
		}
		if posting.Direction == ledgermodel.DirectionDebit {
			debit += posting.Amount
		} else {
			credit += posting.Amount
		}
	}
	if debit != credit {
		return "", ErrUnbalanced
	}
	payload, err := json.Marshal(struct {
		ReferenceType string
		ReferenceID   string
		Description   string
		Metadata      string
		Postings      []Posting
	}{request.ReferenceType, request.ReferenceID, request.Description, normalizeMetadata(request.Metadata), request.Postings})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func uniqueAccountIDs(postings []Posting) []string {
	seen := make(map[string]struct{}, len(postings))
	ids := make([]string, 0, len(postings))
	for _, posting := range postings {
		if _, ok := seen[posting.AccountID]; !ok {
			seen[posting.AccountID] = struct{}{}
			ids = append(ids, posting.AccountID)
		}
	}
	return ids
}

func naturalDelta(category ledgermodel.AccountCategory, direction ledgermodel.Direction, amount int64) int64 {
	debitPositive := category == ledgermodel.CategoryAsset || category == ledgermodel.CategoryExpense
	if (direction == ledgermodel.DirectionDebit) == debitPositive {
		return amount
	}
	return -amount
}

func validCategory(category ledgermodel.AccountCategory) bool {
	return category == ledgermodel.CategoryAsset || category == ledgermodel.CategoryLiability || category == ledgermodel.CategoryEquity || category == ledgermodel.CategoryRevenue || category == ledgermodel.CategoryExpense
}

func validOperationType(operationType ledgermodel.OperationType) bool {
	return operationType == ledgermodel.OperationCredit || operationType == ledgermodel.OperationDebit || operationType == ledgermodel.OperationTransfer
}

func directionForDelta(category ledgermodel.AccountCategory, positive bool) ledgermodel.Direction {
	debitPositive := category == ledgermodel.CategoryAsset || category == ledgermodel.CategoryExpense
	if positive == debitPositive {
		return ledgermodel.DirectionDebit
	}
	return ledgermodel.DirectionCredit
}

func normalizeMetadata(metadata string) string {
	if strings.TrimSpace(metadata) == "" {
		return "{}"
	}
	return metadata
}
