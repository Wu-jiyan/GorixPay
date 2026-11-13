package generic

import (
	"context"
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
		if query.Get("sign") != "" {
			t.Errorf("sign sent although sign_type=none: %+v", query)
		}
		_, _ = writer.Write([]byte(`{"code":"1","trade_no":"UP-2","pay_type":"jump","pay_info":"https://pay.example/cashier?id=1"}`))
	}))
	defer server.Close()

	action, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T4002", Amount: 500, NotifyURL: "https://pay.example/notify",
		Configuration: map[string]string{
			"api_url": server.URL, "method": "GET", "sign_type": "none",
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
		body := `{"code":1,"out_trade_no":"T4001","trade_no":"UP-1","amount":"19.99","sign":"ignored"}`
		request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		result, err := New(nil).VerifyCallback(context.Background(), request, map[string]string{"api_url": "https://upstream.example", "sign_type": "none"})
		if err != nil {
			t.Fatal(err)
		}
		if !result.Paid || result.Amount != 1999 {
			t.Fatalf("result=%+v", result)
		}
	})
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
