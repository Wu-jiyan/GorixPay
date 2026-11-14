// Package jeepay implements the Jeepay aggregator channel: /api/pay/unifiedOrder
// with the MD5 "sorted parameters + &key=" signature and a JSON notification.
package jeepay

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	pluginpayment "gorixpay/internal/plugin/payment"
)

const (
	// unifiedOrderPath is the Jeepay unified order endpoint.
	unifiedOrderPath = "/api/pay/unifiedOrder"
	// maxCallbackBytes bounds a notification body before it is parsed.
	maxCallbackBytes = 1 << 20
	// statePaid is the Jeepay order state for a successful payment.
	statePaid = "2"
)

type Plugin struct {
	client *http.Client
	now    func() time.Time
}

// New builds the plugin. A nil client gets the shared default with a timeout.
func New(client *http.Client) *Plugin {
	return &Plugin{client: pluginpayment.OrDefaultClient(client), now: time.Now}
}

func (*Plugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{
		Code:         "jeepay",
		Name:         "Jeepay 聚合支付",
		Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment},
	}
}

func (*Plugin) ConfigFields() []pluginpayment.ConfigField {
	return []pluginpayment.ConfigField{
		{Key: "api_url", Label: "接口地址", Type: pluginpayment.ConfigFieldURL, Required: true},
		{Key: "mch_no", Label: "商户号", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "app_id", Label: "应用 ID", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "key", Label: "接口密钥", Type: pluginpayment.ConfigFieldPassword, Required: true, Secret: true},
		{Key: "way_code", Label: "默认支付方式", Type: pluginpayment.ConfigFieldText},
	}
}

// wayCode maps a channel payment type onto a Jeepay wayCode. A configured
// default wins; otherwise the common payment types map onto the documented
// Jeepay codes and anything else is upper-cased as-is.
func wayCode(paymentType, configured string) string {
	if configured != "" {
		return configured
	}
	switch strings.ToLower(strings.TrimSpace(paymentType)) {
	case "alipay", "ali_qr", "alipay_qr", "":
		return "ALI_QR"
	case "alipay_wap":
		return "ALI_WAP"
	case "wechat", "wechatpay", "wx_native", "wechat_native":
		return "WX_NATIVE"
	case "wechat_jsapi", "jsapi", "wx_jsapi":
		return "WX_JSAPI"
	case "wechat_h5", "h5", "wap", "mweb":
		return "WX_H5"
	}
	normalized := strings.ToUpper(strings.TrimSpace(paymentType))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return strings.ReplaceAll(normalized, " ", "_")
}

// canonical renders the sorted k=v&k2=v2 string Jeepay signs. Empty values and
// the sign field itself are skipped.
func canonical(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if key == "sign" || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, "&")
}

// sign produces the upper-case MD5 Jeepay signature.
func sign(values map[string]string, key string) string {
	sum := md5.Sum([]byte(canonical(values) + "&key=" + key))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// verify compares the Jeepay signature in constant time, like the MD5 helpers in
// internal/compat/epay/signing. A byte-by-byte early exit would leak the prefix
// of a valid signature to an attacker who can time the callback.
func verify(values map[string]string, key, signature string) bool {
	expected := sign(values, key)
	provided := strings.ToUpper(strings.TrimSpace(signature))
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

type unifiedResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		PayOrderID  string `json:"payOrderId"`
		PayDataType string `json:"payDataType"`
		PayData     string `json:"payData"`
	} `json:"data"`
}

func (plugin *Plugin) CreatePayment(ctx context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "api_url", "mch_no", "app_id", "key")
	if err != nil {
		return pluginpayment.Action{}, err
	}
	if request.TradeNo == "" || request.Amount <= 0 {
		return pluginpayment.Action{}, fmt.Errorf("%w: trade number and amount", pluginpayment.ErrInvalidConfiguration)
	}
	at := plugin.now()
	values := map[string]string{
		"version":    "1.0",
		"reqTime":    at.Format("20060102150405"),
		"reqId":      fmt.Sprintf("%d-%s", at.UnixNano(), request.TradeNo),
		"mchNo":      config["mch_no"],
		"appId":      config["app_id"],
		"wayCode":    wayCode(request.PaymentType, config["way_code"]),
		"amount":     fmt.Sprint(request.Amount),
		"currency":   "cny",
		"mchOrderNo": request.TradeNo,
		"goodsName":  strings.TrimSpace(request.Description),
		"notifyUrl":  request.NotifyURL,
		"returnUrl":  request.ReturnURL,
		"clientIp":   strings.TrimSpace(request.ClientIP),
		"signType":   "MD5",
	}
	values["sign"] = sign(values, config["key"])
	form := make(url.Values, len(values))
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		form.Set(name, value)
	}
	endpoint := strings.TrimRight(config["api_url"], "/") + unifiedOrderPath
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return pluginpayment.Action{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("create jeepay payment: %w", err)
	}
	defer response.Body.Close()
	body, err := pluginpayment.ReadUpstreamBody(response.Body)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	var decoded unifiedResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return pluginpayment.Action{}, fmt.Errorf("invalid jeepay response: %w", err)
	}
	if decoded.Code != 0 || decoded.Data.PayData == "" {
		return pluginpayment.Action{}, fmt.Errorf("jeepay rejected the order: %d %s", decoded.Code, decoded.Msg)
	}
	return actionFor(decoded.Data.PayDataType, decoded.Data.PayData)
}

