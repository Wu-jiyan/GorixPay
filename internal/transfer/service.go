package transfer

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
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
	Kind       Kind
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
		Kind:          KindNormal,
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
	if filter.Kind != "" {
		if !ValidKind(filter.Kind) {
			return nil, 0, ErrInvalidRequest
		}
		query = query.Where("kind = ?", filter.Kind)
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

// MaxRedCount caps one red packet batch.
const MaxRedCount = 100

// RedRequest creates a red packet batch: Count payouts whose amounts sum to
// TotalAmount, every packet at least one fen. OutTransferNo is optional and only
// used to make a retried creation return the original batch.
type RedRequest struct {
	MerchantID    string
	OutTransferNo string
	TotalAmount   int64
	Count         int
	Currency      string
	Remark        string
}

// RedBatch is one red packet creation aggregated from its child payouts.
type RedBatch struct {
	BatchNo      string    `json:"batch_no"`
	TransferNo   string    `json:"transfer_no"`
	MerchantID   string    `json:"merchant_id"`
	Amount       int64     `json:"amount"`
	Count        int       `json:"count"`
	ClaimedCount int       `json:"claimed_count"`
	Status       Status    `json:"status"`
	Remark       string    `json:"remark"`
	CreatedAt    time.Time `json:"created_at"`
}

// CreateRed splits a total into Count frozen payouts of kind red. The whole
// batch is written in one transaction, so a batch either exists with every
// packet frozen or does not exist at all.
func (service *Service) CreateRed(ctx context.Context, request RedRequest) (RedBatch, bool, error) {
	request.MerchantID = strings.TrimSpace(request.MerchantID)
	request.OutTransferNo = strings.TrimSpace(request.OutTransferNo)
	request.Currency = strings.ToUpper(strings.TrimSpace(request.Currency))
	request.Remark = strings.TrimSpace(request.Remark)
	if request.Currency == "" {
		request.Currency = "CNY"
	}
	if request.MerchantID == "" || request.TotalAmount <= 0 || request.TotalAmount > MaxAmountCents ||
		request.Count < 1 || request.Count > MaxRedCount || int64(request.Count) > request.TotalAmount ||
		len(request.Currency) != 3 || len(request.Remark) > 191 ||
		(request.OutTransferNo != "" && (len(request.OutTransferNo) > 50 || !businessNoPattern.MatchString(request.OutTransferNo))) {
		return RedBatch{}, false, ErrInvalidRequest
	}
	if service.ledger == nil {
		return RedBatch{}, false, ErrInvalidRequest
	}
	if request.OutTransferNo != "" {
		existing, err := service.redBatch(ctx, request.MerchantID, request.OutTransferNo)
		if err == nil {
			if existing.Amount != request.TotalAmount || existing.Count != request.Count {
				return RedBatch{}, false, ErrIdempotencyConflict
			}
			return existing, false, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return RedBatch{}, false, err
		}
	}
	amounts, err := splitRedAmount(request.TotalAmount, request.Count)
	if err != nil {
		return RedBatch{}, false, err
	}
	account, err := service.ledger.CreateAccount(ctx, ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: request.MerchantID, Purpose: "balance",
		Category: ledgermodel.CategoryLiability, Currency: request.Currency,
	})
	if err != nil {
		return RedBatch{}, false, err
	}
	now := service.now().UTC()
	batchNo := request.OutTransferNo
	if batchNo == "" {
		batchNo = businessNo("RB", now)
	}
	orders := make([]Order, 0, len(amounts))
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txLedger := ledgerservice.New(tx, service.now)
		for index, amount := range amounts {
			batch := batchNo
			outNo := fmt.Sprintf("%s-%02d", batchNo, index+1)
			order := Order{
				ID: uuid.NewString(), TransferNo: businessNo("TR", now), MerchantID: request.MerchantID,
				OutTransferNo: outNo, Amount: amount, Currency: request.Currency,
				AccountName: redAccountName(request.Remark), AccountNo: outNo,
				Status: StatusPending, Kind: KindRed, BatchNo: &batch, CreatedAt: now, UpdatedAt: now,
			}
			if request.Remark != "" {
				remark := request.Remark
				order.Remark = &remark
			}
			if err := tx.Create(&order).Error; err != nil {
				return err
			}
			if _, _, err := txLedger.Freeze(ctx, ledgerservice.FreezeRequest{
				BusinessKey: "transfer:" + order.ID, AccountID: account.ID, Amount: amount,
				ReferenceType: "transfer", ReferenceID: order.ID,
				Description: "merchant red packet freeze " + order.TransferNo,
			}); err != nil {
				if errors.Is(err, ledgerservice.ErrInsufficientBalance) {
					return ErrInsufficientBalance
				}
				return err
			}
			orders = append(orders, order)
		}
		return nil
	})
	if err != nil {
		return RedBatch{}, false, err
	}
	return RedBatch{
		BatchNo: batchNo, TransferNo: orders[0].TransferNo, MerchantID: request.MerchantID,
		Amount: request.TotalAmount, Count: len(orders), ClaimedCount: 0,
		Status: StatusPending, Remark: request.Remark, CreatedAt: now,
	}, true, nil
}

