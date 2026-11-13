package alipay

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
	"gorixpay/internal/plugin/payment/paymenttest"
)

func TestCreatePaymentSignsPrecreateRequest(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	var checked bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		params := make(map[string]any, len(request.PostForm))
		for name := range request.PostForm {
			params[name] = request.PostForm.Get(name)
		}
		if request.PostForm.Get("method") != "alipay.trade.precreate" || request.PostForm.Get("sign_type") != "RSA2" {
			t.Errorf("unexpected form: %+v", request.PostForm)
		}
		if !signing.VerifyRSA(params, &merchant.Private.PublicKey, request.PostForm.Get("sign")) {
			t.Errorf("request signature does not verify")
		}
		var bizContent map[string]string
		if err := json.Unmarshal([]byte(request.PostForm.Get("biz_content")), &bizContent); err != nil {
			t.Errorf("biz_content: %v", err)
		}
		if bizContent["out_trade_no"] != "T1001" || bizContent["total_amount"] != "12.34" || bizContent["subject"] != "测试订单" {
			t.Errorf("biz_content=%+v", bizContent)
		}
		if request.PostForm.Get("notify_url") != "https://pay.example/api/v1/payment/callback/alipay/channel-1" {
			t.Errorf("notify_url=%q", request.PostForm.Get("notify_url"))
		}
		checked = true
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"alipay_trade_precreate_response":{"code":"10000","msg":"Success","out_trade_no":"T1001","qr_code":"https://qr.alipay.com/abc123"}}`))
	}))
	defer server.Close()

	plugin := New(server.Client())
	action, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T1001", Amount: 1234, Description: "测试订单", PaymentType: "alipay",
		NotifyURL: "https://pay.example/api/v1/payment/callback/alipay/channel-1",
		Configuration: map[string]string{
			"app_id": "2021000000000001", "private_key": merchant.PrivatePEM, "gateway_url": server.URL,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("mock gateway was never called")
	}
	if action.Type != pluginpayment.ActionQRCode || action.URL != "https://qr.alipay.com/abc123" {
		t.Fatalf("action=%+v", action)
	}
	if err := action.Validate(); err != nil {
		t.Fatalf("action invalid: %v", err)
	}
}

func TestCreatePaymentRejectsUpstreamRejection(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"alipay_trade_precreate_response":{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_NOT_EXIST","sub_msg":"交易不存在"}}`))
	}))
	defer server.Close()
	plugin := New(server.Client())
	_, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T1", Amount: 100, Configuration: map[string]string{"app_id": "app", "private_key": merchant.PrivatePEM, "gateway_url": server.URL},
	})
	if err == nil || !strings.Contains(err.Error(), "40004") && !strings.Contains(err.Error(), "Business Failed") {
		t.Fatalf("error=%v", err)
	}
}

func TestCreatePaymentRejectsGarbageResponse(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("<html>bad gateway</html>"))
	}))
	defer server.Close()
	plugin := New(server.Client())
	if _, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T1", Amount: 100, Configuration: map[string]string{"app_id": "app", "private_key": merchant.PrivatePEM, "gateway_url": server.URL},
	}); err == nil {
		t.Fatal("garbage response accepted")
	}
}

func TestCreatePaymentTimesOut(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer server.Close()
	plugin := New(&http.Client{Timeout: 30 * time.Millisecond})
	started := time.Now()
	_, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T1", Amount: 100, Configuration: map[string]string{"app_id": "app", "private_key": merchant.PrivatePEM, "gateway_url": server.URL},
	})
	if err == nil {
		t.Fatal("timeout not reported")
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("timeout not enforced, elapsed=%s", elapsed)
	}
}

