package huifu

// Live gateway test. It is skipped unless HUIFU_LIVE_CONFIG points at a JSON file
// with real credentials:
//
//	{"sys_id":"...","product_id":"...","private_key":"<base64 or PEM>",
//	 "huifu_public_key":"<base64 or PEM>","huifu_id":""}
//
// Run:
//
//	HUIFU_LIVE_CONFIG=/path/to/live.json go test ./internal/plugin/payment/huifu -run TestLiveGatewayCreate -v
//
// It creates ONE real 0.01 CNY order so the signature, the request envelope and
// the response verification are exercised against api.huifu.com. Nothing is paid:
// the order simply expires, and the trade number is printed so an operator can
// close it with /v2/trade/payment/scanpay/close.

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	pluginpayment "gorixpay/internal/plugin/payment"
)

func TestLiveGatewayCreate(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("HUIFU_LIVE_CONFIG"))
	if path == "" {
		t.Skip("set HUIFU_LIVE_CONFIG to run against the real Huifu gateway")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read live config: %v", err)
	}
	config := map[string]string{}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("parse live config: %v", err)
	}
	if strings.TrimSpace(config["sys_id"]) == "" || strings.TrimSpace(config["private_key"]) == "" {
		t.Fatal("live config needs sys_id and private_key")
	}

	now := time.Now()
	tradeNo := now.Format("20060102150405") + strconv.Itoa(now.Nanosecond()%100000)
	plugin := New(nil)
	action, err := plugin.CreatePayment(context.Background(), pluginpayment.CreateRequest{
		TradeNo:     tradeNo,
		Amount:      1, // 0.01 CNY
		Currency:    "CNY",
		Description: "GorixPay 联调测试(未支付自动过期)",
		PaymentType: "alipay",
		NotifyURL:   "https://example.com/pay/notify/" + tradeNo + "/",
		ClientIP:    "127.0.0.1",
		Configuration: map[string]string{
			"sys_id":           config["sys_id"],
			"product_id":       config["product_id"],
			"private_key":      config["private_key"],
			"huifu_public_key": config["huifu_public_key"],
			"huifu_id":         config["huifu_id"],
		},
	})
	if err != nil {
		t.Fatalf("live create: %v", err)
	}
	if action.Type != pluginpayment.ActionQRCode {
		t.Fatalf("action type = %s, want qrcode", action.Type)
	}
	parsed, err := url.Parse(action.URL)
	if err != nil || parsed.Host == "" {
		t.Fatalf("qr payload = %q, want an absolute URI", action.URL)
	}
	t.Logf("LIVE ORDER CREATED trade_no=%s qr_host=%s", tradeNo, parsed.Host)
	t.Logf("close it with: POST /v2/trade/payment/scanpay/close {huifu_id, org_req_date=%s, org_req_seq_id=%s}",
		now.Format("20060102"), tradeNo)
}
