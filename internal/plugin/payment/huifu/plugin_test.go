package huifu

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	pluginpayment "gorixpay/internal/plugin/payment"
)

// recordedRequest captures what the mock gateway received.
type recordedRequest struct {
	sysID     string
	productID string
	data      map[string]json.RawMessage
}

// newGateway stands in for api.huifu.com: it verifies the merchant signature on
// every request and answers with a platform-signed envelope.
func newGateway(t *testing.T, merchant *rsa.PublicKey, platform *rsa.PrivateKey, respond func(data map[string]json.RawMessage) map[string]any) (*httptest.Server, *recordedRequest) {
	t.Helper()
	recorded := &recordedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method=%s", request.Method)
		}
		if contentType := request.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
			t.Errorf("content type=%q", contentType)
		}
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		var envelope requestEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Errorf("decode request: %v", err)
		}
		recorded.sysID = envelope.SysID
		recorded.productID = envelope.ProductID
		recorded.data = envelope.Data
		if !verifyData(merchant, envelope.Data, envelope.Sign) {
			t.Errorf("request signature does not verify: %s", canonicalJSON(envelope.Data))
		}
		data, err := rawMap(respond(envelope.Data))
		if err != nil {
			t.Errorf("encode response: %v", err)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		signature, err := signData(platform, data)
		if err != nil {
			t.Errorf("sign response: %v", err)
		}
		body, err := marshalNoEscape(map[string]any{"data": data, "sign": signature})
		if err != nil {
			t.Errorf("marshal response: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(body)
	}))
	return server, recorded
}

// rawMap renders a response payload into raw JSON values, keeping the encoding
// free of HTML escaping exactly like the real gateway does.
func rawMap(payload map[string]any) (map[string]json.RawMessage, error) {
	data := make(map[string]json.RawMessage, len(payload))
	for name, value := range payload {
		encoded, err := marshalNoEscape(value)
		if err != nil {
			return nil, err
		}
		data[name] = json.RawMessage(encoded)
	}
	return data, nil
}

// createConfig builds a channel configuration from the merchant key pair while
// trusting the platform key pair for response signatures. The gateway URL is
// always injected so no test can reach the real api.huifu.com.
func createConfig(merchant, platform keyMaterial, baseURL string, overrides map[string]string) map[string]string {
	config := map[string]string{
		"sys_id":           "6666000197954883",
		"product_id":       "P0001",
		"private_key":      merchant.privatePEM,
		"huifu_public_key": platform.publicPEM,
		"api_url":          baseURL,
	}
	for name, value := range overrides {
		if value == "" {
			delete(config, name)
			continue
		}
		config[name] = value
	}
	return config
}

func fieldString(t *testing.T, data map[string]json.RawMessage, key string) string {
	t.Helper()
	if _, exists := data[key]; !exists {
		t.Fatalf("request is missing %q: %v", key, keysOf(data))
	}
	return rawString(data, key)
}

func keysOf(data map[string]json.RawMessage) []string {
	names := make([]string, 0, len(data))
	for name := range data {
		names = append(names, name)
	}
	return names
}

func nativeSample(qrCode string) map[string]any {
	return map[string]any{
		"atu_sub_mer_id": "2088580696372505", "bank_message": "Success",
		"hf_seq_id": "002900HS12A261113202335P0520a4f25e800000", "huifu_id": "6666000197954883",
		"is_clean_split": "N", "party_order_id": "03122611137341522203241",
		"qr_code": qrCode, "req_date": "20261113", "req_seq_id": "202611132023344815",
		"resp_code": successCode, "resp_desc": "下单成功", "trade_type": "A_NATIVE",
		"trans_amt": "0.01", "trans_stat": "P", "unconfirm_amt": "0.01",
	}
}

