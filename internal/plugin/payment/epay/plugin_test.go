package epay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

func TestPluginCreatesSignedUpstreamPayment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/mapi.php" || request.Method != http.MethodPost {
			t.Errorf("request=%s %s", request.Method, request.URL.Path)
		}
		if err := request.ParseForm(); err != nil {
			t.Error(err)
		}
		params := make(map[string]any, len(request.Form))
		for name := range request.Form {
			params[name] = request.Form.Get(name)
		}
		if request.Form.Get("pid") != "1001" || request.Form.Get("out_trade_no") != "2026071400000012345" || request.Form.Get("money") != "1.01" {
			t.Errorf("form=%v", request.Form)
		}
		if !signing.VerifyMD5(params, "secret", request.Form.Get("sign")) {
			t.Error("invalid upstream signature")
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"code": 1, "trade_no": "UPSTREAM-1", "pay_type": "qrcode", "pay_info": "https://pay.example/qr"})
	}))
	defer server.Close()
	action, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "2026071400000012345", Amount: 101, Currency: "CNY", Description: "test", NotifyURL: "https://gorix.example/callback",
		ReturnURL: "https://merchant.example/return", PaymentType: "alipay",
		Configuration: map[string]string{"api_url": server.URL, "pid": "1001", "key": "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionQRCode || action.URL != "https://pay.example/qr" {
		t.Fatalf("action=%+v", action)
	}
}

func TestPluginRejectsFailedAndOversizedResponses(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"failed": func(writer http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": -1, "msg": "upstream rejected"})
		},
		"oversized": func(writer http.ResponseWriter, _ *http.Request) {
			for range make([]byte, maxResponseBytes+1) {
				_, _ = writer.Write([]byte("x"))
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			_, err := New(server.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
				TradeNo: "1", Amount: 100, Currency: "CNY", Description: "test", PaymentType: "alipay",
				Configuration: map[string]string{"api_url": server.URL, "pid": "1001", "key": "secret"},
			})
			if err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestNewUsesTimeoutClientAndKeepsInjectedOne(t *testing.T) {
	plugin := New(nil)
	if plugin.client == nil || plugin.client == http.DefaultClient {
		t.Fatal("nil client did not get the shared timeout client")
	}
	if plugin.client.Timeout != pluginpayment.DefaultUpstreamTimeout {
		t.Fatalf("timeout=%s", plugin.client.Timeout)
	}
	custom := &http.Client{Timeout: time.Second}
	if New(custom).client != custom {
		t.Fatal("injected client was replaced")
	}
}
