package epay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

const maxResponseBytes = 64 * 1024

type Plugin struct {
	client *http.Client
}

type createResponse struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	TradeNo string `json:"trade_no"`
	PayType string `json:"pay_type"`
	PayInfo string `json:"pay_info"`
}

// New builds the plugin. A nil client gets the shared default with a timeout:
// http.DefaultClient has none and would let a hung upstream pin a worker.
func New(client *http.Client) *Plugin {
	return &Plugin{client: pluginpayment.OrDefaultClient(client)}
}

func (*Plugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{Code: "epay", Name: "Epay", Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment}}
}

func (plugin *Plugin) CreatePayment(ctx context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	apiURL := strings.TrimRight(request.Configuration["api_url"], "/")
	pid := strings.TrimSpace(request.Configuration["pid"])
	key := strings.TrimSpace(request.Configuration["key"])
	if apiURL == "" || pid == "" || key == "" {
		return pluginpayment.Action{}, errors.New("invalid epay channel configuration")
	}
	params := map[string]any{
		"pid": pid, "type": request.PaymentType, "out_trade_no": request.TradeNo,
		"notify_url": request.NotifyURL, "return_url": request.ReturnURL,
		"name": request.Description, "money": formatAmount(request.Amount), "clientip": request.ClientIP,
		"device": "pc", "sign_type": "MD5",
	}
	params["sign"] = signing.SignMD5(params, key)
	values := make(url.Values, len(params))
	for name, value := range params {
		values.Set(name, fmt.Sprint(value))
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/mapi.php", strings.NewReader(values.Encode()))
	if err != nil {
		return pluginpayment.Action{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("create upstream payment: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return pluginpayment.Action{}, fmt.Errorf("read upstream payment response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return pluginpayment.Action{}, errors.New("upstream payment response exceeds limit")
	}
	var decoded createResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return pluginpayment.Action{}, errors.New("invalid upstream payment response")
	}
	if decoded.Code != 1 {
		return pluginpayment.Action{}, fmt.Errorf("upstream payment rejected: %s", decoded.Message)
	}
	action := pluginpayment.Action{Type: pluginpayment.ActionType(decoded.PayType)}
	switch action.Type {
	case pluginpayment.ActionJump, pluginpayment.ActionQRCode, pluginpayment.ActionURLScheme:
		action.URL = decoded.PayInfo
	default:
		action.Data = decoded.PayInfo
	}
	if err := action.Validate(); err != nil {
		return pluginpayment.Action{}, err
	}
	return action, nil
}

func formatAmount(amount int64) string {
	whole := amount / 100
	fraction := amount % 100
	return strconv.FormatInt(whole, 10) + "." + fmt.Sprintf("%02d", fraction)
}
