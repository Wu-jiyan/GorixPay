package qqpay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pluginpayment "gorixpay/internal/plugin/payment"
)

func TestCreatePaymentSignsUnifiedOrderAndReturnsQRCode(t *testing.T) {
	var received map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if contentType := request.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/xml") {
			t.Errorf("content type=%q", contentType)
		}
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		received, err = decodeXML(raw)
		if err != nil {
			t.Errorf("decode xml: %v", err)
		}
		if !verifySignature(received, "qq-key") {
			t.Errorf("request signature does not verify: %+v", received)
		}
		if received["mch_id"] != "1900000109" || received["appid"] != "1000000001" {
			t.Errorf("fields=%+v", received)
		}
		if received["total_fee"] != "1999" || received["trade_type"] != "NATIVE" {
			t.Errorf("fields=%+v", received)
		}
		if received["body"] != "QQ 钱包&测试" {
			t.Errorf("body=%q", received["body"])
		}
		_, _ = writer.Write([]byte(`<xml><return_code><![CDATA[SUCCESS]]></return_code><return_msg><![CDATA[OK]]></return_msg>` +
			`<result_code><![CDATA[SUCCESS]]></result_code><code_url><![CDATA[https://qpay.qq.com/qr/abc123]]></code_url></xml>`))
	}))
	defer server.Close()

	action, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T6001", Amount: 1999, Description: "QQ 钱包&测试", PaymentType: "qqpay", ClientIP: "203.0.113.9",
		NotifyURL:     "https://pay.example/api/v1/payment/callback/qqpay/channel-1",
		Configuration: map[string]string{"mch_id": "1900000109", "app_id": "1000000001", "key": "qq-key", "api_url": server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionQRCode || action.URL != "https://qpay.qq.com/qr/abc123" {
		t.Fatalf("action=%+v", action)
	}
	if received["spbill_create_ip"] != "203.0.113.9" || received["notify_url"] == "" || received["time_expire"] == "" {
		t.Fatalf("fields=%+v", received)
	}
	if len(received["nonce_str"]) != 32 {
		t.Fatalf("nonce=%q", received["nonce_str"])
	}
}

func TestCreatePaymentRejectsUpstreamFailures(t *testing.T) {
	for name, body := range map[string]string{
		"return-code-fail": `<xml><return_code><![CDATA[FAIL]]></return_code><return_msg><![CDATA[签名失败]]></return_msg></xml>`,
		"result-code-fail": `<xml><return_code><![CDATA[SUCCESS]]></return_code><result_code><![CDATA[FAIL]]></result_code>` +
			`<err_code><![CDATA[PARAM_ERROR]]></err_code><err_code_des><![CDATA[商户号不存在]]></err_code_des></xml>`,
		"missing-code-url": `<xml><return_code><![CDATA[SUCCESS]]></return_code><result_code><![CDATA[SUCCESS]]></result_code></xml>`,
		"garbage":          `not xml at all`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(body))
			}))
			defer server.Close()
			_, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
				TradeNo: "T6002", Amount: 100, NotifyURL: "https://pay.example/notify",
				Configuration: map[string]string{"mch_id": "m", "app_id": "a", "key": "qq-key", "api_url": server.URL},
			})
			if err == nil {
				t.Fatalf("accepted response %q", body)
			}
		})
	}
}

func TestCreatePaymentRequiresConfiguration(t *testing.T) {
	plugin := New(nil)
	for name, request := range map[string]pluginpayment.CreateRequest{
		"missing-everything": {TradeNo: "T1", Amount: 100},
		"missing-key":        {TradeNo: "T1", Amount: 100, Configuration: map[string]string{"mch_id": "m", "app_id": "a"}},
		"missing-notify-url": {TradeNo: "T1", Amount: 100, Configuration: map[string]string{"mch_id": "m", "app_id": "a", "key": "k"}},
		"zero-amount":        {TradeNo: "T1", Amount: 0, NotifyURL: "https://pay.example/notify", Configuration: map[string]string{"mch_id": "m", "app_id": "a", "key": "k"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := plugin.CreatePayment(context.Background(), request); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestCreatePaymentTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = writer.Write([]byte(`<xml></xml>`))
	}))
	defer server.Close()
	started := time.Now()
	_, err := New(&http.Client{Timeout: 30 * time.Millisecond}).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T6003", Amount: 100, NotifyURL: "https://pay.example/notify",
		Configuration: map[string]string{"mch_id": "m", "app_id": "a", "key": "qq-key", "api_url": server.URL},
	})
	if err == nil {
		t.Fatal("timeout not reported")
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("timeout not enforced, elapsed=%s", elapsed)
	}
}

