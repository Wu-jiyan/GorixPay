// Package unionpay implements UnionPay 全渠道 (gateway 5.1.0) card payment: the
// request is signed with the merchant RSA key and handed to the browser as an
// auto-submitting form (or a jump URL), and the notification is verified with the
// UnionPay public key using the sorted "k=v&" signature string.
package unionpay

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

const (
	// DefaultAPIURL is the production UnionPay front-end transaction gateway.
	DefaultAPIURL = "https://gateway.95516.com/gateway/api/frontTransReq.do"
	// apiVersion is the 5.1.0 gateway version this plugin speaks.
	apiVersion = "5.1.0"
	// signMethodSHA256 is the only supported signMethod (01 = SHA256withRSA).
	signMethodSHA256 = "01"
	// successRespCode is the UnionPay response code for a successful payment.
	successRespCode = "00"
	// maxCallbackBytes bounds a notification body before it is parsed.
	maxCallbackBytes = 1 << 20
	// defaultFormID is the DOM id of the generated auto-submit form.
	defaultFormID = "unionpay_submit"
)

type Plugin struct {
	// client is kept for the future query/close calls (txnType=00); CreatePayment
	// itself never performs an outbound request, the browser posts the form.
	client *http.Client
	now    func() time.Time
}

// New builds the plugin. A nil client gets the shared default with a timeout.
func New(client *http.Client) *Plugin {
	return &Plugin{client: pluginpayment.OrDefaultClient(client), now: time.Now}
}

func (*Plugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{
		Code:         "unionpay",
		Name:         "银联全渠道",
		Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment},
	}
}

func (*Plugin) ConfigFields() []pluginpayment.ConfigField {
	return []pluginpayment.ConfigField{
		{Key: "mer_id", Label: "商户号", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "cert_id", Label: "证书 ID", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "private_key", Label: "商户私钥 (PKCS8 PEM)", Type: pluginpayment.ConfigFieldTextarea, Required: true, Secret: true},
		{Key: "unionpay_public_key", Label: "银联公钥 (PEM)", Type: pluginpayment.ConfigFieldTextarea, Required: true, Secret: true},
		{Key: "api_url", Label: "前台交易地址", Type: pluginpayment.ConfigFieldURL},
		{Key: "front_url", Label: "前台回跳地址", Type: pluginpayment.ConfigFieldURL},
		{Key: "submit_mode", Label: "提交方式 (html/jump)", Type: pluginpayment.ConfigFieldSelect},
	}
}

// CreatePayment builds the signed 5.1.0 front-end transaction and renders it for
// the browser: submit_mode=html (default) returns an auto-submitting form,
// submit_mode=jump returns a GET URL carrying the same parameters.
func (plugin *Plugin) CreatePayment(_ context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "mer_id", "cert_id", "private_key")
	if err != nil {
		return pluginpayment.Action{}, err
	}
	privateKey, err := signing.ParseRSAPrivateKeyPEM(config["private_key"])
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("%w: private_key", pluginpayment.ErrInvalidConfiguration)
	}
	if request.TradeNo == "" || request.Amount <= 0 {
		return pluginpayment.Action{}, fmt.Errorf("%w: trade number and amount", pluginpayment.ErrInvalidConfiguration)
	}
	if request.NotifyURL == "" {
		return pluginpayment.Action{}, fmt.Errorf("%w: backUrl", pluginpayment.ErrInvalidConfiguration)
	}
	frontURL := strings.TrimSpace(config["front_url"])
	if frontURL == "" {
		frontURL = strings.TrimSpace(request.ReturnURL)
	}
	if frontURL == "" {
		return pluginpayment.Action{}, fmt.Errorf("%w: front_url", pluginpayment.ErrInvalidConfiguration)
	}
	values := map[string]string{
		"version":      apiVersion,
		"encoding":     "UTF-8",
		"txnType":      "01",
		"txnSubType":   "01",
		"bizType":      "000201",
		"channelType":  "07",
		"accessType":   "0",
		"currencyCode": "156",
		"merId":        config["mer_id"],
		"certId":       config["cert_id"],
		"orderId":      request.TradeNo,
		"txnAmt":       strconv.FormatInt(request.Amount, 10),
		"txnTime":      plugin.now().Format("20060102150405"),
		"frontUrl":     frontURL,
		"backUrl":      request.NotifyURL,
		"signMethod":   signMethodSHA256,
	}
	signature, err := signValues(privateKey, values)
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("sign unionpay request: %w", err)
	}
	values["sign"] = signature
	endpoint := pluginpayment.ConfigValue(config, "api_url", DefaultAPIURL)
	mode := strings.ToLower(pluginpayment.ConfigValue(config, "submit_mode", "html"))
	var action pluginpayment.Action
	if mode == "jump" {
		action = pluginpayment.Action{Type: pluginpayment.ActionJump, URL: endpoint + "?" + orderedValues(values).Encode()}
	} else {
		action = pluginpayment.Action{Type: pluginpayment.ActionHTML, Data: formHTML(endpoint, values)}
	}
	if err := action.Validate(); err != nil {
		return pluginpayment.Action{}, err
	}
	return action, nil
}

