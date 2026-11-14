package device

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	channelrouter "gorixpay/internal/channel/router"
	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentservice "gorixpay/internal/payment/service"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	maxPollBatch   = 50
	maxJobAttempts = 3
	// cleanupMinAge keeps maintenance from deleting recent operational rows.
	cleanupMinAge = 30 * 24 * time.Hour
)

// PaymentStarter is the orchestrator entry the channel test order needs.
type PaymentStarter interface {
	Start(context.Context, string) (paymentorchestrator.Result, error)
}

// Service implements the device gateway, the payment hook and maintenance.
type Service struct {
	db      *gorm.DB
	now     func() time.Time
	orders  *paymentservice.Service
	starter PaymentStarter
}

func New(db *gorm.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}

// WithPayments attaches the order service and orchestrator used by the admin
// channel test order. Both may stay nil; only that one endpoint is disabled.
func (service *Service) WithPayments(orders *paymentservice.Service, starter PaymentStarter) *Service {
	service.orders = orders
	service.starter = starter
	return service
}

// ---------- device side ----------

// VoiceHeartbeat authenticates a device, refreshes last_seen_at and returns the
// pending broadcasts, marking them delivered in the same call.
func (service *Service) VoiceHeartbeat(ctx context.Context, deviceNo, secret string) ([]Broadcast, error) {
	device, err := service.authenticateVoice(ctx, deviceNo, secret)
	if err != nil {
		return nil, err
	}
	var pending []Broadcast
	if err := service.db.WithContext(ctx).
		Where("device_id = ? AND status = ?", device.ID, BroadcastPending).
		Order("created_at ASC").Limit(maxPollBatch).Find(&pending).Error; err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		return pending, nil
	}
	now := service.now().UTC()
	ids := make([]string, 0, len(pending))
	for _, item := range pending {
		ids = append(ids, item.ID)
	}
	if err := service.db.WithContext(ctx).Model(&Broadcast{}).
		Where("id IN ? AND status = ?", ids, BroadcastPending).
		Updates(map[string]any{"status": BroadcastDelivered, "delivered_at": now}).Error; err != nil {
		return nil, err
	}
	for index := range pending {
		pending[index].Status = BroadcastDelivered
		delivered := now
		pending[index].DeliveredAt = &delivered
	}
	return pending, nil
}

// VoiceAck marks broadcasts delivered. Repeating the same ids is a no-op.
func (service *Service) VoiceAck(ctx context.Context, deviceNo, secret string, ids []string) (int64, error) {
	device, err := service.authenticateVoice(ctx, deviceNo, secret)
	if err != nil {
		return 0, err
	}
	ids = cleanIDs(ids)
	if len(ids) == 0 {
		return 0, ErrInvalidRequest
	}
	now := service.now().UTC()
	result := service.db.WithContext(ctx).Model(&Broadcast{}).
		Where("id IN ? AND device_id = ? AND status = ?", ids, device.ID, BroadcastPending).
		Updates(map[string]any{"status": BroadcastDelivered, "delivered_at": now})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// PrinterPoll returns queued jobs. A job that was handed out but not acked is
// handed out again until attempts reaches maxJobAttempts.
func (service *Service) PrinterPoll(ctx context.Context, printerNo, secret string) ([]PrintJob, error) {
	printer, err := service.authenticatePrinter(ctx, printerNo, secret)
	if err != nil {
		return nil, err
	}
	var jobs []PrintJob
	if err := service.db.WithContext(ctx).
		Where("printer_id = ? AND (status = ? OR (status = ? AND attempts < ?))", printer.ID, JobPending, JobSent, maxJobAttempts).
		Order("created_at ASC").Limit(maxPollBatch).Find(&jobs).Error; err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return jobs, nil
	}
	ids := make([]string, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.ID)
	}
	if err := service.db.WithContext(ctx).Model(&PrintJob{}).
		Where("id IN ?", ids).
		Updates(map[string]any{"status": JobSent, "attempts": gorm.Expr("attempts + 1")}).Error; err != nil {
		return nil, err
	}
	for index := range jobs {
		jobs[index].Status = JobSent
		jobs[index].Attempts++
	}
	return jobs, nil
}