func TestCreateAlipayNativeOrderReturnsQRCode(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	gateway, recorded := newGateway(t, &merchant.private.PublicKey, platform.private, func(map[string]json.RawMessage) map[string]any {
		return nativeSample("https://qr.alipay.com/bax0821")
	})
	defer gateway.Close()

	action, err := New(gateway.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "202611132023344815", Amount: 1, Description: "汇付测试", PaymentType: "alipay",
		NotifyURL: "https://pay.example/api/v1/payment/callback/huifu/channel-1", ClientIP: "203.0.113.7",
		Configuration: createConfig(merchant, platform, gateway.URL, map[string]string{"huifu_id": "6666000197954000"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionQRCode || action.URL != "https://qr.alipay.com/bax0821" {
		t.Fatalf("action=%+v", action)
	}
	if recorded.sysID != "6666000197954883" || recorded.productID != "P0001" {
		t.Fatalf("envelope=%s/%s", recorded.sysID, recorded.productID)
	}
	captured := recorded.data
	for key, want := range map[string]string{
		"req_date":   "20261113",
		"req_seq_id": "202611132023344815",
		"huifu_id":   "6666000197954000",
		"trade_type": "A_NATIVE",
		"trans_amt":  "0.01",
		"goods_desc": "汇付测试",
		"notify_url": "https://pay.example/api/v1/payment/callback/huifu/channel-1",
	} {
		if got := fieldString(t, captured, key); got != want {
			t.Errorf("%s=%q want %q", key, got, want)
		}
	}
	if got := fieldString(t, captured, "risk_check_data"); got != `{"ip_addr":"203.0.113.7"}` {
		t.Errorf("risk_check_data=%s", got)
	}
	if got := fieldString(t, captured, "alipay_data"); got != `{"subject":"汇付测试"}` {
		t.Errorf("alipay_data=%s", got)
	}
}

func TestCreateAlipayNativeFallsBackToSysID(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	gateway, recorded := newGateway(t, &merchant.private.PublicKey, platform.private, func(map[string]json.RawMessage) map[string]any {
		return nativeSample("https://qr.alipay.com/x")
	})
	defer gateway.Close()
	if _, err := New(gateway.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "202611130000000001", Amount: 100, PaymentType: "alipay",
		NotifyURL: "https://pay.example/notify", Configuration: createConfig(merchant, platform, gateway.URL, nil),
	}); err != nil {
		t.Fatal(err)
	}
	if got := fieldString(t, recorded.data, "huifu_id"); got != "6666000197954883" {
		t.Fatalf("huifu_id=%q", got)
	}
}

func TestCreateWXJSAPIOrderReturnsPayInfo(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	payInfo := `{"appId":"wxappid","timeStamp":"1794564546","nonceStr":"n1","package":"prepay_id=wx1","signType":"RSA","paySign":"sig"}`
	gateway, recorded := newGateway(t, &merchant.private.PublicKey, platform.private, func(map[string]json.RawMessage) map[string]any {
		return map[string]any{
			"resp_code": successCode, "resp_desc": "下单成功", "trade_type": "T_JSAPI",
			"pay_info": payInfo, "hf_seq_id": "HF-JSAPI-1", "trans_stat": "P",
		}
	})
	defer gateway.Close()

	action, err := New(gateway.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "202611132023344900", Amount: 1999, Description: "微信JSAPI", PaymentType: "wxpay", Method: "jsapi",
		NotifyURL: "https://pay.example/notify", ClientIP: "198.51.100.9",
		Extra:         map[string]string{"sub_openid": "oUser-1", "sub_appid": "wxsubapp"},
		Configuration: createConfig(merchant, platform, gateway.URL, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionJSAPI {
		t.Fatalf("action=%+v", action)
	}
	if payload, ok := action.Data.(string); !ok || payload != payInfo {
		t.Fatalf("data=%T %v", action.Data, action.Data)
	}
	if got := fieldString(t, recorded.data, "trade_type"); got != "T_JSAPI" {
		t.Fatalf("trade_type=%q", got)
	}
	wxData := fieldString(t, recorded.data, "wx_data")
	for _, fragment := range []string{`"sub_openid":"oUser-1"`, `"sub_appid":"wxsubapp"`, `"device_info":"4"`, `"spbill_create_ip":"198.51.100.9"`} {
		if !strings.Contains(wxData, fragment) {
			t.Errorf("wx_data=%s missing %s", wxData, fragment)
		}
	}
	if got := fieldString(t, recorded.data, "trans_amt"); got != "19.99" {
		t.Fatalf("trans_amt=%q", got)
	}
}

func TestCreateWXNativeOrderUsesProductID(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	gateway, recorded := newGateway(t, &merchant.private.PublicKey, platform.private, func(map[string]json.RawMessage) map[string]any {
		return nativeSample("weixin://wxpay/bizpayurl?pr=abc")
	})
	defer gateway.Close()
	action, err := New(gateway.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "202611132023344901", Amount: 100, PaymentType: "wechat", Method: "native",
		NotifyURL: "https://pay.example/notify", ClientIP: "198.51.100.9",
		Configuration: createConfig(merchant, platform, gateway.URL, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionQRCode {
		t.Fatalf("action=%+v", action)
	}
	if got := fieldString(t, recorded.data, "trade_type"); got != "T_NATIVE" {
		t.Fatalf("trade_type=%q", got)
	}
	if got := fieldString(t, recorded.data, "wx_data"); got != `{"product_id":"01001","spbill_create_ip":"198.51.100.9"}` {
		t.Fatalf("wx_data=%s", got)
	}
}

func TestCreateUnionPayNativeOrder(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	gateway, recorded := newGateway(t, &merchant.private.PublicKey, platform.private, func(map[string]json.RawMessage) map[string]any {
		return nativeSample("https://qr.95516.com/00010000/abc")
	})
	defer gateway.Close()
	action, err := New(gateway.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "202611132023344902", Amount: 500, PaymentType: "bank", Method: "native",
		NotifyURL: "https://pay.example/notify", ClientIP: "203.0.113.50",
		Configuration: createConfig(merchant, platform, gateway.URL, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != pluginpayment.ActionQRCode {
		t.Fatalf("action=%+v", action)
	}
	if got := fieldString(t, recorded.data, "trade_type"); got != "U_NATIVE" {
		t.Fatalf("trade_type=%q", got)
	}
	if got := fieldString(t, recorded.data, "unionpay_data"); got != `{"customer_ip":"203.0.113.50"}` {
		t.Fatalf("unionpay_data=%s", got)
	}
}

func TestCreateJSAPIRequiresOpenID(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	gateway, _ := newGateway(t, &merchant.private.PublicKey, platform.private, func(map[string]json.RawMessage) map[string]any {
		t.Error("gateway must not be called without an openid")
		return nativeSample("https://qr.example/x")
	})
	defer gateway.Close()
	for _, paymentType := range []string{"alipay", "wxpay", "bank"} {
		t.Run(paymentType, func(t *testing.T) {
			_, err := New(gateway.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
				TradeNo: "202611132023344903", Amount: 100, PaymentType: paymentType, Method: "jsapi",
				NotifyURL: "https://pay.example/notify", Configuration: createConfig(merchant, platform, gateway.URL, nil),
			})
			if !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestCreatePaymentRejectsBusinessFailure(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	gateway, _ := newGateway(t, &merchant.private.PublicKey, platform.private, func(map[string]json.RawMessage) map[string]any {
		return map[string]any{
			"resp_code": "23000001", "resp_desc": "交易不存在", "bank_message": "订单未找到",
		}
	})
	defer gateway.Close()
	_, err := New(gateway.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "202611132023344904", Amount: 100, PaymentType: "alipay",
		NotifyURL: "https://pay.example/notify", Configuration: createConfig(merchant, platform, gateway.URL, nil),
	})
	if err == nil || !strings.Contains(err.Error(), "23000001") || !strings.Contains(err.Error(), "交易不存在") {
		t.Fatalf("error=%v", err)
	}
}

func TestCreatePaymentRejectsTamperedResponse(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	gateway := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		var envelope requestEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Errorf("decode request: %v", err)
		}
		data, err := rawMap(nativeSample("https://qr.alipay.com/original"))
		if err != nil {
			t.Errorf("encode response: %v", err)
			return
		}
		signature, err := signData(platform.private, data)
		if err != nil {
			t.Errorf("sign: %v", err)
		}
		// Mutate the data after signing: the client must reject it.
		data["qr_code"] = rawValue("https://qr.alipay.com/attacker")
		body, _ := marshalNoEscape(map[string]any{"data": data, "sign": signature})
		_, _ = writer.Write(body)
	}))
	defer gateway.Close()
	_, err := New(gateway.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "202611132023344905", Amount: 100, PaymentType: "alipay",
		NotifyURL: "https://pay.example/notify", Configuration: createConfig(merchant, platform, gateway.URL, nil),
	})
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("error=%v", err)
	}
}

func TestCreatePaymentRejectsMissingActions(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	for name, respond := range map[string]func(map[string]json.RawMessage) map[string]any{
		"no-qr-code": func(map[string]json.RawMessage) map[string]any {
			return map[string]any{"resp_code": successCode, "resp_desc": "下单成功", "trade_type": "A_NATIVE"}
		},
		"no-pay-info": func(map[string]json.RawMessage) map[string]any {
			return map[string]any{"resp_code": successCode, "resp_desc": "下单成功", "trade_type": "T_JSAPI"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			gateway, _ := newGateway(t, &merchant.private.PublicKey, platform.private, respond)
			defer gateway.Close()
			request := pluginpayment.CreateRequest{
				TradeNo: "202611132023344906", Amount: 100, PaymentType: "alipay",
				NotifyURL: "https://pay.example/notify", Configuration: createConfig(merchant, platform, gateway.URL, nil),
			}
			if name == "no-pay-info" {
				request.PaymentType = "wxpay"
				request.Method = "jsapi"
				request.Extra = map[string]string{"sub_openid": "oUser-1"}
			}
			if _, err := New(gateway.Client()).CreatePayment(context.Background(), request); err == nil {
				t.Fatal("missing action payload accepted")
			}
		})
	}
}

func TestCreatePaymentAmountBoundaries(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	gateway, recorded := newGateway(t, &merchant.private.PublicKey, platform.private, func(map[string]json.RawMessage) map[string]any {
		return nativeSample("https://qr.alipay.com/x")
	})
	defer gateway.Close()
	cases := map[int64]string{1: "0.01", 100: "1.00", 1234: "12.34", 10000: "100.00", 999999: "9999.99"}
	for amount, want := range cases {
		plugin := New(gateway.Client())
		if _, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
			TradeNo: "202611132023344907", Amount: amount, PaymentType: "alipay",
			NotifyURL: "https://pay.example/notify", Configuration: createConfig(merchant, platform, gateway.URL, nil),
		}); err != nil {
			t.Fatalf("amount %d: %v", amount, err)
		}
		if got := fieldString(t, recorded.data, "trans_amt"); got != want {
			t.Errorf("amount %d trans_amt=%q want %q", amount, got, want)
		}
	}
}

func TestCreatePaymentUsesBareBase64Keys(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	gateway, _ := newGateway(t, &merchant.private.PublicKey, platform.private, func(map[string]json.RawMessage) map[string]any {
		return nativeSample("https://qr.alipay.com/bare")
	})
	defer gateway.Close()
	config := map[string]string{
		"sys_id": "6666000197954883", "product_id": "P0001",
		"private_key": merchant.privateBare, "huifu_public_key": platform.publicBare,
		"api_url": gateway.URL,
	}
	action, err := New(gateway.Client()).CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "202611132023344908", Amount: 1, PaymentType: "alipay",
		NotifyURL: "https://pay.example/notify", Configuration: config,
	})
	if err != nil {
		t.Fatalf("bare base64 keys rejected: %v", err)
	}
	if action.Type != pluginpayment.ActionQRCode {
		t.Fatalf("action=%+v", action)
	}
}