func TestVerifyCallbackAcceptsSignedNotification(t *testing.T) {
	body := signedNotification(t, map[string]string{
		"return_code": "SUCCESS", "result_code": "SUCCESS", "mch_id": "1900000109",
		"out_trade_no": "T6001", "transaction_id": "QQ-9", "total_fee": "1999",
		"trade_type": "NATIVE", "time_end": "20261113180000",
	}, "qq-key")
	result, err := verifyBody(t, map[string]string{"key": "qq-key", "mch_id": "1900000109"}, body)
	if err != nil {
		t.Fatal(err)
	}
	if result.TradeNo != "T6001" || result.ChannelTradeNo != "QQ-9" || result.Amount != 1999 || !result.Paid {
		t.Fatalf("result=%+v", result)
	}
	if result.Raw["trade_type"] != "NATIVE" {
		t.Fatalf("raw=%+v", result.Raw)
	}
}

func TestVerifyCallbackRejectsBadSignatureAndAmounts(t *testing.T) {
	base := map[string]string{
		"return_code": "SUCCESS", "result_code": "SUCCESS", "mch_id": "1900000109",
		"out_trade_no": "T6001", "transaction_id": "QQ-9", "total_fee": "1999",
	}
	config := map[string]string{"key": "qq-key", "mch_id": "1900000109"}

	t.Run("forged-signature", func(t *testing.T) {
		fields := copyFields(base)
		fields["sign"] = "DEADBEEF"
		if _, err := verifyBody(t, config, encodeFields(t, fields)); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("amount-changed-after-signing", func(t *testing.T) {
		body := strings.Replace(signedNotification(t, base, "qq-key"), "<total_fee>1999</total_fee>", "<total_fee>1</total_fee>", 1)
		if _, err := verifyBody(t, config, body); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("signed-amount-surfaces-for-order-comparison", func(t *testing.T) {
		fields := copyFields(base)
		fields["total_fee"] = "1"
		result, err := verifyBody(t, config, signedNotification(t, fields, "qq-key"))
		if err != nil {
			t.Fatal(err)
		}
		if result.Amount != 1 {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("unpaid-result-code", func(t *testing.T) {
		fields := copyFields(base)
		fields["result_code"] = "FAIL"
		result, err := verifyBody(t, config, signedNotification(t, fields, "qq-key"))
		if err != nil {
			t.Fatal(err)
		}
		if result.Paid || result.Amount != 0 {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("merchant-mismatch", func(t *testing.T) {
		fields := copyFields(base)
		fields["mch_id"] = "9999999999"
		if _, err := verifyBody(t, config, signedNotification(t, fields, "qq-key")); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("missing-transaction-id", func(t *testing.T) {
		fields := copyFields(base)
		delete(fields, "transaction_id")
		if _, err := verifyBody(t, config, signedNotification(t, fields, "qq-key")); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("missing-key-configuration", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(signedNotification(t, base, "qq-key")))
		if _, err := New(nil).VerifyCallback(context.Background(), request, nil); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestMetadataAndConfigFields(t *testing.T) {
	plugin := New(nil)
	metadata := plugin.Metadata()
	if metadata.Code != "qqpay" || metadata.Name == "" || len(metadata.Capabilities) != 1 {
		t.Fatalf("metadata=%+v", metadata)
	}
	wanted := map[string]bool{"mch_id": true, "app_id": true, "key": true, "api_url": true}
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
	if secretFields != 1 {
		t.Fatalf("secret fields=%d", secretFields)
	}
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(New(nil)); err != nil {
		t.Fatalf("registry rejected the plugin: %v", err)
	}
	if _, err := registry.Resolve("qqpay", pluginpayment.CapabilityPayment); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

func verifyBody(t *testing.T, config map[string]string, body string) (pluginpayment.CallbackResult, error) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body))
	request.Header.Set("Content-Type", "text/xml")
	return New(nil).VerifyCallback(context.Background(), request, config)
}

func signedNotification(t *testing.T, fields map[string]string, key string) string {
	t.Helper()
	copied := copyFields(fields)
	copied["sign"] = signParams(copied, key)
	return encodeFields(t, copied)
}

func encodeFields(t *testing.T, fields map[string]string) string {
	t.Helper()
	body, err := encodeXML(fields)
	if err != nil {
		t.Fatalf("encode xml: %v", err)
	}
	return body
}

func copyFields(fields map[string]string) map[string]string {
	copied := make(map[string]string, len(fields))
	for name, value := range fields {
		copied[name] = value
	}
	return copied
}
