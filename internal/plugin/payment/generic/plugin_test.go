package generic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

func TestCreatePaymentPostsDefaultTemplateAndMapsQRCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method=%s", request.Method)
		}
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		params := make(map[string]any, len(request.PostForm))
		for name := range request.PostForm {
			params[name] = request.PostForm.Get(name)
		}
		if !signing.VerifyMD5(params, "generic-key", request.PostForm.Get("sign")) {
			t.Errorf("signature does not verify: %+v", request.PostForm)
		}
		if request.PostForm.Get("pid") != "1001" || request.PostForm.Get("money") != "19.99" || request.PostForm.Get("type") != "alipay" {
			t.Errorf("params=%+v", request.PostForm)
		}
		_, _ = writer.Write([]byte(`{"code":1,"msg":"success","trade_no":"UP-1","pay_type":"qrcode","pay_info":"https://qr.example/abc"}`))
	}))
	defer server.Close()

	action, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T4001", Amount: 1999, Description: "通用支付", PaymentType: "alipay",
		NotifyURL:     "https://pay.example/api/v1/payment/callback/generic/channel-1",
		Configuration: map[string]string{"api_url": server.URL, "pid": "1001", "key": "generic-key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionQRCode || action.URL != "https://qr.example/abc" {
		t.Fatalf("action=%+v", action)
	}
}

func TestCreatePaymentSupportsCustomTemplateAndGET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("method=%s", request.Method)
		}
		query := request.URL.Query()
		if query.Get("order") != "T4002" || query.Get("cents") != "500" || query.Get("callback") != "https://pay.example/notify" {
			t.Errorf("query=%+v", query)
		}
		params := map[string]any{}
		for name, values := range request.URL.Query() {
			params[name] = values[0]
		}
		if !signing.VerifyMD5(params, "generic-key", query.Get("sign")) {
			t.Errorf("custom template request is not signed: %+v", query)
		}
		_, _ = writer.Write([]byte(`{"code":"1","trade_no":"UP-2","pay_type":"jump","pay_info":"https://pay.example/cashier?id=1"}`))
	}))
	defer server.Close()

	action, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T4002", Amount: 500, NotifyURL: "https://pay.example/notify",
		Configuration: map[string]string{
			"api_url": server.URL, "method": "GET", "key": "generic-key",
			"param_template": "order={trade_no}&cents={amount_cents}&callback={notify_url}&sign={sign}",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionJump || action.URL != "https://pay.example/cashier?id=1" {
		t.Fatalf("action=%+v", action)
	}
}

func TestCreatePaymentRejectsUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"code":0,"msg":"余额不足"}`))
	}))
	defer server.Close()
	_, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T4003", Amount: 500, Configuration: map[string]string{"api_url": server.URL, "key": "generic-key"},
	})
	if err == nil || !strings.Contains(err.Error(), "余额不足") {
		t.Fatalf("error=%v", err)
	}
}

func TestCreatePaymentRejectsGarbageAndMissingConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("<html>oops</html>"))
	}))
	defer server.Close()
	if _, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T4004", Amount: 500, Configuration: map[string]string{"api_url": server.URL, "key": "generic-key"},
	}); err == nil {
		t.Fatal("garbage response accepted")
	}
	if _, err := New(nil).CreatePayment(context.Background(), pluginpayment.CreateRequest{TradeNo: "T4004", Amount: 500}); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
		t.Fatalf("error=%v", err)
	}
}

func TestCreatePaymentTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer server.Close()
	started := time.Now()
	if _, err := New(&http.Client{Timeout: 30 * time.Millisecond}).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T4005", Amount: 500, Configuration: map[string]string{"api_url": server.URL, "key": "generic-key"},
	}); err == nil {
		t.Fatal("timeout not reported")
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("timeout not enforced, elapsed=%s", elapsed)
	}
}

func TestVerifyCallbackAcceptsMD5FormNotification(t *testing.T) {
	form := genericNotification("T4001", "UP-1", "19.99", "TRADE_SUCCESS")
	request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	result, err := New(nil).VerifyCallback(context.Background(), request, map[string]string{"api_url": "https://upstream.example", "key": "generic-key"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TradeNo != "T4001" || result.ChannelTradeNo != "UP-1" || result.Amount != 1999 || !result.Paid {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerifyCallbackRejectsBadSignatureAndAmounts(t *testing.T) {
	config := map[string]string{"api_url": "https://upstream.example", "key": "generic-key"}

	t.Run("forged-signature", func(t *testing.T) {
		form := genericNotification("T4001", "UP-1", "19.99", "TRADE_SUCCESS")
		form.Set("sign", "deadbeef")
		if _, err := verifyForm(t, config, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("amount-changed-after-signing", func(t *testing.T) {
		form := genericNotification("T4001", "UP-1", "19.99", "TRADE_SUCCESS")
		form.Set("money", "0.01")
		if _, err := verifyForm(t, config, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("signed-amount-surfaces-for-order-comparison", func(t *testing.T) {
		form := genericNotification("T4001", "UP-1", "0.01", "TRADE_SUCCESS")
		result, err := verifyForm(t, config, form)
		if err != nil {
			t.Fatal(err)
		}
		if result.Amount != 1 {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("unpaid-status", func(t *testing.T) {
		form := genericNotification("T4001", "UP-1", "19.99", "WAIT_BUYER_PAY")
		result, err := verifyForm(t, config, form)
		if err != nil {
			t.Fatal(err)
		}
		if result.Paid {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("json-code-success", func(t *testing.T) {
		params := map[string]any{"code": "1", "out_trade_no": "T4001", "trade_no": "UP-1", "amount": "19.99"}
		params["sign"] = signing.SignMD5(params, "generic-key")
		encoded, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(string(encoded)))
		request.Header.Set("Content-Type", "application/json")
		result, err := New(nil).VerifyCallback(context.Background(), request, config)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Paid || result.Amount != 1999 {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("json-unsigned-without-key", func(t *testing.T) {
		body := `{"code":1,"out_trade_no":"T4001","trade_no":"UP-1","amount":"19.99","sign":"ignored"}`
		request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if _, err := New(nil).VerifyCallback(context.Background(), request, map[string]string{"api_url": "https://upstream.example", "sign_type": "none"}); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("unsigned callback accepted: %v", err)
		}
	})
}

// A generic channel without a signing key used to accept every POST as a paid
// notification. It now rejects the callback instead of trusting it.
func TestVerifyCallbackRejectsKeylessChannel(t *testing.T) {
	params := map[string]any{
		"pid": "1001", "out_trade_no": "T4001", "trade_no": "UP-1",
		"money": "19.99", "trade_status": "TRADE_SUCCESS",
	}
	params["sign"] = signing.SignMD5(params, "attacker-key")
	forged := make(url.Values, len(params))
	for name, value := range params {
		forged.Set(name, value.(string))
	}
	// No key at all: nothing can authenticate the body, so it is rejected with a
	// reason that names the missing key. sign_type does not matter here.
	for name, config := range map[string]map[string]string{
		"no-key":    {"api_url": "https://upstream.example"},
		"none-mode": {"api_url": "https://upstream.example", "sign_type": "none"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := verifyForm(t, config, forged)
			if !errors.Is(err, pluginpayment.ErrCallbackRejected) || !strings.Contains(err.Error(), "key") {
				t.Fatalf("keyless channel did not reject the callback with a clear cause: %v", err)
			}
		})
	}
	// The same forged body is rejected by a correctly configured channel too:
	// only the configured key authenticates a notification.
	if _, err := verifyForm(t, map[string]string{"api_url": "https://upstream.example", "key": "generic-key"}, forged); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
		t.Fatalf("attacker-key signature accepted: %v", err)
	}
	// Callbacks are verified even when the channel is configured unsigned: a
	// properly signed notification from that channel still passes.
	valid := genericNotification("T4001", "UP-1", "19.99", "TRADE_SUCCESS")
	result, err := verifyForm(t, map[string]string{"api_url": "https://upstream.example", "key": "generic-key", "sign_type": "none"}, valid)
	if err != nil || !result.Paid {
		t.Fatalf("valid callback rejected: %+v err=%v", result, err)
	}
}

func TestCreatePaymentRequiresSigningKey(t *testing.T) {
	_, err := New(nil).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T4006", Amount: 500, Configuration: map[string]string{"api_url": "https://upstream.example"},
	})
	if !errors.Is(err, pluginpayment.ErrInvalidConfiguration) || !strings.Contains(err.Error(), "signing key") {
		t.Fatalf("keyless create error=%v", err)
	}
	_, err = New(nil).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T4007", Amount: 500,
		Configuration: map[string]string{"api_url": "https://upstream.example", "key": "generic-key", "sign_type": "rsa"},
	})
	if !errors.Is(err, pluginpayment.ErrInvalidConfiguration) || !strings.Contains(err.Error(), "sign_type") {
		t.Fatalf("unknown sign_type error=%v", err)
	}
}

// sign_type=none keeps working for the outgoing create call: the request is sent
// unsigned, but the channel still needs a key for its callbacks.
func TestCreatePaymentHonoursUnsignedMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if request.PostForm.Get("sign") != "" {
			t.Errorf("unsigned channel signed the request: %+v", request.PostForm)
		}
		_, _ = writer.Write([]byte(`{"code":1,"trade_no":"UP-3","pay_type":"jump","pay_info":"https://pay.example/jump"}`))
	}))
	defer server.Close()
	// The conformance harness creates its generic channel with exactly this
	// configuration (key present, create unsigned); it has to keep working.
	action, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T4008", Amount: 500, NotifyURL: "https://pay.example/notify",
		Configuration: map[string]string{
			"api_url": server.URL, "pid": "v1-upstream-pid", "key": "v1-upstream-key",
			"sign_type": "none", "method": "POST",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionJump {
		t.Fatalf("action=%+v", action)
	}
}

func TestValidateConfigurationRejectsIncompleteChannel(t *testing.T) {
	plugin := New(nil)
	if err := plugin.ValidateConfiguration(map[string]string{"api_url": "https://upstream.example", "key": "generic-key"}); err != nil {
		t.Fatalf("complete configuration rejected: %v", err)
	}
	if err := plugin.ValidateConfiguration(map[string]string{"api_url": "https://upstream.example", "key": "generic-key", "sign_type": "none"}); err != nil {
		t.Fatalf("unsigned create mode rejected: %v", err)
	}
	for name, config := range map[string]map[string]string{
		"missing-key": {"api_url": "https://upstream.example"},
		"missing-url": {"key": "generic-key"},
		"bad-mode":    {"api_url": "https://upstream.example", "key": "generic-key", "sign_type": "rsa"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := plugin.ValidateConfiguration(config); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
				t.Fatalf("incomplete configuration accepted: %v", err)
			}
		})
	}
}

func TestMetadataAndConfigFields(t *testing.T) {
	plugin := New(nil)
	if metadata := plugin.Metadata(); metadata.Code != "generic" || len(metadata.Capabilities) != 1 {
		t.Fatalf("metadata=%+v", metadata)
	}
	wanted := map[string]bool{"api_url": true, "method": true, "param_template": true, "sign_type": true, "key": true}
	for _, field := range plugin.ConfigFields() {
		delete(wanted, field.Key)
	}
	if len(wanted) != 0 {
		t.Fatalf("missing fields=%v", wanted)
	}
}

func verifyForm(t *testing.T, config map[string]string, form url.Values) (pluginpayment.CallbackResult, error) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return New(nil).VerifyCallback(context.Background(), request, config)
}

func genericNotification(tradeNo, channelTradeNo, money, status string) url.Values {
	params := map[string]any{
		"pid": "1001", "out_trade_no": tradeNo, "trade_no": channelTradeNo,
		"money": money, "trade_status": status, "type": "alipay", "sign_type": "MD5",
	}
	params["sign"] = signing.SignMD5(params, "generic-key")
	values := make(url.Values, len(params))
	for name, value := range params {
		values.Set(name, value.(string))
	}
	return values
}