// PrinterAck marks jobs printed. Repeating the same ids is a no-op.
func (service *Service) PrinterAck(ctx context.Context, printerNo, secret string, ids []string) (int64, error) {
	printer, err := service.authenticatePrinter(ctx, printerNo, secret)
	if err != nil {
		return 0, err
	}
	ids = cleanIDs(ids)
	if len(ids) == 0 {
		return 0, ErrInvalidRequest
	}
	now := service.now().UTC()
	result := service.db.WithContext(ctx).Model(&PrintJob{}).
		Where("id IN ? AND printer_id = ? AND status <> ?", ids, printer.ID, JobPrinted).
		Updates(map[string]any{"status": JobPrinted, "printed_at": now})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (service *Service) authenticateVoice(ctx context.Context, deviceNo, secret string) (VoiceDevice, error) {
	deviceNo = strings.TrimSpace(deviceNo)
	if deviceNo == "" || secret == "" {
		return VoiceDevice{}, ErrUnauthorized
	}
	var device VoiceDevice
	if err := service.db.WithContext(ctx).Where("device_no = ?", deviceNo).First(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return VoiceDevice{}, ErrUnauthorized
		}
		return VoiceDevice{}, err
	}
	if !secureEqual(device.Secret, secret) || device.Status != StatusActive {
		return VoiceDevice{}, ErrUnauthorized
	}
	now := service.now().UTC()
	if err := service.db.WithContext(ctx).Model(&VoiceDevice{}).Where("id = ?", device.ID).
		Updates(map[string]any{"last_seen_at": now, "updated_at": now}).Error; err != nil {
		return VoiceDevice{}, err
	}
	device.LastSeenAt = &now
	return device, nil
}

func (service *Service) authenticatePrinter(ctx context.Context, printerNo, secret string) (Printer, error) {
	printerNo = strings.TrimSpace(printerNo)
	if printerNo == "" || secret == "" {
		return Printer{}, ErrUnauthorized
	}
	var printer Printer
	if err := service.db.WithContext(ctx).Where("id = ?", printerNo).First(&printer).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Printer{}, ErrUnauthorized
		}
		return Printer{}, err
	}
	if !secureEqual(printer.Secret, secret) || printer.Status != StatusActive {
		return Printer{}, ErrUnauthorized
	}
	return printer, nil
}

// ---------- admin: devices ----------

func (service *Service) ListVoiceDevices(ctx context.Context, merchantID string) ([]VoiceDevice, error) {
	query := service.db.WithContext(ctx).Model(&VoiceDevice{})
	if merchantID = strings.TrimSpace(merchantID); merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	var devices []VoiceDevice
	err := query.Order("created_at DESC").Find(&devices).Error
	return devices, err
}

type VoiceDeviceInput struct {
	MerchantID *string
	DeviceNo   string
	Name       string
	Status     string
}

