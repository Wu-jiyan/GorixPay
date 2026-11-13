package certification

import (
	"context"
	"errors"
	"strings"
	"time"

	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	"gorixpay/internal/platform/secret"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Service implements merchant certification submission and review.
type Service struct {
	db     *gorm.DB
	cipher *secret.Cipher
	ledger *ledgerservice.Service
	now    func() time.Time
}

func New(db *gorm.DB, cipher *secret.Cipher, ledger *ledgerservice.Service, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, cipher: cipher, ledger: ledger, now: now}
}

type SubmitRequest struct {
	MerchantID string
	CertType   string
	RealName   string
	IDNo       string
	CorpName   string
	CorpNo     string
}

type ReviewRequest struct {
	MerchantID string
	Action     string
	Reason     string
	ReviewerID string
}

type Filter struct {
	Status  Status
	Keyword string
}

type Page struct {
	Number int
	Size   int
}

// Submit creates or replaces a pending certification request.
func (service *Service) Submit(ctx context.Context, request SubmitRequest) (Certification, error) {
	request.MerchantID = strings.TrimSpace(request.MerchantID)
	request.CertType = strings.ToLower(strings.TrimSpace(request.CertType))
	request.RealName = strings.TrimSpace(request.RealName)
	request.IDNo = strings.ToUpper(strings.TrimSpace(request.IDNo))
	request.CorpName = strings.TrimSpace(request.CorpName)
	request.CorpNo = strings.TrimSpace(request.CorpNo)
	if request.MerchantID == "" || request.RealName == "" || len(request.RealName) > 64 {
		return Certification{}, ErrInvalidRequest
	}
	if !validIDNo(request.IDNo) {
		return Certification{}, ErrInvalidIDNo
	}
	if request.CertType != string(CertTypePerson) && request.CertType != string(CertTypeCorp) {
		return Certification{}, ErrInvalidRequest
	}
	if request.CertType == string(CertTypeCorp) && (request.CorpName == "" || request.CorpNo == "") {
		return Certification{}, ErrInvalidRequest
	}
	if len(request.CorpName) > 128 || len(request.CorpNo) > 64 {
		return Certification{}, ErrInvalidRequest
	}
	realNameEncrypted, err := service.cipher.Encrypt([]byte(request.RealName), []byte(request.MerchantID))
	if err != nil {
		return Certification{}, err
	}
	idNoEncrypted, err := service.cipher.Encrypt([]byte(request.IDNo), []byte(request.MerchantID))
	if err != nil {
		return Certification{}, err
	}
	var result Certification
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing Certification
		lookupErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("merchant_id = ?", request.MerchantID).First(&existing).Error
		now := service.now().UTC()
		switch {
		case lookupErr == nil && existing.Status == StatusApproved:
			return ErrAlreadyApproved
		case lookupErr == nil && existing.Status == StatusPending:
			return ErrPendingReview
		case lookupErr == nil:
			result = existing
			result.CertType = CertType(request.CertType)
			result.RealNameEncrypted = realNameEncrypted
			result.IDNoEncrypted = idNoEncrypted
			result.CorpName = optional(request.CorpName)
			result.CorpNo = optional(request.CorpNo)
			result.Status = StatusPending
			result.FeeCents = 0
			result.RejectionReason = nil
			result.ReviewerID = nil
			result.ReviewedAt = nil
			result.SubmittedAt = now
			result.UpdatedAt = now
			return tx.Model(&Certification{}).Where("id = ?", existing.ID).Updates(map[string]any{
				"cert_type":           request.CertType,
				"real_name_encrypted": realNameEncrypted,
				"id_no_encrypted":     idNoEncrypted,
				"corp_name":           result.CorpName,
				"corp_no":             result.CorpNo,
				"status":              StatusPending,
				"fee_cents":           0,
				"rejection_reason":    nil,
				"reviewer_id":         nil,
				"reviewed_at":         nil,
				"submitted_at":        now,
				"updated_at":          now,
			}).Error
		case errors.Is(lookupErr, gorm.ErrRecordNotFound):
			result = Certification{
				ID: uuid.NewString(), MerchantID: request.MerchantID, CertType: CertType(request.CertType),
				RealNameEncrypted: realNameEncrypted, IDNoEncrypted: idNoEncrypted,
				CorpName: optional(request.CorpName), CorpNo: optional(request.CorpNo),
				Status: StatusPending, SubmittedAt: now, CreatedAt: now, UpdatedAt: now,
			}
			return tx.Create(&result).Error
		default:
			return lookupErr
		}
	})
	if err != nil {
		return Certification{}, err
	}
	if result.Status != StatusPending || result.SubmittedAt.IsZero() {
		result.Status = StatusPending
		result.SubmittedAt = service.now().UTC()
	}
	return result, nil
}

// Current returns the merchant certification or ErrNotFound.
func (service *Service) Current(ctx context.Context, merchantID string) (Certification, error) {
	var found Certification
	if err := service.db.WithContext(ctx).Where("merchant_id = ?", strings.TrimSpace(merchantID)).First(&found).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Certification{}, ErrNotFound
		}
		return Certification{}, err
	}
	return found, nil
}

