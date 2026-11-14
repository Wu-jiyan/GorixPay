package usdt

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

	pluginpayment "gorixpay/internal/plugin/payment"
)

func TestCreatePaymentSignsEpusdtRequestAndReturnsJump(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != createPath {
			t.Errorf("path=%q", request.URL.Path)
		}
		var body createRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body.OrderID != "T5001" || body.Amount != "19.99" || body.NotifyURL != "https://pay.example/notify" {
			t.Errorf("body=%+v", body)
		}
		if body.Sign != createSign("T5001", "19.99", "usdt-key") {
			t.Errorf("sign=%q", body.Sign)
		}
		_, _ = writer.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"TR-1","order_id":"T5001","amount":"19.99","actual_amount":"19.99","token":"USDT","payment_url":"https://usdt.example/pay/TR-1"}}`))
	}))
	defer server.Close()

	action, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T5001", Amount: 1999, PaymentType: "usdt",
		NotifyURL:     "https://pay.example/notify",
		Configuration: map[string]string{"api_url": server.URL, "key": "usdt-key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionJump || action.URL != "https://usdt.example/pay/TR-1" {
		t.Fatalf("action=%+v", action)
	}
}

func TestCreatePaymentCanReturnQRCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"status_code":200,"data":{"payment_url":"https://usdt.example/qr/TR-2"}}`))
	}))
	defer server.Close()
	action, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T5002", Amount: 500,
		Configuration: map[string]string{"api_url": server.URL, "key": "usdt-key", "action": "qrcode"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionQRCode || action.URL != "https://usdt.example/qr/TR-2" {
		t.Fatalf("action=%+v", action)
	}
}

func TestCreatePaymentRejectsUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"status_code":401,"message":"sign error","data":{}}`))
	}))
	defer server.Close()
	_, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T5003", Amount: 500, Configuration: map[string]string{"api_url": server.URL, "key": "usdt-key"},
	})
	if err == nil || !strings.Contains(err.Error(), "sign error") {
		t.Fatalf("error=%v", err)
	}
}

func TestCreatePaymentRejectsGarbageAndMissingConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("oops"))
	}))
	defer server.Close()
	if _, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T5004", Amount: 500, Configuration: map[string]string{"api_url": server.URL, "key": "usdt-key"},
	}); err == nil {
		t.Fatal("garbage response accepted")
	}
	if _, err := New(nil).CreatePayment(context.Background(), pluginpayment.CreateRequest{TradeNo: "T5004", Amount: 500}); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
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
		TradeNo: "T5005", Amount: 500, Configuration: map[string]string{"api_url": server.URL, "key": "usdt-key"},
	}); err == nil {
		t.Fatal("timeout not reported")
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("timeout not enforced, elapsed=%s", elapsed)
	}
}

func TestVerifyCallbackAcceptsJSONNotification(t *testing.T) {
	body := usdtJSONNotification("TR-1", "T5001", "19.99", "2")
	result, err := verifyBody(t, map[string]string{"key": "usdt-key"}, body)
	if err != nil {
		t.Fatal(err)
	}
	if result.TradeNo != "T5001" || result.ChannelTradeNo != "TR-1" || result.Amount != 1999 || !result.Paid {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerifyCallbackAcceptsFormNotification(t *testing.T) {
	form := url.Values{
		"trade_id": {"TR-1"}, "order_id": {"T5001"}, "amount": {"19.99"},
		"actual_amount": {"19.99"}, "status": {"2"},
	}
	form.Set("sign", notifySign("TR-1", "T5001", "19.99", "2", "usdt-key"))
	request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	result, err := New(nil).VerifyCallback(context.Background(), request, map[string]string{"key": "usdt-key"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Paid || result.Amount != 1999 {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerifyCallbackRejectsBadSignatureAndAmounts(t *testing.T) {
	config := map[string]string{"key": "usdt-key"}
	t.Run("forged-signature", func(t *testing.T) {
		body := usdtJSONNotification("TR-1", "T5001", "19.99", "2")
		tampered := strings.Replace(body, `"sign":"`, `"sign":"0`, 1)
		if _, err := verifyBody(t, config, tampered); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("amount-changed-after-signing", func(t *testing.T) {
		body := strings.Replace(usdtJSONNotification("TR-1", "T5001", "19.99", "2"), `"amount":"19.99"`, `"amount":"0.01"`, 1)
		if _, err := verifyBody(t, config, body); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("signed-amount-surfaces-for-order-comparison", func(t *testing.T) {
		result, err := verifyBody(t, config, usdtJSONNotification("TR-1", "T5001", "0.01", "2"))
		if err != nil {
			t.Fatal(err)
		}
		if result.Amount != 1 {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("unpaid-status", func(t *testing.T) {
		result, err := verifyBody(t, config, usdtJSONNotification("TR-1", "T5001", "19.99", "1"))
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

func TestVerifyCallbackBindsStatusToTheSignature(t *testing.T) {
	config := map[string]string{"key": "usdt-key"}

	// The attack this pins down: a notification signed for an unpaid order
	// (status=1) is replayed with status flipped to paid (status=2).
	t.Run("unpaid-flipped-to-paid", func(t *testing.T) {
		signed := usdtJSONNotification("TR-1", "T5001", "19.99", "1")
		tampered := strings.Replace(signed, `"status":"1"`, `"status":"2"`, 1)
		if _, err := verifyBody(t, config, tampered); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("tampered status accepted: %v", err)
		}
	})
	t.Run("paid-flipped-to-unpaid", func(t *testing.T) {
		signed := usdtJSONNotification("TR-1", "T5001", "19.99", "2")
		tampered := strings.Replace(signed, `"status":"2"`, `"status":"3"`, 1)
		if _, err := verifyBody(t, config, tampered); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("tampered status accepted: %v", err)
		}
	})
	t.Run("trade-id-changed-after-signing", func(t *testing.T) {
		signed := usdtJSONNotification("TR-1", "T5001", "19.99", "2")
		tampered := strings.Replace(signed, `"trade_id":"TR-1"`, `"trade_id":"TR-9"`, 1)
		if _, err := verifyBody(t, config, tampered); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("tampered trade_id accepted: %v", err)
		}
	})
	// A create-request signature (order_id + amount + key) never authenticated a
	// notification: it carries neither trade_id nor status.
	t.Run("create-signature-is-not-accepted", func(t *testing.T) {
		fields := map[string]string{
			"trade_id": "TR-1", "order_id": "T5001", "amount": "19.99",
			"actual_amount": "19.99", "status": "2",
			"sign": createSign("T5001", "19.99", "usdt-key"),
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := verifyBody(t, config, string(encoded)); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("create-style signature accepted: %v", err)
		}
	})
}

func TestMetadataAndConfigFields(t *testing.T) {
	plugin := New(nil)
	if metadata := plugin.Metadata(); metadata.Code != "usdt" || len(metadata.Capabilities) != 1 {
		t.Fatalf("metadata=%+v", metadata)
	}
	wanted := map[string]bool{"api_url": true, "key": true, "action": true, "notify_url": true}
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
	request.Header.Set("Content-Type", "application/json")
	return New(nil).VerifyCallback(context.Background(), request, config)
}

func usdtJSONNotification(tradeID, orderID, amount, status string) string {
	fields := map[string]string{
		"trade_id": tradeID, "order_id": orderID, "amount": amount,
		"actual_amount": amount, "token": "USDT", "status": status,
		"sign": notifySign(tradeID, orderID, amount, status, "usdt-key"),
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