func actionFor(payDataType, payData string) (pluginpayment.Action, error) {
	var action pluginpayment.Action
	switch strings.ToLower(strings.TrimSpace(payDataType)) {
	case "codeurl", "qrcodeurl", "qrcode", "code_url":
		action = pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: payData}
	case "form", "html", "formhtml":
		action = pluginpayment.Action{Type: pluginpayment.ActionHTML, Data: payData}
	case "payurl", "jumpurl", "url":
		action = pluginpayment.Action{Type: pluginpayment.ActionJump, URL: payData}
	default:
		if strings.HasPrefix(payData, "http://") || strings.HasPrefix(payData, "https://") {
			action = pluginpayment.Action{Type: pluginpayment.ActionJump, URL: payData}
		} else {
			action = pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: payData}
		}
	}
	if err := action.Validate(); err != nil {
		return pluginpayment.Action{}, err
	}
	return action, nil
}

// VerifyCallback authenticates the JSON notification Jeepay posts. Numbers are
// kept in their lexical form so the signature matches what the upstream signed.
func (plugin *Plugin) VerifyCallback(_ context.Context, r *http.Request, config map[string]string) (pluginpayment.CallbackResult, error) {
	if r == nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing request", pluginpayment.ErrCallbackRejected)
	}
	resolved, err := pluginpayment.RequireConfig(config, "key")
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	if r.ContentLength > maxCallbackBytes {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: notification too large", pluginpayment.ErrCallbackRejected)
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, pluginpayment.MaxUpstreamResponseBytes+1))
	if err != nil || len(raw) > pluginpayment.MaxUpstreamResponseBytes {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: unreadable notification", pluginpayment.ErrCallbackRejected)
	}
	values, err := decodeFlatJSON(raw)
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	if !verify(values, resolved["key"], values["sign"]) {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid sign", pluginpayment.ErrCallbackRejected)
	}
	if mchNo := resolved["mch_no"]; mchNo != "" && values["mchNo"] != "" && values["mchNo"] != mchNo {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: mchNo mismatch", pluginpayment.ErrCallbackRejected)
	}
	tradeNo := strings.TrimSpace(values["mchOrderNo"])
	if tradeNo == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing mchOrderNo", pluginpayment.ErrCallbackRejected)
	}
	amount, err := pluginpayment.ParseCentsAmount(values["amount"])
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	paid := strings.TrimSpace(values["state"]) == statePaid
	if !paid {
		// A non-success notification carries no payable amount.
		amount = 0
	}
	return pluginpayment.CallbackResult{
		TradeNo:        tradeNo,
		ChannelTradeNo: strings.TrimSpace(values["payOrderId"]),
		Amount:         amount,
		Paid:           paid,
		Raw:            values,
	}, nil
}

// decodeFlatJSON flattens a notification object into string values, preserving
// the exact lexical form of numbers so signatures stay stable.
func decodeFlatJSON(raw []byte) (map[string]string, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var decoded map[string]any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: invalid notification body", pluginpayment.ErrCallbackRejected)
	}
	values := make(map[string]string, len(decoded))
	for name, value := range decoded {
		switch typed := value.(type) {
		case nil:
			continue
		case string:
			values[name] = typed
		case json.Number:
			values[name] = typed.String()
		case bool:
			values[name] = fmt.Sprint(typed)
		default:
			// Nested objects and arrays are not part of the Jeepay signature.
			continue
		}
	}
	return values, nil
}

// compile-time assertions
var (
	_ pluginpayment.Plugin           = (*Plugin)(nil)
	_ pluginpayment.Configurable     = (*Plugin)(nil)
	_ pluginpayment.CallbackVerifier = (*Plugin)(nil)
)
