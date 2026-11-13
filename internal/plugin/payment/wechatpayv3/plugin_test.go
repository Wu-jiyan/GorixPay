package wechatpayv3

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	pluginpayment "gorixpay/internal/plugin/payment"
	"gorixpay/internal/plugin/payment/paymenttest"
)

const testAPIV3Key = "0123456789abcdef0123456789abcdef"

func TestCreateNativePaymentSignsRequestAndReturnsQRCode(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != nativePath {
			t.Errorf("path=%q", request.URL.Path)
		}
		header := request.Header.Get("Authorization")
		if !strings.HasPrefix(header, "WECHATPAY2-SHA256-RSA2048 ") {
			t.Errorf("authorization=%q", header)
		}
		fields := parseAuthorization(t, header)
		if fields["mchid"] != "1900000001" || fields["serial_no"] != "SERIAL-1" {
			t.Errorf("fields=%+v", fields)
		}
		body := readBody(t, request)
		if !verifyMessage(&merchant.Private.PublicKey, requestMessage(http.MethodPost, nativePath, fields["timestamp"], fields["nonce_str"], body), fields["signature"]) {
			t.Errorf("request signature does not verify")
		}
		if !strings.Contains(body, `"out_trade_no":"T2001"`) || !strings.Contains(body, `"total":1999`) {
			t.Errorf("body=%s", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code_url":"weixin://wxpay/bizpayurl?pr=abc123"}`))
	}))
	defer server.Close()

	plugin := New(server.Client())
	action, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T2001", Amount: 1999, Currency: "CNY", Description: "微信下单", PaymentType: "wechatpayv3_native",
		NotifyURL: "https://pay.example/api/v1/payment/callback/wechatpayv3/channel-1",
		Configuration: map[string]string{
			"mch_id": "1900000001", "app_id": "wxappid", "serial_no": "SERIAL-1",
			"private_key": merchant.PrivatePEM, "api_v3_key": testAPIV3Key, "api_base_url": server.URL,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionQRCode || action.URL != "weixin://wxpay/bizpayurl?pr=abc123" {
		t.Fatalf("action=%+v", action)
	}
}

func TestCreateJSAPIPaymentReturnsSignedBridgeParameters(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != jsapiPath {
			t.Errorf("path=%q", request.URL.Path)
		}
		body := readBody(t, request)
		if !strings.Contains(body, `"payer":{"openid":"oUser-1"}`) {
			t.Errorf("body=%s", body)
		}
		_, _ = writer.Write([]byte(`{"prepay_id":"wx20110101"}`))
	}))
	defer server.Close()

	plugin := New(server.Client())
	action, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T2002", Amount: 500, PaymentType: "wechatpayv3_jsapi", Extra: map[string]string{"openid": "oUser-1"},
		Configuration: map[string]string{
			"mch_id": "1900000001", "app_id": "wxappid", "serial_no": "SERIAL-1",
			"private_key": merchant.PrivatePEM, "api_v3_key": testAPIV3Key, "api_base_url": server.URL,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionJSAPI {
		t.Fatalf("action=%+v", action)
	}
	data, ok := action.Data.(map[string]string)
	if !ok {
		t.Fatalf("data=%T", action.Data)
	}
	for _, key := range []string{"appId", "timeStamp", "nonceStr", "package", "signType", "paySign"} {
		if data[key] == "" {
			t.Errorf("missing %s in %+v", key, data)
		}
	}
	if data["package"] != "prepay_id=wx20110101" || data["signType"] != "RSA" {
		t.Fatalf("data=%+v", data)
	}
	message := data["appId"] + "\n" + data["timeStamp"] + "\n" + data["nonceStr"] + "\n" + data["package"] + "\n"
	if !verifyMessage(&merchant.Private.PublicKey, message, data["paySign"]) {
		t.Fatal("paySign does not verify")
	}
}

func TestCreateJSAPIRequiresOpenID(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	plugin := New(nil)
	_, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T2003", Amount: 500, PaymentType: "jsapi",
		Configuration: map[string]string{
			"mch_id": "1900000001", "app_id": "wxappid", "serial_no": "SERIAL-1",
			"private_key": merchant.PrivatePEM, "api_v3_key": testAPIV3Key,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "openid") {
		t.Fatalf("error=%v", err)
	}
}

