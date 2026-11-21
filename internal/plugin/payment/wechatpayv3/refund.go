package wechatpayv3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

// refundPath is the APIv3 refund endpoint. The transaction reference goes in the
// URL, the refund terms in the body, and out_refund_no doubles as the idempotency
// key — a repeat with the same value returns the same refund instead of a second
// one.
const refundPath = "/v3/refund/domestic/refunds"

type refundRequest struct {
	TransactionID string       `json:"-"`
	OutTradeNo    string       `json:"-"`
	OutRefundNo   string       `json:"out_refund_no"`
	Reason        string       `json:"reason,omitempty"`
	Amount        refundAmount `json:"amount"`
}

type refundAmount struct {
	Refund   int64  `json:"refund"`
	Total    int64  `json:"total"`
	Currency string `json:"currency"`
}

type refundResponse struct {
	RefundID    string `json:"refund_id"`
	OutRefundNo string `json:"out_refund_no"`
	Status      string `json:"status"`
}

// Refund asks WeChat Pay to return money for a paid transaction. We address the
// transaction by our own out_trade_no, which is what we keep; the channel's
// transaction id is used only when it is known.
func (plugin *Plugin) Refund(ctx context.Context, request pluginpayment.RefundRequest) (pluginpayment.RefundResult, error) {
	config, err := pluginpayment.RequireConfig(request.Configuration, "mch_id", "serial_no", "private_key")
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	privateKey, err := signing.ParseRSAPrivateKeyPEM(config["private_key"])
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("%w: private_key", pluginpayment.ErrInvalidConfiguration)
	}
	refundNo := strings.TrimSpace(request.RefundNo)
	if refundNo == "" {
		return pluginpayment.RefundResult{}, errors.New("wechat pay refund requires a refund number")
	}
	if request.Amount <= 0 {
		return pluginpayment.RefundResult{}, errors.New("wechat pay refund requires a positive amount")
	}
	transactionID := strings.TrimSpace(request.OrgTradeNo)
	outTradeNo := strings.TrimSpace(request.TradeNo)
	path := refundPath
	if transactionID != "" {
		path = refundPath + "/" + transactionID
	} else if outTradeNo != "" {
		path = refundPath + "/out-trade-no/" + outTradeNo
	} else {
		return pluginpayment.RefundResult{}, errors.New("wechat pay refund requires the original trade number")
	}
	body := refundRequest{
		OutRefundNo: refundNo,
		Reason:      strings.TrimSpace(request.Description),
		Amount:      refundAmount{Refund: request.Amount, Total: request.Amount, Currency: "CNY"},
	}
	if currency := strings.ToUpper(strings.TrimSpace(request.Currency)); currency != "" {
		body.Amount.Currency = currency
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	timestamp := strconv.FormatInt(plugin.now().Unix(), 10)
	nonce, err := nonceString()
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	signature, err := signMessage(privateKey, requestMessage(http.MethodPost, path, timestamp, nonce, string(encoded)))
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("sign wechat pay refund: %w", err)
	}
	baseURL := strings.TrimRight(pluginpayment.ConfigValue(config, "api_base_url", DefaultAPIBase), "/")
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, strings.NewReader(string(encoded)))
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", "gorixpay/1.0")
	httpRequest.Header.Set("Authorization", authorization(config["mch_id"], config["serial_no"], nonce, timestamp, signature))
	response, err := plugin.client.Do(httpRequest)
	if err != nil {
		return pluginpayment.RefundResult{}, fmt.Errorf("wechat pay refund request: %w", err)
	}
	defer response.Body.Close()
	raw, err := pluginpayment.ReadUpstreamBody(response.Body)
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure apiError
		if json.Unmarshal(raw, &failure) == nil && failure.Code != "" {
			return pluginpayment.RefundResult{}, fmt.Errorf("wechat pay rejected the refund: %s %s", failure.Code, failure.Message)
		}
		return pluginpayment.RefundResult{}, fmt.Errorf("wechat pay rejected the refund: status %d", response.StatusCode)
	}
	var decoded refundResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return pluginpayment.RefundResult{}, errors.New("invalid wechat pay refund response")
	}
	reference := decoded.RefundID
	if reference == "" {
		reference = decoded.OutRefundNo
	}
	return pluginpayment.RefundResult{ChannelRefundNo: reference, Amount: request.Amount}, nil
}

var _ pluginpayment.Refunder = (*Plugin)(nil)
