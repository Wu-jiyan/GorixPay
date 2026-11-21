package jeepay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	pluginpayment "gorixpay/internal/plugin/payment"
)

// TestRefundPostsSignedForm pins the Jeepay refund call: same MD5 signature as the
// unified order, mchOrderNo addressing the original payment, mchRefundNo as the
// idempotency key.
func TestRefundPostsSignedForm(t *testing.T) {
	var captured url.Values
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/api/refund/refundOrder") {
			t.Errorf("path=%q", request.URL.Path)
		}
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		captured = request.PostForm
		_, _ = writer.Write([]byte(`{"code":0,"msg":"ok","data":{"refundOrderId":"R20261121001","state":1}}`))
	}))
	defer server.Close()

	result, err := New(server.Client()).Refund(context.Background(), pluginpayment.RefundRequest{
		TradeNo: "MCH-ORDER-1", RefundNo: "RF-J1", Amount: 100, OrgAmount: 500,
		Configuration: map[string]string{
			"api_url": server.URL, "mch_no": "M1621873433953", "app_id": "60cc09bce4b80c1d1a0fea7a", "key": "secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChannelRefundNo != "R20261121001" {
		t.Fatalf("channel refund no=%q", result.ChannelRefundNo)
	}
	for key, want := range map[string]string{
		"mchOrderNo":   "MCH-ORDER-1",
		"mchRefundNo":  "RF-J1",
		"refundAmount": "100",
		"signType":     "MD5",
	} {
		if got := captured.Get(key); got != want {
			t.Errorf("%s=%q want %q", key, got, want)
		}
	}
	params := make(map[string]string, len(captured))
	for name := range captured {
		params[name] = captured.Get(name)
	}
	if !verify(params, "secret", captured.Get("sign")) {
		t.Fatal("refund signature does not verify")
	}
}
