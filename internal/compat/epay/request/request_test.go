package request

import (
	"testing"

	paymentorchestrator "gorixpay/internal/payment/orchestrator"
)

func optionsFrom(values map[string]string) paymentorchestrator.StartOptions {
	return Options(func(name string) string { return values[name] })
}

// TestOptionsKeepsInAppDevice pins the in-app device name. Upstream keeps $mdevice
// next to $device (Pay.php:301-305) because a plugin has to know whether it is
// inside WeChat or Alipay: only there can a JSAPI trade type run. Flattening
// "wechat" into "mobile" made the hosted channel skip T_JSAPI and send a WeChat
// buyer to a cashier page instead of straight to 公众号支付.
func TestOptionsKeepsInAppDevice(t *testing.T) {
	for _, device := range []string{"wechat", "alipay", "qq", "app"} {
		options := optionsFrom(map[string]string{"device": device})
		if options.Device != device {
			t.Fatalf("device=%q want %q kept", options.Device, device)
		}
	}
	if options := optionsFrom(nil); options.Device != "pc" {
		t.Fatalf("missing device = %q want pc", options.Device)
	}
	if options := optionsFrom(map[string]string{"device": "mobile"}); options.Device != "mobile" {
		t.Fatalf("device=%q want mobile", options.Device)
	}
}

// TestOptionsJumpDeviceForcesMethod keeps the upstream rule that a jump device
// overrides whatever method was sent (Pay.php:302-303).
func TestOptionsJumpDeviceForcesMethod(t *testing.T) {
	options := optionsFrom(map[string]string{"device": "jump", "method": "jsapi"})
	if options.Method != "jump" {
		t.Fatalf("method=%q want jump", options.Method)
	}
}
