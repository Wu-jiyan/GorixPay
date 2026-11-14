// Package usdt implements an Epusdt-style USDT gateway: a signed create call to
// /api/v1/pay/create returning a hosted payment URL, and a signed notification.
package usdt

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
	"strings"

	pluginpayment "gorixpay/internal/plugin/payment"
)

const (
	// createPath is the Epusdt create-transaction endpoint.
	createPath = "/api/v1/pay/create"
	// maxCallbackBytes bounds a notification body before it is parsed.
	maxCallbackBytes = 1 << 20
	// statusPaid is the Epusdt trade status for a confirmed payment.
	statusPaid = "2"
	// successStatus is the HTTP-like status code Epusdt returns on success.
	successStatus = 200
)

type Plugin struct {
	client *http.Client
}

// New builds the plugin. A nil client gets the shared default with a timeout.
func New(client *http.Client) *Plugin {
	return &Plugin{client: pluginpayment.OrDefaultClient(client)}
}

func (*Plugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{
		Code:         "usdt",
		Name:         "USDT (Epusdt)",
		Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment},
	}
}

func (*Plugin) ConfigFields() []pluginpayment.ConfigField {
	return []pluginpayment.ConfigField{
		{Key: "api_url", Label: "网关地址", Type: pluginpayment.ConfigFieldURL, Required: true},
		{Key: "key", Label: "商户密钥", Type: pluginpayment.ConfigFieldPassword, Required: true, Secret: true},
		{Key: "action", Label: "返回方式 (jump/qrcode)", Type: pluginpayment.ConfigFieldSelect},
		{Key: "notify_url", Label: "异步通知地址覆盖", Type: pluginpayment.ConfigFieldURL},
	}
}

type createRequest struct {
	OrderID     string `json:"order_id"`
	Amount      string `json:"amount"`
	NotifyURL   string `json:"notify_url,omitempty"`
	RedirectURL string `json:"redirect_url,omitempty"`
	Sign        string `json:"sign"`
}

type createResponse struct {
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
	Data       struct {
		TradeID      string `json:"trade_id"`
		OrderID      string `json:"order_id"`
		Amount       string `json:"amount"`
		ActualAmount string `json:"actual_amount"`
		Token        string `json:"token"`
		PaymentURL   string `json:"payment_url"`
	} `json:"data"`
}

// createSign is md5(order_id + amount + key), the create-request signature.
func createSign(orderID, amount, key string) string {
	sum := md5.Sum([]byte(orderID + amount + key))
	return strings.ToLower(hex.EncodeToString(sum[:]))
}

// notifySign is md5(trade_id + order_id + amount + status + key). The status is
// part of the digest on purpose: it decides whether the payment counts as paid,
// so leaving it out lets anyone flip a pending notification into a paid one by
// replaying the same signature.
func notifySign(tradeID, orderID, amount, status, key string) string {
	sum := md5.Sum([]byte(tradeID + orderID + amount + status + key))
	return strings.ToLower(hex.EncodeToString(sum[:]))
}

func (plugin *Plugin) CreatePayment(ctx context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "api_url", "key")
	if err != nil {
		return pluginpayment.Action{}, err
	}
	if request.TradeNo == "" || request.Amount <= 0 {
		return pluginpayment.Action{}, fmt.Errorf("%w: trade number and amount", pluginpayment.ErrInvalidConfiguration)
	}
	amount := pluginpayment.FormatAmount(request.Amount)
	notifyURL := pluginpayment.ConfigValue(config, "notify_url", request.NotifyURL)
	body := createRequest{
		OrderID: request.TradeNo, Amount: amount, NotifyURL: notifyURL,
		RedirectURL: request.ReturnURL, Sign: createSign(request.TradeNo, amount, config["key"]),
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	endpoint := strings.TrimRight(config["api_url"], "/") + createPath
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(encoded)))
	if err != nil {
		return pluginpayment.Action{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("create usdt payment: %w", err)
	}
	defer response.Body.Close()
	raw, err := pluginpayment.ReadUpstreamBody(response.Body)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	var decoded createResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return pluginpayment.Action{}, fmt.Errorf("invalid usdt gateway response: %w", err)
	}
	if decoded.StatusCode != successStatus || strings.TrimSpace(decoded.Data.PaymentURL) == "" {
		return pluginpayment.Action{}, fmt.Errorf("usdt gateway rejected the order: %d %s", decoded.StatusCode, decoded.Message)
	}
	actionType := pluginpayment.ActionJump
	if strings.EqualFold(pluginpayment.ConfigValue(config, "action", "jump"), "qrcode") {
		actionType = pluginpayment.ActionQRCode
	}
	action := pluginpayment.Action{Type: actionType, URL: decoded.Data.PaymentURL}
	if err := action.Validate(); err != nil {
		return pluginpayment.Action{}, err
	}
	return action, nil
}

type notification struct {
	TradeID           string `json:"trade_id"`
	OrderID           string `json:"order_id"`
	Amount            string `json:"amount"`
	ActualAmount      string `json:"actual_amount"`
	Token             string `json:"token"`
	BlockTransactions string `json:"block_transaction_id"`
	Status            string `json:"status"`
	Sign              string `json:"sign"`
}

// VerifyCallback authenticates a notification delivered either as JSON or as a
// form post, and reports the amount in cents for the order comparison.
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
	fields, err := notificationFields(raw)
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	payload := notification{
		TradeID: fields["trade_id"], OrderID: fields["order_id"], Amount: fields["amount"],
		ActualAmount: fields["actual_amount"], Token: fields["token"],
		BlockTransactions: fields["block_transaction_id"], Status: fields["status"], Sign: strings.ToLower(strings.TrimSpace(fields["sign"])),
	}
	if !verifySign(payload, resolved["key"]) {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid sign", pluginpayment.ErrCallbackRejected)
	}
	tradeNo := strings.TrimSpace(payload.OrderID)
	if tradeNo == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing order_id", pluginpayment.ErrCallbackRejected)
	}
	amount, err := pluginpayment.ParseAmount(payload.Amount)
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	paid := payload.Status == statusPaid
	if !paid {
		amount = 0
	}
	return pluginpayment.CallbackResult{
		TradeNo:        tradeNo,
		ChannelTradeNo: strings.TrimSpace(payload.TradeID),
		Amount:         amount,
		Paid:           paid,
		Raw:            fields,
	}, nil
}

func verifySign(payload notification, key string) bool {
	if payload.Sign == "" {
		return false
	}
	expected := notifySign(payload.TradeID, payload.OrderID, payload.Amount, payload.Status, key)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(payload.Sign)) == 1
}

// notificationFields accepts both a JSON body and a form post.
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

// compile-time assertions
var (
	_ pluginpayment.Plugin           = (*Plugin)(nil)
	_ pluginpayment.Configurable     = (*Plugin)(nil)
	_ pluginpayment.CallbackVerifier = (*Plugin)(nil)
)
