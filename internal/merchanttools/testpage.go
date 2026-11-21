package merchanttools

import (
	"context"
	"errors"
	"strings"
	"time"

	channelrouter "gorixpay/internal/channel/router"
	identitymodel "gorixpay/internal/identity/model"
	paymentservice "gorixpay/internal/payment/service"
	pluginpayment "gorixpay/internal/plugin/payment"
	"gorixpay/internal/settings"

	"gorm.io/gorm"
)

// Setting keys of the public test page. They live in system_settings so the
// operator can change them from 系统设置 → 站点, exactly like the site identity.
const (
	SettingTestEnabled     = "site.test_payment_enabled"
	SettingTestPID         = "site.test_payment_pid"
	SettingTestMaxCents    = "site.test_payment_max_cents"
	SettingTestHourlyLimit = "site.test_payment_hourly_limit"
	// testOrderName marks the orders this page creates. It doubles as the label
	// the payer sees in the cashier and as the counter key of the per-IP quota.
	testOrderName = "支付测试"
)

// Defaults when the settings row is absent. The page is on out of the box (that
// is what upstream Epay does), but the amount and the per-IP quota are capped so
// an open page cannot be used to flood the demo merchant with orders.
const (
	defaultTestMaxCents    int64 = 100
	defaultTestHourlyLimit int64 = 10
	minTestCents           int64 = 1
)

// ErrTestPageDisabled means the operator switched the public test page off.
var ErrTestPageDisabled = errors.New("test payment page is disabled")

// ErrTestRateLimited means this client already created its hourly quota of test
// orders.
var ErrTestRateLimited = errors.New("test payment hourly quota exceeded")

// ChannelSelector is the channel router entry the page uses to decide which
// payment methods can actually be paid right now. `*channelrouter.Router`
// satisfies it; a nil selector only skips that filter.
type ChannelSelector interface {
	Select(ctx context.Context, paymentType string) (channelrouter.Channel, pluginpayment.Plugin, error)
}

// TestMethod is one selectable payment method on the page.
type TestMethod struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// TestPage is everything the page needs to render its form.
type TestPage struct {
	Enabled     bool         `json:"enabled"`
	Reason      string       `json:"reason"`
	Methods     []TestMethod `json:"methods"`
	MinCents    int64        `json:"min_cents"`
	MaxCents    int64        `json:"max_cents"`
	HourlyLimit int64        `json:"hourly_limit"`
	/** Display name of the demo merchant the test orders belong to. */
	Merchant string `json:"merchant"`
}

