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

	"gorixpay/internal/compat/epay/signing"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/merchant/credential"
	"gorixpay/internal/outbox"
	paymentmodel "gorixpay/internal/payment/model"

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
}

func NewSender(db *gorm.DB, credentials *credential.Service, client *http.Client) *Sender {
	if client == nil {
		client = http.DefaultClient
	}
	return &Sender{db: db, credentials: credentials, client: client}
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
	resolved, err := sender.credentials.ResolveActive(ctx, order.MerchantID, identitymodel.CredentialMD5)
	if err != nil {
		return fmt.Errorf("resolve merchant credential: %w", err)
	}
	values := notificationValues(order, strconv.FormatInt(*merchant.LegacyUID, 10), resolved.Secret)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, order.NotifyURL, strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("create notification request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
	acknowledgement := string(body)
	if !strings.Contains(acknowledgement, "success") && !strings.Contains(acknowledgement, "SUCCESS") && !strings.Contains(acknowledgement, "Success") {
		return errors.New("notification acknowledgement not accepted")
	}
	return nil
}

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
