package groupshop

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// currency is the only currency group prices are expressed in.
const currency = "CNY"

// maxPriceCents caps a group price at 100000.00 CNY, matching the V2 amount rule.
const maxPriceCents int64 = 100000 * 100

// Service implements the group shop.
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

// Plans lists the groups a merchant can buy: active groups with a price above
// zero. The zero-price group stays free and is assigned by the administrator.
func (service *Service) Plans(ctx context.Context) ([]Plan, error) {
	var plans []Plan
	err := service.db.WithContext(ctx).
		Where("status = ? AND price_cents > 0", "active").
		Order("price_cents ASC, name ASC").Find(&plans).Error
	return plans, err
}

// AdminPlans lists every group with its price, so the console can edit prices.
func (service *Service) AdminPlans(ctx context.Context) ([]Plan, error) {
	var plans []Plan
	err := service.db.WithContext(ctx).Order("name ASC").Find(&plans).Error
	return plans, err
}

// SetPrice writes the buyout price of one group. Zero disables the sale.
func (service *Service) SetPrice(ctx context.Context, groupID string, priceCents int64) (Plan, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || priceCents < 0 || priceCents > maxPriceCents {
		return Plan{}, ErrInvalidRequest
	}
	var plan Plan
	if err := service.db.WithContext(ctx).First(&plan, "id = ?", groupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Plan{}, ErrNotFound
		}
		return Plan{}, err
	}
	now := service.now().UTC()
	if err := service.db.WithContext(ctx).Model(&Plan{}).Where("id = ?", plan.ID).
		Updates(map[string]any{"price_cents": priceCents, "updated_at": now}).Error; err != nil {
		return Plan{}, err
	}
	plan.PriceCents, plan.UpdatedAt = priceCents, now
	return plan, nil
}

// Purchase charges the merchant balance for a group and moves the merchant into
// it. A purchase that is already active is returned untouched, so a double
// submit never charges twice.
func (service *Service) Purchase(ctx context.Context, merchantID, groupID string) (Purchase, bool, error) {
	merchantID = strings.TrimSpace(merchantID)
	groupID = strings.TrimSpace(groupID)
	if merchantID == "" || groupID == "" {
		return Purchase{}, false, ErrInvalidRequest
	}
	if service.ledger == nil {
		return Purchase{}, false, ErrInvalidRequest
	}
	var plan Plan
	if err := service.db.WithContext(ctx).First(&plan, "id = ?", groupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Purchase{}, false, ErrNotFound
		}
		return Purchase{}, false, err
	}
	if !strings.EqualFold(plan.Status, "active") || plan.PriceCents <= 0 {
		return Purchase{}, false, ErrGroupNotForSale
	}
	now := service.now().UTC()
	var purchase Purchase
	created := false
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loadErr := tx.WithContext(ctx).
			Where("merchant_id = ? AND group_id = ?", merchantID, groupID).First(&purchase).Error
		switch {
		case loadErr == nil && purchase.Status == PurchaseActive:
			return nil
		case loadErr == nil:
			// Refunded earlier: reactivate in place with a fresh attempt number so
			// the ledger key differs from the original purchase.
			purchase.Attempts++
		case errors.Is(loadErr, gorm.ErrRecordNotFound):
			purchase = Purchase{
				ID: uuid.NewString(), MerchantID: merchantID, GroupID: groupID,
				Attempts: 1, CreatedAt: now,
			}
			created = true
		default:
			return loadErr
		}
		transactionID, postErr := service.post(tx, merchantID, plan.PriceCents, purchase.ID,
			"group-purchase:"+merchantID+":"+groupID+":"+strconv.Itoa(purchase.Attempts),
			"merchant group purchase")
		if postErr != nil {
			return postErr
		}
		purchase.PriceCents = plan.PriceCents
		purchase.Status = PurchaseActive
		purchase.LedgerTransactionID = &transactionID
		purchase.UpdatedAt = now
		if created {
			if err := tx.Create(&purchase).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&Purchase{}).Where("id = ?", purchase.ID).Updates(map[string]any{
				"price_cents": purchase.PriceCents, "status": purchase.Status,
				"attempts": purchase.Attempts, "ledger_transaction_id": transactionID,
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		return tx.Table("merchants").Where("id = ?", merchantID).Update("group_id", groupID).Error
	})
	if err != nil {
		// Two concurrent submits can race on the unique pair; the loser reports the
		// winner's row instead of an internal error.
		var existing Purchase
		if lookupErr := service.db.WithContext(ctx).
			Where("merchant_id = ? AND group_id = ?", merchantID, groupID).First(&existing).Error; lookupErr == nil &&
			existing.Status == PurchaseActive {
			return existing, false, nil
		}
		return Purchase{}, false, mapLedgerError(err)
	}
	return purchase, created, nil
}

