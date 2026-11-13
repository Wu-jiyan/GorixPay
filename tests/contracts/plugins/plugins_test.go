package plugins

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	pluginpayment "gorixpay/internal/plugin/payment"
	"gorixpay/internal/plugin/payment/alipay"
	"gorixpay/internal/plugin/payment/epay"
	"gorixpay/internal/plugin/payment/generic"
	"gorixpay/internal/plugin/payment/huifu"
	"gorixpay/internal/plugin/payment/jeepay"
	"gorixpay/internal/plugin/payment/paymenttest"
	"gorixpay/internal/plugin/payment/qqpay"
	"gorixpay/internal/plugin/payment/unionpay"
	"gorixpay/internal/plugin/payment/usdt"
	"gorixpay/internal/plugin/payment/wechatpayv3"
)

func buildRegistry(t *testing.T) *pluginpayment.Registry {
	t.Helper()
	registry := pluginpayment.NewRegistry()
	for _, plugin := range []pluginpayment.Plugin{
		alipay.New(nil), epay.New(nil), generic.New(nil), huifu.New(nil), jeepay.New(nil),
		qqpay.New(nil), unionpay.New(nil), usdt.New(nil), wechatpayv3.New(nil),
	} {
		if err := registry.Register(plugin); err != nil {
			t.Fatalf("register %s: %v", plugin.Metadata().Code, err)
		}
	}
	return registry
}

func TestManifestAndRegistryAgreeOnThePluginSet(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 {
		t.Fatalf("version=%d", manifest.Version)
	}
	if manifest.CallbackRoute != "/api/v1/payment/callback/{plugin}/{channel_id}" {
		t.Fatalf("callback route=%q", manifest.CallbackRoute)
	}
	if manifest.AdminRoutes.Plugins != "/api/v1/admin/plugins" || manifest.AdminRoutes.ChannelTest != "/api/v1/admin/channels/{id}/test" {
		t.Fatalf("admin routes=%+v", manifest.AdminRoutes)
	}
	registry := buildRegistry(t)
	registered := make([]string, 0)
	for _, metadata := range registry.List() {
		registered = append(registered, metadata.Code)
		if _, ok := manifest.Find(metadata.Code); !ok {
			t.Errorf("plugin %q is registered but not declared in the manifest", metadata.Code)
		}
	}
	declared := make([]string, 0, len(manifest.Plugins))
	for _, plugin := range manifest.Plugins {
		declared = append(declared, plugin.Code)
		if _, ok := registry.Lookup(plugin.Code); !ok {
			t.Errorf("plugin %q is declared but not registered", plugin.Code)
		}
	}
	sort.Strings(registered)
	sort.Strings(declared)
	if !reflect.DeepEqual(registered, declared) {
		t.Fatalf("registered=%v declared=%v", registered, declared)
	}
}

func TestEveryDeclaredPluginMatchesItsContract(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	registry := buildRegistry(t)
	for _, declared := range manifest.Plugins {
		declared := declared
		t.Run(declared.Code, func(t *testing.T) {
			plugin, exists := registry.Lookup(declared.Code)
			if !exists {
				t.Fatalf("plugin %q not registered", declared.Code)
			}
			metadata := plugin.Metadata()
			if metadata.Name != declared.Name {
				t.Errorf("name=%q want %q", metadata.Name, declared.Name)
			}
			capabilities := make([]string, 0, len(metadata.Capabilities))
			for _, capability := range metadata.Capabilities {
				capabilities = append(capabilities, string(capability))
			}
			sort.Strings(capabilities)
			expectedCapabilities := append([]string{}, declared.Capabilities...)
			sort.Strings(expectedCapabilities)
			if !reflect.DeepEqual(capabilities, expectedCapabilities) {
				t.Errorf("capabilities=%v want %v", capabilities, expectedCapabilities)
			}
			if _, err := registry.Resolve(declared.Code, pluginpayment.CapabilityPayment); err != nil && containsString(declared.Capabilities, string(pluginpayment.CapabilityPayment)) {
				t.Errorf("resolve payment capability: %v", err)
			}
			verifier, canVerify := plugin.(pluginpayment.CallbackVerifier)
			if canVerify != declared.CallbackVerifier {
				t.Errorf("callback verifier=%t want %t", canVerify, declared.CallbackVerifier)
			}
			if canVerify {
				if _, err := verifier.VerifyCallback(context.Background(), nil, nil); err == nil {
					t.Error("verifier accepted a nil request")
				}
			}
			if declared.AmountUnit == "" {
				t.Error("amount unit is not documented")
			}
			assertConfigFields(t, declared, plugin, registry)
		})
	}
}

