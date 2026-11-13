package epayconformance

// V1 protocol conformance: submit.php, mapi.php and api.php act=*.
//
// Every assertion cites the upstream Epay file and line it was derived from.
// The tests drive the real GorixPay application over HTTP (harness_test.go), not
// internal functions, because the protocol is what a running server returns.
//
// 兼容性偏差 (deliberate deviations from the PHP source, asserted as current
// behaviour by the tests below):
//
//	兼容性偏差 D1: mapi.php 成功响应在上游扁平字段之外，额外返回 pay_type/pay_info 这对别名。
//	  上游 Epay/includes/lib/Payment.php:246-296 只输出 payurl|qrcode|urlscheme|html，
//	  但本仓库的收银台与 payment 包既有用例使用 pay_type/pay_info。理由：删掉它们会
//	  破坏仓内调用方，而多余字段对 SDK 无害。测试断言上游字段是权威的：四种 action 各自
//	  只出现对应的一个字段（见 TestV1MapiActionFieldsPerPluginAction）。
//
//	兼容性偏差 D2: submit.php 总是返回收银台跳转（<script>window.location.replace('/cashier.php?trade_no=...&sitename=...')</script>），
//	  包括显式传了 type 的情况。上游 Epay/includes/lib/api/Pay.php:160-257 在 type 非空时
//	  会调用插件并直接渲染插件页面。理由：GorixPay 的支付页是收银台 SPA（/cashier.php），
//	  submit.php 不渲染插件 HTML；买家最终落点相同（收银台再发起通道）。另外 cashier.php
//	  的地址由 siteURL 拼成绝对 URL，而不是上游的相对路径（Pay.php:161），以支持反向代理
//	  前缀部署。
//
//	兼容性偏差 D3（已消除）: 鉴权失败与上游一致，一律 HTTP 200，且失败码按上游区分：
//	  缺 pid → -4「未传入任何参数」（Epay/mapi.php:2-5），pid 为空 → -1「商户ID不能为空」
//	  （Pay.php:280-281），商户不存在 → -3「商户ID不存在」（ApiHelper.php:52-54），
//	  签名错误 → -3「MD5/RSA签名校验失败」（Payment.php:41-43）。JSON 入口走 echojsonmsg
//	  （functions.php:1430），submit.php 走 sysmsg HTML 错误页（functions.php:382-401）。
//	  上游从不返回 4xx，因为 .NET/Java 等 SDK 见到 4xx 会在读 body 之前直接抛异常。
//	  同理 HTTP 方法由 Go 路由决定：GET /mapi.php 不存在（上游 mapi.php 会回 {"code":-4}）。
//
//	兼容性偏差 D4: act=orders 的 count 是符合条件的总行数，不是本页行数
//	  （上游 Epay/api.php:110 用 count($data)）。理由：本平台分页契约（offset/limit + 总数）
//	  已对外文档化，且现有客户端用它判断是否还有下一页；本页行数由 len(data) 表达。
//
//	兼容性偏差 D5（已消除）: act=query 的 type 与 account 现在来自商户结算资料
//	  （merchants.settle_type / settle_account，迁移 000016），对应上游
//	  Epay/api.php:28 读的 pre_user.settle_id / pre_user.account；未设置时给 0 / 空串。
//	  username 仍用商户登录名（邮箱/手机号），上游是开户名。
//
//	兼容性偏差 D6: act=order 的 bill_trade_no 恒为空串。上游 Epay/api.php:80 有独立的银行流水
//	  列 pre_order.bill_trade_no，本平台只记录一个通道交易号（也就是 api_trade_no）。理由：字段
//	  保留，缺失值给空串。
//
//	兼容性偏差 D7: act=order 的免鉴权模式（sign=md5(SYS_KEY.trade_no.SYS_KEY)）与
//	  act=refundapi（key=md5(trade_no.SYS_KEY.trade_no)）的系统密钥存放在 system_settings 的
//	  epay.v1.system_key 行（首次使用时生成），而不是上游的 Epay/config.php。理由：运行时没有
//	  PHP 配置文件；密钥不出接口、只落库，等价于上游运维持有 syskey。测试通过数据库读取它来
//	  模拟平台自身调用方（见 v1SystemKey）。
//
//	兼容性偏差 D8: act=refund 的 reducemoney 等于退款金额。上游
//	  Epay/includes/lib/Order.php:85-94 按通道费率/fee_type 计算扣减额，本平台账本按退款金额
//	  精确扣减商户余额，因此两者相等。
//
//	兼容性偏差 D9: api.php 的 act 与各 act 的参数同时接受 GET query 与 POST body（合并读取）。
//	  上游 query/order/orders/settle 只读 $_GET，refund/refundapi 只读 $_POST
//	  （Epay/api.php:11-160）。理由：这是宽松的超集，不会拒绝任何合法 SDK 请求，也不会放宽
//	  鉴权（每个 act 仍校验各自的 pid/key 或系统签名），见 TestV1APIAcceptsPostParameters。
//	  submit.php 的"只读一组"规则不在此列：它按上游严格实现（见
//	  TestV1SubmitReadsOneParameterGroup）。

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	paymentmodel "gorixpay/internal/payment/model"
)

// ---- helpers -------------------------------------------------------------

// v1Do performs a request and also returns the response headers, which env.do
// in the harness discards.
func v1Do(t *testing.T, env *environment, method, path string, values url.Values) (int, http.Header, string) {
	t.Helper()
	var body io.Reader
	if values != nil {
		body = strings.NewReader(values.Encode())
	}
	request, err := http.NewRequest(method, env.url+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if values != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header.Clone(), string(raw)
}

// v1Without drops a parameter and re-signs, so a "missing field" case is still a
// genuine, correctly signed request.
func v1Without(t *testing.T, env *environment, values url.Values, name string) url.Values {
	t.Helper()
	values.Del(name)
	signV1(t, values, env.md5Key)
	return values
}

// v1Resigned overrides parameters and re-signs.
func v1Resigned(t *testing.T, env *environment, values url.Values, overrides map[string]string) url.Values {
	t.Helper()
	for name, value := range overrides {
		values.Set(name, value)
	}
	signV1(t, values, env.md5Key)
	return values
}

// v1RedirectTarget extracts the cashier URL from the inline redirect script of
// Epay/includes/lib/api/Pay.php:161.
func v1RedirectTarget(t *testing.T, body string) *url.URL {
	t.Helper()
	const marker = "window.location.replace('"
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("no cashier redirect in %q", body)
	}
	rest := body[start+len(marker):]
	end := strings.Index(rest, "')")
	if end < 0 {
		t.Fatalf("unterminated redirect in %q", body)
	}
	parsed, err := url.Parse(rest[:end])
	if err != nil {
		t.Fatalf("redirect url %q: %v", rest[:end], err)
	}
	return parsed
}

// v1Query calls api.php?act=query and returns the decoded body.
func v1Query(t *testing.T, env *environment) map[string]any {
	t.Helper()
	status, body := env.get("/api.php", url.Values{
		"act": {"query"}, "pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key},
	})
	if status != http.StatusOK {
		t.Fatalf("api.php act=query = %d %v", status, body)
	}
	return body
}

// v1APIOrder looks an order up through api.php?act=order with the merchant key.
func v1APIOrder(t *testing.T, env *environment, tradeNo string) map[string]any {
	t.Helper()
	status, body := env.get("/api.php", url.Values{
		"act": {"order"}, "pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key},
		"trade_no": {tradeNo},
	})
	if status != http.StatusOK {
		t.Fatalf("api.php act=order = %d %v", status, body)
	}
	return body
}

