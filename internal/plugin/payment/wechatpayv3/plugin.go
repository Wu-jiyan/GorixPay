// Package wechatpayv3 implements WeChat Pay APIv3 for the Native, JSAPI and H5
// products: merchant RSA-SHA256 request signing, platform certificate
// verification and AES-256-GCM notification decryption.
package wechatpayv3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

const (
	// DefaultAPIBase is the production WeChat Pay APIv3 host.
	DefaultAPIBase = "https://api.mch.weixin.qq.com"
	nativePath     = "/v3/pay/transactions/native"
	jsapiPath      = "/v3/pay/transactions/jsapi"
	h5Path         = "/v3/pay/transactions/h5"
	// notificationToleranceSeconds bounds the age of a signed notification.
	notificationToleranceSeconds = 300
	// maxNotificationBytes bounds a notification body before it is parsed.
	maxNotificationBytes = 1 << 20
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
		Code:         "wechatpayv3",
		Name:         "微信支付 V3",
		Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment},
	}
}

func (*Plugin) ConfigFields() []pluginpayment.ConfigField {
	return []pluginpayment.ConfigField{
		{Key: "mch_id", Label: "商户号", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "app_id", Label: "应用 APPID", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "serial_no", Label: "商户证书序列号", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "private_key", Label: "商户私钥 (PKCS8 PEM)", Type: pluginpayment.ConfigFieldTextarea, Required: true, Secret: true},
		{Key: "api_v3_key", Label: "APIv3 密钥 (32 字节)", Type: pluginpayment.ConfigFieldPassword, Required: true, Secret: true},
		{Key: "platform_public_key", Label: "平台证书公钥 (PEM)", Type: pluginpayment.ConfigFieldTextarea, Secret: true},
		{Key: "platform_serial", Label: "平台证书序列号", Type: pluginpayment.ConfigFieldText},
		{Key: "api_base_url", Label: "API 地址", Type: pluginpayment.ConfigFieldURL},
	}
}

type orderAmount struct {
	Total    int64  `json:"total"`
	Currency string `json:"currency,omitempty"`
}

type payer struct {
	OpenID string `json:"openid"`
}

type h5Info struct {
	Type    string `json:"type"`
	AppName string `json:"app_name,omitempty"`
	AppURL  string `json:"app_url,omitempty"`
}

type sceneInfo struct {
	PayerClientIP string  `json:"payer_client_ip"`
	H5Info        *h5Info `json:"h5_info,omitempty"`
}

