package merchanttools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"
	paymentservice "gorixpay/internal/payment/service"
	pluginpayment "gorixpay/internal/plugin/payment"

	"gorm.io/gorm"
)

var paymentTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

// cashierPath is the SPA cashier route the compat layer also links to.
const cashierPath = "/cashier.php?trade_no="

// Service implements the merchant pay code and the self-service test order.
type Service struct {
	db        *gorm.DB
	orders    *paymentservice.Service
	starter   PaymentStarter
	publicURL string
	now       func() time.Time
}

func New(db *gorm.DB, publicURL string, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, publicURL: strings.TrimRight(strings.TrimSpace(publicURL), "/"), now: now}
}

// WithPayments attaches the order service and the payment starter. Both may be
// nil: the endpoints that need them answer ErrUnavailable instead of panicking.
func (service *Service) WithPayments(orders *paymentservice.Service, starter PaymentStarter) *Service {
	copied := *service
	copied.orders = orders
	copied.starter = starter
	return &copied
}

// CashierURL builds the public cashier link for one order.
func (service *Service) CashierURL(tradeNo string) string {
	return service.publicURL + cashierPath + url.QueryEscape(tradeNo)
}

// PayCode renders the aggregated receiving code of one merchant. An amount of
// zero produces an open code the payer fills in on the page.
func (service *Service) PayCode(ctx context.Context, merchantID, paymentType string, amountCents int64) (PayCode, error) {
	merchantID = strings.TrimSpace(merchantID)
	paymentType = strings.ToLower(strings.TrimSpace(paymentType))
	if merchantID == "" || amountCents < 0 || amountCents > maxPayCodeAmount {
		return PayCode{}, ErrInvalidRequest
	}
	if service.publicURL == "" {
		return PayCode{}, ErrNotConfigured
	}
	if err := service.validatePaymentType(ctx, paymentType); err != nil {
		return PayCode{}, err
	}
	merchant, err := service.merchant(ctx, merchantID)
	if err != nil {
		return PayCode{}, err
	}
	if merchant.LegacyUID == nil {
		return PayCode{}, fmt.Errorf("%w: merchant has no legacy uid for the pay code", ErrInvalidRequest)
	}
	query := url.Values{}
	query.Set("pid", strconv.FormatInt(*merchant.LegacyUID, 10))
	if amountCents > 0 {
		query.Set("amount", strconv.FormatInt(amountCents, 10))
	}
	link := service.publicURL + "/pay/code/" + url.PathEscape(paymentType) + "?" + query.Encode()
	return PayCode{
		CodeSVG:   renderPayCodeSVG(link),
		URL:       link,
		ExpiresAt: service.now().UTC().Add(payCodeTTL),
	}, nil
}

// CreateTestOrder creates a real order capped at one yuan and starts it through
// the orchestrator, so a merchant can verify a channel from their own console.
func (service *Service) CreateTestOrder(ctx context.Context, merchantID, paymentType string, amountCents int64) (TestOrder, error) {
	if service.orders == nil || service.starter == nil {
		return TestOrder{}, ErrUnavailable
	}
	merchantID = strings.TrimSpace(merchantID)
	paymentType = strings.ToLower(strings.TrimSpace(paymentType))
	if merchantID == "" || paymentType == "" || amountCents <= 0 || amountCents > maxTestOrderAmount {
		return TestOrder{}, ErrInvalidRequest
	}
	if _, err := service.merchant(ctx, merchantID); err != nil {
		return TestOrder{}, err
	}
	now := service.now().UTC()
	order, _, err := service.orders.Create(ctx, paymentservice.CreateCommand{
		MerchantID:  merchantID,
		OutTradeNo:  "TEST" + now.Format("20060102150405") + randomSuffix(),
		PaymentType: paymentType,
		Name:        "商户自助测试订单",
		Amount:      amountCents,
		NotifyURL:   "https://example.invalid/merchant-test-order",
		ClientIP:    "127.0.0.1",
	})
	if err != nil {
		return TestOrder{}, err
	}
	result, err := service.starter.Start(ctx, order.TradeNo)
	if err != nil {
		return TestOrder{TradeNo: order.TradeNo}, err
	}
	payType, payInfo := actionPayload(result.Action)
	return TestOrder{
		TradeNo: order.TradeNo, PayType: payType, PayInfo: payInfo,
		CashierURL: service.CashierURL(order.TradeNo),
	}, nil
}