func TestCreatePaymentRejectsInvalidConfiguration(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	plugin := New(nil)
	for name, mutate := range map[string]func(map[string]string){
		"missing-sys-id":    func(config map[string]string) { delete(config, "sys_id") },
		"missing-product":   func(config map[string]string) { delete(config, "product_id") },
		"missing-private":   func(config map[string]string) { delete(config, "private_key") },
		"missing-public":    func(config map[string]string) { delete(config, "huifu_public_key") },
		"garbage-private":   func(config map[string]string) { config["private_key"] = "not a key" },
		"garbage-key-shape": func(config map[string]string) { config["huifu_public_key"] = "AAAA" },
	} {
		t.Run(name, func(t *testing.T) {
			config := createConfig(merchant, platform, "https://api.example.test", nil)
			mutate(config)
			_, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
				TradeNo: "202611132023344909", Amount: 100, PaymentType: "alipay",
				NotifyURL: "https://pay.example/notify", Configuration: config,
			})
			if !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	t.Run("missing-notify-url", func(t *testing.T) {
		_, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
			TradeNo: "202611132023344909", Amount: 100, PaymentType: "alipay",
			Configuration: createConfig(merchant, platform, "https://api.example.test", nil),
		})
		if !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("zero-amount", func(t *testing.T) {
		_, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
			TradeNo: "202611132023344909", Amount: 0, PaymentType: "alipay",
			NotifyURL: "https://pay.example/notify", Configuration: createConfig(merchant, platform, "https://api.example.test", nil),
		})
		if !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestCreatePaymentReqDateFallback(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	gateway, recorded := newGateway(t, &merchant.private.PublicKey, platform.private, func(map[string]json.RawMessage) map[string]any {
		return nativeSample("https://qr.alipay.com/x")
	})
	defer gateway.Close()
	plugin := New(gateway.Client())
	plugin.now = func() time.Time { return time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC) }
	if _, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo: "GXP202611132023344815", Amount: 1, PaymentType: "alipay",
		NotifyURL: "https://pay.example/notify", Configuration: createConfig(merchant, platform, gateway.URL, nil),
	}); err != nil {
		t.Fatal(err)
	}
	if got := fieldString(t, recorded.data, "req_date"); got != "20261113" {
		t.Fatalf("req_date=%q", got)
	}
}