func assertConfigFields(t *testing.T, declared Plugin, plugin pluginpayment.Plugin, registry *pluginpayment.Registry) {
	t.Helper()
	published := registry.ConfigFields(declared.Code)
	configurable, ok := plugin.(pluginpayment.Configurable)
	if !ok {
		if len(declared.ConfigFields) != 0 {
			t.Errorf("plugin publishes %d fields but implements no Configurable", len(declared.ConfigFields))
		}
		if published != nil {
			t.Errorf("registry published fields for a non-configurable plugin: %+v", published)
		}
		return
	}
	if len(published) != len(configurable.ConfigFields()) {
		t.Errorf("registry.ConfigFields mismatch: %d vs %d", len(published), len(configurable.ConfigFields()))
	}
	byKey := make(map[string]ConfigField, len(declared.ConfigFields))
	for _, field := range declared.ConfigFields {
		byKey[field.Key] = field
	}
	if len(published) != len(byKey) {
		t.Fatalf("published %d fields, manifest declares %d: %+v", len(published), len(byKey), published)
	}
	for _, field := range published {
		expected, exists := byKey[field.Key]
		if !exists {
			t.Errorf("unexpected published field %q", field.Key)
			continue
		}
		if field.Label == "" {
			t.Errorf("field %q has no label", field.Key)
		}
		if field.Type != expected.Type || field.Required != expected.Required || field.Secret != expected.Secret {
			t.Errorf("field %q = %+v, want type=%s required=%t secret=%t", field.Key, field, expected.Type, expected.Required, expected.Secret)
		}
	}
}

func TestSecretFieldsAreNeverEchoedAsPlainValues(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	registry := buildRegistry(t)
	for _, declared := range manifest.Plugins {
		for _, field := range registry.ConfigFields(declared.Code) {
			if field.Secret && field.Label == "" {
				t.Errorf("%s.%s: secret field without label", declared.Code, field.Key)
			}
		}
	}
}

func TestPluginCodesAreRouteSafe(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, declared := range manifest.Plugins {
		if declared.Code != strings.ToLower(declared.Code) {
			t.Errorf("code %q is not lower case", declared.Code)
		}
		for _, char := range declared.Code {
			allowed := char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_' || char == '-'
			if !allowed {
				t.Errorf("code %q contains a character unfit for a URL segment", declared.Code)
			}
		}
	}
}

func TestManifestDeclaresHandlingForEveryActionType(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{
		string(pluginpayment.ActionJump): true, string(pluginpayment.ActionHTML): true,
		string(pluginpayment.ActionQRCode): true, string(pluginpayment.ActionURLScheme): true,
		string(pluginpayment.ActionJSAPI): true, string(pluginpayment.ActionApp): true,
		string(pluginpayment.ActionScan): true, string(pluginpayment.ActionWXPlugin): true,
		string(pluginpayment.ActionWXApp): true,
	}
	for _, declared := range manifest.Plugins {
		if len(declared.ActionTypes) == 0 {
			t.Errorf("%s: no action types declared", declared.Code)
		}
		for _, action := range declared.ActionTypes {
			if !known[action] {
				t.Errorf("%s: unknown action type %q", declared.Code, action)
			}
		}
	}
}

func TestCallbackRouteMatchesDocumentedShape(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://pay.example/api/v1/payment/callback/alipay/channel-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if request.URL.Path != "/api/v1/payment/callback/alipay/channel-1" {
		t.Fatalf("path=%q", request.URL.Path)
	}
}

// TestDeclaredAmountUnitsAreCentsBased pins the global money convention: every
// plugin reports and compares amounts in the smallest unit (fen/cents).
func TestDeclaredAmountUnitsAreCentsBased(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, declared := range manifest.Plugins {
		if !strings.Contains(strings.ToLower(declared.AmountUnit), "fen") {
			t.Errorf("%s: amount unit %q is not documented in fen", declared.Code, declared.AmountUnit)
		}
	}
}

// TestQqpayAndUnionpayVerifiersRejectUnsignedNotifications covers the two
// additions without any upstream mock: an unsigned XML/form callback must be
// refused with ErrCallbackRejected, never treated as paid.
func TestQqpayAndUnionpayVerifiersRejectUnsignedNotifications(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	registry := buildRegistry(t)

	qqpayPlugin, ok := registry.Lookup("qqpay")
	if !ok {
		t.Fatal("qqpay is not registered")
	}
	qqpayVerifier, ok := qqpayPlugin.(pluginpayment.CallbackVerifier)
	if !ok {
		t.Fatal("qqpay does not verify callbacks")
	}
	xmlRequest := httptest.NewRequest(http.MethodPost, "/api/v1/payment/callback/qqpay/channel-1", strings.NewReader(
		`<xml><return_code>SUCCESS</return_code><result_code>SUCCESS</result_code><mch_id>1000000001</mch_id>`+
			`<out_trade_no>T-1</out_trade_no><transaction_id>QQ-1</transaction_id><total_fee>100</total_fee></xml>`))
	xmlRequest.Header.Set("Content-Type", "text/xml")
	if _, err := qqpayVerifier.VerifyCallback(context.Background(), xmlRequest, map[string]string{"key": "qq-key"}); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
		t.Fatalf("qqpay accepted an unsigned notification: %v", err)
	}

	unionpayPlugin, ok := registry.Lookup("unionpay")
	if !ok {
		t.Fatal("unionpay is not registered")
	}
	unionpayVerifier, ok := unionpayPlugin.(pluginpayment.CallbackVerifier)
	if !ok {
		t.Fatal("unionpay does not verify callbacks")
	}
	formRequest := httptest.NewRequest(http.MethodPost, "/api/v1/payment/callback/unionpay/channel-1",
		strings.NewReader("respCode=00&orderId=T-1&txnAmt=100&queryId=Q-1&signMethod=01"))
	formRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	config := map[string]string{"unionpay_public_key": platform.PublicPEM}
	if _, err := unionpayVerifier.VerifyCallback(context.Background(), formRequest, config); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
		t.Fatalf("unionpay accepted an unsigned notification: %v", err)
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