func TestCreateH5PaymentReturnsJumpAction(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != h5Path {
			t.Errorf("path=%q", request.URL.Path)
		}
		body := readBody(t, request)
		if !strings.Contains(body, `"payer_client_ip":"203.0.113.7"`) || !strings.Contains(body, `"h5_info":{"type":"Wap"}`) {
			t.Errorf("body=%s", body)
		}
		_, _ = writer.Write([]byte(`{"h5_url":"https://wx.tenpay.com/cgi-bin/mmpayweb-bin/checkmweb?prepay_id=wx1"}`))
	}))
	defer server.Close()
	plugin := New(server.Client())
	action, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T2004", Amount: 700, PaymentType: "h5", ClientIP: "203.0.113.7",
		Configuration: map[string]string{
			"mch_id": "1900000001", "app_id": "wxappid", "serial_no": "SERIAL-1",
			"private_key": merchant.PrivatePEM, "api_v3_key": testAPIV3Key, "api_base_url": server.URL,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionJump || !strings.HasPrefix(action.URL, "https://wx.tenpay.com/") {
		t.Fatalf("action=%+v", action)
	}
}

func TestCreatePaymentRejectsUpstreamError(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"code":"PARAM_ERROR","message":"商户号不存在"}`))
	}))
	defer server.Close()
	plugin := New(server.Client())
	_, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T2005", Amount: 100,
		Configuration: map[string]string{
			"mch_id": "1900000001", "app_id": "wxappid", "serial_no": "SERIAL-1",
			"private_key": merchant.PrivatePEM, "api_v3_key": testAPIV3Key, "api_base_url": server.URL,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "PARAM_ERROR") {
		t.Fatalf("error=%v", err)
	}
}

func TestCreatePaymentRejectsGarbageResponse(t *testing.T) {
	merchant := paymenttest.MustMerchantKeys(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"unexpected":true}`))
	}))
	defer server.Close()
	plugin := New(server.Client())
	if _, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T2006", Amount: 100,
		Configuration: map[string]string{
			"mch_id": "1900000001", "app_id": "wxappid", "serial_no": "SERIAL-1",
			"private_key": merchant.PrivatePEM, "api_v3_key": testAPIV3Key, "api_base_url": server.URL,
		},
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
	if _, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "T2007", Amount: 100,
		Configuration: map[string]string{
			"mch_id": "1900000001", "app_id": "wxappid", "serial_no": "SERIAL-1",
			"private_key": merchant.PrivatePEM, "api_v3_key": testAPIV3Key, "api_base_url": server.URL,
		},
	}); err == nil {
		t.Fatal("timeout not reported")
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("timeout not enforced, elapsed=%s", elapsed)
	}
}

func TestVerifyCallbackDecryptsSignedNotification(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	request := signedNotification(t, platform, testAPIV3Key, "TRANSACTION.SUCCESS", transactionPayload("T2001", "4200001", "SUCCESS", 1999), time.Now())
	result, err := New(nil).VerifyCallback(context.Background(), request, callbackConfig(platform))
	if err != nil {
		t.Fatal(err)
	}
	if result.TradeNo != "T2001" || result.ChannelTradeNo != "4200001" || result.Amount != 1999 || !result.Paid {
		t.Fatalf("result=%+v", result)
	}
	if result.Raw["trade_state"] != "SUCCESS" || result.Raw["event_type"] != "TRANSACTION.SUCCESS" {
		t.Fatalf("raw=%+v", result.Raw)
	}
}

