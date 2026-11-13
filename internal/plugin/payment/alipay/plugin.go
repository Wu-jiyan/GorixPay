// Package alipay implements the Alipay "face to face" (当面付) channel through
// the alipay.trade.precreate API: an RSA2-signed request that returns a QR code
// the buyer scans, plus asynchronous notifications verified with the Alipay
// public key.
package alipay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

const (
	// DefaultGatewayURL is the production Alipay open-platform gateway.
	DefaultGatewayURL = "https://openapi.alipay.com/gateway.do"
	// precreateMethod is the face-to-face pre-order API.
	precreateMethod = "alipay.trade.precreate"
	// maxCallbackBytes bounds a notification body before it is parsed.
	maxCallbackBytes = 1 << 20
	// maxSubjectBytes mirrors the 256 byte subject limit of the open platform.
	maxSubjectBytes = 256
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
		Code:         "alipay",
		Name:         "支付宝当面付",
		Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment},
	}
}

func (*Plugin) ConfigFields() []pluginpayment.ConfigField {
	return []pluginpayment.ConfigField{
		{Key: "app_id", Label: "应用 APPID", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "private_key", Label: "应用私钥 (PKCS8 PEM)", Type: pluginpayment.ConfigFieldTextarea, Required: true, Secret: true},
		{Key: "alipay_public_key", Label: "支付宝公钥 (PEM)", Type: pluginpayment.ConfigFieldTextarea, Required: true, Secret: true},
		{Key: "gateway_url", Label: "网关地址", Type: pluginpayment.ConfigFieldURL},
	}
}

type precreateResponse struct {
	Response struct {
		Code    string `json:"code"`
		Msg     string `json:"msg"`
		SubCode string `json:"sub_code"`
		SubMsg  string `json:"sub_msg"`
		QRCode  string `json:"qr_code"`
	} `json:"alipay_trade_precreate_response"`
}

func (plugin *Plugin) CreatePayment(ctx context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "app_id", "private_key")
	if err != nil {
		return pluginpayment.Action{}, err
	}
	privateKey, err := signing.ParseRSAPrivateKeyPEM(config["private_key"])
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("%w: private_key", pluginpayment.ErrInvalidConfiguration)
	}
	if request.TradeNo == "" || request.Amount <= 0 {
		return pluginpayment.Action{}, errors.New("alipay payment requires a trade number and a positive amount")
	}
	bizContent, err := json.Marshal(map[string]string{
		"out_trade_no":    request.TradeNo,
		"total_amount":    pluginpayment.FormatAmount(request.Amount),
		"subject":         subject(request.Description),
		"timeout_express": "15m",
	})
	if err != nil {
		return pluginpayment.Action{}, err
	}
	params := map[string]any{
		"app_id":      config["app_id"],
		"method":      precreateMethod,
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   plugin.timestamp(),
		"version":     "1.0",
		"biz_content": string(bizContent),
	}
	if request.NotifyURL != "" {
		params["notify_url"] = request.NotifyURL
	}
	signature, err := signing.SignRSA(params, privateKey)
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("sign alipay request: %w", err)
	}
	params["sign"] = signature
	values := make(url.Values, len(params))
	for name, value := range params {
		values.Set(name, fmt.Sprint(value))
	}
	gatewayURL := pluginpayment.ConfigValue(config, "gateway_url", DefaultGatewayURL)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayURL, strings.NewReader(values.Encode()))
	if err != nil {
		return pluginpayment.Action{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("create alipay payment: %w", err)
	}
	defer response.Body.Close()
	body, err := pluginpayment.ReadUpstreamBody(response.Body)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	var decoded precreateResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return pluginpayment.Action{}, errors.New("invalid alipay response")
	}
	if decoded.Response.Code != "10000" || decoded.Response.QRCode == "" {
		return pluginpayment.Action{}, fmt.Errorf("alipay rejected the order: code=%s msg=%s sub_code=%s sub_msg=%s",
			decoded.Response.Code, decoded.Response.Msg, decoded.Response.SubCode, decoded.Response.SubMsg)
	}
	action := pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: decoded.Response.QRCode}
	if err := action.Validate(); err != nil {
		return pluginpayment.Action{}, err
	}
	return action, nil
}

// VerifyCallback authenticates an asynchronous notification. Alipay signs the
// sorted, URL-decoded form values with RSA2, excluding sign and sign_type, which
// is exactly signing.Canonical's rule.
func (plugin *Plugin) VerifyCallback(_ context.Context, r *http.Request, config map[string]string) (pluginpayment.CallbackResult, error) {
	if r == nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing request", pluginpayment.ErrCallbackRejected)
	}
	if r.ContentLength > maxCallbackBytes {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: notification too large", pluginpayment.ErrCallbackRejected)
	}
	if err := r.ParseForm(); err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid notification body", pluginpayment.ErrCallbackRejected)
	}
	resolved, err := pluginpayment.RequireConfig(config, "alipay_public_key")
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	publicKey, err := signing.ParseRSAPublicKeyPEM(resolved["alipay_public_key"])
	if err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: alipay_public_key", pluginpayment.ErrInvalidConfiguration)
	}
	params := make(map[string]any, len(r.Form))
	for name := range r.Form {
		params[name] = r.PostForm.Get(name)
	}
	signature := r.PostForm.Get("sign")
	if signature == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing sign", pluginpayment.ErrCallbackRejected)
	}
	if !signing.VerifyRSA(params, publicKey, signature) {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid sign", pluginpayment.ErrCallbackRejected)
	}
	if appID := resolved["app_id"]; appID != "" && r.PostForm.Get("app_id") != "" && r.PostForm.Get("app_id") != appID {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: app_id mismatch", pluginpayment.ErrCallbackRejected)
	}
	tradeNo := strings.TrimSpace(r.PostForm.Get("out_trade_no"))
	if tradeNo == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing out_trade_no", pluginpayment.ErrCallbackRejected)
	}
	amount, err := pluginpayment.ParseAmount(r.PostForm.Get("total_amount"))
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	return pluginpayment.CallbackResult{
		TradeNo:        tradeNo,
		ChannelTradeNo: strings.TrimSpace(r.PostForm.Get("trade_no")),
		Amount:         amount,
		Paid:           paidStatus(r.PostForm.Get("trade_status")),
		Raw:            pluginpayment.StringMap(r.PostForm),
	}, nil
}

func paidStatus(status string) bool {
	return status == "TRADE_SUCCESS" || status == "TRADE_FINISHED"
}

// timestamp renders Beijing time, the only clock Alipay accepts.
func (plugin *Plugin) timestamp() string {
	location := time.FixedZone("CST", 8*3600)
	return plugin.now().In(location).Format("2006-01-02 15:04:05")
}

func subject(description string) string {
	trimmed := strings.TrimSpace(description)
	if trimmed == "" {
		return "订单支付"
	}
	if len(trimmed) > maxSubjectBytes {
		return trimmed[:maxSubjectBytes]
	}
	return trimmed
}

// compile-time assertions
var (
	_ pluginpayment.Plugin           = (*Plugin)(nil)
	_ pluginpayment.Configurable     = (*Plugin)(nil)
	_ pluginpayment.CallbackVerifier = (*Plugin)(nil)
)