// TestPageOrder is the result of a public test order.
type TestPageOrder struct {
	TradeNo     string    `json:"trade_no"`
	PaymentType string    `json:"payment_type"`
	Amount      int64     `json:"amount"`
	PayType     string    `json:"pay_type"`
	PayInfo     any       `json:"pay_info"`
	CashierURL  string    `json:"cashier_url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// publicTestSettings is the resolved configuration of the page.
type publicTestSettings struct {
	enabled     bool
	merchantPID int64
	maxCents    int64
	hourlyLimit int64
}

// WithChannels attaches the channel selector used to filter payment methods by
// "has an active channel right now".
func (service *Service) WithChannels(selector ChannelSelector) *Service {
	copied := *service
	copied.channels = selector
	return &copied
}

// TestPage resolves the page configuration and the methods worth offering.
func (service *Service) TestPage(ctx context.Context) (TestPage, error) {
	config, err := service.testSettings(ctx)
	if err != nil {
		return TestPage{}, err
	}
	page := TestPage{
		Enabled:     config.enabled,
		MinCents:    minTestCents,
		MaxCents:    config.maxCents,
		HourlyLimit: config.hourlyLimit,
	}
	if !config.enabled {
		page.Reason = "站点未开放支付测试"
		return page, nil
	}
	merchant, err := service.testMerchant(ctx, config.merchantPID)
	if err != nil {
		// No usable merchant means the page cannot create anything; say so
		// instead of letting the form fail on submit.
		page.Enabled = false
		if errors.Is(err, ErrNotFound) {
			page.Reason = "未找到可用的演示商户"
		} else {
			return TestPage{}, err
		}
		return page, nil
	}
	page.Merchant = firstNonEmpty(merchant.DisplayName, deref(merchant.Email))
	page.Methods = service.availableMethods(ctx)
	if len(page.Methods) == 0 {
		page.Reason = "暂无可用支付通道"
	}
	return page, nil
}

// CreatePublicTestOrder creates and starts one test order for the demo merchant.
// Nothing about it is special-cased downstream: it is a normal order whose name
// says 支付测试, so it lands in the merchant's order list and, if the payer
// really pays, in their balance.
func (service *Service) CreatePublicTestOrder(ctx context.Context, paymentType string, amountCents int64, clientIP string) (TestPageOrder, error) {
	if service.orders == nil || service.starter == nil {
		return TestPageOrder{}, ErrUnavailable
	}
	config, err := service.testSettings(ctx)
	if err != nil {
		return TestPageOrder{}, err
	}
	if !config.enabled {
		return TestPageOrder{}, ErrTestPageDisabled
	}
	paymentType = strings.ToLower(strings.TrimSpace(paymentType))
	if !paymentTypePattern.MatchString(paymentType) {
		return TestPageOrder{}, ErrInvalidRequest
	}
	if amountCents < minTestCents || amountCents > config.maxCents {
		return TestPageOrder{}, ErrInvalidRequest
	}
	if err := service.validatePaymentType(ctx, paymentType); err != nil {
		return TestPageOrder{}, err
	}
	if !service.methodAvailable(ctx, paymentType) {
		return TestPageOrder{}, ErrPaymentTypeUnavailable
	}
	clientIP = strings.TrimSpace(clientIP)
	if service.quotaExceeded(ctx, clientIP, config.hourlyLimit) {
		return TestPageOrder{}, ErrTestRateLimited
	}
	merchant, err := service.testMerchant(ctx, config.merchantPID)
	if err != nil {
		return TestPageOrder{}, err
	}
	notifyURL := "https://example.invalid/test-payment"
	if service.publicURL != "" {
		notifyURL = service.publicURL + "/pay/test/ack"
	}
	now := service.now().UTC()
	order, _, err := service.orders.Create(ctx, paymentservice.CreateCommand{
		MerchantID:  merchant.ID,
		OutTradeNo:  "PT" + now.Format("20060102150405") + randomSuffix(),
		PaymentType: paymentType,
		Name:        testOrderName,
		Amount:      amountCents,
		NotifyURL:   notifyURL,
		ClientIP:    clientIP,
	})
	if err != nil {
		return TestPageOrder{}, err
	}
	result, err := service.starter.Start(ctx, order.TradeNo)
	if err != nil {
		return TestPageOrder{}, err
	}
	payType, payInfo := actionPayload(result.Action)
	return TestPageOrder{
		TradeNo:     order.TradeNo,
		PaymentType: order.PaymentType,
		Amount:      order.Amount,
		PayType:     payType,
		PayInfo:     payInfo,
		CashierURL:  service.CashierURL(order.TradeNo),
		ExpiresAt:   order.ExpiresAt,
	}, nil
}

// testSettings reads the four keys, falling back to the compiled defaults.
func (service *Service) testSettings(ctx context.Context) (publicTestSettings, error) {
	config := publicTestSettings{
		enabled:     true,
		maxCents:    defaultTestMaxCents,
		hourlyLimit: defaultTestHourlyLimit,
	}
	values, err := service.settingValues(ctx, []string{
		SettingTestEnabled, SettingTestPID, SettingTestMaxCents, SettingTestHourlyLimit,
	})
	if err != nil {
		return config, err
	}
	if raw, ok := values[SettingTestEnabled]; ok {
		config.enabled = truthy(raw)
	}
	if raw, ok := values[SettingTestPID]; ok {
		if pid, parseErr := parseInt64(settings.Text(raw)); parseErr == nil && pid > 0 {
			config.merchantPID = pid
		}
	}
	if raw, ok := values[SettingTestMaxCents]; ok {
		if cents, parseErr := parseInt64(settings.Text(raw)); parseErr == nil && cents >= minTestCents {
			config.maxCents = cents
		}
	}
	if raw, ok := values[SettingTestHourlyLimit]; ok {
		if limit, parseErr := parseInt64(settings.Text(raw)); parseErr == nil && limit > 0 {
			config.hourlyLimit = limit
		}
	}
	return config, nil
}

// settingValues reads the raw stored values of the requested keys.
func (service *Service) settingValues(ctx context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	if !service.db.Migrator().HasTable("system_settings") {
		return values, nil
	}
	var rows []struct {
		SettingKey   string
		SettingValue string
	}
	if err := service.db.WithContext(ctx).Table("system_settings").
		Select("setting_key, setting_value").
		Where("setting_key IN ?", keys).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		values[row.SettingKey] = row.SettingValue
	}
	return values, nil
}

// testMerchant resolves the demo merchant: the configured legacy uid, or the
// oldest active merchant with one. Epay behaves the same way — its test page
// pays into the site's demo account.
func (service *Service) testMerchant(ctx context.Context, legacyPID int64) (identitymodel.Merchant, error) {
	if legacyPID > 0 {
		return service.merchantByLegacyUID(ctx, legacyPID)
	}
	var merchant identitymodel.Merchant
	err := service.db.WithContext(ctx).
		Where("status = ? AND legacy_uid IS NOT NULL", identitymodel.StatusActive).
		Order("legacy_uid ASC").
		First(&merchant).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return identitymodel.Merchant{}, ErrNotFound
		}
		return identitymodel.Merchant{}, err
	}
	return merchant, nil
}

// availableMethods lists the enabled catalogue entries that a payer can really
// use: the pay_types row is active and the router can pick a channel for it.
func (service *Service) availableMethods(ctx context.Context) []TestMethod {
	codes := service.catalogCodes(ctx)
	methods := make([]TestMethod, 0, len(codes))
	for _, entry := range codes {
		if !service.methodAvailable(ctx, entry.code) {
			continue
		}
		methods = append(methods, TestMethod{Code: entry.code, Name: entry.name})
	}
	return methods
}

// catalogEntry is one enabled pay_types row.
type catalogEntry struct {
	code string
	name string
}

// catalogCodes reads the enabled catalogue in display order. Without a
// catalogue it falls back to the payment types that have an active channel.
func (service *Service) catalogCodes(ctx context.Context) []catalogEntry {
	entries := []catalogEntry{}
	if service.db.Migrator().HasTable("pay_types") {
		var rows []struct {
			Code string
			Name string
		}
		if err := service.db.WithContext(ctx).Table("pay_types").
			Select("code, name").
			Where("status = ?", "active").
			Order("sort ASC, code ASC").
			Limit(30).
			Scan(&rows).Error; err == nil {
			for _, row := range rows {
				code := strings.ToLower(strings.TrimSpace(row.Code))
				if code == "" {
					continue
				}
				entries = append(entries, catalogEntry{code: code, name: firstNonEmpty(strings.TrimSpace(row.Name), code)})
			}
		}
	}
	if len(entries) > 0 {
		return entries
	}
	var codes []string
	if err := service.db.WithContext(ctx).Table("payment_channels").
		Distinct("payment_type").
		Where("status = ? AND weight > 0", channelrouter.StatusActive).
		Order("payment_type ASC").
		Pluck("payment_type", &codes).Error; err != nil {
		return entries
	}
	for _, code := range codes {
		trimmed := strings.ToLower(strings.TrimSpace(code))
		if trimmed != "" {
			entries = append(entries, catalogEntry{code: trimmed, name: trimmed})
		}
	}
	return entries
}

// methodAvailable asks the router whether one payment type has a usable channel.
// A nil selector keeps every catalogue entry, which is what the console-side
// test order does.
func (service *Service) methodAvailable(ctx context.Context, paymentType string) bool {
	if service.channels == nil {
		return true
	}
	_, _, err := service.channels.Select(ctx, paymentType)
	return err == nil
}

// quotaExceeded counts the test orders this client created in the last hour.
// Counting rows instead of keeping a counter in memory means a restart does not
// hand the visitor a fresh quota.
func (service *Service) quotaExceeded(ctx context.Context, clientIP string, limit int64) bool {
	if clientIP == "" || limit <= 0 {
		return false
	}
	since := service.now().UTC().Add(-time.Hour)
	var count int64
	if err := service.db.WithContext(ctx).Table("payment_orders").
		Where("client_ip = ? AND name = ? AND created_at >= ?", clientIP, testOrderName, since).
		Count(&count).Error; err != nil {
		return false
	}
	return count >= limit
}

// truthy reads a stored boolean. The admin API stores JSON, so both `true` and
// `"true"` show up; the set of strings covers values written by other tools.
func truthy(raw string) bool {
	switch strings.ToLower(settings.Text(raw)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

func parseInt64(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errors.New("empty")
	}
	var parsed int64
	for _, char := range trimmed {
		if char < '0' || char > '9' {
			return 0, errors.New("not a number")
		}
		parsed = parsed*10 + int64(char-'0')
		if parsed > 1<<40 {
			return 0, errors.New("out of range")
		}
	}
	return parsed, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
