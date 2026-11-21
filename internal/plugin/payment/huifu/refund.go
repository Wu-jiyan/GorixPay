package huifu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	pluginpayment "gorixpay/internal/plugin/payment"
)

// Refund returns money for a paid 托管 order. Huifu exposes two different refund
// endpoints and the right one depends on how the payment was taken, exactly like
// upstream's huifu_plugin.php which calls hotRefund for 托管 and scanpay/refund
// for the direct product.
func (plugin *Plugin) Refund(ctx context.Context, request pluginpayment.RefundRequest) (pluginpayment.RefundResult, error) {
	client, err := newClient(request.Configuration, plugin.client)
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	if strings.TrimSpace(request.RefundNo) == "" || request.Amount <= 0 {
		return pluginpayment.RefundResult{}, fmt.Errorf("%w: refund number and amount", pluginpayment.ErrInvalidConfiguration)
	}
	huifuID := pluginpayment.ConfigValue(request.Configuration, "huifu_id", client.sysID)
	refundDate := request.RefundNo
	if len(refundDate) > 8 {
		refundDate = refundDate[:8]
	}
	orgDate := request.TradeNo
	if len(orgDate) > 8 {
		orgDate = orgDate[:8]
	}

	var path string
	data := map[string]json.RawMessage{
		"req_date":   rawValue(refundDate),
		"req_seq_id": rawValue(request.RefundNo),
		"huifu_id":   rawValue(huifuID),
		"ord_amt":    rawValue(pluginpayment.FormatAmount(request.Amount)),
	}
	if hostingMode(request.Configuration) {
		path = hostingRefundPath
		data["org_req_date"] = rawValue(orgDate)
		data["org_req_seq_id"] = rawValue(orgReqSeqID(request))
	} else {
		path = directRefundPath
		data["org_req_seq_id"] = rawValue(orgReqSeqID(request))
		data["org_req_date"] = rawValue(orgDate)
	}

	response, err := client.post(ctx, path, data)
	if err != nil {
		return pluginpayment.RefundResult{}, err
	}
	if err := businessError(response); err != nil {
		return pluginpayment.RefundResult{}, err
	}
	result := pluginpayment.RefundResult{Amount: request.Amount}
	if value := strings.TrimSpace(rawString(response, "hf_seq_id")); value != "" {
		result.ChannelRefundNo = value
	}
	return result, nil
}

// orgReqSeqID is the original request sequence the channel knows: the channel's
// own transaction number when it gave one, otherwise our trade number, which is
// what we sent as req_seq_id at payment time.
func orgReqSeqID(request pluginpayment.RefundRequest) string {
	if trimmed := strings.TrimSpace(request.OrgTradeNo); trimmed != "" {
		return trimmed
	}
	return request.TradeNo
}