// RedBatches lists red packet batches, newest first. Amounts and counts come
// from the child payouts; ClaimedCount stays zero because no channel disburses
// red packets yet.
func (service *Service) RedBatches(ctx context.Context, filter Filter, page Page) ([]RedBatch, int64, error) {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 || page.Size > 100 {
		page.Size = 20
	}
	scoped := func() *gorm.DB {
		query := service.db.WithContext(ctx).Table("transfer_orders").Where("kind = ? AND batch_no IS NOT NULL", KindRed)
		if merchantID := strings.TrimSpace(filter.MerchantID); merchantID != "" {
			query = query.Where("merchant_id = ?", merchantID)
		}
		return query
	}
	var total int64
	if err := scoped().Distinct("batch_no").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	// Batch numbers embed their creation timestamp, so ordering by the number
	// keeps the listing chronological without aggregating a timestamp column.
	var batchNos []string
	if err := scoped().Group("batch_no").Order("MIN(created_at) DESC").Order("batch_no DESC").
		Offset((page.Number-1)*page.Size).Limit(page.Size).
		Pluck("batch_no", &batchNos).Error; err != nil {
		return nil, 0, err
	}
	if len(batchNos) == 0 {
		return []RedBatch{}, total, nil
	}
	var children []Order
	if err := service.db.WithContext(ctx).
		Where("batch_no IN ?", batchNos).Order("out_transfer_no ASC").Find(&children).Error; err != nil {
		return nil, 0, err
	}
	grouped := make(map[string][]Order, len(batchNos))
	for _, child := range children {
		if child.BatchNo == nil {
			continue
		}
		grouped[*child.BatchNo] = append(grouped[*child.BatchNo], child)
	}
	batches := make([]RedBatch, 0, len(batchNos))
	for _, batchNo := range batchNos {
		rows := grouped[batchNo]
		if len(rows) == 0 {
			continue
		}
		batches = append(batches, buildRedBatch(batchNo, rows))
	}
	return batches, total, nil
}

// redBatch loads one batch by its identifier.
func (service *Service) redBatch(ctx context.Context, merchantID, batchNo string) (RedBatch, error) {
	var orders []Order
	if err := service.db.WithContext(ctx).
		Where("merchant_id = ? AND batch_no = ?", merchantID, batchNo).
		Order("out_transfer_no ASC").Find(&orders).Error; err != nil {
		return RedBatch{}, err
	}
	if len(orders) == 0 {
		return RedBatch{}, ErrNotFound
	}
	batch := RedBatch{
		BatchNo: batchNo, TransferNo: orders[0].TransferNo, MerchantID: merchantID,
		Count: len(orders), ClaimedCount: 0, Status: StatusPending, CreatedAt: orders[0].CreatedAt,
	}
	pending, approved := 0, 0
	for _, order := range orders {
		batch.Amount += order.Amount
		switch order.Status {
		case StatusPending:
			pending++
		case StatusApproved:
			approved++
		}
		if order.Remark != nil && batch.Remark == "" {
			batch.Remark = *order.Remark
		}
	}
	switch {
	case pending > 0:
		batch.Status = StatusPending
	case approved > 0:
		batch.Status = StatusApproved
	default:
		batch.Status = StatusRejected
	}
	return batch, nil
}

// buildRedBatch aggregates the child payouts of one batch.
func buildRedBatch(batchNo string, orders []Order) RedBatch {
	batch := RedBatch{
		BatchNo: batchNo, TransferNo: orders[0].TransferNo, MerchantID: orders[0].MerchantID,
		Count: len(orders), ClaimedCount: 0, CreatedAt: orders[0].CreatedAt,
	}
	pending, approved := 0, 0
	for _, order := range orders {
		batch.Amount += order.Amount
		switch order.Status {
		case StatusPending:
			pending++
		case StatusApproved:
			approved++
		}
		if order.Remark != nil && batch.Remark == "" {
			batch.Remark = *order.Remark
		}
	}
	switch {
	case pending > 0:
		batch.Status = StatusPending
	case approved > 0:
		batch.Status = StatusApproved
	default:
		batch.Status = StatusRejected
	}
	return batch
}

