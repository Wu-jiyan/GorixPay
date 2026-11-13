// Package qqpay implements QQ Wallet (QQ 钱包) Native payment through the
// unified-order XML API with MD5 signing, plus XML notification verification.
package qqpay

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	pluginpayment "gorixpay/internal/plugin/payment"
)

const (
	// DefaultAPIURL is the production QQ Wallet unified-order endpoint.
	DefaultAPIURL = "https://qpay.qq.com/cgi-bin/pay/qpay_unified_order.cgi"
	// tradeTypeNative is the only product this plugin offers today.
	tradeTypeNative = "NATIVE"
	// expirationWindow is advertised through time_expire.
	expirationWindow = 15 * time.Minute
	// maxCallbackBytes bounds a notification body before it is parsed.
	maxCallbackBytes = 1 << 20
	// successCode is the value QQ Pay uses for a successful return/result.
	successCode = "SUCCESS"
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
		Code:         "qqpay",
		Name:         "QQ 钱包",
		Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment},
	}
}

func (*Plugin) ConfigFields() []pluginpayment.ConfigField {
	return []pluginpayment.ConfigField{
		{Key: "mch_id", Label: "商户号", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "app_id", Label: "应用 APPID", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "key", Label: "商户密钥", Type: pluginpayment.ConfigFieldPassword, Required: true, Secret: true},
		{Key: "api_url", Label: "统一下单地址", Type: pluginpayment.ConfigFieldURL},
	}
}

type unifiedResponse struct {
	XMLName    xml.Name `xml:"xml"`
	ReturnCode string   `xml:"return_code"`
	ReturnMsg  string   `xml:"return_msg"`
	ResultCode string   `xml:"result_code"`
	ErrCode    string   `xml:"err_code"`
	ErrCodeDes string   `xml:"err_code_des"`
	CodeURL    string   `xml:"code_url"`
	PrepayID   string   `xml:"prepay_id"`
}

func (plugin *Plugin) CreatePayment(ctx context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "mch_id", "app_id", "key")
	if err != nil {
		return pluginpayment.Action{}, err
	}
	if request.TradeNo == "" || request.Amount <= 0 {
		return pluginpayment.Action{}, fmt.Errorf("%w: trade number and amount", pluginpayment.ErrInvalidConfiguration)
	}
	if request.NotifyURL == "" {
		return pluginpayment.Action{}, fmt.Errorf("%w: notify_url", pluginpayment.ErrInvalidConfiguration)
	}
	nonce, err := nonceString()
	if err != nil {
		return pluginpayment.Action{}, err
	}
	clientIP := strings.TrimSpace(request.ClientIP)
	if clientIP == "" {
		clientIP = "127.0.0.1"
	}
	values := map[string]string{
		"mch_id":           config["mch_id"],
		"appid":            config["app_id"],
		"nonce_str":        nonce,
		"body":             bodyText(request.Description),
		"out_trade_no":     request.TradeNo,
		"total_fee":        strconv.FormatInt(request.Amount, 10),
		"spbill_create_ip": clientIP,
		"notify_url":       request.NotifyURL,
		"trade_type":       tradeTypeNative,
		"time_expire":      plugin.now().Add(expirationWindow).Format("20060102150405"),
	}
	values["sign"] = signParams(values, config["key"])
	payload, err := encodeXML(values)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	endpoint := pluginpayment.ConfigValue(config, "api_url", DefaultAPIURL)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(payload))
	if err != nil {
		return pluginpayment.Action{}, err
	}
	httpRequest.Header.Set("Content-Type", "text/xml; charset=utf-8")
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("create qq pay order: %w", err)
	}
	defer response.Body.Close()
	raw, err := pluginpayment.ReadUpstreamBody(response.Body)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	var decoded unifiedResponse
	if err := xml.Unmarshal(raw, &decoded); err != nil {
		return pluginpayment.Action{}, fmt.Errorf("invalid qq pay response: %w", err)
	}
	if decoded.ReturnCode != successCode {
		return pluginpayment.Action{}, fmt.Errorf("qq pay rejected the order: %s", firstNonEmpty(decoded.ReturnMsg, decoded.ReturnCode))
	}
	if decoded.ResultCode != successCode {
		return pluginpayment.Action{}, fmt.Errorf("qq pay rejected the order: %s %s",
			firstNonEmpty(decoded.ErrCode, decoded.ResultCode), decoded.ErrCodeDes)
	}
	if strings.TrimSpace(decoded.CodeURL) == "" {
		return pluginpayment.Action{}, errors.New("qq pay returned no code_url")
	}
	action := pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: decoded.CodeURL}
	if err := action.Validate(); err != nil {
		return pluginpayment.Action{}, err
	}
	return action, nil
}

// VerifyCallback authenticates the flat XML notification QQ Pay posts. The whole
// document participates in the MD5 signature except sign and empty values.
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
	fields, err := decodeXML(raw)
	if err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: %v", pluginpayment.ErrCallbackRejected, err)
	}
	if !verifySignature(fields, resolved["key"]) {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid sign", pluginpayment.ErrCallbackRejected)
	}
	if mchID := strings.TrimSpace(resolved["mch_id"]); mchID != "" && fields["mch_id"] != "" && fields["mch_id"] != mchID {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: mch_id mismatch", pluginpayment.ErrCallbackRejected)
	}
	tradeNo := strings.TrimSpace(fields["out_trade_no"])
	if tradeNo == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing out_trade_no", pluginpayment.ErrCallbackRejected)
	}
	paid := fields["return_code"] == successCode && fields["result_code"] == successCode
	channelTradeNo := strings.TrimSpace(fields["transaction_id"])
	amount := int64(0)
	if paid {
		channelTradeNo = firstNonEmpty(channelTradeNo, strings.TrimSpace(fields["out_transaction_id"]))
		if channelTradeNo == "" {
			return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing transaction_id", pluginpayment.ErrCallbackRejected)
		}
		parsed, err := pluginpayment.ParseCentsAmount(fields["total_fee"])
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
		Raw:            fields,
	}, nil
}

func bodyText(description string) string {
	trimmed := strings.TrimSpace(description)
	if trimmed == "" {
		return "订单支付"
	}
	if len(trimmed) > 128 {
		return trimmed[:128]
	}
	return trimmed
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
