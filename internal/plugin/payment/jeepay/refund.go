package jeepay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	pluginpayment "gorixpay/internal/plugin/payment"
)

// refundPath is the Jeepay refund endpoint; it takes the same MD5 signature as
// the unified order and answers a JSON envelope with the refund order number.
const refundPath = "/api/refund/refundOrder"

type refundEnvelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		RefundOrderId string `json:"refundOrderId"`
		MchRefundNo   string `json:"mchRefundNo"`
		State         int    `json:"state"`
	} `json:"data"`
}

// Refund returns money for a paid Jeepay order through the same application the
// payment used.
func (plugin *Plugin) Refund(ctx context.Context, request pluginpayment.RefundRequest) (pluginpayment.RefundResult, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "api_url", "mch_no", "app_id", "key")
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	refundNo := strings.TrimSpace(request.RefundNo)
	if refundNo == "" || request.Amount <= 0 || strings.TrimSpace(request.TradeNo) == "" {
		return pluginpayment.RefundResult{}, errors.New("jeepay refund requires a trade number, a refund number and a positive amount")
	}
	at := plugin.now()
	values := map[string]string{
		"version":      "1.0",
		"reqTime":      at.Format("20060102150405"),
		"reqId":        fmt.Sprintf("%d-%s", at.UnixNano(), refundNo),
		"mchNo":        config["mch_no"],
		"appId":        config["app_id"],
		"mchOrderNo":   request.TradeNo,
		"refundAmount": fmt.Sprint(request.Amount),
		"mchRefundNo":  refundNo,
		"currency":     "cny",
		"refundReason": strings.TrimSpace(request.Description),
		"signType":     "MD5",
	}
	values["sign"] = sign(values, config["key"])
	form := make(url.Values, len(values))
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		form.Set(name, value)
	}
	endpoint := strings.TrimRight(config["api_url"], "/") + refundPath
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("jeepay refund request: %w", err)
	}
	defer response.Body.Close()
	raw, err := pluginpayment.ReadUpstreamBody(response.Body)
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	var decoded refundEnvelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("invalid jeepay refund response: %w", err)
	}
	if decoded.Code != 0 {
		return pluginpayment.RefundResult{}, fmt.Errorf("jeepay rejected the refund: %s (%d)", decoded.Msg, decoded.Code)
	}
	reference := decoded.Data.RefundOrderId
	if reference == "" {
		reference = decoded.Data.MchRefundNo
	}
	return pluginpayment.RefundResult{ChannelRefundNo: reference, Amount: request.Amount}, nil
}

var _ pluginpayment.Refunder = (*Plugin)(nil)
