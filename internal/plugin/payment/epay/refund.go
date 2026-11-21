package epay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

// refundAction is the upstream Epay refund API: POST /api.php with act=refund and
// an MD5 signature over the same key the payment used. Upstream answers
// {"code":1,"trade_no":...,"refund_no":...} on success.
const refundAction = "refund"

type refundResponse struct {
	Code     int    `json:"code"`
	Message  string `json:"msg"`
	TradeNo  string `json:"trade_no"`
	RefundNo string `json:"refund_no"`
}

// Refund asks the upstream Epay installation to return money. It is the same
// merchant identity used for payments: pid plus the MD5 key.
func (plugin *Plugin) Refund(ctx context.Context, request pluginpayment.RefundRequest) (pluginpayment.RefundResult, error) {
	apiURL := strings.TrimRight(request.Configuration["api_url"], "/")
	pid := strings.TrimSpace(request.Configuration["pid"])
	key := strings.TrimSpace(request.Configuration["key"])
	if apiURL == "" || pid == "" || key == "" {
		return pluginpayment.RefundResult{}, errors.New("invalid epay channel configuration")
	}
	if request.Amount <= 0 || strings.TrimSpace(request.TradeNo) == "" {
		return pluginpayment.RefundResult{}, errors.New("epay refund requires a trade number and a positive amount")
	}
	params := map[string]any{
		"act":          refundAction,
		"pid":          pid,
		"trade_no":     request.TradeNo,
		"out_trade_no": strings.TrimSpace(request.TradeNo),
		"money":        formatAmount(request.Amount),
		"api_trade_no": strings.TrimSpace(request.OrgTradeNo),
	}
	if refundNo := strings.TrimSpace(request.RefundNo); refundNo != "" {
		params["out_refund_no"] = refundNo
	}
	params["sign"] = signing.SignMD5(params, key)
	params["sign_type"] = "MD5"
	values := make(url.Values, len(params))
	for name, value := range params {
		values.Set(name, fmt.Sprint(value))
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api.php", strings.NewReader(values.Encode()))
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("upstream refund request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("read upstream refund response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return pluginpayment.RefundResult{}, errors.New("upstream refund response exceeds limit")
	}
	var decoded refundResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return pluginpayment.RefundResult{}, errors.New("invalid upstream refund response")
	}
	if decoded.Code != 1 {
		return pluginpayment.RefundResult{}, fmt.Errorf("upstream refund rejected: %s", decoded.Message)
	}
	reference := decoded.RefundNo
	if reference == "" {
		reference = decoded.TradeNo
	}
	return pluginpayment.RefundResult{ChannelRefundNo: reference, Amount: request.Amount}, nil
}

var _ pluginpayment.Refunder = (*Plugin)(nil)