// CreateVoiceDevice registers a speaker and returns the plaintext secret once.
func (service *Service) CreateVoiceDevice(ctx context.Context, input VoiceDeviceInput) (VoiceDevice, string, error) {
	name := strings.TrimSpace(input.Name)
	deviceNo := strings.TrimSpace(input.DeviceNo)
	if name == "" || len(name) > 64 || len(deviceNo) > 64 {
		return VoiceDevice{}, "", ErrInvalidRequest
	}
	merchantID, err := service.resolveMerchant(ctx, input.MerchantID)
	if err != nil {
		return VoiceDevice{}, "", err
	}
	if deviceNo == "" {
		deviceNo = "VD" + randomHex(10)
	}
	var existing VoiceDevice
	if err := service.db.WithContext(ctx).Where("device_no = ?", deviceNo).First(&existing).Error; err == nil {
		return VoiceDevice{}, "", ErrDuplicate
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return VoiceDevice{}, "", err
	}
	secret := randomHex(24)
	now := service.now().UTC()
	device := VoiceDevice{
		ID: uuid.NewString(), MerchantID: merchantID, DeviceNo: deviceNo, Name: name,
		Secret: secret, Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := service.db.WithContext(ctx).Create(&device).Error; err != nil {
		return VoiceDevice{}, "", err
	}
	return device, secret, nil
}

func (service *Service) UpdateVoiceDevice(ctx context.Context, id string, input VoiceDeviceInput) (VoiceDevice, error) {
	var device VoiceDevice
	if err := service.db.WithContext(ctx).First(&device, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return VoiceDevice{}, ErrNotFound
		}
		return VoiceDevice{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 64 {
		return VoiceDevice{}, ErrInvalidRequest
	}
	status := Status(strings.ToLower(strings.TrimSpace(input.Status)))
	if status == "" {
		status = device.Status
	}
	if !ValidStatus(status) {
		return VoiceDevice{}, ErrInvalidRequest
	}
	merchantID, err := service.resolveMerchant(ctx, input.MerchantID)
	if err != nil {
		return VoiceDevice{}, err
	}
	now := service.now().UTC()
	if err := service.db.WithContext(ctx).Model(&VoiceDevice{}).Where("id = ?", device.ID).
		Updates(map[string]any{"name": name, "merchant_id": merchantID, "status": status, "updated_at": now}).Error; err != nil {
		return VoiceDevice{}, err
	}
	return service.voiceDevice(ctx, device.ID)
}

// ResetVoiceSecret issues a new secret and returns it once.
func (service *Service) ResetVoiceSecret(ctx context.Context, id string) (string, error) {
	secret := randomHex(24)
	now := service.now().UTC()
	result := service.db.WithContext(ctx).Model(&VoiceDevice{}).Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{"secret": secret, "updated_at": now})
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", ErrNotFound
	}
	return secret, nil
}