func v1MD5(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

// v1SystemKey returns the platform wide V1 key used by the internal api.php
// modes (deviation D7). It is created on first use, so a deliberately invalid
// call initialises it and the value is then read straight from the database the
// way the platform's own tooling would.
func v1SystemKey(t *testing.T, env *environment) string {
	t.Helper()
	env.get("/api.php", url.Values{
		"act": {"order"}, "trade_no": {"v1-initialise"}, "sign": {strings.Repeat("0", 32)},
	})
	var row struct{ Value string }
	if err := env.app.DB().Table("system_settings").
		Select("setting_value AS value").
		Where("setting_key = ?", "epay.v1.system_key").
		Scan(&row).Error; err != nil {
		t.Fatalf("read system key: %v", err)
	}
	if strings.TrimSpace(row.Value) == "" {
		t.Fatal("platform system key was not initialised")
	}
	return row.Value
}

// v1UpstreamAction is one canned reply of the generic plugin upstream, used to
// drive every mapi action field through the real HTTP stack.
type v1UpstreamAction struct {
	payType string
	payInfo string
}

// v1UpstreamServer answers the shared epay create contract that the generic
// plugin consumes: {"code":1,"trade_no":...,"pay_type":...,"pay_info":...}
// (internal/plugin/payment/generic). Each path maps to one canned action so a
// channel can be pinned to jump/qrcode/scheme/html.
func v1UpstreamServer(t *testing.T, actions map[string]v1UpstreamAction) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		action, ok := actions[strings.TrimPrefix(request.URL.Path, "/")]
		if !ok {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"code": 1, "trade_no": "UPSTREAM-" + request.URL.Path,
			"pay_type": action.payType, "pay_info": action.payInfo,
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// v1CreateGenericChannel installs a channel whose plugin is the generic HTTP
// upstream, so the requested payment type selects a plugin action of our choice.
func v1CreateGenericChannel(t *testing.T, env *environment, name, paymentType, apiURL string) {
	t.Helper()
	status, body := env.doJSON(http.MethodPost, "/api/v1/admin/channels", map[string]any{
		"name": name, "plugin_code": "generic", "payment_type": paymentType,
		"priority": 1, "weight": 1, "status": "active",
		"api_url": apiURL, "pid": "v1-upstream-pid", "key": "v1-upstream-key",
		"config": map[string]string{"sign_type": "none", "method": "POST"},
	}, env.adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create channel %s = %d %v", name, status, body)
	}
}

// ---- mapi.php ------------------------------------------------------------

// TestV1MapiCreatesOrderWithFlatPayurl covers the jump branch of
// Epay/includes/lib/Payment.php:246-250: code, trade_no and a flat payurl, no
// pay_type/pay_info payload object.
func TestV1MapiCreatesOrderWithFlatPayurl(t *testing.T) {
	env := newEnvironment(t)
	values := env.signedValues("V1-JUMP-"+randomSuffix(t), map[string]string{"type": "alipay"})
	status, body := env.form("/mapi.php", values)
	if status != http.StatusOK {
		t.Fatalf("mapi.php = %d %v", status, body)
	}
	if code := intOf(t, body["code"]); code != 1 {
		t.Fatalf("code = %d (%v)", code, body)
	}
	requireKeys(t, body, "code", "trade_no", "payurl")
	// Payment.php:246-296 publishes exactly one of the four flat fields.
	forbidKeys(t, body, "qrcode", "urlscheme", "html")
	tradeNo := mustString(t, body["trade_no"])
	payurl := mustString(t, body["payurl"])
	if !strings.Contains(payurl, tradeNo) {
		t.Errorf("payurl %q does not carry trade_no %q", payurl, tradeNo)
	}
	if !strings.HasPrefix(payurl, "http") {
		t.Errorf("payurl %q is not absolute", payurl)
	}
}

// TestV1MapiActionFieldsPerPluginAction covers the remaining flat fields of
// Epay/includes/lib/Payment.php:251-282: qrcode, urlscheme and html.
func TestV1MapiActionFieldsPerPluginAction(t *testing.T) {
	env := newEnvironment(t)
	upstream := v1UpstreamServer(t, map[string]v1UpstreamAction{
		"qrcode":    {payType: "qrcode", payInfo: "weixin://wxpay/bizpayurl?pr=v1conformance"},
		"urlscheme": {payType: "urlscheme", payInfo: "https://scheme.example/enter?order=v1"},
		"html":      {payType: "html", payInfo: `<form action="https://bank.example/pay" method="post"></form>`},
		"jump":      {payType: "jump", payInfo: "https://bank.example/jump?v1=1"},
	})
	cases := []struct {
		name        string
		upstreamKey string
		field       string
		want        string
	}{
		{"qrcode", "qrcode", "qrcode", "weixin://wxpay/bizpayurl?pr=v1conformance"},
		{"urlscheme", "urlscheme", "urlscheme", "https://scheme.example/enter?order=v1"},
		{"html", "html", "html", `<form action="https://bank.example/pay" method="post"></form>`},
		{"payurl", "jump", "payurl", "https://bank.example/jump?v1=1"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			paymentType := "v1-field-" + testCase.name
			v1CreateGenericChannel(t, env, "v1-generic-"+testCase.name, paymentType, upstream.URL+"/"+testCase.upstreamKey)
			values := env.signedValues("V1-FIELD-"+testCase.name+"-"+randomSuffix(t), map[string]string{"type": paymentType})
			status, body := env.form("/mapi.php", values)
			if status != http.StatusOK {
				t.Fatalf("mapi.php = %d %v", status, body)
			}
			if code := intOf(t, body["code"]); code != 1 {
				t.Fatalf("code = %d (%v)", code, body)
			}
			requireKeys(t, body, "code", "trade_no", testCase.field)
			if got := body[testCase.field]; got != testCase.want {
				t.Errorf("%s = %#v, want %#v (Epay/includes/lib/Payment.php:246-282)", testCase.field, got, testCase.want)
			}
			for _, other := range []string{"payurl", "qrcode", "urlscheme", "html"} {
				if other == testCase.field {
					continue
				}
				forbidKeys(t, body, other)
			}
		})
	}
}

// TestV1MapiRejectsInvalidSignature covers ApiHelper::api_verify
// (Epay/includes/lib/ApiHelper.php:67-76) reached from Pay.php:276-280: a bad
// signature is code -3 on HTTP 200, exactly like echojsonmsg
// (Epay/includes/functions.php:1430).
func TestV1MapiRejectsInvalidSignature(t *testing.T) {
	env := newEnvironment(t)
	t.Run("wrong-key", func(t *testing.T) {
		values := env.signedValues("V1-BADSIGN-"+randomSuffix(t), map[string]string{"type": "alipay"})
		signV1(t, values, "not-the-merchant-key")
		status, body := env.form("/mapi.php", values)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (functions.php:1430)", status)
		}
		if code := intOf(t, body["code"]); code != -3 {
			t.Fatalf("code = %d, want -3 (%v)", code, body)
		}
		if msg := mustString(t, body["msg"]); !strings.Contains(msg, "签名校验失败") {
			t.Errorf("msg = %q, want the signature error (Pay.php:75)", msg)
		}
		forbidKeys(t, body, "trade_no", "payurl")
	})
	t.Run("tampered-parameter", func(t *testing.T) {
		values := env.signedValues("V1-TAMPER-"+randomSuffix(t), map[string]string{"type": "alipay"})
		values.Set("money", "0.01")
		status, body := env.form("/mapi.php", values)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (functions.php:1430)", status)
		}
		if code := intOf(t, body["code"]); code != -3 {
			t.Fatalf("code = %d, want -3 (%v)", code, body)
		}
	})
}

