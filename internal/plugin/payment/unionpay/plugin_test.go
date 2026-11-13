package unionpay

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	pluginpayment "gorixpay/internal/plugin/payment"
	"gorixpay/internal/plugin/payment/paymenttest"
)

var inputPattern = regexp.MustCompile(`name="([^"]+)" value="([^"]*)"`)

func TestCreatePaymentReturnsAutoSubmitForm(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = writer.Write([]byte("should not be called"))
	}))
	defer server.Close()

	action, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T7001", Amount: 1999, Description: "银联下单", PaymentType: "unionpay",
		NotifyURL: "https://pay.example/api/v1/payment/callback/unionpay/channel-1",
		ReturnURL: "https://merchant.example/return",
		Configuration: map[string]string{
			"mer_id": "777290058110048", "cert_id": "68759529225", "private_key": merchant.PrivatePEM,
			"unionpay_public_key": "unused-for-create", "api_url": server.URL,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionHTML {
		t.Fatalf("action=%+v", action)
	}
	document, ok := action.Data.(string)
	if !ok {
		t.Fatalf("data=%T", action.Data)
	}
	if calls.Load() != 0 {
		t.Fatalf("plugin performed %d upstream calls, expected a browser-side form", calls.Load())
	}
	if !strings.Contains(document, `action="`+server.URL+`"`) {
		t.Fatalf("form action missing: %s", document)
	}
	if !strings.Contains(document, `document.forms["unionpay_submit"].submit()`) {
		t.Fatalf("auto submit script missing: %s", document)
	}
	fields := formFields(t, document)
	for name, want := range map[string]string{
		"version": "5.1.0", "encoding": "UTF-8", "txnType": "01", "txnSubType": "01",
		"bizType": "000201", "channelType": "07", "currencyCode": "156", "signMethod": "01",
		"merId": "777290058110048", "certId": "68759529225", "orderId": "T7001", "txnAmt": "1999",
		"backUrl":  "https://pay.example/api/v1/payment/callback/unionpay/channel-1",
		"frontUrl": "https://merchant.example/return",
	} {
		if fields[name] != want {
			t.Errorf("field %s=%q want %q", name, fields[name], want)
		}
	}
	if len(fields["txnTime"]) != 14 {
		t.Errorf("txnTime=%q", fields["txnTime"])
	}
	if !verifyValues(&merchant.Private.PublicKey, fields) {
		t.Fatal("form signature does not verify with the merchant public key")
	}
}