// CreatePayCodeOrder creates the order behind a scanned pay code.
func (service *Service) CreatePayCodeOrder(ctx context.Context, legacyUID, amountCents int64, paymentType string) (paymentmodel.Order, error) {
	if service.orders == nil || service.starter == nil {
		return paymentmodel.Order{}, ErrUnavailable
	}
	paymentType = strings.ToLower(strings.TrimSpace(paymentType))
	if legacyUID <= 0 || amountCents <= 0 || amountCents > maxPayCodeAmount {
		return paymentmodel.Order{}, ErrInvalidRequest
	}
	if err := service.validatePaymentType(ctx, paymentType); err != nil {
		return paymentmodel.Order{}, err
	}
	merchant, err := service.merchantByLegacyUID(ctx, legacyUID)
	if err != nil {
		return paymentmodel.Order{}, err
	}
	notifyURL := "https://example.invalid/pay-code-notify"
	if service.publicURL != "" {
		notifyURL = service.publicURL + "/pay/code/ack"
	}
	now := service.now().UTC()
	order, _, err := service.orders.Create(ctx, paymentservice.CreateCommand{
		MerchantID:  merchant.ID,
		OutTradeNo:  "PC" + now.Format("20060102150405") + randomSuffix(),
		PaymentType: paymentType,
		Name:        "收款码支付",
		Amount:      amountCents,
		NotifyURL:   notifyURL,
		ClientIP:    "127.0.0.1",
	})
	return order, err
}

// StartPayCodeOrder hands a freshly created order to the orchestrator.
func (service *Service) StartPayCodeOrder(ctx context.Context, tradeNo string) (TestOrder, error) {
	if service.starter == nil {
		return TestOrder{}, ErrUnavailable
	}
	result, err := service.starter.Start(ctx, tradeNo)
	if err != nil {
		return TestOrder{}, err
	}
	payType, payInfo := actionPayload(result.Action)
	return TestOrder{TradeNo: tradeNo, PayType: payType, PayInfo: payInfo, CashierURL: service.CashierURL(tradeNo)}, nil
}

// validatePaymentType checks the code format and, when the pay_types catalogue
// exists, that the method is enabled for merchants.
func (service *Service) validatePaymentType(ctx context.Context, paymentType string) error {
	if !paymentTypePattern.MatchString(paymentType) {
		return ErrInvalidRequest
	}
	if !service.db.Migrator().HasTable("pay_types") {
		return nil
	}
	var status string
	if err := service.db.WithContext(ctx).Table("pay_types").
		Select("status").Where("code = ?", paymentType).Scan(&status).Error; err != nil {
		return err
	}
	if status == "" || !strings.EqualFold(status, "active") {
		return ErrPaymentTypeUnavailable
	}
	return nil
}

func (service *Service) merchant(ctx context.Context, merchantID string) (identitymodel.Merchant, error) {
	var merchant identitymodel.Merchant
	if err := service.db.WithContext(ctx).First(&merchant, "id = ?", merchantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return identitymodel.Merchant{}, ErrNotFound
		}
		return identitymodel.Merchant{}, err
	}
	return merchant, nil
}

func (service *Service) merchantByLegacyUID(ctx context.Context, legacyUID int64) (identitymodel.Merchant, error) {
	var merchant identitymodel.Merchant
	err := service.db.WithContext(ctx).
		Where("legacy_uid = ? AND status = ?", legacyUID, identitymodel.StatusActive).
		First(&merchant).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return identitymodel.Merchant{}, ErrNotFound
		}
		return identitymodel.Merchant{}, err
	}
	return merchant, nil
}

func actionPayload(action pluginpayment.Action) (string, any) {
	switch action.Type {
	case pluginpayment.ActionJump, pluginpayment.ActionQRCode, pluginpayment.ActionURLScheme:
		return string(action.Type), action.URL
	default:
		return string(action.Type), action.Data
	}
}

func randomSuffix() string {
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(buffer)
}