// TestV1MapiValidatesParameters covers the parameter checks of
// Epay/includes/lib/api/Pay.php:313-322, including their exact messages and the
// default code -1 of echojsonmsg (Epay/includes/functions.php:1430).
func TestV1MapiValidatesParameters(t *testing.T) {
	env := newEnvironment(t)
	cases := []struct {
		name    string
		drop    string
		set     map[string]string
		wantMsg string
		line    string
	}{
		{"missing-out-trade-no", "out_trade_no", nil, "订单号(out_trade_no)不能为空", "Pay.php:313"},
		{"missing-notify-url", "notify_url", nil, "通知地址(notify_url)不能为空", "Pay.php:314"},
		{"missing-name", "name", nil, "商品名称(name)不能为空", "Pay.php:315"},
		{"missing-money", "money", nil, "金额(money)不能为空", "Pay.php:316"},
		{"missing-type", "type", nil, "支付方式(type)不能为空", "Pay.php:317"},
		{"missing-clientip", "clientip", nil, "用户IP地址(clientip)不能为空", "Pay.php:318"},
		{"bad-out-trade-no", "", map[string]string{"out_trade_no": "bad number!"}, "订单号(out_trade_no)格式不正确", "Pay.php:319"},
		{"non-numeric-money", "", map[string]string{"money": "abc"}, "金额不合法", "Pay.php:320"},
		{"zero-money", "", map[string]string{"money": "0"}, "金额不合法", "Pay.php:320"},
		{"bad-clientip", "", map[string]string{"clientip": "not-an-ip"}, "用户IP地址不合法", "Pay.php:321"},
		{"bad-notify-url", "", map[string]string{"notify_url": "merchant.example/notify"}, "通知地址(notify_url)格式不正确", "Pay.php:322"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			values := env.signedValues("V1-PARAM-"+randomSuffix(t), map[string]string{"type": "alipay"})
			if testCase.drop != "" {
				values = v1Without(t, env, values, testCase.drop)
			}
			if testCase.set != nil {
				values = v1Resigned(t, env, values, testCase.set)
			}
			status, body := env.form("/mapi.php", values)
			if status != http.StatusOK {
				t.Fatalf("mapi.php = %d %v", status, body)
			}
			if code := intOf(t, body["code"]); code != -1 {
				t.Fatalf("code = %d, want -1 (%v) [%s]", code, body, testCase.line)
			}
			if msg := mustString(t, body["msg"]); msg != testCase.wantMsg {
				t.Errorf("msg = %q, want %q [%s]", msg, testCase.wantMsg, testCase.line)
			}
			forbidKeys(t, body, "trade_no")
		})
	}
}

// TestV1MapiReusesPendingOrderAndRejectsChangedParameters covers
// Epay/includes/lib/api/Pay.php:406-413: an identical retry keeps the trade_no,
// a changed payload is refused with code -1.
func TestV1MapiReusesPendingOrderAndRejectsChangedParameters(t *testing.T) {
	env := newEnvironment(t)
	outTradeNo := "V1-RETRY-" + randomSuffix(t)
	first := env.signedValues(outTradeNo, map[string]string{"type": "alipay"})
	if _, body := env.form("/mapi.php", first); intOf(t, body["code"]) != 1 {
		t.Fatalf("first order = %v", body)
	} else {
		tradeNo := mustString(t, body["trade_no"])
		// Pay.php:414: the retry answers the same trade_no instead of creating one.
		_, retry := env.form("/mapi.php", env.signedValues(outTradeNo, map[string]string{"type": "alipay"}))
		if code := intOf(t, retry["code"]); code != 1 {
			t.Fatalf("retry code = %d (%v)", code, retry)
		}
		if got := mustString(t, retry["trade_no"]); got != tradeNo {
			t.Errorf("retry trade_no = %q, want %q (Pay.php:414)", got, tradeNo)
		}
		requireKeys(t, retry, "payurl")
	}
	// Pay.php:411-412: a changed payload must be refused.
	changed := env.signedValues(outTradeNo, map[string]string{"type": "alipay", "money": "10.01"})
	_, body := env.form("/mapi.php", changed)
	if code := intOf(t, body["code"]); code != -1 {
		t.Fatalf("changed code = %d (%v)", code, body)
	}
	want := fmt.Sprintf("该订单(%s)支付参数有变化，请更换订单号重新发起支付", outTradeNo)
	if msg := mustString(t, body["msg"]); msg != want {
		t.Errorf("msg = %q, want %q (Pay.php:412)", msg, want)
	}
}

// TestV1MapiRejectsRepeatOfPaidOrder covers Pay.php:407-410: a finished order
// cannot be paid twice.
func TestV1MapiRejectsRepeatOfPaidOrder(t *testing.T) {
	env := newEnvironment(t)
	outTradeNo := "V1-PAID-" + randomSuffix(t)
	env.createPaidOrder(outTradeNo)
	_, body := env.form("/mapi.php", env.signedValues(outTradeNo, map[string]string{"type": "alipay"}))
	if code := intOf(t, body["code"]); code != -1 {
		t.Fatalf("code = %d (%v)", code, body)
	}
	want := fmt.Sprintf("该订单(%s)已完成支付，请勿重复发起支付", outTradeNo)
	if msg := mustString(t, body["msg"]); msg != want {
		t.Errorf("msg = %q, want %q (Pay.php:409)", msg, want)
	}
	forbidKeys(t, body, "payurl", "qrcode", "urlscheme", "html")
}

