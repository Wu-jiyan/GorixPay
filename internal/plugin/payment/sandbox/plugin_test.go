package sandbox

import (
	"context"
	"errors"
	"testing"

	pluginpayment "gorixpay/internal/plugin/payment"
)

func TestMetadataAndRegistration(t *testing.T) {
	plugin := New()
	metadata := plugin.Metadata()
	if metadata.Code != "sandbox" || metadata.Name != "沙箱支付" {
		t.Fatalf("metadata=%+v", metadata)
	}
	if len(metadata.Capabilities) != 1 || metadata.Capabilities[0] != pluginpayment.CapabilityPayment {
		t.Fatalf("capabilities=%+v", metadata.Capabilities)
	}
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(plugin); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Resolve("sandbox", pluginpayment.CapabilityPayment); err != nil {
		t.Fatal(err)
	}
}

func TestCreatePaymentUsesSiteURLFromExtra(t *testing.T) {
	action, err := New().CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo:   "20260101120000001",
		NotifyURL: "https://merchant.example/notify",
		Extra:     map[string]string{"site_url": "https://gorix.example/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionJump {
		t.Fatalf("action type=%q", action.Type)
	}
	if action.URL != "https://gorix.example/pay/sandbox/20260101120000001" {
		t.Fatalf("action url=%q", action.URL)
	}
	if err := action.Validate(); err != nil {
		t.Fatalf("action does not validate: %v", err)
	}
}

func TestCreatePaymentKeepsSitePathPrefix(t *testing.T) {
	action, err := New().CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T1",
		Extra:   map[string]string{"site_url": "https://gorix.example/gp/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.URL != "https://gorix.example/gp/pay/sandbox/T1" {
		t.Fatalf("action url=%q", action.URL)
	}
}

func TestCreatePaymentFallsBackToNotifyURLHost(t *testing.T) {
	action, err := New().CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo:   "T2",
		NotifyURL: "https://gorix.example:8443/api/v1/payment/callback/sandbox/channel-1",
		Extra:     map[string]string{"site_url": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.URL != "https://gorix.example:8443/pay/sandbox/T2" {
		t.Fatalf("action url=%q", action.URL)
	}
}

func TestCreatePaymentRejectsInvalidSiteURL(t *testing.T) {
	_, err := New().CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo:   "T3",
		NotifyURL: "merchant.example/notify",
		Extra:     map[string]string{"site_url": "not-a-url"},
	})
	if !errors.Is(err, ErrMissingSiteURL) {
		t.Fatalf("err=%v", err)
	}
}

func TestCreatePaymentRejectsMissingTradeNo(t *testing.T) {
	_, err := New().CreatePayment(context.Background(), pluginpayment.CreateRequest{
		Extra: map[string]string{"site_url": "https://gorix.example"},
	})
	if !errors.Is(err, ErrMissingTradeNo) {
		t.Fatalf("err=%v", err)
	}
}