func TestCreatePaymentJumpModeProducesGatewayURL(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	action, err := New(nil).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T7002", Amount: 100,
		NotifyURL: "https://pay.example/notify", ReturnURL: "https://merchant.example/return",
		Configuration: map[string]string{
			"mer_id": "777290058110048", "cert_id": "68759529225", "private_key": merchant.PrivatePEM,
			"unionpay_public_key": "unused-for-create", "api_url": "https://gateway.example/frontTransReq.do",
			"submit_mode": "jump",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionJump {
		t.Fatalf("action=%+v", action)
	}
	parsed, err := url.Parse(action.URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != "https://gateway.example/frontTransReq.do" {
		t.Fatalf("url=%q", action.URL)
	}
	query := parsed.Query()
	fields := make(map[string]string, len(query))
	for name := range query {
		fields[name] = query.Get(name)
	}
	if fields["txnAmt"] != "100" || fields["orderId"] != "T7002" {
		t.Fatalf("query=%+v", fields)
	}
	if !verifyValues(&merchant.Private.PublicKey, fields) {
		t.Fatal("jump signature does not verify with the merchant public key")
	}
}

func TestBrowserSubmissionIsAcceptedByMockGateway(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	var verified bool
	gateway := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		fields := make(map[string]string, len(request.PostForm))
		for name := range request.PostForm {
			fields[name] = request.PostForm.Get(name)
		}
		if !verifyValues(&merchant.Private.PublicKey, fields) {
			t.Errorf("gateway could not verify the signature: %+v", fields)
		}
		verified = true
		_, _ = writer.Write([]byte("ok"))
	}))
	defer gateway.Close()

	action, err := New(gateway.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T7003", Amount: 500,
		NotifyURL: "https://pay.example/notify", ReturnURL: "https://merchant.example/return",
		Configuration: map[string]string{
			"mer_id": "777290058110048", "cert_id": "68759529225", "private_key": merchant.PrivatePEM,
			"unionpay_public_key": "unused-for-create", "api_url": gateway.URL,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	document, _ := action.Data.(string)
	fields := formFields(t, document)
	form := make(url.Values, len(fields))
	for name, value := range fields {
		form.Set(name, value)
	}
	response, err := gateway.Client().PostForm(gateway.URL, form)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if !verified {
		t.Fatal("gateway never verified the submission")
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestCreatePaymentRequiresConfiguration(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	complete := func() map[string]string {
		return map[string]string{
			"mer_id": "777290058110048", "cert_id": "68759529225", "private_key": merchant.PrivatePEM,
			"unionpay_public_key": "unused-for-create", "front_url": "https://merchant.example/return",
		}
	}
	for name, mutate := range map[string]func(map[string]string){
		"missing-mer-id":      func(config map[string]string) { delete(config, "mer_id") },
		"missing-cert-id":     func(config map[string]string) { delete(config, "cert_id") },
		"missing-private-key": func(config map[string]string) { delete(config, "private_key") },
		"broken-private-key":  func(config map[string]string) { config["private_key"] = "not a pem" },
		"missing-front-url":   func(config map[string]string) { delete(config, "front_url") },
	} {
		t.Run(name, func(t *testing.T) {
			config := complete()
			mutate(config)
			_, err := New(nil).CreatePayment(context.Background(), pluginpayment.CreateRequest{
				TradeNo: "T7004", Amount: 100, NotifyURL: "https://pay.example/notify", Configuration: config,
			})
			if !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	t.Run("missing-back-url", func(t *testing.T) {
		_, err := New(nil).CreatePayment(context.Background(), pluginpayment.CreateRequest{
			TradeNo: "T7004", Amount: 100, Configuration: complete(),
		})
		if !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("zero-amount", func(t *testing.T) {
		_, err := New(nil).CreatePayment(context.Background(), pluginpayment.CreateRequest{
			TradeNo: "T7004", Amount: 0, NotifyURL: "https://pay.example/notify", Configuration: complete(),
		})
		if !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestVerifyCallbackAcceptsSignedNotification(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	fields := map[string]string{
		"version": "5.1.0", "encoding": "UTF-8", "signMethod": "01",
		"merId": "777290058110048", "orderId": "T7001", "txnAmt": "1999",
		"queryId": "Q-1", "respCode": "00", "respMsg": "成功",
		"txnTime": "20261113180000", "settleAmt": "1999",
	}
	result, err := verifyForm(t, map[string]string{"unionpay_public_key": platform.PublicPEM, "mer_id": "777290058110048"}, signedCallback(t, fields, platform))
	if err != nil {
		t.Fatal(err)
	}
	if result.TradeNo != "T7001" || result.ChannelTradeNo != "Q-1" || result.Amount != 1999 || !result.Paid {
		t.Fatalf("result=%+v", result)
	}
	if result.Raw["respCode"] != "00" {
		t.Fatalf("raw=%+v", result.Raw)
	}
}

func TestVerifyCallbackRejectsBadSignatureAndAmounts(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	config := map[string]string{"unionpay_public_key": platform.PublicPEM, "mer_id": "777290058110048"}
	base := func() map[string]string {
		return map[string]string{
			"version": "5.1.0", "encoding": "UTF-8", "signMethod": "01",
			"merId": "777290058110048", "orderId": "T7001", "txnAmt": "1999",
			"queryId": "Q-1", "respCode": "00", "respMsg": "成功",
		}
	}

	t.Run("forged-signature", func(t *testing.T) {
		form := signedCallback(t, base(), platform)
		form.Set("sign", "Zm9yZ2Vk")
		if _, err := verifyForm(t, config, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("amount-changed-after-signing", func(t *testing.T) {
		form := signedCallback(t, base(), platform)
		form.Set("txnAmt", "1")
		if _, err := verifyForm(t, config, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("signed-amount-surfaces-for-order-comparison", func(t *testing.T) {
		fields := base()
		fields["txnAmt"] = "1"
		result, err := verifyForm(t, config, signedCallback(t, fields, platform))
		if err != nil {
			t.Fatal(err)
		}
		if result.Amount != 1 {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("failed-resp-code", func(t *testing.T) {
		fields := base()
		fields["respCode"] = "01"
		fields["respMsg"] = "交易失败"
		result, err := verifyForm(t, config, signedCallback(t, fields, platform))
		if err != nil {
			t.Fatal(err)
		}
		if result.Paid || result.Amount != 0 {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("merchant-mismatch", func(t *testing.T) {
		fields := base()
		fields["merId"] = "000000000000001"
		if _, err := verifyForm(t, config, signedCallback(t, fields, platform)); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("unsupported-sign-method", func(t *testing.T) {
		fields := base()
		fields["signMethod"] = "05"
		if _, err := verifyForm(t, config, signedCallback(t, fields, platform)); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("missing-public-key", func(t *testing.T) {
		if _, err := verifyForm(t, map[string]string{}, signedCallback(t, base(), platform)); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("missing-query-id", func(t *testing.T) {
		fields := base()
		delete(fields, "queryId")
		if _, err := verifyForm(t, config, signedCallback(t, fields, platform)); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestMetadataAndConfigFields(t *testing.T) {
	plugin := New(nil)
	metadata := plugin.Metadata()
	if metadata.Code != "unionpay" || metadata.Name == "" || len(metadata.Capabilities) != 1 {
		t.Fatalf("metadata=%+v", metadata)
	}
	wanted := map[string]bool{
		"mer_id": true, "cert_id": true, "private_key": true,
		"unionpay_public_key": true, "api_url": true, "front_url": true, "submit_mode": true,
	}
	secretFields := 0
	for _, field := range plugin.ConfigFields() {
		delete(wanted, field.Key)
		if field.Label == "" || field.Type == "" {
			t.Errorf("field=%+v", field)
		}
		if field.Secret {
			secretFields++
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("missing fields=%v", wanted)
	}
	if secretFields != 2 {
		t.Fatalf("secret fields=%d", secretFields)
	}
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(New(nil)); err != nil {
		t.Fatalf("registry rejected the plugin: %v", err)
	}
	if _, err := registry.Resolve("unionpay", pluginpayment.CapabilityPayment); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

// formFields extracts the hidden inputs of the generated auto-submit form.
func formFields(t *testing.T, document string) map[string]string {
	t.Helper()
	fields := make(map[string]string)
	for _, match := range inputPattern.FindAllStringSubmatch(document, -1) {
		fields[match[1]] = html.UnescapeString(match[2])
	}
	if len(fields) == 0 {
		t.Fatalf("no form inputs found in %q", document)
	}
	return fields
}

func verifyForm(t *testing.T, config map[string]string, form url.Values) (pluginpayment.CallbackResult, error) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return New(nil).VerifyCallback(context.Background(), request, config)
}

// signedCallback renders the notification the UnionPay front-end posts back,
// signed with the platform private key.
func signedCallback(t *testing.T, fields map[string]string, platform paymenttest.Pair) url.Values {
	t.Helper()
	copied := make(map[string]string, len(fields)+1)
	for name, value := range fields {
		copied[name] = value
	}
	signature, err := signValues(platform.Private, copied)
	if err != nil {
		t.Fatalf("sign callback: %v", err)
	}
	copied["sign"] = signature
	form := make(url.Values, len(copied))
	for name, value := range copied {
		form.Set(name, value)
	}
	return form
}