// TestV1MapiReportsChannelFailure covers the error branch of
// Epay/includes/lib/Payment.php:283-291, where a channel failure is code -2.
func TestV1MapiReportsChannelFailure(t *testing.T) {
	env := newEnvironment(t)
	// An upstream that rejects the order makes the generic plugin return an error.
	rejecting := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"msg":"upstream rejected"}`))
	}))
	t.Cleanup(rejecting.Close)
	v1CreateGenericChannel(t, env, "v1-generic-error", "v1-field-error", rejecting.URL)
	values := env.signedValues("V1-CHANNELERR-"+randomSuffix(t), map[string]string{"type": "v1-field-error"})
	_, body := env.form("/mapi.php", values)
	if code := intOf(t, body["code"]); code != -2 {
		t.Fatalf("code = %d, want -2 (%v) [Epay/includes/lib/Payment.php:283-291]", code, body)
	}
	if msg := mustString(t, body["msg"]); strings.TrimSpace(msg) == "" {
		t.Error("channel failure must carry a msg")
	}
}

// ---- submit.php ----------------------------------------------------------

// TestV1SubmitReadsOneParameterGroup covers Pay.php:12-18: submit.php reads the
// query string when the query carries pid and reads the body otherwise. The
// group that upstream ignores must not influence the order.
func TestV1SubmitReadsOneParameterGroup(t *testing.T) {
	env := newEnvironment(t)
	t.Run("get-group-wins", func(t *testing.T) {
		// pid in the query: the unsigned body parameter must be ignored, so the
		// order keeps the signed money of 10.00.
		outTradeNo := "V1-GROUP-GET-" + randomSuffix(t)
		values := env.signedValues(outTradeNo, map[string]string{"type": "alipay"})
		status, _, body := v1Do(t, env, http.MethodPost, "/submit.php?"+values.Encode(),
			url.Values{"money": {"999.00"}})
		if status != http.StatusOK || !strings.Contains(body, "window.location.replace") {
			t.Fatalf("submit.php = %d %q", status, body)
		}
		tradeNo := v1RedirectTarget(t, body).Query().Get("trade_no")
		order := v1APIOrder(t, env, tradeNo)
		if money := mustString(t, order["money"]); money != "10.00" {
			t.Errorf("money = %q, want the signed 10.00 (Pay.php:12-18)", money)
		}
	})
	t.Run("post-group-wins", func(t *testing.T) {
		// No pid in the query: the query group must be ignored, so the unsigned
		// extra parameter cannot invalidate the signature.
		outTradeNo := "V1-GROUP-POST-" + randomSuffix(t)
		values := env.signedValues(outTradeNo, map[string]string{"type": "alipay"})
		status, _, body := v1Do(t, env, http.MethodPost,
			"/submit.php?ignored=1&money=999.00", values)
		if status != http.StatusOK || !strings.Contains(body, "window.location.replace") {
			t.Fatalf("submit.php = %d %q", status, body)
		}
		tradeNo := v1RedirectTarget(t, body).Query().Get("trade_no")
		order := v1APIOrder(t, env, tradeNo)
		if money := mustString(t, order["money"]); money != "10.00" {
			t.Errorf("money = %q, want the signed 10.00 (Pay.php:12-18)", money)
		}
	})
	t.Run("ignored-group-cannot-satisfy-a-required-field", func(t *testing.T) {
		// out_trade_no only in the body, pid in the query: upstream reads the query
		// group and reports the missing order number.
		values := env.signedValues("V1-GROUP-CROSS-"+randomSuffix(t), map[string]string{"type": "alipay"})
		body := url.Values{"out_trade_no": {values.Get("out_trade_no")}}
		values = v1Without(t, env, values, "out_trade_no")
		status, _, raw := v1Do(t, env, http.MethodPost, "/submit.php?"+values.Encode(), body)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (Pay.php:21-23 sysmsg)", status)
		}
		if !strings.Contains(raw, "订单号(out_trade_no)不能为空") {
			t.Fatalf("body = %q, want the missing order number message (Pay.php:53)", raw)
		}
	})
}

// TestV1SubmitTypeEmptyRedirectsToCashier covers Pay.php:160-163: without a type
// the page redirects to the cashier with the urlencoded base64 sitename.
func TestV1SubmitTypeEmptyRedirectsToCashier(t *testing.T) {
	env := newEnvironment(t)
	sitename := "兼容性站点"
	values := env.signedValues("V1-SUBMIT-EMPTY-"+randomSuffix(t), map[string]string{"sitename": sitename})
	status, header, body := v1Do(t, env, http.MethodPost, "/submit.php", values)
	if status != http.StatusOK {
		t.Fatalf("submit.php = %d %q", status, body)
	}
	if contentType := header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Errorf("Content-Type = %q, want text/html (Pay.php:11)", contentType)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "<script>window.location.replace('") {
		t.Fatalf("body = %q, want the inline redirect script (Pay.php:161)", body)
	}
	target := v1RedirectTarget(t, body)
	if !strings.HasSuffix(target.Path, "/cashier.php") {
		t.Errorf("redirect path = %q, want /cashier.php", target.Path)
	}
	tradeNo := target.Query().Get("trade_no")
	if tradeNo == "" {
		t.Fatalf("redirect has no trade_no: %q", body)
	}
	wantSitename := url.QueryEscape(base64.StdEncoding.EncodeToString([]byte(sitename)))
	if got := target.Query().Get("sitename"); got != wantSitename {
		t.Errorf("sitename = %q, want %q (Pay.php:45)", got, wantSitename)
	}
	order := v1APIOrder(t, env, tradeNo)
	if got := mustString(t, order["trade_no"]); got != tradeNo {
		t.Errorf("order trade_no = %q, want %q", got, tradeNo)
	}
}

// TestV1SubmitExplicitTypeRedirectsToCashier pins deviation D2: an explicit type
// also ends on the cashier instead of rendering the plugin page
// (Pay.php:160-257).
func TestV1SubmitExplicitTypeRedirectsToCashier(t *testing.T) {
	env := newEnvironment(t)
	values := env.signedValues("V1-SUBMIT-TYPED-"+randomSuffix(t), map[string]string{"type": "alipay"})
	status, header, body := v1Do(t, env, http.MethodPost, "/submit.php", values)
	if status != http.StatusOK || !strings.Contains(header.Get("Content-Type"), "text/html") {
		t.Fatalf("submit.php = %d %v %q", status, header.Get("Content-Type"), body)
	}
	target := v1RedirectTarget(t, body)
	if !strings.HasSuffix(target.Path, "/cashier.php") {
		t.Fatalf("redirect path = %q, want /cashier.php (deviation D2)", target.Path)
	}
	if target.Query().Get("trade_no") == "" {
		t.Fatalf("redirect has no trade_no: %q", body)
	}
}

// TestV1SubmitParameterErrorsRenderSysMessage covers the sysmsg() pages of
// Pay.php:53-57 with HTTP 200 (Epay/includes/functions.php:382-401).
func TestV1SubmitParameterErrorsRenderSysMessage(t *testing.T) {
	env := newEnvironment(t)
	cases := []struct {
		name    string
		drop    string
		wantMsg string
		line    string
	}{
		{"missing-out-trade-no", "out_trade_no", "订单号(out_trade_no)不能为空", "Pay.php:53"},
		{"missing-notify-url", "notify_url", "通知地址(notify_url)不能为空", "Pay.php:54"},
		{"missing-return-url", "return_url", "回调地址(return_url)不能为空", "Pay.php:55"},
		{"missing-name", "name", "商品名称(name)不能为空", "Pay.php:56"},
		{"missing-money", "money", "金额(money)不能为空", "Pay.php:57"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			values := v1Without(t, env, env.signedValues("V1-SUBMIT-ERR-"+randomSuffix(t), nil), testCase.drop)
			status, header, body := v1Do(t, env, http.MethodPost, "/submit.php", values)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", status, testCase.line)
			}
			if contentType := header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
				t.Errorf("Content-Type = %q, want text/html (%s)", contentType, testCase.line)
			}
			if !strings.Contains(body, testCase.wantMsg) {
				t.Errorf("body = %q, want %q (%s)", body, testCase.wantMsg, testCase.line)
			}
			if !strings.Contains(body, "站点提示信息") {
				t.Errorf("body = %q, want the sysmsg page title (%s)", body, testCase.line)
			}
		})
	}
	t.Run("bad-money", func(t *testing.T) {
		values := v1Resigned(t, env, env.signedValues("V1-SUBMIT-BADMONEY-"+randomSuffix(t), nil), map[string]string{"money": "-1"})
		_, _, body := v1Do(t, env, http.MethodPost, "/submit.php", values)
		if !strings.Contains(body, "金额不合法") {
			t.Fatalf("body = %q, want 金额不合法 (Pay.php:59)", body)
		}
	})
}

// TestV1SubmitRejectsChangedParameters covers Pay.php:145-146: reusing an
// out_trade_no with a different payload is a sysmsg page, not an order.
func TestV1SubmitRejectsChangedParameters(t *testing.T) {
	env := newEnvironment(t)
	outTradeNo := "V1-SUBMIT-CONFLICT-" + randomSuffix(t)
	first := env.signedValues(outTradeNo, map[string]string{"type": "alipay"})
	if status, _, body := v1Do(t, env, http.MethodPost, "/submit.php", first); status != http.StatusOK ||
		!strings.Contains(body, "window.location.replace") {
		t.Fatalf("first submit = %d %q", status, body)
	}
	changed := env.signedValues(outTradeNo, map[string]string{"type": "alipay", "money": "10.01"})
	status, header, body := v1Do(t, env, http.MethodPost, "/submit.php", changed)
	if status != http.StatusOK || !strings.Contains(header.Get("Content-Type"), "text/html") {
		t.Fatalf("conflict submit = %d %q", status, body)
	}
	want := fmt.Sprintf("该订单(%s)支付参数有变化，请更换订单号重新发起支付", outTradeNo)
	if !strings.Contains(body, want) {
		t.Errorf("body = %q, want %q (Pay.php:146)", body, want)
	}
	if !strings.Contains(body, "站点提示信息") {
		t.Errorf("body = %q, want the sysmsg page (functions.php:382-401)", body)
	}
}

// TestV1SubmitRejectsInvalidSignature covers the submit.php failure mode:
// sysmsg renders an HTML error page with HTTP 200 (Epay/includes/functions.php:
// 382-401 called from Pay.php:32), never a 4xx.
func TestV1SubmitRejectsInvalidSignature(t *testing.T) {
	env := newEnvironment(t)
	values := env.signedValues("V1-SUBMIT-BADSIGN-"+randomSuffix(t), nil)
	signV1(t, values, "another-key")
	status, header, body := v1Do(t, env, http.MethodPost, "/submit.php", values)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (functions.php:382-401)", status)
	}
	if contentType := header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Errorf("Content-Type = %q, want the sysmsg HTML page", contentType)
	}
	if !strings.Contains(body, "签名校验失败") {
		t.Errorf("body = %q, want the signature error", body)
	}
	if !strings.Contains(body, "站点提示信息") {
		t.Errorf("body = %q, want the sysmsg page title", body)
	}
	if strings.Contains(body, "trade_no") {
		t.Errorf("body = %q, must not leak an order", body)
	}
}

// ---- api.php -------------------------------------------------------------

// TestV1APIQueryReturnsUpstreamAccountFields covers Epay/api.php:13-30.
func TestV1APIQueryReturnsUpstreamAccountFields(t *testing.T) {
	env := newEnvironment(t)
	body := v1Query(t, env)
	if code := intOf(t, body["code"]); code != 1 {
		t.Fatalf("code = %d (%v)", code, body)
	}
	requireKeys(t, body, "code", "pid", "key", "active", "money", "type", "account",
		"username", "orders", "orders_today", "orders_lastday")
	if got := mustString(t, body["key"]); got != env.md5Key {
		t.Errorf("key = %q, want the merchant key (api.php:28)", got)
	}
	if pid := intOf(t, body["pid"]); int64(pid) != env.pid {
		t.Errorf("pid = %d, want %d", pid, env.pid)
	}
	if active := intOf(t, body["active"]); active != 1 {
		t.Errorf("active = %d, want 1 for an active merchant", active)
	}
	if money := mustString(t, body["money"]); money != "0.00" {
		t.Errorf("money = %q, want 0.00 for a new merchant", money)
	}
	if username := mustString(t, body["username"]); username != "conformance@example.com" {
		t.Errorf("username = %q, want the login name (deviation D5)", username)
	}
	if settleType := intOf(t, body["type"]); settleType != 0 {
		t.Errorf("type = %d, want 0 before the merchant configures settlement", settleType)
	}
	if account := mustString(t, body["account"]); account != "" {
		t.Errorf("account = %q, want empty before the merchant configures settlement", account)
	}
	// api.php:24 republishes pre_user.settle_id/account; the merchant sets them on
	// the profile and the query has to reflect them immediately.
	status, updated := env.doJSON(http.MethodPut, "/api/v1/merchant/profile", map[string]any{
		"display_name": "Conformance Merchant", "settle_type": 2, "settle_account": "6222000000000000",
	}, env.merchantToken)
	if status != http.StatusOK {
		t.Fatalf("update profile = %d %v", status, updated)
	}
	body = v1Query(t, env)
	if settleType := intOf(t, body["type"]); settleType != 2 {
		t.Errorf("type = %d, want the configured settle type 2 (api.php:28)", settleType)
	}
	if account := mustString(t, body["account"]); account != "6222000000000000" {
		t.Errorf("account = %q, want the configured settlement account (api.php:28)", account)
	}
	for _, field := range []string{"orders", "orders_today", "orders_lastday"} {
		if count := intOf(t, body[field]); count != 0 {
			t.Errorf("%s = %d, want 0 for a new merchant", field, count)
		}
	}

	// A paid order is counted, and only a paid one counts as today's revenue.
	env.createPaidOrder("V1-QUERY-COUNT-" + randomSuffix(t))
	body = v1Query(t, env)
	if count := intOf(t, body["orders"]); count != 1 {
		t.Errorf("orders = %d, want 1 (api.php:22)", count)
	}
	if count := intOf(t, body["orders_today"]); count != 1 {
		t.Errorf("orders_today = %d, want 1 (api.php:25)", count)
	}
	if count := intOf(t, body["orders_lastday"]); count != 0 {
		t.Errorf("orders_lastday = %d, want 0 (api.php:26)", count)
	}
	if money := mustString(t, body["money"]); money != "10.00" {
		t.Errorf("money = %q, want the 10.00 credited balance", money)
	}
}

// TestV1APIQueryRejectsBadCredentials covers the -3 branches of
// Epay/api.php:17-20.
func TestV1APIQueryRejectsBadCredentials(t *testing.T) {
	env := newEnvironment(t)
	_, body := env.get("/api.php", url.Values{"act": {"query"}, "pid": {strconv.FormatInt(env.pid, 10)}, "key": {"wrong"}})
	if code := intOf(t, body["code"]); code != -3 {
		t.Fatalf("wrong key code = %d (%v)", code, body)
	}
	if msg := mustString(t, body["msg"]); !strings.Contains(msg, "密钥") {
		t.Errorf("msg = %q, want the key error (api.php:19)", msg)
	}
	_, body = env.get("/api.php", url.Values{"act": {"query"}, "pid": {"999999"}, "key": {env.md5Key}})
	if code := intOf(t, body["code"]); code != -3 {
		t.Fatalf("unknown merchant code = %d (%v)", code, body)
	}
	if msg := mustString(t, body["msg"]); !strings.Contains(msg, "商户ID") {
		t.Errorf("msg = %q, want the merchant error (api.php:18)", msg)
	}
}

// TestV1APIOrderReturnsUpstreamOrderFields covers Epay/api.php:54-85.
func TestV1APIOrderReturnsUpstreamOrderFields(t *testing.T) {
	env := newEnvironment(t)
	outTradeNo := "V1-ORDER-" + randomSuffix(t)
	tradeNo := env.createPaidOrder(outTradeNo)
	body := v1APIOrder(t, env, tradeNo)
	if code := intOf(t, body["code"]); code != 1 {
		t.Fatalf("code = %d (%v)", code, body)
	}
	requireKeys(t, body, "code", "msg", "trade_no", "out_trade_no", "api_trade_no",
		"bill_trade_no", "type", "pid", "addtime", "endtime", "name", "money",
		"param", "buyer", "status", "payurl")
	if msg := mustString(t, body["msg"]); msg != "succ" {
		t.Errorf("msg = %q, want succ (api.php:80)", msg)
	}
	if got := mustString(t, body["trade_no"]); got != tradeNo {
		t.Errorf("trade_no = %q, want %q", got, tradeNo)
	}
	if got := mustString(t, body["out_trade_no"]); got != outTradeNo {
		t.Errorf("out_trade_no = %q, want %q", got, outTradeNo)
	}
	if got := mustString(t, body["type"]); got != "alipay" {
		t.Errorf("type = %q, want alipay", got)
	}
	if money := mustString(t, body["money"]); money != "10.00" {
		t.Errorf("money = %q, want 10.00", money)
	}
	if status := intOf(t, body["status"]); status != 1 {
		t.Errorf("status = %d, want 1 for a paid order", status)
	}
	if pid := intOf(t, body["pid"]); int64(pid) != env.pid {
		t.Errorf("pid = %d, want %d (api.php:80 echoes the order uid)", pid, env.pid)
	}
	if addtime := mustString(t, body["addtime"]); addtime == "" {
		t.Error("addtime is empty")
	}
	if endtime := mustString(t, body["endtime"]); endtime == "" {
		t.Error("endtime is empty for a paid order")
	}
	if apiTradeNo := mustString(t, body["api_trade_no"]); !strings.Contains(apiTradeNo, tradeNo) {
		t.Errorf("api_trade_no = %q, want the channel trade number of %q", apiTradeNo, tradeNo)
	}
	payurl := mustString(t, body["payurl"])
	if !strings.Contains(payurl, tradeNo) {
		t.Errorf("payurl = %q, want the replayed channel url of %q", payurl, tradeNo)
	}

	// out_trade_no is an alternative key for the same lookup (api.php:71-73).
	_, byOutTradeNo := env.get("/api.php", url.Values{
		"act": {"order"}, "pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key},
		"out_trade_no": {outTradeNo},
	})
	if got := mustString(t, byOutTradeNo["trade_no"]); got != tradeNo {
		t.Errorf("lookup by out_trade_no = %q, want %q", got, tradeNo)
	}
}

// TestV1APIOrderErrorCodes covers the -4 and -1 branches of
// Epay/api.php:74-83.
func TestV1APIOrderErrorCodes(t *testing.T) {
	env := newEnvironment(t)
	base := url.Values{"act": {"order"}, "pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key}}
	cases := []struct {
		name    string
		extra   url.Values
		want    int
		wantMsg string
	}{
		{"no-number", url.Values{}, -4, "订单号不能为空"},
		{"unknown", url.Values{"out_trade_no": {"missing"}}, -1, "订单号不存在"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			values := url.Values{}
			for name, entries := range base {
				values[name] = entries
			}
			for name, entries := range testCase.extra {
				values[name] = entries
			}
			_, body := env.get("/api.php", values)
			if code := intOf(t, body["code"]); code != testCase.want {
				t.Fatalf("code = %d, want %d (%v)", code, testCase.want, body)
			}
			if msg := mustString(t, body["msg"]); msg != testCase.wantMsg {
				t.Errorf("msg = %q, want %q", msg, testCase.wantMsg)
			}
		})
	}
}

// TestV1APIOrderSystemSignMode covers Epay/api.php:56-59: sign and trade_no
// without pid/key are authenticated with md5(SYS_KEY.trade_no.SYS_KEY).
func TestV1APIOrderSystemSignMode(t *testing.T) {
	env := newEnvironment(t)
	outTradeNo := "V1-SIGNMODE-" + randomSuffix(t)
	tradeNo := env.createPaidOrder(outTradeNo)
	systemKey := v1SystemKey(t, env)

	values := url.Values{
		"act": {"order"}, "trade_no": {tradeNo},
		"sign": {v1MD5(systemKey + tradeNo + systemKey)},
	}
	status, body := env.get("/api.php", values)
	if status != http.StatusOK {
		t.Fatalf("api.php = %d %v", status, body)
	}
	if code := intOf(t, body["code"]); code != 1 {
		t.Fatalf("code = %d (%v)", code, body)
	}
	if got := mustString(t, body["trade_no"]); got != tradeNo {
		t.Errorf("trade_no = %q, want %q", got, tradeNo)
	}
	if pid := intOf(t, body["pid"]); int64(pid) != env.pid {
		t.Errorf("pid = %d, want the order owner %d", pid, env.pid)
	}
	requireKeys(t, body, "payurl", "api_trade_no", "status")

	// A forged signature is refused with -3 (api.php:58).
	values.Set("sign", v1MD5(systemKey+tradeNo))
	_, forged := env.get("/api.php", values)
	if code := intOf(t, forged["code"]); code != -3 {
		t.Fatalf("forged code = %d (%v)", code, forged)
	}
	if msg := mustString(t, forged["msg"]); msg != "verify sign failed" {
		t.Errorf("msg = %q, want verify sign failed (api.php:58)", msg)
	}
}

// TestV1APIOrdersPaginates covers Epay/api.php:86-115 with the documented
// count semantics of deviation D4.
func TestV1APIOrdersPaginates(t *testing.T) {
	env := newEnvironment(t)
	for index := 0; index < 3; index++ {
		values := env.signedValues(fmt.Sprintf("V1-LIST-%s-%d", randomSuffix(t), index), map[string]string{"type": "alipay"})
		if _, body := env.form("/mapi.php", values); intOf(t, body["code"]) != 1 {
			t.Fatalf("create order %d = %v", index, body)
		}
	}
	base := url.Values{"act": {"orders"}, "pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key}}
	_, body := env.get("/api.php", base)
	if code := intOf(t, body["code"]); code != 1 {
		t.Fatalf("code = %d (%v)", code, body)
	}
	if msg := mustString(t, body["msg"]); msg != "查询订单记录成功！" {
		t.Errorf("msg = %q, want the upstream message (api.php:110)", msg)
	}
	if count := intOf(t, body["count"]); count != 3 {
		t.Errorf("count = %d, want 3 (deviation D4: total rows)", count)
	}
	data, ok := body["data"].([]any)
	if !ok || len(data) != 3 {
		t.Fatalf("data = %v, want 3 rows", body["data"])
	}
	requireKeys(t, data[0].(map[string]any), "trade_no", "out_trade_no", "type", "pid",
		"addtime", "endtime", "name", "money", "param", "buyer", "status")

	page := url.Values{}
	for name, entries := range base {
		page[name] = entries
	}
	page.Set("limit", "2")
	_, body = env.get("/api.php", page)
	if len(body["data"].([]any)) != 2 {
		t.Errorf("limit=2 returned %d rows", len(body["data"].([]any)))
	}
	page.Set("offset", "2")
	_, body = env.get("/api.php", page)
	if rows := len(body["data"].([]any)); rows != 1 {
		t.Errorf("limit=2&offset=2 returned %d rows, want 1", rows)
	}
}

// TestV1APIOrdersLimitCappedAtFifty covers the limit cap of
// Epay/api.php:93 (`if($limit>50)$limit=50;`).
func TestV1APIOrdersLimitCappedAtFifty(t *testing.T) {
	env := newEnvironment(t)
	for index := 0; index < 51; index++ {
		values := env.signedValues(fmt.Sprintf("V1-CAP-%s-%02d", randomSuffix(t), index), map[string]string{"type": "alipay"})
		if _, body := env.form("/mapi.php", values); intOf(t, body["code"]) != 1 {
			t.Fatalf("create order %d = %v", index, body)
		}
	}
	_, body := env.get("/api.php", url.Values{
		"act": {"orders"}, "pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key},
		"limit": {"100"},
	})
	if code := intOf(t, body["code"]); code != 1 {
		t.Fatalf("code = %d (%v)", code, body)
	}
	if count := intOf(t, body["count"]); count != 51 {
		t.Errorf("count = %d, want 51 (deviation D4)", count)
	}
	rows, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("data = %v", body["data"])
	}
	if len(rows) != 50 {
		t.Errorf("limit=100 returned %d rows, want the upstream cap of 50 (api.php:93)", len(rows))
	}
}

// TestV1APIRefundReturnsUpstreamFields covers Epay/api.php:116-146 with the
// Order::refund payload of Epay/includes/lib/Order.php:134, including uid and
// reducemoney.
func TestV1APIRefundReturnsUpstreamFields(t *testing.T) {
	env := newEnvironment(t)
	outTradeNo := "V1-REFUND-" + randomSuffix(t)
	tradeNo := env.createPaidOrder(outTradeNo)
	form := url.Values{
		"pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key},
		"trade_no": {tradeNo}, "money": {"2.00"},
	}
	status, body := env.form("/api.php?act=refund", form)
	if status != http.StatusOK {
		t.Fatalf("api.php act=refund = %d %v", status, body)
	}
	if code := intOf(t, body["code"]); code != 0 {
		t.Fatalf("code = %d, want 0 (Epay/api.php:142-145): %v", code, body)
	}
	requireKeys(t, body, "code", "msg", "refund_no", "out_refund_no", "trade_no",
		"out_trade_no", "uid", "money", "reducemoney")
	if got := mustString(t, body["trade_no"]); got != tradeNo {
		t.Errorf("trade_no = %q, want %q", got, tradeNo)
	}
	if got := mustString(t, body["out_trade_no"]); got != outTradeNo {
		t.Errorf("out_trade_no = %q, want %q", got, outTradeNo)
	}
	if uid := intOf(t, body["uid"]); int64(uid) != env.pid {
		t.Errorf("uid = %d, want %d (Order.php:134)", uid, env.pid)
	}
	if money := mustString(t, body["money"]); money != "2.00" {
		t.Errorf("money = %q, want 2.00", money)
	}
	if reduce := mustString(t, body["reducemoney"]); reduce != "2.00" {
		t.Errorf("reducemoney = %q, want 2.00 (deviation D8)", reduce)
	}
	if msg := mustString(t, body["msg"]); !strings.HasPrefix(msg, "退款成功！退款金额¥") {
		t.Errorf("msg = %q, want the upstream success message (api.php:143)", msg)
	}
	if refundNo := mustString(t, body["refund_no"]); refundNo == "" {
		t.Error("refund_no is empty")
	}
	if outRefundNo := mustString(t, body["out_refund_no"]); outRefundNo == "" {
		t.Error("out_refund_no is empty")
	}
}

// TestV1APIRefundErrorCodes covers the -3, -4 and -1 branches of
// Epay/api.php:119-137.
func TestV1APIRefundErrorCodes(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("V1-REFUND-ERR-" + randomSuffix(t))
	cases := []struct {
		name    string
		form    url.Values
		want    int
		wantMsg string
	}{
		{"wrong-key", url.Values{"pid": {strconv.FormatInt(env.pid, 10)}, "key": {"nope"}, "trade_no": {tradeNo}, "money": {"1.00"}}, -3, ""},
		{"missing-number", url.Values{"pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key}, "money": {"1.00"}}, -4, "订单号不能为空"},
		{"bad-money", url.Values{"pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key}, "trade_no": {tradeNo}, "money": {"abc"}}, -1, "金额输入错误"},
		{"unknown-order", url.Values{"pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key}, "trade_no": {"missing"}, "money": {"1.00"}}, -1, "当前订单不存在！"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, body := env.form("/api.php?act=refund", testCase.form)
			if code := intOf(t, body["code"]); code != testCase.want {
				t.Fatalf("code = %d, want %d (%v)", code, testCase.want, body)
			}
			if testCase.wantMsg != "" {
				if msg := mustString(t, body["msg"]); msg != testCase.wantMsg {
					t.Errorf("msg = %q, want %q", msg, testCase.wantMsg)
				}
			}
		})
	}
}

// TestV1APIRefundAPIInternalKey covers Epay/api.php:147-160: the platform
// internal refund authenticated with key=md5(trade_no.SYS_KEY.trade_no), with no
// merchant pid.
func TestV1APIRefundAPIInternalKey(t *testing.T) {
	env := newEnvironment(t)
	outTradeNo := "V1-REFUNDAPI-" + randomSuffix(t)
	tradeNo := env.createPaidOrder(outTradeNo)
	systemKey := v1SystemKey(t, env)
	form := url.Values{
		"trade_no": {tradeNo}, "money": {"1.00"},
		"key": {v1MD5(tradeNo + systemKey + tradeNo)},
	}
	status, body := env.form("/api.php?act=refundapi", form)
	if status != http.StatusOK {
		t.Fatalf("api.php act=refundapi = %d %v", status, body)
	}
	if code := intOf(t, body["code"]); code != 0 {
		t.Fatalf("code = %d, want 0 (%v) [Epay/api.php:156-158]", code, body)
	}
	requireKeys(t, body, "refund_no", "out_refund_no", "trade_no", "uid", "money", "reducemoney")
	if money := mustString(t, body["money"]); money != "1.00" {
		t.Errorf("money = %q, want 1.00", money)
	}
	if uid := intOf(t, body["uid"]); int64(uid) != env.pid {
		t.Errorf("uid = %d, want the order owner %d", uid, env.pid)
	}
	if msg := mustString(t, body["msg"]); !strings.HasPrefix(msg, "退款成功！退款金额￥") {
		t.Errorf("msg = %q, want the internal success message (Epay/api.php:157)", msg)
	}

	// A wrong key is refused with -1 密钥错误 (Epay/api.php:152).
	form.Set("key", v1MD5(tradeNo))
	_, forged := env.form("/api.php?act=refundapi", form)
	if code := intOf(t, forged["code"]); code != -1 {
		t.Fatalf("forged code = %d (%v)", code, forged)
	}
	if msg := mustString(t, forged["msg"]); msg != "密钥错误" {
		t.Errorf("msg = %q, want 密钥错误 (Epay/api.php:152)", msg)
	}
}

// TestV1APISettleListsRecords covers Epay/api.php:31-53.
func TestV1APISettleListsRecords(t *testing.T) {
	env := newEnvironment(t)
	status, body := env.get("/api.php", url.Values{
		"act": {"settle"}, "pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key},
	})
	if status != http.StatusOK {
		t.Fatalf("api.php act=settle = %d %v", status, body)
	}
	if code := intOf(t, body["code"]); code != 1 {
		t.Fatalf("code = %d (%v)", code, body)
	}
	if msg := mustString(t, body["msg"]); msg != "查询结算记录成功！" {
		t.Errorf("msg = %q, want the upstream message (api.php:48)", msg)
	}
	if _, ok := body["data"].([]any); !ok {
		t.Errorf("data = %v, want an array (api.php:48)", body["data"])
	}
	// limit is capped instead of rejected (api.php:35-37).
	_, capped := env.get("/api.php", url.Values{
		"act": {"settle"}, "pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key},
		"limit": {"100"}, "offset": {"0"},
	})
	if code := intOf(t, capped["code"]); code != 1 {
		t.Fatalf("limit=100 code = %d (%v)", code, capped)
	}
	// Wrong key is refused with -3 (api.php:40).
	_, refused := env.get("/api.php", url.Values{
		"act": {"settle"}, "pid": {strconv.FormatInt(env.pid, 10)}, "key": {"nope"},
	})
	if code := intOf(t, refused["code"]); code != -3 {
		t.Fatalf("wrong key code = %d (%v)", code, refused)
	}
}

// TestV1APIAcceptsPostParameters pins deviation D9: act and its parameters are
// read from query and body together, which is a superset of the upstream split
// between $_GET acts and the $_POST refund acts.
func TestV1APIAcceptsPostParameters(t *testing.T) {
	env := newEnvironment(t)
	form := url.Values{
		"act": {"query"}, "pid": {strconv.FormatInt(env.pid, 10)}, "key": {env.md5Key},
	}
	status, body := env.form("/api.php", form)
	if status != http.StatusOK {
		t.Fatalf("api.php = %d %v", status, body)
	}
	if code := intOf(t, body["code"]); code != 1 {
		t.Fatalf("code = %d, want 1 (deviation D9): %v", code, body)
	}
	requireKeys(t, body, "pid", "key", "username", "orders")
}

// TestV1APIUnknownActReturnsNoAct covers Epay/api.php:161-164.
func TestV1APIUnknownActReturnsNoAct(t *testing.T) {
	env := newEnvironment(t)
	for _, path := range []string{"/api.php?act=nope", "/api.php", "/api.php?act="} {
		_, body := env.get(path, nil)
		if code := intOf(t, body["code"]); code != -5 {
			t.Fatalf("%s code = %d, want -5 (%v)", path, code, body)
		}
		if msg := mustString(t, body["msg"]); msg != "No Act!" {
			t.Errorf("%s msg = %q, want No Act!", path, msg)
		}
	}
}

// TestV1AuthFailureCodes pins the distinct pre-signature failures of the V1
// entrypoints, which every SDK branches on:
//
//	missing pid      -> -4 "未传入任何参数"      (Epay/mapi.php:2-5)
//	empty pid        -> -1 "商户ID不能为空"      (Epay/includes/lib/api/Pay.php:280-281)
//	unknown merchant -> -3 "商户ID不存在"        (Epay/includes/lib/ApiHelper.php:52-54)
//	bad signature    -> -3 "<type>签名校验失败"  (Epay/includes/lib/Payment.php:41-43)
//
// submit.php answers the missing-pid case with the configuration page instead of
// the sysmsg template (Epay/submit.php:2-6).
func TestV1AuthFailureCodes(t *testing.T) {
	env := newEnvironment(t)
	cases := []struct {
		name     string
		mutate   func(url.Values)
		wantCode int
		wantMsg  string
	}{
		{"missing-pid", func(values url.Values) { values.Del("pid") }, -4, "未传入任何参数"},
		{"empty-pid", func(values url.Values) { values.Set("pid", "0") }, -1, "商户ID不能为空"},
		{"unknown-pid", func(values url.Values) { values.Set("pid", "2147480000") }, -3, "商户ID不存在"},
		{"bad-signature", func(values url.Values) { values.Set("sign", strings.Repeat("0", 32)) }, -3, "MD5签名校验失败"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			values := env.signedValues("V1-AUTH-"+randomSuffix(t), map[string]string{"type": "alipay"})
			testCase.mutate(values)
			status, body := env.form("/mapi.php", values)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (functions.php:1430)", status)
			}
			if code := intOf(t, body["code"]); code != testCase.wantCode {
				t.Fatalf("code = %d, want %d (%v)", code, testCase.wantCode, body)
			}
			if msg := mustString(t, body["msg"]); !strings.Contains(msg, testCase.wantMsg) {
				t.Errorf("msg = %q, want %q", msg, testCase.wantMsg)
			}
		})
	}

	values := env.signedValues("V1-AUTH-SUBMIT", nil)
	values.Del("pid")
	status, body := env.do(http.MethodPost, "/submit.php", values, "")
	if status != http.StatusOK {
		t.Fatalf("submit.php status = %d, want 200", status)
	}
	if !strings.Contains(string(body), "你还未配置支付接口商户") {
		t.Errorf("submit.php body = %q, want the configuration notice", body)
	}
}

// TestV1MapiScanAndDeviceHints pins the method/device parameters of
// Epay/includes/lib/api/Pay.php:296-321: the barcode wallet is derived from the
// auth_code prefix, jsapi requires sub_openid, scan requires auth_code, and a
// jump device overrides the method before the jsapi check runs.
func TestV1MapiScanAndDeviceHints(t *testing.T) {
	env := newEnvironment(t)

	findOrder := func(outTradeNo string) paymentmodel.Order {
		t.Helper()
		var order paymentmodel.Order
		if err := env.app.DB().Where("out_trade_no = ?", outTradeNo).First(&order).Error; err != nil {
			t.Fatalf("load order %s: %v", outTradeNo, err)
		}
		return order
	}

	t.Run("scan-derives-wallet-from-auth-code", func(t *testing.T) {
		id := "V1-SCAN-" + randomSuffix(t)
		status, body := env.form("/mapi.php", env.signedValues(id, map[string]string{
			"type": "", "method": "scan", "auth_code": "134567890123456789",
		}))
		if status != http.StatusOK || intOf(t, body["code"]) != 1 {
			t.Fatalf("scan order = %d %v", status, body)
		}
		if order := findOrder(id); order.PaymentType != "wxpay" {
			t.Fatalf("derived payment type = %q, want wxpay (functions.php:553-575)", order.PaymentType)
		}
	})

	t.Run("scan-rejects-unknown-barcode", func(t *testing.T) {
		status, body := env.form("/mapi.php", env.signedValues("V1-SCAN-BAD-"+randomSuffix(t), map[string]string{
			"type": "", "method": "scan", "auth_code": "999999999999",
		}))
		if status != http.StatusOK || intOf(t, body["code"]) != -1 {
			t.Fatalf("unknown barcode = %d %v", status, body)
		}
		if msg := mustString(t, body["msg"]); !strings.Contains(msg, "未知的付款码类型") {
			t.Fatalf("msg = %q", msg)
		}
	})

	t.Run("scan-requires-auth-code", func(t *testing.T) {
		status, body := env.form("/mapi.php", env.signedValues("V1-SCAN-NOCODE-"+randomSuffix(t), map[string]string{
			"type": "wxpay", "method": "scan",
		}))
		if status != http.StatusOK || intOf(t, body["code"]) != -1 {
			t.Fatalf("scan without auth_code = %d %v", status, body)
		}
		if msg := mustString(t, body["msg"]); !strings.Contains(msg, "auth_code") {
			t.Fatalf("msg = %q", msg)
		}
	})

	t.Run("jsapi-requires-sub-openid", func(t *testing.T) {
		status, body := env.form("/mapi.php", env.signedValues("V1-JSAPI-BAD-"+randomSuffix(t), map[string]string{
			"type": "wxpay", "method": "jsapi", "device": "wechat",
		}))
		if status != http.StatusOK || intOf(t, body["code"]) != -1 {
			t.Fatalf("jsapi without sub_openid = %d %v", status, body)
		}
		if msg := mustString(t, body["msg"]); !strings.Contains(msg, "sub_openid") {
			t.Fatalf("msg = %q", msg)
		}
	})

	t.Run("jsapi-persists-buyer", func(t *testing.T) {
		id := "V1-JSAPI-" + randomSuffix(t)
		openID := "oUpF8uMuAJO_M2pxb1Q9zNjWeS6o"
		status, body := env.form("/mapi.php", env.signedValues(id, map[string]string{
			"type": "wxpay", "method": "jsapi", "device": "wechat", "sub_openid": openID,
		}))
		if status != http.StatusOK || intOf(t, body["code"]) != 1 {
			t.Fatalf("jsapi order = %d %v", status, body)
		}
		order := findOrder(id)
		if order.Buyer == nil || *order.Buyer != openID {
			t.Fatalf("order buyer = %v, want %s (Pay.php:425)", order.Buyer, openID)
		}
	})

	t.Run("jump-device-overrides-jsapi", func(t *testing.T) {
		// Pay.php:304: device=jump forces method=jump, so the jsapi sub_openid
		// requirement no longer applies.
		status, body := env.form("/mapi.php", env.signedValues("V1-JUMP-"+randomSuffix(t), map[string]string{
			"type": "alipay", "method": "jsapi", "device": "jump",
		}))
		if status != http.StatusOK || intOf(t, body["code"]) != 1 {
			t.Fatalf("jump device = %d %v", status, body)
		}
	})
}

// TestSubmit2RetryPage pins Epay/submit2.php: an unknown or finished order renders
// the sysmsg page while an open order returns to the cashier.
func TestSubmit2RetryPage(t *testing.T) {
	env := newEnvironment(t)
	status, body := env.do(http.MethodGet, "/submit2.php?trade_no=NOT-A-TRADE-NO", nil, "")
	if status != http.StatusOK || !strings.Contains(string(body), "该订单号不存在") {
		t.Fatalf("unknown order = %d %s", status, body)
	}

	values := env.signedValues("SUBMIT2-"+randomSuffix(t), map[string]string{"type": "alipay"})
	status, orderBody := env.form("/mapi.php", values)
	if status != http.StatusOK || intOf(t, orderBody["code"]) != 1 {
		t.Fatalf("mapi = %d %v", status, orderBody)
	}
	tradeNo := mustString(t, orderBody["trade_no"])
	if location := env.redirect("/submit2.php?trade_no=" + tradeNo); !strings.Contains(location, "cashier.php") {
		t.Fatalf("open order redirect = %q, want the cashier", location)
	}

	env.paySandboxOrder(tradeNo)
	status, body = env.do(http.MethodGet, "/submit2.php?trade_no="+tradeNo, nil, "")
	if status != http.StatusOK || !strings.Contains(string(body), "已完成支付") {
		t.Fatalf("paid order = %d %s", status, body)
	}
}
