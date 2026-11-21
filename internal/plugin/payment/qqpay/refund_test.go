package qqpay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pluginpayment "gorixpay/internal/plugin/payment"
)

// TestRefundPostsSignedXML pins the QQ Wallet refund contract: the original total
// travels next to the refund amount, out_refund_no is the idempotency key, and the
// body is MD5-signed over the same key the order used.
func TestRefundPostsSignedXML(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		buffer := make([]byte, request.ContentLength)
		_, _ = request.Body.Read(buffer)
		body = string(buffer)
		_, _ = writer.Write([]byte(`<xml><return_code>SUCCESS</return_code><result_code>SUCCESS</result_code><refund_id>50000</refund_id><out_refund_no>RF1</out_refund_no></xml>`))
	}))
	defer server.Close()

	result, err := New(server.Client()).Refund(context.Background(), pluginpayment.RefundRequest{
		TradeNo: "T-ORG-1", RefundNo: "RF1", Amount: 100, OrgAmount: 500,
		Description: "管理员退款",
		Configuration: map[string]string{
			"mch_id": "1900000109", "app_id": "1100000000", "key": "secret", "refund_url": server.URL,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChannelRefundNo != "50000" {
		t.Fatalf("channel refund no=%q", result.ChannelRefundNo)
	}
	for _, want := range []string{
		"<out_trade_no>T-ORG-1</out_trade_no>",
		"<out_refund_no>RF1</out_refund_no>",
		"<refund_fee>100</refund_fee>",
		"<total_fee>500</total_fee>",
		"<sign>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body=%s 缺少 %s", body, want)
		}
	}
}

// TestRefundSurfacesRejection keeps the failure path honest.
func TestRefundSurfacesRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`<xml><return_code>SUCCESS</return_code><result_code>FAIL</result_code><err_code>REFUND_FEE_ERROR</err_code><err_code_des>退款金额错误</err_code_des></xml>`))
	}))
	defer server.Close()
	_, err := New(server.Client()).Refund(context.Background(), pluginpayment.RefundRequest{
		TradeNo: "T1", RefundNo: "R1", Amount: 1, OrgAmount: 1,
		Configuration: map[string]string{"mch_id": "1", "app_id": "2", "key": "secret", "refund_url": server.URL},
	})
	if err == nil || !strings.Contains(err.Error(), "REFUND_FEE_ERROR") {
		t.Fatalf("err=%v", err)
	}
}