func TestVerifyCallbackRejectsTamperedOrStaleNotifications(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	config := callbackConfig(platform)

	t.Run("body-tampered", func(t *testing.T) {
		request := signedNotification(t, platform, testAPIV3Key, "TRANSACTION.SUCCESS", transactionPayload("T2001", "4200001", "SUCCESS", 1999), time.Now())
		body := readBody(t, request)
		tampered := strings.Replace(body, `"id":"EV-1"`, `"id":"EV-2"`, 1)
		if tampered == body {
			t.Fatal("notification body not tampered")
		}
		request.Body = io.NopCloser(strings.NewReader(tampered))
		if _, err := New(nil).VerifyCallback(context.Background(), request, config); err == nil {
			t.Fatal("tampered body accepted")
		}
	})
	t.Run("forged-signature", func(t *testing.T) {
		request := signedNotification(t, platform, testAPIV3Key, "TRANSACTION.SUCCESS", transactionPayload("T2001", "4200001", "SUCCESS", 1999), time.Now())
		request.Header.Set("Wechatpay-Signature", "Zm9yZ2Vk")
		if _, err := New(nil).VerifyCallback(context.Background(), request, config); err == nil {
			t.Fatal("forged signature accepted")
		}
	})
	t.Run("signed-by-another-key", func(t *testing.T) {
		merchant := paymenttest.MustMerchantKeys(t)
		request := signedNotification(t, merchant, testAPIV3Key, "TRANSACTION.SUCCESS", transactionPayload("T2001", "4200001", "SUCCESS", 1999), time.Now())
		if _, err := New(nil).VerifyCallback(context.Background(), request, config); err == nil {
			t.Fatal("foreign signature accepted")
		}
	})
	t.Run("stale-timestamp", func(t *testing.T) {
		request := signedNotification(t, platform, testAPIV3Key, "TRANSACTION.SUCCESS", transactionPayload("T2001", "4200001", "SUCCESS", 1999), time.Now().Add(-time.Hour))
		if _, err := New(nil).VerifyCallback(context.Background(), request, config); err == nil {
			t.Fatal("stale notification accepted")
		}
	})
	t.Run("missing-headers", func(t *testing.T) {
		request := signedNotification(t, platform, testAPIV3Key, "TRANSACTION.SUCCESS", transactionPayload("T2001", "4200001", "SUCCESS", 1999), time.Now())
		request.Header.Del("Wechatpay-Nonce")
		if _, err := New(nil).VerifyCallback(context.Background(), request, config); err == nil {
			t.Fatal("missing header accepted")
		}
	})
	t.Run("wrong-api-v3-key", func(t *testing.T) {
		request := signedNotification(t, platform, testAPIV3Key, "TRANSACTION.SUCCESS", transactionPayload("T2001", "4200001", "SUCCESS", 1999), time.Now())
		broken := callbackConfig(platform)
		broken["api_v3_key"] = "ffffffffffffffffffffffffffffffff"
		if _, err := New(nil).VerifyCallback(context.Background(), request, broken); err == nil {
			t.Fatal("undecryptable resource accepted")
		}
	})
}

