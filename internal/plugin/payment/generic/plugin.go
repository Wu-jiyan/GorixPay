// Package generic implements a configurable HTTP upstream: the channel owner
// writes the request template (placeholders + signing style) and GorixPay posts
// it, then reads the epay-compatible {code, trade_no, pay_type, pay_info}
// response. It covers the long tail of acquirers that expose a simple API.
package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

// DefaultParamTemplate mirrors the epay mapi request so an existing epay
// merchant endpoint works without a custom template.
const DefaultParamTemplate = "pid={pid}&type={pay_type}&out_trade_no={trade_no}&money={amount}" +
	"&name={name}&notify_url={notify_url}&return_url={return_url}&clientip={client_ip}" +
	"&sign_type=MD5&sign={sign}"

// maxCallbackBytes bounds a notification body before it is parsed.
const maxCallbackBytes = 1 << 20

type Plugin struct {
	client *http.Client
}

// New builds the plugin. A nil client gets the shared default with a timeout.
func New(client *http.Client) *Plugin {
	return &Plugin{client: pluginpayment.OrDefaultClient(client)}
}

func (*Plugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{
		Code:         "generic",
		Name:         "通用 HTTP 上游",
		Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment},
	}
}

func (*Plugin) ConfigFields() []pluginpayment.ConfigField {
	return []pluginpayment.ConfigField{
		{Key: "api_url", Label: "下单接口地址", Type: pluginpayment.ConfigFieldURL, Required: true},
		{Key: "method", Label: "请求方法 (POST/GET)", Type: pluginpayment.ConfigFieldText},
		{Key: "param_template", Label: "参数模板", Type: pluginpayment.ConfigFieldTextarea},
		{Key: "sign_type", Label: "签名方式 (md5/none，回调始终 md5 验签)", Type: pluginpayment.ConfigFieldSelect},
		{Key: "key", Label: "签名密钥（必填，回调验签依赖它）", Type: pluginpayment.ConfigFieldPassword, Required: true, Secret: true},
	}
}

type createResponse struct {
	Code    json.RawMessage `json:"code"`
	Msg     string          `json:"msg"`
	Message string          `json:"message"`
	TradeNo string          `json:"trade_no"`
	PayType string          `json:"pay_type"`
	PayInfo string          `json:"pay_info"`
}

func (plugin *Plugin) CreatePayment(ctx context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "api_url")
	if err != nil {
		return pluginpayment.Action{}, err
	}
	if request.TradeNo == "" || request.Amount <= 0 {
		return pluginpayment.Action{}, fmt.Errorf("%w: trade number and amount", pluginpayment.ErrInvalidConfiguration)
	}
	key, err := requireKey(config)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	mode, err := signMode(config)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	template := pluginpayment.ConfigValue(config, "param_template", DefaultParamTemplate)
	params, err := renderTemplate(template, request, config)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	if mode == "md5" {
		params["sign"] = signing.SignMD5(anyMap(params), key)
	} else {
		delete(params, "sign")
	}
	endpoint := config["api_url"]
	values := make(url.Values, len(params))
	for name, value := range params {
		values.Set(name, value)
	}
	method := strings.ToUpper(pluginpayment.ConfigValue(config, "method", http.MethodPost))
	var httpRequest *http.Request
	if method == http.MethodGet {
		separator := "?"
		if strings.Contains(endpoint, "?") {
			separator = "&"
		}
		httpRequest, err = http.NewRequestWithContext(ctx, http.MethodGet, endpoint+separator+values.Encode(), nil)
	} else {
		httpRequest, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
		if err == nil {
			httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return pluginpayment.Action{}, err
	}
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("create generic upstream payment: %w", err)
	}
	defer response.Body.Close()
	body, err := pluginpayment.ReadUpstreamBody(response.Body)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	var decoded createResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return pluginpayment.Action{}, fmt.Errorf("invalid generic upstream response: %w", err)
	}
	if !successCode(decoded.Code) || decoded.PayInfo == "" {
		message := decoded.Msg
		if message == "" {
			message = decoded.Message
		}
		return pluginpayment.Action{}, fmt.Errorf("generic upstream rejected the order: %s", message)
	}
	return actionFor(decoded.PayType, decoded.PayInfo)
}