// Refund reverses the purchase posting and detaches the merchant when the group
// is still the purchased one.
func (service *Service) Refund(ctx context.Context, purchaseID string) (Purchase, error) {
	purchaseID = strings.TrimSpace(purchaseID)
	if purchaseID == "" {
		return Purchase{}, ErrInvalidRequest
	}
	var purchase Purchase
	if err := service.db.WithContext(ctx).First(&purchase, "id = ?", purchaseID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Purchase{}, ErrNotFound
		}
		return Purchase{}, err
	}
	if purchase.Status != PurchaseActive {
		return Purchase{}, ErrNotRefundable
	}
	if service.ledger == nil {
		return Purchase{}, ErrInvalidRequest
	}
	now := service.now().UTC()
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactionID, postErr := service.refundPost(tx, purchase)
		if postErr != nil {
			return postErr
		}
		if err := tx.Model(&Purchase{}).Where("id = ? AND status = ?", purchase.ID, PurchaseActive).
			Updates(map[string]any{"status": PurchaseRefunded, "updated_at": now}).Error; err != nil {
			return err
		}
		// Detach only when the merchant still sits in the refunded group.
		if err := tx.Table("merchants").
			Where("id = ? AND group_id = ?", purchase.MerchantID, purchase.GroupID).
			Update("group_id", nil).Error; err != nil {
			return err
		}
		purchase.Status = PurchaseRefunded
		purchase.UpdatedAt = now
		purchase.LedgerTransactionID = &transactionID
		return nil
	})
	if err != nil {
		return Purchase{}, mapLedgerError(err)
	}
	return purchase, nil
}

// Purchases lists purchases for the administrator, optionally scoped by merchant
// or status.
func (service *Service) Purchases(ctx context.Context, filter PurchaseFilter, page Page) ([]Purchase, int64, error) {
	if filter.Status != "" && !validPurchaseStatus(filter.Status) {
		return nil, 0, ErrInvalidRequest
	}
	query := service.db.WithContext(ctx).Model(&Purchase{})
	if merchantID := strings.TrimSpace(filter.MerchantID); merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []Purchase
	if err := query.Order("created_at DESC, id ASC").Offset(page.Offset()).Limit(page.Limit()).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// post debits the merchant balance and credits the platform cash account.
func (service *Service) post(tx *gorm.DB, merchantID string, amount int64, referenceID, key, description string) (string, error) {
	if amount <= 0 {
		return "", ErrInvalidRequest
	}
	merchantAccount, platformAccount, err := service.accountsInTransaction(tx, merchantID)
	if err != nil {
		return "", err
	}
	result, err := service.ledger.PostInTransaction(tx, ledgerservice.TransactionRequest{
		IdempotencyKey: key, ReferenceType: "group-purchase", ReferenceID: referenceID,
		Description: description,
		Postings: []ledgerservice.Posting{
			{AccountID: merchantAccount.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: amount},
			{AccountID: platformAccount.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: amount},
		},
	})
	if err != nil {
		return "", err
	}
	return result.Transaction.ID, nil
}

// refundPost is the exact reverse of post: platform pays the merchant back.
func (service *Service) refundPost(tx *gorm.DB, purchase Purchase) (string, error) {
	merchantAccount, platformAccount, err := service.accountsInTransaction(tx, purchase.MerchantID)
	if err != nil {
		return "", err
	}
	result, err := service.ledger.PostInTransaction(tx, ledgerservice.TransactionRequest{
		IdempotencyKey: "group-purchase-refund:" + purchase.ID + ":" + strconv.Itoa(purchase.Attempts),
		ReferenceType:  "group-purchase",
		ReferenceID:    purchase.ID,
		Description:    "merchant group purchase refund",
		Postings: []ledgerservice.Posting{
			{AccountID: merchantAccount.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: purchase.PriceCents},
			{AccountID: platformAccount.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: purchase.PriceCents},
		},
	})
	if err != nil {
		return "", err
	}
	return result.Transaction.ID, nil
}

func (service *Service) accountsInTransaction(tx *gorm.DB, merchantID string) (ledgermodel.Account, ledgermodel.Account, error) {
	merchantAccount, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: merchantID, Purpose: "balance",
		Category: ledgermodel.CategoryLiability, Currency: currency,
	})
	if err != nil {
		return ledgermodel.Account{}, ledgermodel.Account{}, err
	}
	platformAccount, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
		OwnerType: "platform", OwnerID: "platform", Purpose: "cash",
		Category: ledgermodel.CategoryAsset, Currency: currency,
	})
	return merchantAccount, platformAccount, err
}

// mapLedgerError translates the ledger's balance failure into this package's
// error so the handler can answer with a merchant friendly message.
func mapLedgerError(err error) error {
	if errors.Is(err, ledgerservice.ErrInsufficientBalance) {
		return ErrInsufficientBalance
	}
	return err
}