func (service *Service) DeleteVoiceDevice(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	result := service.db.WithContext(ctx).Where("id = ?", id).Delete(&VoiceDevice{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return service.db.WithContext(ctx).Where("device_id = ?", id).Delete(&Broadcast{}).Error
}

func (service *Service) VoiceTest(ctx context.Context, id, text string) (Broadcast, error) {
	device, err := service.voiceDevice(ctx, id)
	if err != nil {
		return Broadcast{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = "测试播报"
	}
	if len([]rune(text)) > 255 {
		return Broadcast{}, ErrInvalidRequest
	}
	broadcast := Broadcast{
		ID: uuid.NewString(), DeviceID: device.ID, Text: text,
		Status: BroadcastPending, CreatedAt: service.now().UTC(),
	}
	if err := service.db.WithContext(ctx).Create(&broadcast).Error; err != nil {
		return Broadcast{}, err
	}
	return broadcast, nil
}

func (service *Service) voiceDevice(ctx context.Context, id string) (VoiceDevice, error) {
	var device VoiceDevice
	if err := service.db.WithContext(ctx).First(&device, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return VoiceDevice{}, ErrNotFound
		}
		return VoiceDevice{}, err
	}
	return device, nil
}

// ---------- admin: printers ----------

func (service *Service) ListPrinters(ctx context.Context, merchantID string) ([]Printer, error) {
	query := service.db.WithContext(ctx).Model(&Printer{})
	if merchantID = strings.TrimSpace(merchantID); merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	var printers []Printer
	err := query.Order("created_at DESC").Find(&printers).Error
	return printers, err
}

type PrinterInput struct {
	MerchantID string
	Name       string
	Status     string
}

func (service *Service) CreatePrinter(ctx context.Context, input PrinterInput) (Printer, string, error) {
	name := strings.TrimSpace(input.Name)
	merchantID := strings.TrimSpace(input.MerchantID)
	if name == "" || len(name) > 64 || merchantID == "" {
		return Printer{}, "", ErrInvalidRequest
	}
	if _, err := service.merchant(ctx, merchantID); err != nil {
		return Printer{}, "", err
	}
	secret := randomHex(24)
	now := service.now().UTC()
	printer := Printer{ID: uuid.NewString(), MerchantID: merchantID, Name: name, Secret: secret, Status: StatusActive, CreatedAt: now, UpdatedAt: now}
	if err := service.db.WithContext(ctx).Create(&printer).Error; err != nil {
		return Printer{}, "", err
	}
	return printer, secret, nil
}

func (service *Service) UpdatePrinter(ctx context.Context, id string, input PrinterInput) (Printer, error) {
	var printer Printer
	if err := service.db.WithContext(ctx).First(&printer, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Printer{}, ErrNotFound
		}
		return Printer{}, err
	}
	name := strings.TrimSpace(input.Name)
	status := Status(strings.ToLower(strings.TrimSpace(input.Status)))
	if status == "" {
		status = printer.Status
	}
	if name == "" || len(name) > 64 || !ValidStatus(status) {
		return Printer{}, ErrInvalidRequest
	}
	merchantID := strings.TrimSpace(input.MerchantID)
	if merchantID == "" {
		merchantID = printer.MerchantID
	}
	if _, err := service.merchant(ctx, merchantID); err != nil {
		return Printer{}, err
	}
	now := service.now().UTC()
	if err := service.db.WithContext(ctx).Model(&Printer{}).Where("id = ?", printer.ID).
		Updates(map[string]any{"name": name, "merchant_id": merchantID, "status": status, "updated_at": now}).Error; err != nil {
		return Printer{}, err
	}
	return service.printer(ctx, printer.ID)
}

func (service *Service) ResetPrinterSecret(ctx context.Context, id string) (string, error) {
	secret := randomHex(24)
	now := service.now().UTC()
	result := service.db.WithContext(ctx).Model(&Printer{}).Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{"secret": secret, "updated_at": now})
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", ErrNotFound
	}
	return secret, nil
}

func (service *Service) DeletePrinter(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	result := service.db.WithContext(ctx).Where("id = ?", id).Delete(&Printer{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return service.db.WithContext(ctx).Where("printer_id = ?", id).Delete(&PrintJob{}).Error
}

func (service *Service) PrinterTest(ctx context.Context, id string) (PrintJob, error) {
	printer, err := service.printer(ctx, id)
	if err != nil {
		return PrintJob{}, err
	}
	job := PrintJob{
		ID: uuid.NewString(), PrinterID: printer.ID,
		Content: fmt.Sprintf("GorixPay 测试小票\n打印机：%s\n时间：%s\n", printer.Name, service.now().UTC().Format("2006-01-02 15:04:05")),
		Status:  JobPending, CreatedAt: service.now().UTC(),
	}
	if err := service.db.WithContext(ctx).Create(&job).Error; err != nil {
		return PrintJob{}, err
	}
	return job, nil
}

func (service *Service) printer(ctx context.Context, id string) (Printer, error) {
	var printer Printer
	if err := service.db.WithContext(ctx).First(&printer, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Printer{}, ErrNotFound
		}
		return Printer{}, err
	}
	return printer, nil
}

// ---------- payment hook ----------

// PaymentHook implements the callback package's PostPaymentHook structurally
// (OnPayment) without importing it.
type PaymentHook struct {
	service *Service
}

func (service *Service) PaymentHook() *PaymentHook {
	return &PaymentHook{service: service}
}

// OnPayment queues a voice broadcast and a receipt print for a paid sale order.
// It runs inside the payment transaction and is idempotent per order and device.
func (hook *PaymentHook) OnPayment(ctx context.Context, tx *gorm.DB, order paymentmodel.Order) error {
	if hook == nil || hook.service == nil || tx == nil {
		return nil
	}
	if !order.IsSale() || order.Amount <= 0 {
		return nil
	}
	now := hook.service.now().UTC()
	text := fmt.Sprintf("%s到账 %s 元", paymentLabel(order.PaymentType), yuan(order.Amount))
	var devices []VoiceDevice
	if err := tx.WithContext(ctx).
		Where("status = ? AND (merchant_id = ? OR merchant_id IS NULL)", StatusActive, order.MerchantID).
		Find(&devices).Error; err != nil {
		return err
	}
	for _, item := range devices {
		var existing int64
		if err := tx.WithContext(ctx).Model(&Broadcast{}).
			Where("device_id = ? AND order_id = ?", item.ID, order.ID).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			continue
		}
		broadcast := Broadcast{
			ID: uuid.NewString(), DeviceID: item.ID, OrderID: &order.ID,
			Text: text, AmountCents: order.Amount, Status: BroadcastPending, CreatedAt: now,
		}
		if err := tx.WithContext(ctx).Create(&broadcast).Error; err != nil {
			return err
		}
	}
	var printers []Printer
	if err := tx.WithContext(ctx).
		Where("merchant_id = ? AND status = ?", order.MerchantID, StatusActive).
		Find(&printers).Error; err != nil {
		return err
	}
	for _, item := range printers {
		var existing int64
		if err := tx.WithContext(ctx).Model(&PrintJob{}).
			Where("printer_id = ? AND order_id = ?", item.ID, order.ID).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			continue
		}
		job := PrintJob{
			ID: uuid.NewString(), PrinterID: item.ID, OrderID: &order.ID,
			Content: receipt(order), Status: JobPending, CreatedAt: now,
		}
		if err := tx.WithContext(ctx).Create(&job).Error; err != nil {
			return err
		}
	}
	return nil
}

