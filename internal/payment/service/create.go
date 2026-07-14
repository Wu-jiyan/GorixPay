package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	paymentmodel "gorixpay/internal/payment/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrInvalidCommand      = errors.New("invalid create order command")
	ErrIdempotencyConflict = errors.New("order idempotency conflict")
)

type CreateCommand struct {
	MerchantID  string
	OutTradeNo  string
	PaymentType string
	Name        string
	Amount      int64
	NotifyURL   string
	ReturnURL   string
	ClientIP    string
	Attach      string
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

func (service *Service) Create(ctx context.Context, command CreateCommand) (paymentmodel.Order, bool, error) {
	if err := validateCommand(command); err != nil {
		return paymentmodel.Order{}, false, err
	}
	var existing paymentmodel.Order
	err := service.db.WithContext(ctx).
		Where("merchant_id = ? AND out_trade_no = ?", command.MerchantID, command.OutTradeNo).
		First(&existing).Error
	if err == nil {
		return existing, false, compareCommand(existing, command)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return paymentmodel.Order{}, false, err
	}
	tradeNo, err := generateTradeNo(service.now().UTC())
	if err != nil {
		return paymentmodel.Order{}, false, err
	}
	now := service.now().UTC()
	created := paymentmodel.Order{
		ID:          uuid.NewString(),
		TradeNo:     tradeNo,
		MerchantID:  command.MerchantID,
		OutTradeNo:  command.OutTradeNo,
		PaymentType: command.PaymentType,
		Name:        command.Name,
		Amount:      command.Amount,
		Currency:    "CNY",
		Status:      paymentmodel.StatusPending,
		NotifyURL:   command.NotifyURL,
		ReturnURL:   optional(command.ReturnURL),
		ClientIP:    optional(command.ClientIP),
		Attach:      optional(command.Attach),
		ExpiresAt:   now.Add(5 * time.Minute),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := service.db.WithContext(ctx).Create(&created).Error; err != nil {
		if lookupErr := service.db.WithContext(ctx).
			Where("merchant_id = ? AND out_trade_no = ?", command.MerchantID, command.OutTradeNo).
			First(&existing).Error; lookupErr == nil {
			return existing, false, compareCommand(existing, command)
		}
		return paymentmodel.Order{}, false, err
	}
	return created, true, nil
}

func validateCommand(command CreateCommand) error {
	if strings.TrimSpace(command.MerchantID) == "" || strings.TrimSpace(command.OutTradeNo) == "" ||
		len(command.OutTradeNo) > 64 || strings.TrimSpace(command.PaymentType) == "" ||
		strings.TrimSpace(command.Name) == "" || len(command.Name) > 255 || paymentmodel.ValidateAmount(command.Amount) != nil ||
		!validHTTPURL(command.NotifyURL) || (command.ReturnURL != "" && !validHTTPURL(command.ReturnURL)) {
		return ErrInvalidCommand
	}
	return nil
}

func validHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func compareCommand(order paymentmodel.Order, command CreateCommand) error {
	if order.PaymentType != command.PaymentType || order.Name != command.Name || order.Amount != command.Amount ||
		order.NotifyURL != command.NotifyURL || stringValue(order.ReturnURL) != command.ReturnURL ||
		stringValue(order.ClientIP) != command.ClientIP || stringValue(order.Attach) != command.Attach {
		return ErrIdempotencyConflict
	}
	return nil
}

func generateTradeNo(now time.Time) (string, error) {
	random, err := rand.Int(rand.Reader, big.NewInt(100000))
	if err != nil {
		return "", err
	}
	return now.Format("20060102150405") + fmt.Sprintf("%05d", random.Int64()), nil
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
