package orchestrator

import (
	"context"
	"errors"
	"strings"
	"time"

	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	paymentmodel "gorixpay/internal/payment/model"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrOrderNotFound     = errors.New("payment order not found")
	ErrOrderNotStartable = errors.New("payment order cannot be started")
)

type Result struct {
	Order   paymentmodel.Order
	Channel channelrouter.Channel
	Action  pluginpayment.Action
}

type Service struct {
	db        *gorm.DB
	router    *channelrouter.Router
	now       func() time.Time
	configs   *channelconfig.Service
	publicURL string
}

func New(db *gorm.DB, router *channelrouter.Router, now func() time.Time) *Service {
	return NewConfigured(db, router, now, nil, "")
}

func NewConfigured(db *gorm.DB, router *channelrouter.Router, now func() time.Time, configs *channelconfig.Service, publicURL string) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, router: router, now: now, configs: configs, publicURL: strings.TrimRight(publicURL, "/")}
}

func (service *Service) Start(ctx context.Context, tradeNo string) (Result, error) {
	var order paymentmodel.Order
	if err := service.db.WithContext(ctx).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Result{}, ErrOrderNotFound
		}
		return Result{}, err
	}
	if order.Status != paymentmodel.StatusPending {
		return Result{}, ErrOrderNotStartable
	}
	channel, plugin, err := service.router.Select(ctx, order.PaymentType)
	if err != nil {
		return Result{}, err
	}
	configuration := make(map[string]string)
	if service.configs != nil {
		decoded, err := service.configs.Decrypt(channel.ID, channel.ConfigEncrypted)
		if err != nil {
			return Result{}, err
		}
		configuration = map[string]string{"api_url": decoded.APIURL, "pid": decoded.PID, "key": decoded.Key}
	}
	attempt, err := service.claim(ctx, order.ID, channel.ID)
	if err != nil {
		return Result{}, err
	}
	notifyURL := order.NotifyURL
	if service.publicURL != "" {
		notifyURL = service.publicURL + "/api/v1/payment/callback/" + channel.PluginCode + "/" + channel.ID
	}
	action, pluginErr := plugin.CreatePayment(ctx, pluginpayment.CreateRequest{
		TradeNo:       order.TradeNo,
		Amount:        order.Amount,
		Currency:      order.Currency,
		Description:   order.Name,
		PaymentType:   order.PaymentType,
		NotifyURL:     notifyURL,
		ReturnURL:     stringValue(order.ReturnURL),
		ClientIP:      stringValue(order.ClientIP),
		Configuration: configuration,
	})
	if pluginErr == nil {
		pluginErr = action.Validate()
	}
	if pluginErr != nil {
		message := pluginErr.Error()
		now := service.now().UTC()
		updateErr := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&paymentmodel.Attempt{}).
				Where("id = ? AND status = ?", attempt.ID, paymentmodel.AttemptCreating).
				Updates(map[string]any{"status": paymentmodel.AttemptFailed, "error_message": message, "updated_at": now}).Error; err != nil {
				return err
			}
			return tx.Model(&paymentmodel.Order{}).
				Where("id = ? AND status = ?", order.ID, paymentmodel.StatusProcessing).
				Updates(map[string]any{"status": paymentmodel.StatusPending, "updated_at": now}).Error
		})
		if updateErr != nil {
			return Result{}, updateErr
		}
		return Result{}, pluginErr
	}
	now := service.now().UTC()
	err = service.db.WithContext(ctx).Model(&paymentmodel.Attempt{}).
		Where("id = ? AND status = ?", attempt.ID, paymentmodel.AttemptCreating).
		Updates(map[string]any{"status": paymentmodel.AttemptSucceeded, "updated_at": now}).Error
	if err != nil {
		return Result{}, err
	}
	order.Status = paymentmodel.StatusProcessing
	order.UpdatedAt = now
	return Result{Order: order, Channel: channel, Action: action}, nil
}

func (service *Service) claim(ctx context.Context, orderID, channelID string) (paymentmodel.Attempt, error) {
	var attempt paymentmodel.Attempt
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := service.now().UTC()
		result := tx.Model(&paymentmodel.Order{}).
			Where("id = ? AND status = ?", orderID, paymentmodel.StatusPending).
			Updates(map[string]any{"status": paymentmodel.StatusProcessing, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOrderNotStartable
		}
		var count int64
		if err := tx.Model(&paymentmodel.Attempt{}).Where("order_id = ?", orderID).Count(&count).Error; err != nil {
			return err
		}
		attempt = paymentmodel.Attempt{
			ID: uuid.NewString(), OrderID: orderID, ChannelID: &channelID, AttemptNo: int(count) + 1,
			Status: paymentmodel.AttemptCreating, CreatedAt: now, UpdatedAt: now,
		}
		return tx.Create(&attempt).Error
	})
	return attempt, err
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