func TestVerifyCallbackAcceptsSignedNotification(t *testing.T) {
	platform := newKeyMaterial(t)
	respData := map[string]any{
		"resp_code": successCode, "resp_desc": "交易成功", "trans_stat": "S",
		"req_seq_id": "202611132023344815", "req_date": "20261113",
		"hf_seq_id": "002900HS12A261113202335P0520a4f25e800000", "out_trans_id": "202611132003130000000000000073",
		"party_order_id": "03122611137341522203241", "trans_amt": "0.01",
		"alipay_response": map[string]any{"buyer_id": "2088999999999999", "trade_no": "2026111322001"},
	}
	form := signedRespData(t, platform.private, respData)
	result, err := verifyCallbackForm(t, map[string]string{"huifu_public_key": platform.publicPEM}, form)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Paid || result.TradeNo != "202611132023344815" || result.ChannelTradeNo != "002900HS12A261113202335P0520a4f25e800000" || result.Amount != 1 {
		t.Fatalf("result=%+v", result)
	}
	if result.Ack != "RECV_ORD_ID_202611132023344815" {
		t.Fatalf("ack=%q", result.Ack)
	}
	if result.Raw["buyer"] != "2088999999999999" {
		t.Fatalf("raw=%+v", result.Raw)
	}
}