type orderRequest struct {
	AppID       string      `json:"appid"`
	MchID       string      `json:"mchid"`
	Description string      `json:"description"`
	OutTradeNo  string      `json:"out_trade_no"`
	NotifyURL   string      `json:"notify_url,omitempty"`
	Amount      orderAmount `json:"amount"`
	Payer       *payer      `json:"payer,omitempty"`
	SceneInfo   *sceneInfo  `json:"scene_info,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type nativeResponse struct {
	CodeURL string `json:"code_url"`
}

type jsapiResponse struct {
	PrepayID string `json:"prepay_id"`
}

type h5Response struct {
	H5URL string `json:"h5_url"`
}

// productKind normalizes the channel payment type into a WeChat Pay product.
func productKind(paymentType string) string {
	normalized := strings.ToLower(strings.TrimSpace(paymentType))
	switch {
	case strings.Contains(normalized, "jsapi"), strings.Contains(normalized, "mp"):
		return "jsapi"
	case strings.Contains(normalized, "h5"), strings.Contains(normalized, "wap"), strings.Contains(normalized, "mweb"):
		return "h5"
	default:
		return "native"
	}
}

func pathFor(kind string) string {
	switch kind {
	case "jsapi":
		return jsapiPath
	case "h5":
		return h5Path
	default:
		return nativePath
	}
}

func (plugin *Plugin) CreatePayment(ctx context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "mch_id", "app_id", "serial_no", "private_key")
	if err != nil {
		return pluginpayment.Action{}, err
	}
	privateKey, err := signing.ParseRSAPrivateKeyPEM(config["private_key"])
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("%w: private_key", pluginpayment.ErrInvalidConfiguration)
	}
	if request.TradeNo == "" || request.Amount <= 0 {
		return pluginpayment.Action{}, errors.New("wechat pay requires a trade number and a positive amount")
	}
	kind := productKind(request.PaymentType)
	body, err := buildOrder(kind, request, config)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	path := pathFor(kind)
	timestamp := strconv.FormatInt(plugin.now().Unix(), 10)
	nonce, err := nonceString()
	if err != nil {
		return pluginpayment.Action{}, err
	}
	signature, err := signMessage(privateKey, requestMessage(http.MethodPost, path, timestamp, nonce, string(encoded)))
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("sign wechat pay request: %w", err)
	}
	baseURL := strings.TrimRight(pluginpayment.ConfigValue(config, "api_base_url", DefaultAPIBase), "/")
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, strings.NewReader(string(encoded)))
	if err != nil {
		return pluginpayment.Action{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", "gorixpay/1.0")
	httpRequest.Header.Set("Authorization", authorization(config["mch_id"], config["serial_no"], nonce, timestamp, signature))
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("create wechat pay order: %w", err)
	}
	defer response.Body.Close()
	raw, err := pluginpayment.ReadUpstreamBody(response.Body)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure apiError
		if json.Unmarshal(raw, &failure) == nil && failure.Code != "" {
			return pluginpayment.Action{}, fmt.Errorf("wechat pay rejected the order: %s %s", failure.Code, failure.Message)
		}
		return pluginpayment.Action{}, fmt.Errorf("wechat pay rejected the order: status %d", response.StatusCode)
	}
	return buildAction(kind, raw, config, plugin.now())
}

func buildOrder(kind string, request pluginpayment.CreateRequest, config map[string]string) (orderRequest, error) {
	currency := strings.ToUpper(strings.TrimSpace(request.Currency))
	if currency == "" {
		currency = "CNY"
	}
	description := strings.TrimSpace(request.Description)
	if description == "" {
		description = "订单支付"
	}
	body := orderRequest{
		AppID: config["app_id"], MchID: config["mch_id"], Description: description,
		OutTradeNo: request.TradeNo, NotifyURL: request.NotifyURL,
		Amount: orderAmount{Total: request.Amount, Currency: currency},
	}
	switch kind {
	case "jsapi":
		openID := strings.TrimSpace(request.Extra["openid"])
		if openID == "" {
			openID = config["openid"]
		}
		if openID == "" {
			return orderRequest{}, fmt.Errorf("%w: openid", pluginpayment.ErrInvalidConfiguration)
		}
		body.Payer = &payer{OpenID: openID}
	case "h5":
		clientIP := strings.TrimSpace(request.ClientIP)
		if clientIP == "" {
			clientIP = "127.0.0.1"
		}
		body.SceneInfo = &sceneInfo{PayerClientIP: clientIP, H5Info: &h5Info{Type: "Wap"}}
	}
	return body, nil
}

func buildAction(kind string, raw []byte, config map[string]string, now time.Time) (pluginpayment.Action, error) {
	switch kind {
	case "jsapi":
		var decoded jsapiResponse
		if err := json.Unmarshal(raw, &decoded); err != nil || decoded.PrepayID == "" {
			return pluginpayment.Action{}, errors.New("invalid wechat pay response")
		}
		data, err := jsapiPayData(config, decoded.PrepayID, now)
		if err != nil {
			return pluginpayment.Action{}, err
		}
		action := pluginpayment.Action{Type: pluginpayment.ActionJSAPI, Data: data}
		if err := action.Validate(); err != nil {
			return pluginpayment.Action{}, err
		}
		return action, nil
	case "h5":
		var decoded h5Response
		if err := json.Unmarshal(raw, &decoded); err != nil || decoded.H5URL == "" {
			return pluginpayment.Action{}, errors.New("invalid wechat pay response")
		}
		action := pluginpayment.Action{Type: pluginpayment.ActionJump, URL: decoded.H5URL}
		if err := action.Validate(); err != nil {
			return pluginpayment.Action{}, err
		}
		return action, nil
	default:
		var decoded nativeResponse
		if err := json.Unmarshal(raw, &decoded); err != nil || decoded.CodeURL == "" {
			return pluginpayment.Action{}, errors.New("invalid wechat pay response")
		}
		action := pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: decoded.CodeURL}
		if err := action.Validate(); err != nil {
			return pluginpayment.Action{}, err
		}
		return action, nil
	}
}

// jsapiPayData renders the WeChat JSAPI bridge parameters, including the RSA
// paySign over appId/timeStamp/nonceStr/package.
func jsapiPayData(config map[string]string, prepayID string, now time.Time) (map[string]string, error) {
	privateKey, err := signing.ParseRSAPrivateKeyPEM(config["private_key"])
	if err != nil {
		return nil, fmt.Errorf("%w: private_key", pluginpayment.ErrInvalidConfiguration)
	}
	nonce, err := nonceString()
	if err != nil {
		return nil, err
	}
	timeStamp := strconv.FormatInt(now.Unix(), 10)
	packageValue := "prepay_id=" + prepayID
	paySign, err := signMessage(privateKey, config["app_id"]+"\n"+timeStamp+"\n"+nonce+"\n"+packageValue+"\n")
	if err != nil {
		return nil, fmt.Errorf("sign wechat pay jsapi parameters: %w", err)
	}
	return map[string]string{
		"appId":     config["app_id"],
		"timeStamp": timeStamp,
		"nonceStr":  nonce,
		"package":   packageValue,
		"signType":  "RSA",
		"paySign":   paySign,
	}, nil
}

type notification struct {
	ID           string `json:"id"`
	EventType    string `json:"event_type"`
	ResourceType string `json:"resource_type"`
	Resource     struct {
		Algorithm      string `json:"algorithm"`
		Ciphertext     string `json:"ciphertext"`
		AssociatedData string `json:"associated_data"`
		OriginalType   string `json:"original_type"`
		Nonce          string `json:"nonce"`
	} `json:"resource"`
}

type transaction struct {
	AppID          string `json:"appid"`
	MchID          string `json:"mchid"`
	OutTradeNo     string `json:"out_trade_no"`
	TransactionID  string `json:"transaction_id"`
	TradeState     string `json:"trade_state"`
	TradeStateDesc string `json:"trade_state_desc"`
	SuccessTime    string `json:"success_time"`
	Amount         struct {
		Total      int64  `json:"total"`
		PayerTotal int64  `json:"payer_total"`
		Currency   string `json:"currency"`
	} `json:"amount"`
}

// VerifyCallback authenticates the Wechatpay-* headers against the configured
// platform certificate and decrypts the AEAD_AES_256_GCM resource.
func (plugin *Plugin) VerifyCallback(_ context.Context, r *http.Request, config map[string]string) (pluginpayment.CallbackResult, error) {
	if r == nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing request", pluginpayment.ErrCallbackRejected)
	}
	resolved, err := pluginpayment.RequireConfig(config, "api_v3_key", "platform_public_key")
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	if r.ContentLength > maxNotificationBytes {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: notification too large", pluginpayment.ErrCallbackRejected)
	}
	body, err := pluginpayment.ReadUpstreamBody(r.Body)
	if err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: unreadable notification", pluginpayment.ErrCallbackRejected)
	}
	timestamp := strings.TrimSpace(r.Header.Get("Wechatpay-Timestamp"))
	nonce := strings.TrimSpace(r.Header.Get("Wechatpay-Nonce"))
	signature := strings.TrimSpace(r.Header.Get("Wechatpay-Signature"))
	serial := strings.TrimSpace(r.Header.Get("Wechatpay-Serial"))
	if timestamp == "" || nonce == "" || signature == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing signature headers", pluginpayment.ErrCallbackRejected)
	}
	if expected := strings.TrimSpace(resolved["platform_serial"]); expected != "" && serial != "" && serial != expected {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: platform serial mismatch", pluginpayment.ErrCallbackRejected)
	}
	publicKey, err := signing.ParseRSAPublicKeyPEM(resolved["platform_public_key"])
	if err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: platform_public_key", pluginpayment.ErrInvalidConfiguration)
	}
	if !verifyMessage(publicKey, notificationMessage(timestamp, nonce, string(body)), signature) {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid signature", pluginpayment.ErrCallbackRejected)
	}
	issuedAt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid timestamp", pluginpayment.ErrCallbackRejected)
	}
	if delta := plugin.now().Unix() - issuedAt; delta > notificationToleranceSeconds || delta < -notificationToleranceSeconds {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: stale notification", pluginpayment.ErrCallbackRejected)
	}
	var decoded notification
	if err := json.Unmarshal(body, &decoded); err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid notification body", pluginpayment.ErrCallbackRejected)
	}
	if decoded.Resource.Algorithm != "AEAD_AES_256_GCM" || decoded.Resource.Ciphertext == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: unsupported resource", pluginpayment.ErrCallbackRejected)
	}
	plain, err := decryptResource(resolved["api_v3_key"], decoded.Resource.Ciphertext, decoded.Resource.Nonce, decoded.Resource.AssociatedData)
	if err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: %v", pluginpayment.ErrCallbackRejected, err)
	}
	var payload transaction
	if err := json.Unmarshal(plain, &payload); err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid decrypted resource", pluginpayment.ErrCallbackRejected)
	}
	if mchID := strings.TrimSpace(resolved["mch_id"]); mchID != "" && payload.MchID != "" && payload.MchID != mchID {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: mchid mismatch", pluginpayment.ErrCallbackRejected)
	}
	if appID := strings.TrimSpace(resolved["app_id"]); appID != "" && payload.AppID != "" && payload.AppID != appID {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: appid mismatch", pluginpayment.ErrCallbackRejected)
	}
	tradeNo := strings.TrimSpace(payload.OutTradeNo)
	if tradeNo == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing out_trade_no", pluginpayment.ErrCallbackRejected)
	}
	paid := decoded.EventType == "TRANSACTION.SUCCESS" && payload.TradeState == "SUCCESS"
	amount := int64(0)
	if paid {
		if payload.Amount.Total <= 0 {
			return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing amount", pluginpayment.ErrCallbackRejected)
		}
		amount = payload.Amount.Total
	}
	return pluginpayment.CallbackResult{
		TradeNo:        tradeNo,
		ChannelTradeNo: strings.TrimSpace(payload.TransactionID),
		Amount:         amount,
		Paid:           paid,
		Raw: map[string]string{
			"event_type":     decoded.EventType,
			"id":             decoded.ID,
			"out_trade_no":   tradeNo,
			"trade_state":    payload.TradeState,
			"transaction_id": payload.TransactionID,
			"appid":          payload.AppID,
			"mchid":          payload.MchID,
			"amount_total":   strconv.FormatInt(payload.Amount.Total, 10),
		},
	}, nil
}

// compile-time assertions
var (
	_ pluginpayment.Plugin           = (*Plugin)(nil)
	_ pluginpayment.Configurable     = (*Plugin)(nil)
	_ pluginpayment.CallbackVerifier = (*Plugin)(nil)
)
