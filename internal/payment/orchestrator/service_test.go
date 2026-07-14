package orchestrator

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"
	pluginpayment "gorixpay/internal/plugin/payment"

	"gorm.io/gorm"
)

type actionPlugin struct {
	action pluginpayment.Action
	err    error
}

type blockingPlugin struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

type configurationPlugin struct {
	configuration map[string]string
}

func (plugin *configurationPlugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{Code: "mock", Name: "mock", Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment}}
}

func (plugin *configurationPlugin) CreatePayment(_ context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	plugin.configuration = request.Configuration
	return pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: "https://pay.example/code"}, nil
}

func (plugin *blockingPlugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{Code: "mock", Name: "mock", Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment}}
}

func (plugin *blockingPlugin) CreatePayment(context.Context, pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	plugin.calls.Add(1)
	select {
	case plugin.started <- struct{}{}:
	default:
	}
	<-plugin.release
	return pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: "https://pay.example/code"}, nil
}

func (plugin actionPlugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{Code: "mock", Name: "mock", Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment}}
}

func (plugin actionPlugin) CreatePayment(context.Context, pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	return plugin.action, plugin.err
}

func TestStartSelectsChannelRecordsAttemptAndTransitionsOrder(t *testing.T) {
	db, order := setup(t, "orchestrator-success")
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(actionPlugin{action: pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: "https://pay.example/code"}}); err != nil {
		t.Fatal(err)
	}
	router := channelrouter.New(db, registry, func(int) (int, error) { return 0, nil })
	result, err := New(db, router, time.Now).Start(context.Background(), order.TradeNo)
	if err != nil || result.Action.Type != pluginpayment.ActionQRCode || result.Channel.ID != "channel-1" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var updated paymentmodel.Order
	if err := db.First(&updated, "id = ?", order.ID).Error; err != nil || updated.Status != paymentmodel.StatusProcessing {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	var attempt paymentmodel.Attempt
	if err := db.First(&attempt, "order_id = ?", order.ID).Error; err != nil || attempt.Status != paymentmodel.AttemptSucceeded {
		t.Fatalf("attempt=%+v err=%v", attempt, err)
	}
}

func TestStartRecordsPluginFailureWithoutClosingOrder(t *testing.T) {
	db, order := setup(t, "orchestrator-failure")
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(actionPlugin{err: errors.New("upstream unavailable")}); err != nil {
		t.Fatal(err)
	}
	_, err := New(db, channelrouter.New(db, registry, func(int) (int, error) { return 0, nil }), time.Now).Start(context.Background(), order.TradeNo)
	if err == nil {
		t.Fatal("plugin failure accepted")
	}
	var updated paymentmodel.Order
	if err := db.First(&updated, "id = ?", order.ID).Error; err != nil || updated.Status != paymentmodel.StatusPending {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	var attempt paymentmodel.Attempt
	if err := db.First(&attempt, "order_id = ?", order.ID).Error; err != nil || attempt.Status != paymentmodel.AttemptFailed || attempt.ErrorMessage == nil {
		t.Fatalf("attempt=%+v err=%v", attempt, err)
	}
}

func TestStartClaimsOrderBeforeCallingPlugin(t *testing.T) {
	db, order := setup(t, "orchestrator-concurrent")
	plugin := &blockingPlugin{started: make(chan struct{}, 1), release: make(chan struct{})}
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(plugin); err != nil {
		t.Fatal(err)
	}
	service := New(db, channelrouter.New(db, registry, func(int) (int, error) { return 0, nil }), time.Now)
	firstDone := make(chan error, 1)
	go func() {
		_, err := service.Start(context.Background(), order.TradeNo)
		firstDone <- err
	}()
	select {
	case <-plugin.started:
	case <-time.After(time.Second):
		t.Fatal("first plugin call did not start")
	}
	secondDone := make(chan error, 1)
	go func() {
		_, err := service.Start(context.Background(), order.TradeNo)
		secondDone <- err
	}()
	time.Sleep(20 * time.Millisecond)
	close(plugin.release)
	if firstErr := <-firstDone; firstErr != nil {
		t.Fatal(firstErr)
	}
	secondErr := <-secondDone
	if !errors.Is(secondErr, ErrOrderNotStartable) {
		t.Fatalf("second error=%v", secondErr)
	}
	if plugin.calls.Load() != 1 {
		t.Fatalf("plugin calls=%d", plugin.calls.Load())
	}
}

func TestStartDecryptsSelectedChannelConfiguration(t *testing.T) {
	db, order := setup(t, "orchestrator-config")
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	configs := channelconfig.NewService(cipher)
	encrypted, err := configs.Encrypt("channel-1", channelconfig.Config{APIURL: "https://upstream.example", PID: "1001", Key: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&channelrouter.Channel{}).Where("id = ?", "channel-1").Update("config_encrypted", encrypted).Error; err != nil {
		t.Fatal(err)
	}
	plugin := &configurationPlugin{}
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(plugin); err != nil {
		t.Fatal(err)
	}
	_, err = NewConfigured(db, channelrouter.New(db, registry, func(int) (int, error) { return 0, nil }), time.Now, configs, "https://gorix.example").Start(context.Background(), order.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	if plugin.configuration["api_url"] != "https://upstream.example" || plugin.configuration["pid"] != "1001" || plugin.configuration["key"] != "secret" {
		t.Fatalf("configuration=%v", plugin.configuration)
	}
}

func setup(t *testing.T, name string) (*gorm.DB, paymentmodel.Order) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &paymentmodel.Order{}, &paymentmodel.Attempt{}, &channelrouter.Channel{}); err != nil {
		t.Fatal(err)
	}
	merchant := identitymodel.Merchant{ID: "merchant-1", DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	order, _, err := paymentservice.New(db, time.Now).Create(context.Background(), paymentservice.CreateCommand{
		MerchantID: merchant.ID, OutTradeNo: "ORDER-1", PaymentType: "alipay", Name: "test", Amount: 100,
		NotifyURL: "https://merchant.example/notify", ReturnURL: "https://merchant.example/return", ClientIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	channel := channelrouter.Channel{ID: "channel-1", Name: "mock", PluginCode: "mock", PaymentType: "alipay", Priority: 1, Weight: 100, Status: channelrouter.StatusActive}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	return db, order
}
