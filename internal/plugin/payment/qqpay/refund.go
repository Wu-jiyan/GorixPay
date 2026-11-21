package qqpay

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	pluginpayment "gorixpay/internal/plugin/payment"
)

// DefaultRefundURL is the QQ Wallet refund endpoint. It takes the original total
// next to the refund amount, so the caller has to pass both.
const DefaultRefundURL = "https://qpay.qq.com/cgi-bin/pay/qpay_refund.cgi"

type refundResponse struct {
	XMLName     xml.Name `xml:"xml"`
	ReturnCode  string   `xml:"return_code"`
	ReturnMsg   string   `xml:"return_msg"`
	ResultCode  string   `xml:"result_code"`
	ErrCode     string   `xml:"err_code"`
	ErrCodeDes  string   `xml:"err_code_des"`
	RefundID    string   `xml:"refund_id"`
	OutRefundNo string   `xml:"out_refund_no"`
}

// Refund returns money for a paid QQ Wallet order. out_refund_no is the
// idempotency key: a repeat with the same value returns the same refund.
func (plugin *Plugin) Refund(ctx context.Context, request pluginpayment.RefundRequest) (pluginpayment.RefundResult, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "mch_id", "app_id", "key")
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	refundNo := strings.TrimSpace(request.RefundNo)
	if refundNo == "" || request.Amount <= 0 || strings.TrimSpace(request.TradeNo) == "" {
		return pluginpayment.RefundResult{}, errors.New("qq pay refund requires a trade number, a refund number and a positive amount")
	}
	orgAmount := request.OrgAmount
	if orgAmount <= 0 {
		orgAmount = request.Amount
	}
	nonce, err := nonceString()
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	values := map[string]string{
		"mch_id":        config["mch_id"],
		"appid":         config["app_id"],
		"nonce_str":     nonce,
		"out_trade_no":  request.TradeNo,
		"out_refund_no": refundNo,
		"refund_fee":    strconv.FormatInt(request.Amount, 10),
		"total_fee":     strconv.FormatInt(orgAmount, 10),
		"op_user_id":    config["mch_id"],
	}
	if reason := strings.TrimSpace(request.Description); reason != "" {
		values["refund_desc"] = reason
	}
	values["sign"] = signParams(values, config["key"])
	payload, err := encodeXML(values)
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	endpoint := pluginpayment.ConfigValue(config, "refund_url", DefaultRefundURL)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(payload))
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "text/xml; charset=utf-8")
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("qq pay refund request: %w", err)
	}
	defer response.Body.Close()
	raw, err := pluginpayment.ReadUpstreamBody(response.Body)
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	var decoded refundResponse
	if err := xml.Unmarshal(raw, &decoded); err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("invalid qq pay refund response: %w", err)
	}
	if decoded.ReturnCode != successCode {
		return pluginpayment.RefundResult{}, fmt.Errorf("qq pay rejected the refund: %s", firstNonEmpty(decoded.ReturnMsg, decoded.ReturnCode))
	}
	if decoded.ResultCode != successCode {
		return pluginpayment.RefundResult{}, fmt.Errorf("qq pay rejected the refund: %s %s",
			firstNonEmpty(decoded.ErrCode, decoded.ResultCode), decoded.ErrCodeDes)
	}
	reference := decoded.RefundID
	if reference == "" {
		reference = decoded.OutRefundNo
	}
	return pluginpayment.RefundResult{ChannelRefundNo: reference, Amount: request.Amount}, nil
}

var _ pluginpayment.Refunder = (*Plugin)(nil)