func TestVerifyCallbackReportsPayloadAmountForHandlerComparison(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	// The signature and the envelope are valid but the payload states 1 cent:
	// the plugin must surface it so the handler can reject the mismatch.
	request := signedNotification(t, platform, testAPIV3Key, "TRANSACTION.SUCCESS", transactionPayload("T2001", "4200001", "SUCCESS", 1), time.Now())
	result, err := New(nil).VerifyCallback(context.Background(), request, callbackConfig(platform))
	if err != nil {
		t.Fatal(err)
	}
	if result.Amount != 1 || result.Amount == 1999 {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerifyCallbackReportsUnpaidEvents(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	request := signedNotification(t, platform, testAPIV3Key, "TRANSACTION.CLOSED", transactionPayload("T2001", "4200001", "CLOSED", 0), time.Now())
	result, err := New(nil).VerifyCallback(context.Background(), request, callbackConfig(platform))
	if err != nil {
		t.Fatal(err)
	}
	if result.Paid || result.Amount != 0 {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerifyCallbackRequiresPlatformKey(t *testing.T) {
	platform := paymenttest.MustPlatformKeys(t)
	request := signedNotification(t, platform, testAPIV3Key, "TRANSACTION.SUCCESS", transactionPayload("T2001", "4200001", "SUCCESS", 1999), time.Now())
	if _, err := New(nil).VerifyCallback(context.Background(), request, map[string]string{"api_v3_key": testAPIV3Key}); err == nil {
		t.Fatal("missing platform key accepted")
	}
}

func TestMetadataAndConfigFields(t *testing.T) {
	plugin := New(nil)
	if metadata := plugin.Metadata(); metadata.Code != "wechatpayv3" || len(metadata.Capabilities) != 1 {
		t.Fatalf("metadata=%+v", metadata)
	}
	wanted := map[string]bool{"mch_id": true, "app_id": true, "serial_no": true, "private_key": true, "api_v3_key": true}
	for _, field := range plugin.ConfigFields() {
		delete(wanted, field.Key)
	}
	if len(wanted) != 0 {
		t.Fatalf("missing fields=%v", wanted)
	}
}

func callbackConfig(platform paymenttest.Pair) map[string]string {
	return map[string]string{
		"mch_id": "1900000001", "app_id": "wxappid", "serial_no": "SERIAL-1",
		"api_v3_key": testAPIV3Key, "platform_public_key": platform.PublicPEM,
	}
}

func transactionPayload(tradeNo, transactionID, state string, total int64) map[string]any {
	return map[string]any{
		"appid": "wxappid", "mchid": "1900000001", "out_trade_no": tradeNo,
		"transaction_id": transactionID, "trade_state": state,
		"amount": map[string]any{"total": total, "payer_total": total, "currency": "CNY"},
	}
}

// signedNotification encrypts the payload, wraps it in the APIv3 envelope and
// signs the exact body the way the WeChat Pay platform does.
func signedNotification(t *testing.T, platform paymenttest.Pair, apiV3Key, eventType string, payload map[string]any, issuedAt time.Time) *http.Request {
	t.Helper()
	plaintext, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	resourceNonce := "resourcenonc"
	associatedData := "transaction"
	ciphertext, err := encryptResource(apiV3Key, plaintext, resourceNonce, associatedData)
	if err != nil {
		t.Fatalf("encrypt resource: %v", err)
	}
	body := paymenttest.JSONBody(t, map[string]any{
		"id": "EV-1", "event_type": eventType, "resource_type": "encrypt-resource",
		"resource": map[string]any{
			"algorithm": "AEAD_AES_256_GCM", "ciphertext": ciphertext, "nonce": resourceNonce,
			"associated_data": associatedData, "original_type": "transaction",
		},
	})
	timestamp := strconv.FormatInt(issuedAt.Unix(), 10)
	headerNonce := "headernonce"
	signature, err := signMessage(platform.Private, notificationMessage(timestamp, headerNonce, body))
	if err != nil {
		t.Fatalf("sign notification: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/payment/callback/wechatpayv3/channel-1", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Wechatpay-Timestamp", timestamp)
	request.Header.Set("Wechatpay-Nonce", headerNonce)
	request.Header.Set("Wechatpay-Signature", signature)
	request.Header.Set("Wechatpay-Serial", "PLATFORM-SERIAL")
	return request
}

// encryptResource is the platform-side half of decryptResource.
func encryptResource(apiV3Key string, plaintext []byte, nonce, associatedData string) (string, error) {
	block, err := aes.NewCipher([]byte(apiV3Key))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nil, []byte(nonce), plaintext, []byte(associatedData))), nil
}

var authorizationFieldPattern = regexp.MustCompile(`([a-z_]+)="([^"]*)"`)

func parseAuthorization(t *testing.T, header string) map[string]string {
	t.Helper()
	fields := make(map[string]string)
	for _, match := range authorizationFieldPattern.FindAllStringSubmatch(header, -1) {
		fields[match[1]] = match[2]
	}
	for _, key := range []string{"mchid", "nonce_str", "signature", "timestamp", "serial_no"} {
		if fields[key] == "" {
			t.Fatalf("authorization %q missing %s", header, key)
		}
	}
	return fields
}

func readBody(t *testing.T, request *http.Request) string {
	t.Helper()
	if request.Body == nil {
		return ""
	}
	defer func() { _ = request.Body.Close() }()
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(raw)
}
