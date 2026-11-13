// Package funding implements merchant account funding: balance top-ups
// (kind=recharge) and shop deposits (kind=deposit). Funding orders reuse the
// normal payment pipeline, so the cashier and callback path stay unchanged;
// a recharge credits the merchant balance through the existing payment income
// posting, while a deposit is credited to a dedicated ledger account by the
// post-payment hook registered on the callback service.
package funding

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	paymentcallback "gorixpay/internal/payment/callback"
	paymentmodel "gorixpay/internal/payment/model"
	paymentservice "gorixpay/internal/payment/service"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrInvalidRequest   = errors.New("invalid funding request")
	ErrSiteURLMissing   = errors.New("funding requires the public site URL")
	ErrRefundNotAllowed = errors.New("refund is only allowed for sale orders")
)

const (
	depositPurpose      = "deposit"
	platformCashPurpose = "cash"
	fundingNotifyPath   = "/api/v1/funding/notify"
	cashierPath         = "/cashier.php?trade_no="

	// fundingExpiry keeps a top-up link usable far longer than a normal 5 minute
	// checkout: merchants usually pay these from a bank app on another device.
	fundingExpiry = 30 * time.Minute

	defaultPageSize = 20
	maxPageSize     = 100

	// SettingDepositRequired is the platform deposit floor, in cents.
	SettingDepositRequired = "merchant.deposit_required_cents"
)

type OrderResult struct {
	TradeNo    string `json:"trade_no"`
	Amount     int64  `json:"amount"`
	CashierURL string `json:"cashier_url"`
}

type DepositInfo struct {
	DepositBalance int64 `json:"deposit_balance"`
	RequiredCents  int64 `json:"required_cents"`
	Enabled        bool  `json:"enabled"`
}

type ListFilter struct {
	MerchantID string
	Kind       paymentmodel.Kind
}

type Page struct {
	Number int
	Size   int
}

type systemSetting struct {
	Key   string `gorm:"column:setting_key;primaryKey"`
	Value string `gorm:"column:setting_value;type:text;not null"`
}

func (systemSetting) TableName() string {
	return "system_settings"
}

// Service creates funding orders, lists them and keeps the deposit account in
// sync with paid deposit orders.
type Service struct {
	db      *gorm.DB
	orders  *paymentservice.Service
	ledger  *ledgerservice.Service
	siteURL string
	now     func() time.Time
}

// The callback service runs this hook inside the payment transaction.
var _ paymentcallback.PostPaymentHook = (*Service)(nil)

// New wires the funding service. The order service is built here on purpose:
// funding orders must not be blocked by the certification, domain or risk guards
// that the merchant-facing order service carries.
func New(db *gorm.DB, ledger *ledgerservice.Service, siteURL string) *Service {
	now := time.Now
	return &Service{
		db:      db,
		orders:  paymentservice.New(db, now),
		ledger:  ledger,
		siteURL: strings.TrimRight(strings.TrimSpace(siteURL), "/"),
		now:     now,
	}
}

func (service *Service) CreateRecharge(ctx context.Context, merchantID string, amount int64) (OrderResult, error) {
	return service.createFundingOrder(ctx, merchantID, amount, paymentmodel.KindRecharge, "账户余额充值", "RC")
}

func (service *Service) CreateDeposit(ctx context.Context, merchantID string, amount int64) (OrderResult, error) {
	return service.createFundingOrder(ctx, merchantID, amount, paymentmodel.KindDeposit, "店铺保证金", "DP")
}

// createFundingOrder writes the pending order with its funding kind already set,
// so a callback can never observe a deposit/recharge row as a sale. The only
// follow-up write extends the checkout window for these self-service payments.
func (service *Service) createFundingOrder(ctx context.Context, merchantID string, amount int64, kind paymentmodel.Kind, label, prefix string) (OrderResult, error) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" || paymentmodel.ValidateAmount(amount) != nil {
		return OrderResult{}, ErrInvalidRequest
	}
	if service.siteURL == "" {
		return OrderResult{}, ErrSiteURLMissing
	}
	order, _, err := service.orders.Create(ctx, paymentservice.CreateCommand{
		MerchantID:  merchantID,
		OutTradeNo:  prefix + "-" + uuid.NewString(),
		PaymentType: "cashier",
		Name:        label,
		Amount:      amount,
		NotifyURL:   service.siteURL + fundingNotifyPath,
		Kind:        kind,
	})
	if err != nil {
		return OrderResult{}, err
	}
	now := service.now().UTC()
	if err := service.db.WithContext(ctx).Model(&paymentmodel.Order{}).
		Where("id = ?", order.ID).
		Updates(map[string]any{"expires_at": now.Add(fundingExpiry), "updated_at": now}).Error; err != nil {
		return OrderResult{}, err
	}
	return OrderResult{
		TradeNo:    order.TradeNo,
		Amount:     order.Amount,
		CashierURL: service.siteURL + cashierPath + order.TradeNo,
	}, nil
}