// VerifyCallback authenticates the UnionPay back notification: parameters are
// URL-decoded form fields, the signature covers every non-empty field except
// sign, and respCode=00 marks a successful transaction.
func (plugin *Plugin) VerifyCallback(_ context.Context, r *http.Request, config map[string]string) (pluginpayment.CallbackResult, error) {
	if r == nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing request", pluginpayment.ErrCallbackRejected)
	}
	resolved, err := pluginpayment.RequireConfig(config, "unionpay_public_key")
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	if r.ContentLength > maxCallbackBytes {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: notification too large", pluginpayment.ErrCallbackRejected)
	}
	if err := r.ParseForm(); err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid notification body", pluginpayment.ErrCallbackRejected)
	}
	values := make(map[string]string, len(r.PostForm))
	for name := range r.PostForm {
		values[name] = r.PostForm.Get(name)
	}
	if len(values) == 0 {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: empty notification", pluginpayment.ErrCallbackRejected)
	}
	if method := strings.TrimSpace(values["signMethod"]); method != "" && method != signMethodSHA256 {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: unsupported signMethod %q", pluginpayment.ErrCallbackRejected, method)
	}
	publicKey, err := signing.ParseRSAPublicKeyPEM(resolved["unionpay_public_key"])
	if err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: unionpay_public_key", pluginpayment.ErrInvalidConfiguration)
	}
	if !verifyValues(publicKey, values) {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid sign", pluginpayment.ErrCallbackRejected)
	}
	if merID := strings.TrimSpace(resolved["mer_id"]); merID != "" && values["merId"] != "" && values["merId"] != merID {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: merId mismatch", pluginpayment.ErrCallbackRejected)
	}
	tradeNo := strings.TrimSpace(values["orderId"])
	if tradeNo == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing orderId", pluginpayment.ErrCallbackRejected)
	}
	paid := strings.TrimSpace(values["respCode"]) == successRespCode
	channelTradeNo := firstNonEmpty(strings.TrimSpace(values["queryId"]), strings.TrimSpace(values["traceNo"]))
	amount := int64(0)
	if paid {
		if channelTradeNo == "" {
			return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing queryId", pluginpayment.ErrCallbackRejected)
		}
		parsed, err := pluginpayment.ParseCentsAmount(values["txnAmt"])
		if err != nil {
			return pluginpayment.CallbackResult{}, err
		}
		amount = parsed
	}
	return pluginpayment.CallbackResult{
		TradeNo:        tradeNo,
		ChannelTradeNo: channelTradeNo,
		Amount:         amount,
		Paid:           paid,
		Raw:            values,
	}, nil
}

// orderedValues sorts the parameters so a generated form stays reproducible.
func orderedValues(values map[string]string) url.Values {
	names := make([]string, 0, len(values))
	for name := range values {
		if strings.TrimSpace(values[name]) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make(url.Values, len(names))
	for _, name := range names {
		ordered.Set(name, values[name])
	}
	return ordered
}

// formHTML renders the auto-submitting form UnionPay expects the browser to POST.
func formHTML(endpoint string, values map[string]string) string {
	var builder strings.Builder
	builder.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
	builder.WriteString(`<title>正在跳转银联收银台</title></head><body>`)
	builder.WriteString(`<form id="`)
	builder.WriteString(defaultFormID)
	builder.WriteString(`" name="`)
	builder.WriteString(defaultFormID)
	builder.WriteString(`" method="POST" action="`)
	builder.WriteString(html.EscapeString(endpoint))
	builder.WriteString(`">`)
	for _, name := range sortedNames(values) {
		builder.WriteString(`<input type="hidden" name="`)
		builder.WriteString(html.EscapeString(name))
		builder.WriteString(`" value="`)
		builder.WriteString(html.EscapeString(values[name]))
		builder.WriteString(`"/>`)
	}
	builder.WriteString(`</form><script>document.forms["`)
	builder.WriteString(defaultFormID)
	builder.WriteString(`"].submit();</script></body></html>`)
	return builder.String()
}

func sortedNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		if strings.TrimSpace(values[name]) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
)
