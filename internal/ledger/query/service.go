package query

import (
	"context"
	"errors"

	ledgermodel "gorixpay/internal/ledger/model"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("ledger record not found")

type Page struct {
	Number int
	Size   int
}

type AccountFilter struct {
	OwnerType string
	OwnerID   string
	Purpose   string
	Currency  string
	Status    string
}

type TransactionFilter struct {
	ReferenceType  string
	ReferenceID    string
	IdempotencyKey string
}

type OperationFilter struct {
	Status        string
	OperationType string
	AccountID     string
	ReferenceType string
	ReferenceID   string
	CreatedBy     string
}

type AccountView struct {
	Account ledgermodel.Account `json:"account"`
	Balance ledgermodel.Balance `json:"balance"`
}

type TransactionView struct {
	Transaction ledgermodel.Transaction `json:"transaction"`
	Entries     []ledgermodel.Entry     `json:"entries"`
}

type Service struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Service { return &Service{db: db} }

func (service *Service) Accounts(ctx context.Context, filter AccountFilter, page Page) ([]AccountView, int64, error) {
	query := service.db.WithContext(ctx).Model(&ledgermodel.Account{})
	for column, value := range map[string]string{"owner_type": filter.OwnerType, "owner_id": filter.OwnerID, "purpose": filter.Purpose, "currency": filter.Currency, "status": filter.Status} {
		if value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var accounts []ledgermodel.Account
	if err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&accounts).Error; err != nil {
		return nil, 0, err
	}
	accountIDs := make([]string, 0, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.ID)
	}
	var balances []ledgermodel.Balance
	if len(accountIDs) > 0 {
		if err := service.db.WithContext(ctx).Where("account_id IN ?", accountIDs).Find(&balances).Error; err != nil {
			return nil, 0, err
		}
	}
	balanceByAccount := make(map[string]ledgermodel.Balance, len(balances))
	for _, balance := range balances {
		balanceByAccount[balance.AccountID] = balance
	}
	views := make([]AccountView, 0, len(accounts))
	for _, account := range accounts {
		balance, ok := balanceByAccount[account.ID]
		if !ok {
			return nil, 0, ErrNotFound
		}
		views = append(views, AccountView{Account: account, Balance: balance})
	}
	return views, total, nil
}

func (service *Service) Account(ctx context.Context, id string) (AccountView, error) {
	var account ledgermodel.Account
	if err := service.db.WithContext(ctx).First(&account, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AccountView{}, ErrNotFound
		}
		return AccountView{}, err
	}
	var balance ledgermodel.Balance
	if err := service.db.WithContext(ctx).First(&balance, "account_id = ?", id).Error; err != nil {
		return AccountView{}, err
	}
	return AccountView{Account: account, Balance: balance}, nil
}

func (service *Service) Entries(ctx context.Context, accountID string, page Page) ([]ledgermodel.Entry, int64, error) {
	query := service.db.WithContext(ctx).Model(&ledgermodel.Entry{}).Where("account_id = ?", accountID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var entries []ledgermodel.Entry
	if err := query.Order("created_at DESC, sequence DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&entries).Error; err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

func (service *Service) Transactions(ctx context.Context, filter TransactionFilter, page Page) ([]ledgermodel.Transaction, int64, error) {
	query := service.db.WithContext(ctx).Model(&ledgermodel.Transaction{})
	for column, value := range map[string]string{"reference_type": filter.ReferenceType, "reference_id": filter.ReferenceID, "idempotency_key": filter.IdempotencyKey} {
		if value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var transactions []ledgermodel.Transaction
	if err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&transactions).Error; err != nil {
		return nil, 0, err
	}
	return transactions, total, nil
}

func (service *Service) Transaction(ctx context.Context, id string) (TransactionView, error) {
	var transaction ledgermodel.Transaction
	if err := service.db.WithContext(ctx).First(&transaction, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return TransactionView{}, ErrNotFound
		}
		return TransactionView{}, err
	}
	var entries []ledgermodel.Entry
	if err := service.db.WithContext(ctx).Where("transaction_id = ?", id).Order("sequence ASC").Find(&entries).Error; err != nil {
		return TransactionView{}, err
	}
	return TransactionView{Transaction: transaction, Entries: entries}, nil
}

func (service *Service) Operations(ctx context.Context, filter OperationFilter, page Page) ([]ledgermodel.Operation, int64, error) {
	query := service.db.WithContext(ctx).Model(&ledgermodel.Operation{})
	for column, value := range map[string]string{"status": filter.Status, "operation_type": filter.OperationType, "account_id": filter.AccountID, "reference_type": filter.ReferenceType, "reference_id": filter.ReferenceID, "created_by": filter.CreatedBy} {
		if value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var operations []ledgermodel.Operation
	if err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&operations).Error; err != nil {
		return nil, 0, err
	}
	return operations, total, nil
}

func (service *Service) Operation(ctx context.Context, id string) (ledgermodel.Operation, error) {
	var operation ledgermodel.Operation
	if err := service.db.WithContext(ctx).First(&operation, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ledgermodel.Operation{}, ErrNotFound
		}
		return ledgermodel.Operation{}, err
	}
	return operation, nil
}
