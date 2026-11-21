package alipay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

// refundMethod is the open-platform refund API (alipay.trade.refund). Upstream
// Epay's alipay plugin calls the same method for its 当面付 refunds.
const refundMethod = "alipay.trade.refund"

type refundResponse struct {
	Response struct {
		Code       string `json:"code"`
		Msg        string `json:"msg"`
		SubCode    string `json:"sub_code"`
		SubMsg     string `json:"sub_msg"`
		TradeNo    string `json:"trade_no"`
		OutTradeNo string `json:"out_trade_no"`
		RefundFee  string `json:"refund_fee"`
		FundChange string `json:"fund_change"`
	} `json:"alipay_trade_refund_response"`
}

// Refund returns money for a paid order by out_trade_no, which is the only
// reference we keep for our own orders. out_request_no is the idempotency key:
// Alipay treats a repeat with the same value as the same refund.
func (plugin *Plugin) Refund(ctx context.Context, request pluginpayment.RefundRequest) (pluginpayment.RefundResult, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "app_id", "private_key")
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	privateKey, err := signing.ParseRSAPrivateKeyPEM(config["private_key"])
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("%w: private_key", pluginpayment.ErrInvalidConfiguration)
	}
	if strings.TrimSpace(request.TradeNo) == "" || request.Amount <= 0 {
		return pluginpayment.RefundResult{}, errors.New("alipay refund requires a trade number and a positive amount")
	}
	refundNo := strings.TrimSpace(request.RefundNo)
	if refundNo == "" {
		return pluginpayment.RefundResult{}, errors.New("alipay refund requires a refund number")
	}
	bizContent, err := json.Marshal(map[string]string{
		"out_trade_no":   request.TradeNo,
		"refund_amount":  pluginpayment.FormatAmount(request.Amount),
		"out_request_no": refundNo,
		"refund_reason":  subject(request.Description),
	})
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	params := map[string]any{
		"app_id":      config["app_id"],
		"method":      refundMethod,
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   plugin.timestamp(),
		"version":     "1.0",
		"biz_content": string(bizContent),
	}
	signature, err := signing.SignRSA(params, privateKey)
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("sign alipay refund: %w", err)
	}
	params["sign"] = signature
	values := make(url.Values, len(params))
	for name, value := range params {
		values.Set(name, fmt.Sprint(value))
	}
	gatewayURL := pluginpayment.ConfigValue(config, "gateway_url", DefaultGatewayURL)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayURL, strings.NewReader(values.Encode()))
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("alipay refund request: %w", err)
	}
	defer response.Body.Close()
	body, err := pluginpayment.ReadUpstreamBody(response.Body)
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	var decoded refundResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return pluginpayment.RefundResult{}, errors.New("invalid alipay refund response")
	}
	if decoded.Response.Code != "10000" {
		return pluginpayment.RefundResult{}, fmt.Errorf("alipay rejected the refund: code=%s msg=%s sub_code=%s sub_msg=%s",
			decoded.Response.Code, decoded.Response.Msg, decoded.Response.SubCode, decoded.Response.SubMsg)
	}
	return pluginpayment.RefundResult{
		ChannelRefundNo: decoded.Response.TradeNo,
		Amount:          request.Amount,
	}, nil
}

var _ pluginpayment.Refunder = (*Plugin)(nil)
