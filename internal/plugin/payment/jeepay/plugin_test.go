package jeepay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	pluginpayment "gorixpay/internal/plugin/payment"
)

func TestCreatePaymentSignsUnifiedOrderAndReturnsQRCode(t *testing.T) {
	var received map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != unifiedOrderPath {
			t.Errorf("path=%q", request.URL.Path)
		}
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		received = make(map[string]string, len(request.PostForm))
		for name := range request.PostForm {
			received[name] = request.PostForm.Get(name)
		}
		if !verify(received, "jeepay-key", received["sign"]) {
			t.Errorf("request signature does not verify: %+v", received)
		}
		if received["wayCode"] != "ALI_QR" || received["amount"] != "1999" || received["mchOrderNo"] != "T3001" {
			t.Errorf("params=%+v", received)
		}
		_, _ = writer.Write([]byte(`{"code":0,"msg":"SUCCESS","data":{"payOrderId":"P3001","payDataType":"codeUrl","payData":"https://qr.jeepay.example/abc"}}`))
	}))
	defer server.Close()

	action, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T3001", Amount: 1999, Description: "Jeepay 下单", PaymentType: "alipay", ClientIP: "203.0.113.9",
		NotifyURL: "https://pay.example/api/v1/payment/callback/jeepay/channel-1",
		Configuration: map[string]string{
			"api_url": server.URL, "mch_no": "M3001", "app_id": "A3001", "key": "jeepay-key",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionQRCode || action.URL != "https://qr.jeepay.example/abc" {
		t.Fatalf("action=%+v", action)
	}
	if received["version"] != "1.0" || received["signType"] != "MD5" || received["clientIp"] != "203.0.113.9" {
		t.Fatalf("params=%+v", received)
	}
}

func TestCreatePaymentMapsFormPayDataToHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"code":0,"msg":"SUCCESS","data":{"payOrderId":"P3002","payDataType":"form","payData":"<form action=\"https://bank.example\"></form>"}}`))
	}))
	defer server.Close()
	action, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T3002", Amount: 500, Configuration: map[string]string{
			"api_url": server.URL, "mch_no": "M3001", "app_id": "A3001", "key": "jeepay-key",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionHTML || !strings.Contains(action.Data.(string), "bank.example") {
		t.Fatalf("action=%+v", action)
	}
}

func TestCreatePaymentRejectsUpstreamRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"code":9999,"msg":"签名错误"}`))
	}))
	defer server.Close()
	_, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T3003", Amount: 500, Configuration: map[string]string{
			"api_url": server.URL, "mch_no": "M3001", "app_id": "A3001", "key": "jeepay-key",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "签名错误") {
		t.Fatalf("error=%v", err)
	}
}

func TestCreatePaymentRejectsGarbageAndMissingConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("not json"))
	}))
	defer server.Close()
	if _, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T3004", Amount: 500, Configuration: map[string]string{
			"api_url": server.URL, "mch_no": "M3001", "app_id": "A3001", "key": "jeepay-key",
		},
	}); err == nil {
		t.Fatal("garbage response accepted")
	}
	if _, err := New(nil).CreatePayment(context.Background(), pluginpayment.CreateRequest{TradeNo: "T3004", Amount: 500}); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
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
		TradeNo: "T3005", Amount: 500, Configuration: map[string]string{
			"api_url": server.URL, "mch_no": "M3001", "app_id": "A3001", "key": "jeepay-key",
		},
	}); err == nil {
		t.Fatal("timeout not reported")
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("timeout not enforced, elapsed=%s", elapsed)
	}
}

func TestVerifyCallbackAcceptsSignedNotification(t *testing.T) {
	body := jeepayNotification(map[string]any{
		"payOrderId": "P3001", "mchOrderNo": "T3001", "amount": 1999, "state": 2,
		"mchNo": "M3001", "appId": "A3001", "currency": "cny",
	})
	result, err := verifyBody(t, map[string]string{"key": "jeepay-key", "mch_no": "M3001"}, body)
	if err != nil {
		t.Fatal(err)
	}
	if result.TradeNo != "T3001" || result.ChannelTradeNo != "P3001" || result.Amount != 1999 || !result.Paid {
		t.Fatalf("result=%+v", result)
	}
	if result.Raw["state"] != "2" {
		t.Fatalf("raw=%+v", result.Raw)
	}
}

