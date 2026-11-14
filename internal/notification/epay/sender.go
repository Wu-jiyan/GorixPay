package epay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gorixpay/internal/compat/epay/signing"
	epayv2 "gorixpay/internal/compat/epay/v2"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/merchant/credential"
	"gorixpay/internal/outbox"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/netguard"

	"gorm.io/gorm"
)

const (
	notificationTopic = "merchant.payment.notify"
	maxResponseBytes  = 64 * 1024
)

type Sender struct {
	db          *gorm.DB
	credentials *credential.Service
	client      *http.Client
	signer      *epayv2.Signer
}

func NewSender(db *gorm.DB, credentials *credential.Service, client *http.Client) *Sender {
	return NewSenderWithSigner(db, credentials, client, nil)
}

// NewSenderWithSigner adds V2 support: merchants with an active RSA credential
// receive RSA signed notifications (sign_type=RSA, platform private key), while
// the remaining merchants keep the MD5 format unchanged.
func NewSenderWithSigner(db *gorm.DB, credentials *credential.Service, client *http.Client, signer *epayv2.Signer) *Sender {
	if client == nil {
		// A nil client used to fall back to http.DefaultClient, which has no
		// timeout: one merchant that accepts the connection and never answers
		// parked the single outbox worker until restart.
		client = netguard.Client(netguard.Options{})
	}
	return &Sender{db: db, credentials: credentials, client: client, signer: signer}
}

func (sender *Sender) Handle(ctx context.Context, message outbox.Message) error {
	if message.Topic != notificationTopic {
		return fmt.Errorf("unsupported outbox topic %q", message.Topic)
	}
	var order paymentmodel.Order
	if err := sender.db.WithContext(ctx).First(&order, "id = ?", message.AggregateID).Error; err != nil {
		return fmt.Errorf("load payment order: %w", err)
	}
	if order.Status != paymentmodel.StatusPaid {
		return errors.New("payment order is not paid")
	}
	var merchant identitymodel.Merchant
	if err := sender.db.WithContext(ctx).First(&merchant, "id = ?", order.MerchantID).Error; err != nil {
		return fmt.Errorf("load merchant: %w", err)
	}
	if merchant.LegacyUID == nil {
		return errors.New("merchant has no legacy uid")
	}
	merchantUID := strconv.FormatInt(*merchant.LegacyUID, 10)
	values, err := sender.notificationValues(ctx, order, merchant, merchantUID)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, notificationEndpoint(order.NotifyURL, values), nil)
	if err != nil {
		return fmt.Errorf("create notification request: %w", err)
	}
	response, err := sender.client.Do(request)
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read notification response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return errors.New("notification response exceeds limit")
	}
	acknowledgement := strings.TrimSpace(string(body))
	// EpayCore::creat_callback accepts exactly 'success' (includes/functions.php).
	// A loose contains() check marked "failed successfully" style error pages as
	// delivered, so the merchant never received the notification and the outbox
	// message was retired anyway.
	if !strings.EqualFold(acknowledgement, "success") {
		return errors.New("notification acknowledgement not accepted")
	}
	return nil
}

// NotificationValues returns the signed merchant parameter set for a paid order.
// The browser return page in internal/compat/epay/page reuses it so /pay/return/
// and the asynchronous notification carry the same signature format, which is
// what EpayCore::verifyReturn expects.
func (sender *Sender) NotificationValues(ctx context.Context, order paymentmodel.Order) (url.Values, error) {
	if order.Status != paymentmodel.StatusPaid {
		return nil, errors.New("payment order is not paid")
	}
	var merchant identitymodel.Merchant
	if err := sender.db.WithContext(ctx).First(&merchant, "id = ?", order.MerchantID).Error; err != nil {
		return nil, fmt.Errorf("load merchant: %w", err)
	}
	if merchant.LegacyUID == nil {
		return nil, errors.New("merchant has no legacy uid")
	}
	return sender.notificationValues(ctx, order, merchant, strconv.FormatInt(*merchant.LegacyUID, 10))
}

// notificationEndpoint appends the signed parameters to the merchant notify URL
// as a GET query string, matching Epay/includes/functions.php:522 which calls
// curl_get(notify_url . '?' . http_build_query(...)). Merchant SDKs verify
// $_GET, so a POST body would be invisible to them.
func notificationEndpoint(notifyURL string, values url.Values) string {
	separator := "?"
	if strings.Contains(notifyURL, "?") {
		separator = "&"
	}
	return notifyURL + separator + values.Encode()
}

// notificationValues picks the notification generation from the order, not from
// the merchant credential: upstream stores order.version at creation and
// creat_callback branches on it (Epay/includes/functions.php:503). A merchant
// that enabled an RSA key keeps receiving MD5 payloads for legacy orders.
func (sender *Sender) notificationValues(ctx context.Context, order paymentmodel.Order, merchant identitymodel.Merchant, merchantUID string) (url.Values, error) {
	if sender.signer != nil && order.Version >= 1 {
		if _, err := sender.credentials.ResolveRSA(ctx, merchant.ID); err == nil {
			return notificationValuesRSA(order, merchantUID, sender.signer)
		}
	}
	resolved, err := sender.credentials.ResolveActive(ctx, merchant.ID, identitymodel.CredentialMD5)
	if err != nil {
		return nil, fmt.Errorf("resolve merchant credential: %w", err)
	}
	return notificationValues(order, merchantUID, resolved.Secret), nil
}

// notificationValues keeps the legacy MD5 notification layout.
func notificationValues(order paymentmodel.Order, merchantUID, key string) url.Values {
	params := map[string]any{
		"pid":          merchantUID,
		"trade_no":     order.TradeNo,
		"out_trade_no": order.OutTradeNo,
		"type":         order.PaymentType,
		"name":         order.Name,
		"money":        fmt.Sprintf("%.2f", float64(order.Amount)/100),
		"trade_status": "TRADE_SUCCESS",
		"sign_type":    "MD5",
	}
	params["sign"] = signing.SignMD5(params, key)
	values := make(url.Values, len(params))
	for name, value := range params {
		values.Set(name, fmt.Sprint(value))
	}
	return values
}

// notificationValuesRSA builds the V2 notification. The signature is computed
// last with the platform private key and no merchant MD5 key is mixed in.
func notificationValuesRSA(order paymentmodel.Order, merchantUID string, signer *epayv2.Signer) (url.Values, error) {
	params := map[string]string{
		"pid":          merchantUID,
		"trade_no":     order.TradeNo,
		"out_trade_no": order.OutTradeNo,
		"api_trade_no": optionalValue(order.ChannelTradeNo),
		"type":         order.PaymentType,
		"name":         order.Name,
		"money":        fmt.Sprintf("%.2f", float64(order.Amount)/100),
		"trade_status": "TRADE_SUCCESS",
		"param":        optionalValue(order.Attach),
		"buyer":        "",
		"timestamp":    strconv.FormatInt(time.Now().Unix(), 10),
		"sign_type":    "RSA",
	}
	signature, err := signer.SignValues(params)
	if err != nil {
		return nil, fmt.Errorf("sign notification: %w", err)
	}
	params["sign"] = signature
	values := make(url.Values, len(params))
	for name, value := range params {
		values.Set(name, value)
	}
	return values, nil
}

func optionalValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
