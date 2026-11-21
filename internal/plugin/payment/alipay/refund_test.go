package alipay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	pluginpayment "gorixpay/internal/plugin/payment"
	"gorixpay/internal/plugin/payment/paymenttest"
)

// TestRefundPostsTradeRefund pins the refund call: out_trade_no plus an
// out_request_no idempotency key, signed RSA2, and a rejected refund surfaces the
// platform's sub_code instead of pretending it worked.
func TestRefundPostsTradeRefund(t *testing.T) {
	material := paymenttest.MustMerchantKeys(t)
	var captured url.Values
	gateway := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		captured = request.PostForm
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"alipay_trade_refund_response":{"code":"10000","msg":"Success","trade_no":"2026112122001","refund_fee":"0.01"}}`))
	}))
	defer gateway.Close()

	config := map[string]string{
		"app_id":      "2021000000000000",
		"private_key": material.PrivatePEM,
		"gateway_url": gateway.URL,
	}
	result, err := New(gateway.Client()).Refund(context.Background(), pluginpayment.RefundRequest{
		TradeNo: "202611152200000001", RefundNo: "RF20261121001", Amount: 1,
		Description: "管理员退款", Configuration: config,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChannelRefundNo != "2026112122001" {
		t.Fatalf("channel refund no=%q", result.ChannelRefundNo)
	}
	if method := captured.Get("method"); method != refundMethod {
		t.Fatalf("method=%q want %q", method, refundMethod)
	}
	var biz map[string]string
	if err := json.Unmarshal([]byte(captured.Get("biz_content")), &biz); err != nil {
		t.Fatalf("biz_content=%q err=%v", captured.Get("biz_content"), err)
	}
	for key, want := range map[string]string{
		"out_trade_no":   "202611152200000001",
		"refund_amount":  "0.01",
		"out_request_no": "RF20261121001",
	} {
		if got := biz[key]; got != want {
			t.Errorf("%s=%q want %q", key, got, want)
		}
	}
	if captured.Get("sign") == "" || captured.Get("sign_type") != "RSA2" {
		t.Fatalf("sign=%q sign_type=%q", captured.Get("sign"), captured.Get("sign_type"))
	}
	if !strings.Contains(captured.Get("app_id"), "2021") {
		t.Fatalf("app_id=%q", captured.Get("app_id"))
	}
}

// TestRefundSurfacesPlatformRejection keeps the failure path honest.
func TestRefundSurfacesPlatformRejection(t *testing.T) {
	material := paymenttest.MustMerchantKeys(t)
	gateway := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"alipay_trade_refund_response":{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_NOT_EXIST","sub_msg":"交易不存在"}}`))
	}))
	defer gateway.Close()
	_, err := New(gateway.Client()).Refund(context.Background(), pluginpayment.RefundRequest{
		TradeNo: "202611152200000001", RefundNo: "RF1", Amount: 1,
		Configuration: map[string]string{"app_id": "2021", "private_key": material.PrivatePEM, "gateway_url": gateway.URL},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "TRADE_NOT_EXIST") {
		t.Fatalf("err=%v", err)
	}
}