// signMode is the signing mode of the outgoing create call: md5 (default) signs
// the request, none leaves it unsigned. Only those two exist; an unknown value is
// a configuration error instead of silently falling back to an unsigned call.
func signMode(config map[string]string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(pluginpayment.ConfigValue(config, "sign_type", "md5")))
	switch mode {
	case "md5", "none":
		return mode, nil
	default:
		return "", fmt.Errorf("%w: unsupported sign_type %q (md5 or none)", pluginpayment.ErrInvalidConfiguration, mode)
	}
}

// requireKey enforces the one field a generic channel cannot work without: the
// signing key. Without it no callback can be authenticated, so the channel is an
// open door. sign_type only decides whether the create call is signed.
func requireKey(config map[string]string) (string, error) {
	key := strings.TrimSpace(config["key"])
	if key == "" {
		return "", fmt.Errorf("%w: generic channel has no signing key, callbacks cannot be authenticated", pluginpayment.ErrInvalidConfiguration)
	}
	return key, nil
}

// callbackKey is the callback-side counterpart: a channel without a key rejects
// every notification instead of trusting it.
func callbackKey(config map[string]string) (string, error) {
	key := strings.TrimSpace(config["key"])
	if key == "" {
		return "", fmt.Errorf("%w: generic channel has no signing key, callback cannot be verified", pluginpayment.ErrCallbackRejected)
	}
	return key, nil
}

// ValidateConfiguration lets the channel administration reject an incomplete
// generic channel when it is saved, before any order reaches it.
func (*Plugin) ValidateConfiguration(config map[string]string) error {
	if _, err := pluginpayment.RequireConfig(config, "api_url"); err != nil {
		return err
	}
	if _, err := signMode(config); err != nil {
		return err
	}
	_, err := requireKey(config)
	return err
}

// successCode accepts the epay-style success markers: 1, "1" or true.
func successCode(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	switch trimmed {
	case `1`, `"1"`, `true`, `"true"`, `"success"`:
		return true
	}
	return false
}

func actionFor(payType, payInfo string) (pluginpayment.Action, error) {
	action := pluginpayment.Action{Type: pluginpayment.ActionType(strings.ToLower(strings.TrimSpace(payType)))}
	switch action.Type {
	case pluginpayment.ActionJump, pluginpayment.ActionQRCode, pluginpayment.ActionURLScheme:
		action.URL = payInfo
	case pluginpayment.ActionHTML, pluginpayment.ActionJSAPI, pluginpayment.ActionApp,
		pluginpayment.ActionScan, pluginpayment.ActionWXPlugin, pluginpayment.ActionWXApp:
		action.Data = payInfo
	default:
		if strings.HasPrefix(payInfo, "http://") || strings.HasPrefix(payInfo, "https://") {
			action = pluginpayment.Action{Type: pluginpayment.ActionJump, URL: payInfo}
		} else {
			return pluginpayment.Action{}, fmt.Errorf("%w: unsupported pay_type %q", pluginpayment.ErrInvalidAction, payType)
		}
	}
	if err := action.Validate(); err != nil {
		return pluginpayment.Action{}, err
	}
	return action, nil
}

// renderTemplate expands the {placeholder} values of the configured template.
func renderTemplate(template string, request pluginpayment.CreateRequest, config map[string]string) (map[string]string, error) {
	parsed, err := url.ParseQuery(strings.TrimSpace(template))
	if err != nil {
		return nil, fmt.Errorf("%w: param_template", pluginpayment.ErrInvalidConfiguration)
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("%w: param_template", pluginpayment.ErrInvalidConfiguration)
	}
	params := make(map[string]string, len(parsed))
	for name, entries := range parsed {
		if len(entries) == 0 {
			continue
		}
		params[name] = expand(entries[0], request, config)
	}
	return params, nil
}

