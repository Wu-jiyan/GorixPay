package router

import (
	"context"
	"errors"
	"testing"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	pluginpayment "gorixpay/internal/plugin/payment"
)

type plugin struct{ metadata pluginpayment.Metadata }

func (value plugin) Metadata() pluginpayment.Metadata { return value.metadata }
func (value plugin) CreatePayment(context.Context, pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	return pluginpayment.Action{}, nil
}

func TestSelectFiltersByTypeStatusPriorityAndCapability(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:channel-router?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Channel{}); err != nil {
		t.Fatal(err)
	}
	channels := []Channel{
		{ID: "disabled", Name: "disabled", PluginCode: "mock", PaymentType: "alipay", Priority: 1, Weight: 100, Status: StatusDisabled},
		{ID: "unsupported", Name: "unsupported", PluginCode: "query-only", PaymentType: "alipay", Priority: 1, Weight: 100, Status: StatusActive},
		{ID: "priority-2", Name: "priority-2", PluginCode: "mock", PaymentType: "alipay", Priority: 2, Weight: 100, Status: StatusActive},
		{ID: "wechat", Name: "wechat", PluginCode: "mock", PaymentType: "wxpay", Priority: 1, Weight: 100, Status: StatusActive},
		{ID: "selected", Name: "selected", PluginCode: "mock", PaymentType: "alipay", Priority: 1, Weight: 100, Status: StatusActive},
	}
	if err := db.Create(&channels).Error; err != nil {
		t.Fatal(err)
	}
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(plugin{pluginpayment.Metadata{Code: "mock", Name: "mock", Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment}}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(plugin{pluginpayment.Metadata{Code: "query-only", Name: "query", Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityQuery}}}); err != nil {
		t.Fatal(err)
	}
	selected, resolved, err := New(db, registry, func(max int) (int, error) { return 0, nil }).Select(context.Background(), "alipay")
	if err != nil || selected.ID != "selected" || resolved.Metadata().Code != "mock" {
		t.Fatalf("selected=%+v plugin=%+v err=%v", selected, resolved, err)
	}
}

func TestSelectUsesWeightsWithinBestPriority(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:channel-weights?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Channel{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]Channel{
		{ID: "first", Name: "first", PluginCode: "mock", PaymentType: "alipay", Priority: 1, Weight: 20, Status: StatusActive},
		{ID: "second", Name: "second", PluginCode: "mock", PaymentType: "alipay", Priority: 1, Weight: 80, Status: StatusActive},
	}).Error; err != nil {
		t.Fatal(err)
	}
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(plugin{pluginpayment.Metadata{Code: "mock", Name: "mock", Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment}}}); err != nil {
		t.Fatal(err)
	}
	selected, _, err := New(db, registry, func(max int) (int, error) {
		if max != 100 {
			t.Fatalf("weight max=%d", max)
		}
		return 20, nil
	}).Select(context.Background(), "alipay")
	if err != nil || selected.ID != "second" {
		t.Fatalf("selected=%+v err=%v", selected, err)
	}
}

func TestSelectReturnsNoChannel(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:no-channel?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Channel{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := New(db, pluginpayment.NewRegistry(), nil).Select(context.Background(), "alipay"); !errors.Is(err, ErrNoAvailableChannel) {
		t.Fatalf("error=%v", err)
	}
}
