package orchestrator

import (
	"context"
	"testing"
	"time"

	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"
	paymentservice "gorixpay/internal/payment/service"
	platformconfig "gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"
	pluginpayment "gorixpay/internal/plugin/payment"

	"gorm.io/gorm"
)

// recordingPlugin captures the whole CreateRequest so the configuration
// passthrough can be asserted field by field.
type recordingPlugin struct {
	request pluginpayment.CreateRequest
}

func (*recordingPlugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{Code: "recorder", Name: "recorder", Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment}}
}

func (plugin *recordingPlugin) CreatePayment(_ context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	plugin.request = request
	return pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: "https://pay.example/code"}, nil
}

func TestStartPassesExtraChannelConfigurationAndLetsItOverride(t *testing.T) {
	db, order := setupForExtraConfiguration(t)
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	configs := channelconfig.NewService(cipher)
	encrypted, err := configs.Encrypt("channel-extra", channelconfig.Config{
		APIURL: "https://upstream.example", PID: "1001", Key: "legacy-secret",
		Extra: map[string]string{"app_id": "2021000000000001", "api_url": "https://override.example", "key": "extra-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	channel := channelrouter.Channel{
		ID: "channel-extra", Name: "alipay", PluginCode: "recorder", PaymentType: "alipay",
		Priority: 1, Weight: 100, Status: channelrouter.StatusActive, ConfigEncrypted: encrypted,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	plugin := &recordingPlugin{}
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(plugin); err != nil {
		t.Fatal(err)
	}
	service := NewConfigured(db, channelrouter.New(db, registry, func(int) (int, error) { return 0, nil }), time.Now, configs, "https://pay.example")
	result, err := service.Start(context.Background(), order.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	if result.Channel.ID != "channel-extra" {
		t.Fatalf("channel=%+v", result.Channel)
	}
	configuration := plugin.request.Configuration
	if configuration["app_id"] != "2021000000000001" {
		t.Fatalf("extra configuration was not passed through: %+v", configuration)
	}
	if configuration["api_url"] != "https://override.example" {
		t.Fatalf("extra did not override the legacy key: %+v", configuration)
	}
	if configuration["key"] != "extra-secret" || configuration["pid"] != "1001" {
		t.Fatalf("configuration=%+v", configuration)
	}
	if plugin.request.NotifyURL != "https://pay.example/api/v1/payment/callback/recorder/channel-extra" {
		t.Fatalf("notify url=%q", plugin.request.NotifyURL)
	}
	if plugin.request.Extra["site_url"] != "https://pay.example" {
		t.Fatalf("extra=%+v", plugin.request.Extra)
	}
}

// setupForExtraConfiguration creates a database with a merchant and one pending
// order but no channel, so the test can install the channel it needs.
func setupForExtraConfiguration(t *testing.T) (*gorm.DB, paymentmodel.Order) {
	t.Helper()
	db, _, err := database.Open(context.Background(), platformconfig.DatabaseConfig{Driver: "sqlite", DSN: "file:orchestrator-extra-config?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &paymentmodel.Order{}, &paymentmodel.Attempt{}, &channelrouter.Channel{}); err != nil {
		t.Fatal(err)
	}
	merchant := identitymodel.Merchant{ID: "merchant-extra", DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	order, _, err := paymentservice.New(db, time.Now).Create(context.Background(), paymentservice.CreateCommand{
		MerchantID: merchant.ID, OutTradeNo: "ORDER-EXTRA", PaymentType: "alipay", Name: "test", Amount: 1234,
		NotifyURL: "https://merchant.example/notify", ReturnURL: "https://merchant.example/return", ClientIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, order
}
