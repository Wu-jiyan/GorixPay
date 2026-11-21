package unionpay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	pluginpayment "gorixpay/internal/plugin/payment"
	"gorixpay/internal/plugin/payment/paymenttest"
)

// TestRefundPostsBackTransReq pins the UnionPay refund call: txnType=04 on the
// back-end gateway, the original queryId as the transaction reference, the refund
// number as orderId, and an amount in 分.
func TestRefundPostsBackTransReq(t *testing.T) {
	material := paymenttest.MustMerchantKeys(t)
	var captured url.Values
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		captured = request.PostForm
		_, _ = writer.Write([]byte("respCode=00&respMsg=success&queryId=20261121REFUND1"))
	}))
	defer server.Close()

	result, err := New(server.Client()).Refund(context.Background(), pluginpayment.RefundRequest{
		TradeNo: "202611152200000001", OrgTradeNo: "202611152200000001001", RefundNo: "RF-U1", Amount: 100,
		Configuration: map[string]string{
			"mer_id": "777290058110048", "cert_id": "1234",
			"private_key": material.PrivatePEM, "refund_url": server.URL,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChannelRefundNo != "20261121REFUND1" {
		t.Fatalf("channel refund no=%q", result.ChannelRefundNo)
	}
	for key, want := range map[string]string{
		"txnType":   "04",
		"orderId":   "RF-U1",
		"origQryId": "202611152200000001001",
		"txnAmt":    "100",
		"merId":     "777290058110048",
		"certId":    "1234",
	} {
		if got := captured.Get(key); got != want {
			t.Errorf("%s=%q want %q", key, got, want)
		}
	}
	if captured.Get("signature") == "" {
		t.Fatal("missing signature")
	}
}

// TestRefundSurfacesRejection keeps the failure path honest.
func TestRefundSurfacesRejection(t *testing.T) {
	material := paymenttest.MustMerchantKeys(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("respCode=34&respMsg=重复交易"))
	}))
	defer server.Close()
	_, err := New(server.Client()).Refund(context.Background(), pluginpayment.RefundRequest{
		TradeNo: "T1", OrgTradeNo: "Q1", RefundNo: "R1", Amount: 1,
		Configuration: map[string]string{
			"mer_id": "777", "cert_id": "1", "private_key": material.PrivatePEM, "refund_url": server.URL,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "34") {
		t.Fatalf("err=%v", err)
	}
}
