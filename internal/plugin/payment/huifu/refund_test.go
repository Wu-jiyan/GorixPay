package huifu

import (
	"context"
	"encoding/json"
	"testing"

	pluginpayment "gorixpay/internal/plugin/payment"
)

// TestRefundUsesHostingEndpointForHostedChannels pins the two refund routes: a
// 托管 channel must refund through htRefund (upstream huifu_plugin.php
// refund_combine), while the direct product uses the scanpay refund. Sending a
// 托管 payment to the scanpay endpoint answers "订单不存在", which looks like a
// refund bug but is really a routing mistake.
func TestRefundUsesHostingEndpointForHostedChannels(t *testing.T) {
	merchant, platform := newKeyMaterial(t), newKeyMaterial(t)
	gateway, recorded := newGateway(t, &merchant.private.PublicKey, platform.private, func(map[string]json.RawMessage) map[string]any {
		return map[string]any{"resp_code": "00000000", "resp_desc": "成功", "hf_seq_id": "HF20261121001", "ord_amt": "0.01"}
	})
	defer gateway.Close()

	result, err := New(gateway.Client()).Refund(context.Background(), pluginpayment.RefundRequest{
		TradeNo: "202611152200000001", RefundNo: "AR20261121123456", Amount: 1, Currency: "CNY",
		Description: "管理员退款",
		Configuration: createConfig(merchant, platform, gateway.URL, map[string]string{
			"project_id": "PROJECTID2026051265060546",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChannelRefundNo != "HF20261121001" {
		t.Fatalf("channel refund no=%q", result.ChannelRefundNo)
	}
	for key, want := range map[string]string{
		"req_seq_id":     "AR20261121123456",
		"req_date":       "AR202611",
		"ord_amt":        "0.01",
		"org_req_seq_id": "202611152200000001",
		"org_req_date":   "20261115",
		"huifu_id":       "6666000197954883",
	} {
		if got := fieldString(t, recorded.data, key); got != want {
			t.Errorf("%s=%q want %q", key, got, want)
		}
	}
}
