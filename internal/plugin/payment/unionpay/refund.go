package unionpay

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

// DefaultRefundURL is the UnionPay back-end transaction gateway, which carries the
// refund (txnType=04) and the query (txnType=00) requests. The pay flow here only
// ever posts a form in the browser, so this is the first call the plugin makes
// server-side.
const DefaultRefundURL = "https://gateway.95516.com/gateway/api/backTransReq.do"

// Refund asks UnionPay to return money for a paid order.
//
// UnionPay refunds address the original transaction by its queryId (the id the
// gateway returned when the payment was made), which we keep as the channel trade
// number. Amounts are in 分, the same unit the whole platform uses.
func (plugin *Plugin) Refund(ctx context.Context, request pluginpayment.RefundRequest) (pluginpayment.RefundResult, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "mer_id", "cert_id", "private_key")
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	privateKey, err := signing.ParseRSAPrivateKeyPEM(config["private_key"])
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("%w: private_key", pluginpayment.ErrInvalidConfiguration)
	}
	refundNo := strings.TrimSpace(request.RefundNo)
	if refundNo == "" || request.Amount <= 0 {
		return pluginpayment.RefundResult{}, errors.New("unionpay refund requires a refund number and a positive amount")
	}
	origQryID := strings.TrimSpace(request.OrgTradeNo)
	if origQryID == "" {
		return pluginpayment.RefundResult{}, errors.New("unionpay refund requires the original queryId (channel trade number)")
	}
	values := map[string]string{
		"version":     apiVersion,
		"encoding":    "UTF-8",
		"signMethod":  signMethodSHA256,
		"txnType":     "04",
		"txnSubType":  "00",
		"bizType":     "000201",
		"accessType":  "0",
		"channelType": "07",
		"backUrl":     request.Description,
		"merId":       config["mer_id"],
		"orderId":     refundNo,
		"origQryId":   origQryID,
		"txnAmt":      fmt.Sprint(request.Amount),
		"reqReserved": strings.TrimSpace(request.Description),
	}
	delete(values, "backUrl") // the refund gateway does not take a backUrl
	signature, err := signValues(privateKey, values)
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("sign unionpay refund: %w", err)
	}
	values["signature"] = signature
	values["certId"] = config["cert_id"]
	form := make(url.Values, len(values))
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		form.Set(name, value)
	}
	endpoint := pluginpayment.ConfigValue(config, "refund_url", DefaultRefundURL)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("unionpay refund request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, pluginpayment.MaxUpstreamResponseBytes+1))
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	values2, err := url.ParseQuery(string(body))
	if err != nil {
		return pluginpayment.RefundResult{}, errors.New("invalid unionpay refund response")
	}
	code := values2.Get("respCode")
	if code != successRespCode {
		return pluginpayment.RefundResult{}, fmt.Errorf("unionpay rejected the refund: %s %s", code, values2.Get("respMsg"))
	}
	// The gateway signs its answer; when the merchant configured the platform key
	// the signature is verified, otherwise the code alone is trusted.
	if raw := strings.TrimSpace(config["unionpay_public_key"]); raw != "" {
		if publicKey, err := signing.ParseRSAPublicKeyPEM(raw); err == nil {
			if !verifyValues(publicKey, flatten(values2)) {
				return pluginpayment.RefundResult{}, errors.New("unionpay refund response signature does not verify")
			}
		}
	}
	reference := values2.Get("queryId")
	if reference == "" {
		reference = refundNo
	}
	return pluginpayment.RefundResult{ChannelRefundNo: reference, Amount: request.Amount}, nil
}

// flatten turns the response form into the map the signature check expects.
func flatten(values url.Values) map[string]string {
	result := make(map[string]string, len(values))
	for name := range values {
		result[name] = values.Get(name)
	}
	return result
}

var _ pluginpayment.Refunder = (*Plugin)(nil)
var _ *rsa.PrivateKey