// splitRedAmount distributes total into count amounts of at least one fen that
// add up exactly to total. Each draw is uniform over the range that still leaves
// one fen for every remaining packet.
func splitRedAmount(total int64, count int) ([]int64, error) {
	if count < 1 || count > MaxRedCount || total < int64(count) || total > MaxAmountCents {
		return nil, ErrInvalidRequest
	}
	amounts := make([]int64, 0, count)
	remaining := total
	for index := 0; index < count-1; index++ {
		ceiling := remaining - int64(count-1-index)
		if ceiling < 1 {
			return nil, ErrInvalidRequest
		}
		draw, err := rand.Int(rand.Reader, big.NewInt(ceiling))
		if err != nil {
			return nil, err
		}
		amount := draw.Int64() + 1
		amounts = append(amounts, amount)
		remaining -= amount
	}
	return append(amounts, remaining), nil
}

// redAccountName labels a red packet payout in the review queue.
func redAccountName(remark string) string {
	if remark == "" {
		return "红包"
	}
	return "红包 " + remark
}

// CancelRedBatch rejects every still-pending packet of a red batch, releases the
// frozen funds and returns how many packets were cancelled. A packet that was
// already reviewed blocks the whole batch with ErrStateConflict.
func (service *Service) CancelRedBatch(ctx context.Context, batchNo, reason string) (int, error) {
	batchNo = strings.TrimSpace(batchNo)
	if batchNo == "" {
		return 0, ErrInvalidRequest
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "红包批次已撤销"
	}
	if len(reason) > 255 {
		reason = reason[:255]
	}
	if service.ledger == nil {
		return 0, ErrInvalidRequest
	}
	cancelled := 0
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var orders []Order
		if err := tx.WithContext(ctx).
			Where("batch_no = ? AND kind = ?", batchNo, KindRed).
			Order("out_transfer_no ASC").Find(&orders).Error; err != nil {
			return err
		}
		if len(orders) == 0 {
			return ErrNotFound
		}
		for _, order := range orders {
			switch order.Status {
			case StatusPending, StatusRejected:
				// Rejected packets were cancelled by an earlier call; the batch
				// cancel is idempotent for those.
			default:
				return ErrStateConflict
			}
		}
		txLedger := ledgerservice.New(tx, service.now)
		now := service.now().UTC()
		for _, order := range orders {
			if order.Status != StatusPending {
				continue
			}
			if _, err := txLedger.Release(ctx, "transfer:"+order.ID); err != nil &&
				!errors.Is(err, ledgerservice.ErrFreezeNotFound) {
				return err
			}
			update := tx.Model(&Order{}).Where("id = ? AND status = ?", order.ID, StatusPending).
				Updates(map[string]any{"status": StatusRejected, "rejection_reason": reason, "updated_at": now})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return ErrStateConflict
			}
			cancelled++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return cancelled, nil
}

// ExpireRedBatches cancels the red batches whose packets are still pending and
// older than olderThan, releasing the frozen money. It returns the number of
// batches cancelled, skips batches that were reviewed in the meantime, and is
// safe to call from any scheduler: no ticker is started here.
func (service *Service) ExpireRedBatches(ctx context.Context, now time.Time, olderThan time.Duration) (int, error) {
	if olderThan <= 0 {
		return 0, ErrInvalidRequest
	}
	cutoff := now.UTC().Add(-olderThan)
	var batchNos []string
	if err := service.db.WithContext(ctx).Model(&Order{}).
		Where("kind = ? AND status = ? AND created_at <= ? AND batch_no IS NOT NULL", KindRed, StatusPending, cutoff).
		Distinct("batch_no").Pluck("batch_no", &batchNos).Error; err != nil {
		return 0, err
	}
	cancelled := 0
	for _, batchNo := range batchNos {
		if strings.TrimSpace(batchNo) == "" {
			continue
		}
		if _, err := service.CancelRedBatch(ctx, batchNo, "红包超时未领取，自动退回"); err != nil {
			if errors.Is(err, ErrStateConflict) || errors.Is(err, ErrNotFound) {
				continue
			}
			return cancelled, err
		}
		cancelled++
	}
	return cancelled, nil
}