func (service *Service) List(ctx context.Context, filter ListFilter, page Page) ([]paymentmodel.Order, int64, error) {
	if !ValidFundingKind(filter.Kind) {
		return nil, 0, ErrInvalidRequest
	}
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 {
		page.Size = defaultPageSize
	}
	if page.Size > maxPageSize {
		page.Size = maxPageSize
	}
	query := service.db.WithContext(ctx).Model(&paymentmodel.Order{}).Where("kind = ?", filter.Kind)
	if merchantID := strings.TrimSpace(filter.MerchantID); merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []paymentmodel.Order
	if err := query.Order("created_at DESC, id DESC").
		Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// DepositGuard blocks merchant sales until the guarantee deposit reaches the
// configured minimum. Funding orders themselves use an unguarded payment
// service, so a merchant can always top the deposit up.
type DepositGuard struct {
	service *Service
}

// Guard returns the paymentservice.Guard for the deposit requirement.
func (service *Service) Guard() paymentservice.Guard {
	return DepositGuard{service: service}
}

func (guard DepositGuard) Check(ctx context.Context, command paymentservice.CreateCommand) error {
	required, err := guard.service.settingInt64(ctx, SettingDepositRequired)
	if err != nil || required <= 0 {
		return nil
	}
	balance, err := guard.service.DepositBalance(ctx, command.MerchantID)
	if err != nil {
		return err
	}
	if balance < required {
		return paymentservice.Rejection{Reason: "保证金不足，请先充值保证金"}
	}
	return nil
}

// DepositBalance sums the merchant's deposit account (purpose=deposit), which
// is a liability credited by the post-payment hook.
func (service *Service) DepositBalance(ctx context.Context, merchantID string) (int64, error) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return 0, ErrInvalidRequest
	}
	balance := struct {
		Available int64
		Frozen    int64
	}{}
	if err := service.db.WithContext(ctx).Model(&ledgermodel.Account{}).
		Select("COALESCE(SUM(ledger_balances.available), 0) AS available, COALESCE(SUM(ledger_balances.frozen), 0) AS frozen").
		Joins("LEFT JOIN ledger_balances ON ledger_balances.account_id = ledger_accounts.id").
		Where("ledger_accounts.owner_type = ? AND ledger_accounts.owner_id = ? AND ledger_accounts.purpose = ?", "merchant", merchantID, depositPurpose).
		Scan(&balance).Error; err != nil {
		return 0, err
	}
	return balance.Available + balance.Frozen, nil
}

func (service *Service) DepositInfo(ctx context.Context, merchantID string) (DepositInfo, error) {
	balance, err := service.DepositBalance(ctx, merchantID)
	if err != nil {
		return DepositInfo{}, err
	}
	required, err := service.settingInt64(ctx, SettingDepositRequired)
	if err != nil {
		return DepositInfo{}, err
	}
	if required < 0 {
		required = 0
	}
	return DepositInfo{DepositBalance: balance, RequiredCents: required, Enabled: required > 0}, nil
}

// OnPayment credits the deposit account inside the payment transaction. Only
// deposit orders are handled here: recharge already goes through the shared
// income posting, and sale orders must never touch the deposit account.
func (service *Service) OnPayment(_ context.Context, tx *gorm.DB, order paymentmodel.Order) error {
	if order.Kind != paymentmodel.KindDeposit || order.Amount <= 0 {
		return nil
	}
	if service.ledger == nil || tx == nil {
		return nil
	}
	depositAccount, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: order.MerchantID, Purpose: depositPurpose,
		Category: ledgermodel.CategoryLiability, Currency: order.Currency,
	})
	if err != nil {
		return err
	}
	platformAccount, err := service.ledger.CreateAccountInTransaction(tx, ledgerservice.AccountRequest{
		OwnerType: "platform", OwnerID: "platform", Purpose: platformCashPurpose,
		Category: ledgermodel.CategoryAsset, Currency: order.Currency,
	})
	if err != nil {
		return err
	}
	_, err = service.ledger.PostInTransaction(tx, ledgerservice.TransactionRequest{
		IdempotencyKey: "deposit:" + order.ID,
		ReferenceType:  "deposit",
		ReferenceID:    order.ID,
		Description:    "merchant deposit",
		Postings: []ledgerservice.Posting{
			{AccountID: depositAccount.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: order.Amount},
			{AccountID: platformAccount.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: order.Amount},
		},
	})
	return err
}

// EnsureRefundable rejects refunds for funding orders: a recharge or deposit
// must never be paid back through the merchant refund flow.
func EnsureRefundable(order paymentmodel.Order) error {
	if !order.IsSale() {
		return ErrRefundNotAllowed
	}
	return nil
}

// ValidFundingKind reports whether kind is a funding order kind.
func ValidFundingKind(kind paymentmodel.Kind) bool {
	return kind == paymentmodel.KindRecharge || kind == paymentmodel.KindDeposit
}

func (service *Service) settingInt64(ctx context.Context, key string) (int64, error) {
	var record systemSetting
	err := service.db.WithContext(ctx).Where("setting_key = ?", key).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return decodeInt64(record.Value), nil
}

func decodeInt64(raw string) int64 {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	var number int64
	if err := json.Unmarshal([]byte(trimmed), &number); err == nil {
		return number
	}
	if parsed, err := strconv.ParseInt(strings.Trim(trimmed, `"`), 10, 64); err == nil {
		return parsed
	}
	return 0
}