func paymentLabel(paymentType string) string {
	switch strings.ToLower(strings.TrimSpace(paymentType)) {
	case "wxpay", "wechat", "wechatpay", "wxplugin", "wxapp":
		return "微信"
	case "alipay":
		return "支付宝"
	case "qqpay":
		return "QQ钱包"
	case "bank", "unionpay":
		return "银行卡"
	default:
		return "收款"
	}
}

func yuan(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

func receipt(order paymentmodel.Order) string {
	return fmt.Sprintf("GorixPay 收款成功\n平台订单号：%s\n商户订单号：%s\n支付方式：%s\n金额：%s 元\n时间：%s\n",
		order.TradeNo, order.OutTradeNo, order.PaymentType, yuan(order.Amount), order.CreatedAt.Format("2006-01-02 15:04:05"))
}

// ---------- maintenance ----------

var cleanupTargets = map[string]bool{
	"audit_events":      true,
	"outbox_messages":   true,
	"payment_attempts":  true,
	"payment_callbacks": true,
}

type CleanupItem struct {
	Target      string `json:"target"`
	Deleted     int64  `json:"deleted"`
	WouldDelete int64  `json:"would_delete"`
}

// Cleanup deletes old operational rows. The cutoff must be at least 30 days old,
// dry_run is the default and a real delete needs confirm == "DELETE" at the HTTP
// layer.
func (service *Service) Cleanup(ctx context.Context, targets []string, before time.Time, dryRun bool) ([]CleanupItem, error) {
	if len(targets) == 0 || before.IsZero() {
		return nil, ErrInvalidRequest
	}
	cutoff := service.now().UTC().Add(-cleanupMinAge)
	if before.After(cutoff) {
		return nil, ErrInvalidRequest
	}
	seen := make(map[string]bool, len(targets))
	items := make([]CleanupItem, 0, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if !cleanupTargets[target] || seen[target] {
			if !cleanupTargets[target] {
				return nil, ErrInvalidRequest
			}
			continue
		}
		seen[target] = true
		var count int64
		if err := service.db.WithContext(ctx).Table(target).Where("created_at < ?", before.UTC()).Count(&count).Error; err != nil {
			return nil, err
		}
		item := CleanupItem{Target: target, WouldDelete: count}
		if !dryRun {
			if err := service.db.WithContext(ctx).Exec("DELETE FROM "+target+" WHERE created_at < ?", before.UTC()).Error; err != nil {
				return nil, err
			}
			item.Deleted = count
		}
		items = append(items, item)
	}
	return items, nil
}

// ---------- admin: channel test order ----------

type TestOrderResult struct {
	TradeNo    string `json:"trade_no"`
	OutTradeNo string `json:"out_trade_no"`
	PayType    string `json:"pay_type"`
	PayInfo    any    `json:"pay_info"`
}

// CreateTestOrder creates a real pending order for the merchant and starts the
// payment through the orchestrator. The channel id is checked for existence and
// status; the orchestrator still selects the channel by payment type.
func (service *Service) CreateTestOrder(ctx context.Context, channelID, merchantID, paymentType string, amount int64) (TestOrderResult, error) {
	if service.orders == nil || service.starter == nil {
		return TestOrderResult{}, ErrUnavailable
	}
	channelID = strings.TrimSpace(channelID)
	merchantID = strings.TrimSpace(merchantID)
	paymentType = strings.ToLower(strings.TrimSpace(paymentType))
	if amount <= 0 || amount > 10_000_000 {
		return TestOrderResult{}, ErrInvalidRequest
	}
	var channel channelrouter.Channel
	if err := service.db.WithContext(ctx).First(&channel, "id = ?", channelID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return TestOrderResult{}, ErrNotFound
		}
		return TestOrderResult{}, err
	}
	if paymentType == "" {
		paymentType = channel.PaymentType
	}
	if _, err := service.merchant(ctx, merchantID); err != nil {
		return TestOrderResult{}, err
	}
	now := service.now().UTC()
	order, _, err := service.orders.Create(ctx, paymentservice.CreateCommand{
		MerchantID:  merchantID,
		OutTradeNo:  "TEST" + now.Format("20060102150405") + randomHex(4),
		PaymentType: paymentType,
		Name:        "渠道测试订单",
		Amount:      amount,
		NotifyURL:   "https://example.invalid/test-order",
		ClientIP:    "127.0.0.1",
	})
	if err != nil {
		return TestOrderResult{}, err
	}
	result, err := service.starter.Start(ctx, order.TradeNo)
	if err != nil {
		return TestOrderResult{TradeNo: order.TradeNo, OutTradeNo: order.OutTradeNo}, err
	}
	payType, payInfo := actionPayload(result.Action)
	return TestOrderResult{TradeNo: order.TradeNo, OutTradeNo: order.OutTradeNo, PayType: payType, PayInfo: payInfo}, nil
}