func TestVerifyCallbackRejectsBadSignatureAndAmounts(t *testing.T) {
	config := map[string]string{"key": "jeepay-key", "mch_no": "M3001"}
	t.Run("forged-signature", func(t *testing.T) {
		body := jeepayNotification(map[string]any{"payOrderId": "P3001", "mchOrderNo": "T3001", "amount": 1999, "state": 2, "mchNo": "M3001"})
		tampered := strings.Replace(body, `"sign":"`, `"sign":"0`, 1)
		if _, err := verifyBody(t, config, tampered); err == nil {
			t.Fatal("forged signature accepted")
		}
	})
	t.Run("amount-changed-after-signing", func(t *testing.T) {
		body := strings.Replace(jeepayNotification(map[string]any{"payOrderId": "P3001", "mchOrderNo": "T3001", "amount": 1999, "state": 2, "mchNo": "M3001"}), `"amount":"1999"`, `"amount":1`, 1)
		if _, err := verifyBody(t, config, body); err == nil {
			t.Fatal("tampered amount accepted")
		}
	})
	t.Run("signed-amount-surfaces-for-order-comparison", func(t *testing.T) {
		body := jeepayNotification(map[string]any{"payOrderId": "P3001", "mchOrderNo": "T3001", "amount": 1, "state": 2, "mchNo": "M3001"})
		result, err := verifyBody(t, config, body)
		if err != nil {
			t.Fatal(err)
		}
		if result.Amount != 1 {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("wrong-mch-no", func(t *testing.T) {
		body := jeepayNotification(map[string]any{"payOrderId": "P3001", "mchOrderNo": "T3001", "amount": 1999, "state": 2, "mchNo": "OTHER"})
		if _, err := verifyBody(t, config, body); err == nil {
			t.Fatal("foreign mchNo accepted")
		}
	})
	t.Run("unpaid-state", func(t *testing.T) {
		body := jeepayNotification(map[string]any{"payOrderId": "P3001", "mchOrderNo": "T3001", "amount": 1999, "state": 3, "mchNo": "M3001"})
		result, err := verifyBody(t, config, body)
		if err != nil {
			t.Fatal(err)
		}
		if result.Paid || result.Amount != 0 {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("missing-key-configuration", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader("{}"))
		if _, err := New(nil).VerifyCallback(context.Background(), request, nil); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestMetadataAndConfigFields(t *testing.T) {
	plugin := New(nil)
	if metadata := plugin.Metadata(); metadata.Code != "jeepay" || len(metadata.Capabilities) != 1 {
		t.Fatalf("metadata=%+v", metadata)
	}
	wanted := map[string]bool{"api_url": true, "mch_no": true, "app_id": true, "key": true}
	for _, field := range plugin.ConfigFields() {
		delete(wanted, field.Key)
	}
	if len(wanted) != 0 {
		t.Fatalf("missing fields=%v", wanted)
	}
}

func verifyBody(t *testing.T, config map[string]string, body string) (pluginpayment.CallbackResult, error) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body))
	return New(nil).VerifyCallback(context.Background(), request, config)
}

// jeepayNotification renders a signed JSON notification.
func jeepayNotification(fields map[string]any) string {
	values := make(map[string]string, len(fields))
	for name, value := range fields {
		switch typed := value.(type) {
		case int:
			values[name] = strconv.Itoa(typed)
		case string:
			values[name] = typed
		}
	}
	values["sign"] = sign(values, "jeepay-key")
	encoded, err := json.Marshal(values)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// The constant-time comparison normalizes case and trims padding; a gateway that
// sends a lower-case or space padded signature must still verify, while a
// truncated or wrong-key signature must not.
func TestVerifyAcceptsLowercaseAndPaddedSignature(t *testing.T) {
	values := map[string]string{"payOrderId": "P-1", "mchOrderNo": "T1", "amount": "100", "state": "2"}
	expected := sign(values, "jeepay-key")
	if !verify(values, "jeepay-key", expected) {
		t.Fatal("canonical signature rejected")
	}
	if !verify(values, "jeepay-key", strings.ToLower(expected)) {
		t.Fatal("lower-case signature rejected")
	}
	if !verify(values, "jeepay-key", "  "+expected+"  ") {
		t.Fatal("padded signature rejected")
	}
	if verify(values, "jeepay-key", expected[:len(expected)-1]) {
		t.Fatal("truncated signature accepted")
	}
	if verify(values, "other-key", expected) {
		t.Fatal("signature verified with the wrong key")
	}
	if verify(values, "jeepay-key", "") {
		t.Fatal("empty signature accepted")
	}
}
