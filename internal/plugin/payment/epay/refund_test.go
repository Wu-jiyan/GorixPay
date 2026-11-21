package epay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

// TestRefundCallsActRefund pins the upstream refund contract: act=refund on
// /api.php, signed with the same MD5 key, and a rejected refund surfaces the
// upstream message instead of reporting success.
func TestRefundCallsActRefund(t *testing.T) {
	var capturedForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api.php" {
			t.Errorf("path=%q", request.URL.Path)
		}
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		capturedForm = request.PostForm
		_, _ = writer.Write([]byte(`{"code":1,"trade_no":"20261121001","refund_no":"RF-OUT-1"}`))
	}))
	defer server.Close()

	result, err := New(server.Client()).Refund(context.Background(), pluginpayment.RefundRequest{
		TradeNo: "202611152200000001", RefundNo: "RFX1", Amount: 1,
		Configuration: map[string]string{"api_url": server.URL, "pid": "1001", "key": "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChannelRefundNo != "RF-OUT-1" {
		t.Fatalf("channel refund no=%q", result.ChannelRefundNo)
	}
	params := make(map[string]any, len(capturedForm))
	for name := range capturedForm {
		params[name] = capturedForm.Get(name)
	}
	if capturedForm.Get("act") != "refund" {
		t.Fatalf("act=%q", capturedForm.Get("act"))
	}
	if capturedForm.Get("money") != "0.01" || capturedForm.Get("trade_no") != "202611152200000001" {
		t.Fatalf("form=%v", capturedForm)
	}
	if !signing.VerifyMD5(params, "secret", capturedForm.Get("sign")) {
		t.Fatal("refund signature does not verify")
	}
}

// TestRefundSurfacesUpstreamRejection keeps the failure path honest.
func TestRefundSurfacesUpstreamRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"code":-1,"msg":"订单不存在"}`))
	}))
	defer server.Close()
	_, err := New(server.Client()).Refund(context.Background(), pluginpayment.RefundRequest{
		TradeNo: "T1", RefundNo: "R1", Amount: 1,
		Configuration: map[string]string{"api_url": server.URL, "pid": "1001", "key": "secret"},
	})
	if err == nil || !strings.Contains(err.Error(), "订单不存在") {
		t.Fatalf("err=%v", err)
	}
}