func actionPayload(action pluginpayment.Action) (string, any) {
	switch action.Type {
	case pluginpayment.ActionJump, pluginpayment.ActionQRCode, pluginpayment.ActionURLScheme:
		return string(action.Type), action.URL
	default:
		return string(action.Type), action.Data
	}
}

// ---------- helpers ----------

func (service *Service) merchant(ctx context.Context, id string) (identitymodel.Merchant, error) {
	var merchant identitymodel.Merchant
	if err := service.db.WithContext(ctx).First(&merchant, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return identitymodel.Merchant{}, ErrInvalidRequest
		}
		return identitymodel.Merchant{}, err
	}
	return merchant, nil
}

func (service *Service) resolveMerchant(ctx context.Context, merchantID *string) (*string, error) {
	if merchantID == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*merchantID)
	if trimmed == "" {
		return nil, nil
	}
	if _, err := service.merchant(ctx, trimmed); err != nil {
		return nil, err
	}
	return &trimmed, nil
}

func randomHex(bytes int) string {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return hex.EncodeToString([]byte(uuid.NewString()))[:bytes*2]
	}
	return hex.EncodeToString(buffer)
}

func secureEqual(left, right string) bool {
	if len(left) != len(right) || left == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func cleanIDs(ids []string) []string {
	cleaned := make([]string, 0, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}