func expand(value string, request pluginpayment.CreateRequest, config map[string]string) string {
	replacements := map[string]string{
		"{trade_no}":     request.TradeNo,
		"{out_trade_no}": request.TradeNo,
		"{amount}":       pluginpayment.FormatAmount(request.Amount),
		"{money}":        pluginpayment.FormatAmount(request.Amount),
		"{amount_cents}": fmt.Sprint(request.Amount),
		"{name}":         strings.TrimSpace(request.Description),
		"{notify_url}":   request.NotifyURL,
		"{return_url}":   request.ReturnURL,
		"{pay_type}":     request.PaymentType,
		"{payment_type}": request.PaymentType,
		"{client_ip}":    strings.TrimSpace(request.ClientIP),
		"{pid}":          config["pid"],
		"{site_url}":     request.Extra["site_url"],
	}
	for placeholder, replacement := range replacements {
		value = strings.ReplaceAll(value, placeholder, replacement)
	}
	return value
}

func anyMap(values map[string]string) map[string]any {
	converted := make(map[string]any, len(values))
	for name, value := range values {
		converted[name] = value
	}
	return converted
}

// VerifyCallback authenticates a form (or flat JSON) notification. Callbacks are
// always verified with the md5 canonicalization and the key: sign_type=none only
// makes the outgoing create call unsigned, it never makes a notification
// trustworthy. A channel without a key rejects every callback.
func (plugin *Plugin) VerifyCallback(_ context.Context, r *http.Request, config map[string]string) (pluginpayment.CallbackResult, error) {
	if r == nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing request", pluginpayment.ErrCallbackRejected)
	}
	if r.ContentLength > maxCallbackBytes {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: notification too large", pluginpayment.ErrCallbackRejected)
	}
	key, err := callbackKey(config)
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, pluginpayment.MaxUpstreamResponseBytes+1))
	if err != nil || len(raw) > pluginpayment.MaxUpstreamResponseBytes {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: unreadable notification", pluginpayment.ErrCallbackRejected)
	}
	fields, err := notificationFields(raw)
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	if !signing.VerifyMD5(anyMap(fields), key, fields["sign"]) {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid sign", pluginpayment.ErrCallbackRejected)
	}
	tradeNo := strings.TrimSpace(fields["out_trade_no"])
	if tradeNo == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing out_trade_no", pluginpayment.ErrCallbackRejected)
	}
	amount, err := pluginpayment.ParseAmount(firstNonEmpty(fields["money"], fields["amount"]))
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	return pluginpayment.CallbackResult{
		TradeNo:        tradeNo,
		ChannelTradeNo: strings.TrimSpace(fields["trade_no"]),
		Amount:         amount,
		Paid:           paidNotification(fields),
		Raw:            fields,
	}, nil
}

// notificationFields accepts both an urlencoded form and a flat JSON body.
func notificationFields(raw []byte) (map[string]string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "{") {
		decoder := json.NewDecoder(strings.NewReader(trimmed))
		decoder.UseNumber()
		var decoded map[string]any
		if err := decoder.Decode(&decoded); err != nil {
			return nil, fmt.Errorf("%w: invalid notification body", pluginpayment.ErrCallbackRejected)
		}
		fields := make(map[string]string, len(decoded))
		for name, value := range decoded {
			switch typed := value.(type) {
			case string:
				fields[name] = typed
			case json.Number:
				fields[name] = typed.String()
			case bool:
				fields[name] = fmt.Sprint(typed)
			}
		}
		return fields, nil
	}
	values, err := url.ParseQuery(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid notification body", pluginpayment.ErrCallbackRejected)
	}
	return pluginpayment.StringMap(values), nil
}

func paidNotification(fields map[string]string) bool {
	switch strings.ToUpper(strings.TrimSpace(fields["trade_status"])) {
	case "TRADE_SUCCESS", "TRADE_FINISHED", "SUCCESS":
		return true
	case "":
		return successCode(json.RawMessage(fields["code"])) || strings.EqualFold(fields["status"], "success")
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// compile-time assertions
var (
	_ pluginpayment.Plugin           = (*Plugin)(nil)
	_ pluginpayment.Configurable     = (*Plugin)(nil)
	_ pluginpayment.CallbackVerifier = (*Plugin)(nil)
	_ pluginpayment.ConfigValidator  = (*Plugin)(nil)
)