func TestVerifyCallbackConvertsYuanToCents(t *testing.T) {
	platform := newKeyMaterial(t)
	for amount, want := range map[string]int64{"0.01": 1, "1.00": 100, "100.00": 10000, "9999.99": 999999} {
		respData := map[string]any{
			"resp_code": successCode, "trans_stat": "S",
			"req_seq_id": "202611132023344816", "hf_seq_id": "HF-1", "trans_amt": amount,
		}
		result, err := verifyCallbackForm(t, map[string]string{"huifu_public_key": platform.publicPEM}, signedRespData(t, platform.private, respData))
		if err != nil {
			t.Fatalf("amount %s: %v", amount, err)
		}
		if result.Amount != want {
			t.Errorf("amount %s -> %d want %d", amount, result.Amount, want)
		}
	}
}

func TestVerifyCallbackRejectsTamperedRespData(t *testing.T) {
	platform := newKeyMaterial(t)
	respData := map[string]any{
		"resp_code": successCode, "trans_stat": "S",
		"req_seq_id": "202611132023344817", "hf_seq_id": "HF-1", "trans_amt": "0.01",
	}
	form := signedRespData(t, platform.private, respData)
	// Same signature, different resp_data: must be refused.
	tampered := strings.ReplaceAll(form.Get("resp_data"), `"trans_amt":"0.01"`, `"trans_amt":"0.00"`)
	if tampered == form.Get("resp_data") {
		t.Fatal("resp_data was not tampered")
	}
	form.Set("resp_data", tampered)
	if _, err := verifyCallbackForm(t, map[string]string{"huifu_public_key": platform.publicPEM}, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
		t.Fatalf("error=%v", err)
	}
}