// Review approves or rejects a pending certification. Approval charges the
// configured fee from the merchant available balance.
func (service *Service) Review(ctx context.Context, request ReviewRequest) (Certification, error) {
	request.MerchantID = strings.TrimSpace(request.MerchantID)
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	request.Reason = strings.TrimSpace(request.Reason)
	request.ReviewerID = strings.TrimSpace(request.ReviewerID)
	if request.MerchantID == "" || request.ReviewerID == "" || len(request.Reason) > 255 {
		return Certification{}, ErrInvalidRequest
	}
	if request.Action != "approve" && request.Action != "reject" {
		return Certification{}, ErrInvalidRequest
	}
	if request.Action == "reject" && request.Reason == "" {
		return Certification{}, ErrInvalidRequest
	}
	var result Certification
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("merchant_id = ?", request.MerchantID).First(&result).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if result.Status == StatusApproved || result.Status == StatusRejected {
			if (request.Action == "approve" && result.Status == StatusApproved) || (request.Action == "reject" && result.Status == StatusRejected) {
				return nil
			}
			return ErrStateConflict
		}
		now := service.now().UTC()
		updates := map[string]any{
			"reviewer_id":      request.ReviewerID,
			"reviewed_at":      now,
			"updated_at":       now,
			"fee_cents":        result.FeeCents,
			"status":           StatusApproved,
			"rejection_reason": nil,
		}
		if request.Action == "reject" {
			updates["status"] = StatusRejected
			updates["rejection_reason"] = request.Reason
			result.Status = StatusRejected
			result.RejectionReason = &request.Reason
			result.ReviewerID = &request.ReviewerID
			result.ReviewedAt = &now
			result.UpdatedAt = now
			return tx.Model(&Certification{}).Where("id = ? AND status = ?", result.ID, StatusPending).Updates(updates).Error
		}
		fee := service.feeInTx(ctx, tx)
		if fee > 0 {
			if err := service.chargeFee(tx, request.MerchantID, result.CurrencyOrCNY(), fee); err != nil {
				return err
			}
			updates["fee_cents"] = fee
			result.FeeCents = fee
		}
		result.Status = StatusApproved
		result.ReviewerID = &request.ReviewerID
		result.ReviewedAt = &now
		result.UpdatedAt = now
		result.RejectionReason = nil
		return tx.Model(&Certification{}).Where("id = ? AND status = ?", result.ID, StatusPending).Updates(updates).Error
	})
	return result, err
}

// List returns certifications for the admin console.
func (service *Service) List(ctx context.Context, filter Filter, page Page) ([]Certification, int64, error) {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 || page.Size > 100 {
		page.Size = 20
	}
	query := service.db.WithContext(ctx).Model(&Certification{})
	if filter.Status != "" {
		if !ValidStatus(filter.Status) {
			return nil, 0, ErrInvalidRequest
		}
		query = query.Where("status = ?", filter.Status)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		query = query.Where("merchant_id = ? OR corp_name LIKE ?", keyword, "%"+keyword+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []Certification
	err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&items).Error
	return items, total, err
}

// RealName decrypts the stored real name for display (already masked by callers).
func (service *Service) RealName(item Certification) (string, error) {
	plain, _, err := service.cipher.Decrypt(item.RealNameEncrypted, []byte(item.MerchantID))
	return string(plain), err
}

// IDNo decrypts the stored id number for masking.
func (service *Service) IDNo(item Certification) (string, error) {
	plain, _, err := service.cipher.Decrypt(item.IDNoEncrypted, []byte(item.MerchantID))
	return string(plain), err
}

func (service *Service) chargeFee(tx *gorm.DB, merchantID, currency string, fee int64) error {
	if service.ledger == nil {
		return ErrInsufficientBalance
	}
	merchant, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: merchantID, Purpose: "balance",
		Category: ledgermodel.CategoryLiability, Currency: currency,
	})
	if err != nil {
		return err
	}
	platform, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
		OwnerType: "platform", OwnerID: "platform", Purpose: "cash",
		Category: ledgermodel.CategoryAsset, Currency: currency,
	})
	if err != nil {
		return err
	}
	_, err = service.ledger.PostInTransaction(tx, ledgerservice.TransactionRequest{
		IdempotencyKey: "cert-fee:" + merchantID,
		ReferenceType:  "certification",
		ReferenceID:    merchantID,
		Description:    "merchant certification fee",
		Postings: []ledgerservice.Posting{
			{AccountID: merchant.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: fee},
			{AccountID: platform.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: fee},
		},
	})
	if err != nil {
		if errors.Is(err, ledgerservice.ErrInsufficientBalance) {
			return ErrInsufficientBalance
		}
		return err
	}
	return nil
}

// CurrencyOrCNY keeps the certification fee in the platform settlement currency.
func (item Certification) CurrencyOrCNY() string { return "CNY" }

// MaskedName keeps the first rune and masks the rest.
func MaskedName(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	if len(runes) == 1 {
		return string(runes[0]) + "*"
	}
	return string(runes[0]) + strings.Repeat("*", len(runes)-1)
}

// MaskedIDNo keeps the first four and last four characters.
func MaskedIDNo(value string) string {
	if len(value) <= 8 {
		return strings.Repeat("*", len(value))
	}
	return value[:4] + strings.Repeat("*", len(value)-8) + value[len(value)-4:]
}

func validIDNo(value string) bool {
	if len(value) != 18 {
		return false
	}
	for index := 0; index < 17; index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	last := value[17]
	return (last >= '0' && last <= '9') || last == 'X'
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