func TestCreatePaymentRequiresConfiguration(t *testing.T) {
	plugin := New(nil)
	for name, configuration := range map[string]map[string]string{
		"missing-everything": {},
		"missing-key":        {"app_id": "app"},
		"blank-key":          {"app_id": "app", "private_key": "   "},
		"broken-key":         {"app_id": "app", "private_key": "not a pem"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{TradeNo: "T1", Amount: 100, Configuration: configuration})
			if !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestVerifyCallbackAcceptsSignedNotification(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	form := alipayNotification("T1001", "ALIPAY-9", "12.34", "TRADE_SUCCESS", platform)
	request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	result, err := New(nil).VerifyCallback(context.Background(), request, map[string]string{
		"alipay_public_key": platform.PublicPEM, "app_id": "2021000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TradeNo != "T1001" || result.ChannelTradeNo != "ALIPAY-9" || result.Amount != 1234 || !result.Paid {
		t.Fatalf("result=%+v", result)
	}
	if result.Raw["trade_status"] != "TRADE_SUCCESS" || result.Raw["sign"] == "" {
		t.Fatalf("raw=%+v", result.Raw)
	}
}

func TestVerifyCallbackRejectsTamperedNotification(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	config := map[string]string{"alipay_public_key": platform.PublicPEM, "app_id": "2021000000000001"}

	t.Run("amount-changed-after-signing", func(t *testing.T) {
		form := alipayNotification("T1001", "ALIPAY-9", "12.34", "TRADE_SUCCESS", platform)
		form.Set("total_amount", "0.01")
		if _, err := verifyForm(t, config, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("forged-signature", func(t *testing.T) {
		form := alipayNotification("T1001", "ALIPAY-9", "12.34", "TRADE_SUCCESS", platform)
		form.Set("sign", "Zm9yZ2Vk")
		if _, err := verifyForm(t, config, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("signed-with-another-key", func(t *testing.T) {
		merchant := paymenttest.MustMerchantKeys(t)
		form := alipayNotification("T1001", "ALIPAY-9", "12.34", "TRADE_SUCCESS", merchant)
		if _, err := verifyForm(t, config, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("missing-signature", func(t *testing.T) {
		form := alipayNotification("T1001", "ALIPAY-9", "12.34", "TRADE_SUCCESS", platform)
		form.Del("sign")
		if _, err := verifyForm(t, config, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("application-mismatch", func(t *testing.T) {
		form := alipayNotification("T1001", "ALIPAY-9", "12.34", "TRADE_SUCCESS", platform)
		form.Set("app_id", "2021999999999999")
		form.Set("sign", signAlipay(form, platform))
		if _, err := verifyForm(t, config, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestVerifyCallbackReportsUnpaidTrade(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	form := alipayNotification("T1001", "ALIPAY-9", "12.34", "WAIT_BUYER_PAY", platform)
	result, err := verifyForm(t, map[string]string{"alipay_public_key": platform.PublicPEM}, form)
	if err != nil {
		t.Fatal(err)
	}
	if result.Paid {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerifyCallbackRequiresPublicKey(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	form := alipayNotification("T1001", "ALIPAY-9", "12.34", "TRADE_SUCCESS", platform)
	request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := New(nil).VerifyCallback(context.Background(), request, nil); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
		t.Fatalf("error=%v", err)
	}
}

func TestMetadataAndConfigFields(t *testing.T) {
	plugin := New(nil)
	metadata := plugin.Metadata()
	if metadata.Code != "alipay" || metadata.Name == "" || len(metadata.Capabilities) != 1 {
		t.Fatalf("metadata=%+v", metadata)
	}
	fields := plugin.ConfigFields()
	wanted := map[string]bool{"app_id": true, "private_key": true, "alipay_public_key": true, "gateway_url": true}
	for _, field := range fields {
		delete(wanted, field.Key)
		if field.Label == "" || field.Type == "" {
			t.Errorf("field=%+v", field)
		}
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

// alipayNotification builds a notification signed with the platform key the way
// Alipay does it: sorted form values, no sign/sign_type, RSA2.
func alipayNotification(tradeNo, channelTradeNo, amount, status string, platform paymenttest.Pair) url.Values {
	values := url.Values{
		"app_id":       {"2021000000000001"},
		"charset":      {"utf-8"},
		"notify_time":  {"2026-11-13 18:00:00"},
		"notify_type":  {"trade_status_sync"},
		"out_trade_no": {tradeNo},
		"total_amount": {amount},
		"trade_no":     {channelTradeNo},
		"trade_status": {status},
		"sign_type":    {"RSA2"},
	}
	values.Set("sign", signAlipay(values, platform))
	return values
}

func signAlipay(values url.Values, platform paymenttest.Pair) string {
	params := make(map[string]any, len(values))
	for name := range values {
		params[name] = values.Get(name)
	}
	signature, err := signing.SignRSA(params, platform.Private)
	if err != nil {
		panic(err)
	}
	return signature
}