func TestVerifyCallbackRejectsWrongPublicKey(t *testing.T) {
	platform, stranger := newKeyMaterial(t), newKeyMaterial(t)
	respData := map[string]any{
		"resp_code": successCode, "trans_stat": "S",
		"req_seq_id": "202611132023344818", "hf_seq_id": "HF-1", "trans_amt": "0.01",
	}
	form := signedRespData(t, platform.private, respData)
	if _, err := verifyCallbackForm(t, map[string]string{"huifu_public_key": stranger.publicPEM}, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
		t.Fatalf("error=%v", err)
	}
}

func TestVerifyCallbackAcceptsBareBase64PublicKey(t *testing.T) {
	platform := newKeyMaterial(t)
	respData := map[string]any{
		"resp_code": successCode, "trans_stat": "S",
		"req_seq_id": "202611132023344819", "hf_seq_id": "HF-1", "trans_amt": "0.01",
	}
	result, err := verifyCallbackForm(t, map[string]string{"huifu_public_key": platform.publicBare}, signedRespData(t, platform.private, respData))
	if err != nil {
		t.Fatalf("bare base64 public key rejected: %v", err)
	}
	if !result.Paid || result.Amount != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerifyCallbackPendingAndFailedStatuses(t *testing.T) {
	platform := newKeyMaterial(t)
	for _, status := range []string{"P", "F"} {
		respData := map[string]any{
			"resp_code": successCode, "trans_stat": status,
			"req_seq_id": "202611132023344820", "hf_seq_id": "HF-1", "trans_amt": "0.01",
		}
		result, err := verifyCallbackForm(t, map[string]string{"huifu_public_key": platform.publicPEM}, signedRespData(t, platform.private, respData))
		if err != nil {
			t.Fatalf("status %s: %v", status, err)
		}
		if result.Paid || result.Amount != 0 {
			t.Fatalf("status %s result=%+v", status, result)
		}
		if result.Ack != "RECV_ORD_ID_202611132023344820" {
			t.Fatalf("status %s ack=%q", status, result.Ack)
		}
	}
}

func TestVerifyCallbackBuyerFromWXResponse(t *testing.T) {
	platform := newKeyMaterial(t)
	respData := map[string]any{
		"resp_code": successCode, "trans_stat": "S",
		"req_seq_id": "202611132023344821", "hf_seq_id": "HF-1", "trans_amt": "0.01",
		"wx_response": map[string]any{"sub_openid": "oUser-wx", "transaction_id": "4200001"},
	}
	result, err := verifyCallbackForm(t, map[string]string{"huifu_public_key": platform.publicPEM}, signedRespData(t, platform.private, respData))
	if err != nil {
		t.Fatal(err)
	}
	if result.Raw["buyer"] != "oUser-wx" {
		t.Fatalf("raw=%+v", result.Raw)
	}
}

func TestVerifyCallbackRejectsMissingPieces(t *testing.T) {
	platform := newKeyMaterial(t)
	respData := map[string]any{"resp_code": successCode, "trans_stat": "S", "req_seq_id": "T-1", "trans_amt": "0.01"}

	t.Run("missing-resp-data", func(t *testing.T) {
		form := url.Values{"sign": {"x"}}
		if _, err := verifyCallbackForm(t, map[string]string{"huifu_public_key": platform.publicPEM}, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("missing-sign", func(t *testing.T) {
		form := url.Values{"resp_data": {string(mustJSON(t, respData))}}
		if _, err := verifyCallbackForm(t, map[string]string{"huifu_public_key": platform.publicPEM}, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("missing-public-key", func(t *testing.T) {
		form := signedRespData(t, platform.private, respData)
		if _, err := verifyCallbackForm(t, nil, form); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("missing-req-seq-id", func(t *testing.T) {
		form := signedRespData(t, platform.private, map[string]any{"resp_code": successCode, "trans_stat": "S", "trans_amt": "0.01"})
		if _, err := verifyCallbackForm(t, map[string]string{"huifu_public_key": platform.publicPEM}, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("invalid-amount", func(t *testing.T) {
		form := signedRespData(t, platform.private, map[string]any{
			"resp_code": successCode, "trans_stat": "S", "req_seq_id": "T-1", "trans_amt": "abc",
		})
		if _, err := verifyCallbackForm(t, map[string]string{"huifu_public_key": platform.publicPEM}, form); !errors.Is(err, pluginpayment.ErrCallbackRejected) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestMetadataAndConfigFields(t *testing.T) {
	plugin := New(nil)
	metadata := plugin.Metadata()
	if metadata.Code != "huifu" || metadata.Name != "汇付斗拱" || len(metadata.Capabilities) != 1 {
		t.Fatalf("metadata=%+v", metadata)
	}
	wanted := map[string]bool{"sys_id": true, "product_id": true, "private_key": true, "huifu_public_key": true, "huifu_id": true}
	secret := map[string]bool{}
	required := map[string]bool{}
	for _, field := range plugin.ConfigFields() {
		delete(wanted, field.Key)
		if field.Label == "" || field.Type == "" {
			t.Errorf("field=%+v", field)
		}
		if field.Secret {
			secret[field.Key] = true
		}
		if field.Required {
			required[field.Key] = true
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("missing fields=%v", wanted)
	}
	if !secret["private_key"] || !secret["huifu_public_key"] || len(secret) != 2 {
		t.Fatalf("secret fields=%v", secret)
	}
	if !required["sys_id"] || !required["product_id"] || !required["private_key"] || !required["huifu_public_key"] || required["huifu_id"] {
		t.Fatalf("required fields=%v", required)
	}
	registry := pluginpayment.NewRegistry()
	if err := registry.Register(New(nil)); err != nil {
		t.Fatalf("registry rejected the plugin: %v", err)
	}
	if _, err := registry.Resolve("huifu", pluginpayment.CapabilityPayment); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

// signedRespData renders the notification form Huifu posts back: the exact
// resp_data string and the signature over that string.
func signedRespData(t *testing.T, platform *rsa.PrivateKey, respData map[string]any) url.Values {
	t.Helper()
	raw := string(mustJSON(t, respData))
	signature, err := signCanonical(platform, raw)
	if err != nil {
		t.Fatalf("sign resp_data: %v", err)
	}
	return url.Values{"resp_data": {raw}, "sign": {signature}}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := marshalNoEscape(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return encoded
}

func verifyCallbackForm(t *testing.T, config map[string]string, form url.Values) (pluginpayment.CallbackResult, error) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/payment/callback/huifu/channel-1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return New(nil).VerifyCallback(context.Background(), request, config)
}
