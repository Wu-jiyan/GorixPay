package epayconformance

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/compat/epay/signing"
	paymentmodel "gorixpay/internal/payment/model"
)

// This file pins the epay V2 protocol surface — /api/{class}/{method} and its
// /api.php?s=class/method rewrite — against the PHP reference implementation.
// Every expectation cites the upstream line it comes from, and every request
// goes through the real Gin handler stack over HTTP.
//
// The two response shapes the reference produces are the backbone of these tests
// (ApiHelper.php:20-41):
//
//	1. a method returns an array  -> timestamp + sign_type=RSA + sign are appended
//	   and the whole thing is signed with the platform key, *even when the
//	   business code is negative*;
//	2. anything thrown            -> echojsonmsg() answers a bare
//	   {"code":N,"msg":"..."} that is never signed (functions.php:1430).

// v2Sign signs a parameter set exactly like a V2 merchant client: sorted
// k=v&k=v content, SHA256withRSA, base64 (Payment.php:8-35). sign and sign_type
// are never part of the content.
func v2Sign(t *testing.T, env *environment, values url.Values) string {
	t.Helper()
	key, err := signing.ParseRSAPrivateKeyPEM(env.rsaPrivatePEM)
	if err != nil {
		t.Fatalf("parse merchant private key: %v", err)
	}
	params := make(map[string]any, len(values))
	for name, list := range values {
		if len(list) > 0 {
			params[name] = list[0]
		}
	}
	delete(params, "sign")
	delete(params, "sign_type")
	signature, err := signing.SignRSA(params, key)
	if err != nil {
		t.Fatalf("sign %v: %v", values, err)
	}
	return signature
}

// v2Parameters builds a signed V2 request: pid, a fresh timestamp and
// sign_type=RSA are mandatory on every method (ApiHelper.php:70-75), and sign is
// computed last.
func v2Parameters(t *testing.T, env *environment, extra map[string]string) url.Values {
	t.Helper()
	values := url.Values{
		"pid":       {strconv.FormatInt(env.pid, 10)},
		"timestamp": {strconv.FormatInt(time.Now().Unix(), 10)},
		"sign_type": {"RSA"},
	}
	for name, value := range extra {
		values.Set(name, value)
	}
	values.Set("sign", v2Sign(t, env, values))
	return values
}

// v2UnknownMerchantParameters signs a request for a pid that was never issued, to
// exercise the "商户不存在！" branch of ApiHelper.php:56.
func v2UnknownMerchantParameters(t *testing.T, env *environment, extra map[string]string) url.Values {
	t.Helper()
	values := v2Parameters(t, env, extra)
	values.Set("pid", strconv.FormatInt(env.pid+1, 10))
	values.Set("sign", v2Sign(t, env, values))
	return values
}

// v2ShiftTimestamp rewrites the timestamp of an already signed request and signs it
// again. RSA PKCS#1 v1.5 is deterministic, so two requests built inside the same
// second carry byte-identical signatures and the second one is (correctly) treated as
// a replay; tests that want a *different* document have to move the timestamp.
func v2ShiftTimestamp(t *testing.T, env *environment, values url.Values, offset time.Duration) url.Values {
	t.Helper()
	values.Set("timestamp", strconv.FormatInt(time.Now().Add(offset).Unix(), 10))
	values.Set("sign", v2Sign(t, env, values))
	return values
}

// v2Send performs a request without following redirects, so pay/submit's 302 to the
// cashier page can be asserted instead of chased off-host.
func v2Send(t *testing.T, env *environment, method, path string, values url.Values) (int, http.Header, []byte) {
	t.Helper()
	var body io.Reader
	if values != nil {
		body = strings.NewReader(values.Encode())
	}
	request, err := http.NewRequest(method, env.url+path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if values != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	return response.StatusCode, response.Header, payload
}

// v2Post sends a signed form body to a V2 endpoint and decodes the JSON answer.
func v2Post(t *testing.T, env *environment, path string, values url.Values) map[string]any {
	t.Helper()
	status, body := env.form(path, values)
	if status != http.StatusOK {
		t.Fatalf("POST %s = %d, V2 always answers 200 (manifest transport.business_http_status): %v", path, status, body)
	}
	return body
}

// v2ExpectSigned asserts the signed response envelope and verifies it with the
// platform public key merchants receive from /api/v1/merchant/credentials/v2.
func v2ExpectSigned(t *testing.T, env *environment, body map[string]any) {
	t.Helper()
	requireKeys(t, body, "code", "timestamp", "sign_type", "sign")
	timestamp, ok := body["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp must be a string (Payment.php:27 casts time()), got %#v", body["timestamp"])
	}
	if len(timestamp) != 10 {
		t.Fatalf("timestamp %q is not a unix seconds string", timestamp)
	}
	if body["sign_type"] != "RSA" {
		t.Fatalf("sign_type = %v, want RSA", body["sign_type"])
	}
	signature, ok := body["sign"].(string)
	if !ok || signature == "" {
		t.Fatalf("sign missing: %v", body)
	}
	publicKey, err := signing.ParseRSAPublicKeyPEM(env.platformPublicPEM)
	if err != nil {
		t.Fatalf("parse platform public key: %v", err)
	}
	if !signing.VerifyRSA(body, publicKey, signature) {
		t.Fatalf("response sign does not verify with the platform public key: %v", body)
	}
}

// v2ExpectUnsigned asserts the thrown-exception shape: no timestamp, no sign_type,
// no sign — only code and msg.
func v2ExpectUnsigned(t *testing.T, body map[string]any) {
	t.Helper()
	requireKeys(t, body, "code", "msg")
	forbidKeys(t, body, "timestamp", "sign_type", "sign")
}

// v2ExpectRejected asserts a negative code delivered as an unsigned exception.
func v2ExpectRejected(t *testing.T, body map[string]any) int {
	t.Helper()
	code := intOf(t, body["code"])
	if code >= 0 {
		t.Fatalf("code = %d, want a negative error: %v", code, body)
	}
	v2ExpectUnsigned(t, body)
	return code
}

// v2ExpectOK asserts code=0 plus the signed envelope.
func v2ExpectOK(t *testing.T, env *environment, body map[string]any) {
	t.Helper()
	if code := intOf(t, body["code"]); code != 0 {
		t.Fatalf("code = %d, want 0: %v", code, body)
	}
	v2ExpectSigned(t, env, body)
}

func v2MustContain(t *testing.T, body map[string]any, fragment string) {
	t.Helper()
	message, _ := body["msg"].(string)
	if !strings.Contains(message, fragment) {
		t.Fatalf("msg = %q, want it to contain %q", message, fragment)
	}
}

// v2CreatePendingOrder opens an order through mapi.php and leaves it unpaid, which
// is what close/refund rejection paths need.
func v2CreatePendingOrder(t *testing.T, env *environment, id string) string {
	t.Helper()
	status, body := env.form("/mapi.php", env.signedValues(id, map[string]string{"type": "alipay"}))
	if status != http.StatusOK {
		t.Fatalf("mapi %s = %d %v", id, status, body)
	}
	if code := intOf(t, body["code"]); code != 1 {
		t.Fatalf("mapi %s code = %d (%v)", id, code, body)
	}
	return mustString(t, body["trade_no"])
}

// TestV2PayQuerySignedEnvelope covers the happy path of Pay.php:535-557: a signed
// query returns the order fields, the platform envelope and a verifiable sign.
func TestV2PayQuerySignedEnvelope(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("v2-query-paid")

	body := v2Post(t, env, "/api/pay/query", v2Parameters(t, env, map[string]string{"trade_no": tradeNo}))
	v2ExpectOK(t, env, body)
	requireKeys(t, body, "trade_no", "out_trade_no", "type", "pid", "name", "money", "status", "addtime", "endtime")
	if body["trade_no"] != tradeNo {
		t.Errorf("trade_no = %v, want %s", body["trade_no"], tradeNo)
	}
	if body["out_trade_no"] != "v2-query-paid" {
		t.Errorf("out_trade_no = %v", body["out_trade_no"])
	}
	if body["pid"] != strconv.FormatInt(env.pid, 10) {
		t.Errorf("pid = %v, want %d", body["pid"], env.pid)
	}
	if body["money"] != "10.00" || intOf(t, body["status"]) != 1 {
		t.Errorf("money/status = %v/%v, want 10.00/1", body["money"], body["status"])
	}

	// The out_trade_no selector is the documented alternative.
	byOut := v2Post(t, env, "/api/pay/query", v2Parameters(t, env, map[string]string{"out_trade_no": "v2-query-paid"}))
	v2ExpectOK(t, env, byOut)
	if byOut["trade_no"] != tradeNo {
		t.Errorf("out_trade_no selector returned %v, want %s", byOut["trade_no"], tradeNo)
	}
}

// TestV2PayQueryMissingSelectorRejected covers Pay.php:546-547: with neither
// selector the method throws, so the answer is an unsigned -1.
func TestV2PayQueryMissingSelectorRejected(t *testing.T) {
	env := newEnvironment(t)
	body := v2Post(t, env, "/api/pay/query", v2Parameters(t, env, nil))
	if code := v2ExpectRejected(t, body); code != -1 {
		t.Fatalf("code = %d, want -1: %v", code, body)
	}
	v2MustContain(t, body, "订单号不能为空")
}

// TestV2ApiPhpSuffixMatchesPathDispatch pins the rewrite target of the manifest:
// /api.php?s=pay/query and /api/pay/query are the same method (api.php:6-9).
func TestV2ApiPhpSuffixMatchesPathDispatch(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("v2-suffix")

	pathBody := v2Post(t, env, "/api/pay/query", v2Parameters(t, env, map[string]string{"trade_no": tradeNo}))
	suffixBody := v2Post(t, env, "/api.php?s=pay/query", v2Parameters(t, env, map[string]string{"trade_no": tradeNo}))

	v2ExpectOK(t, env, pathBody)
	v2ExpectOK(t, env, suffixBody)
	for _, key := range []string{"code", "trade_no", "out_trade_no", "type", "pid", "name", "money", "status", "addtime", "endtime"} {
		if pathBody[key] != suffixBody[key] {
			t.Errorf("%q differs: path=%v suffix=%v", key, pathBody[key], suffixBody[key])
		}
	}

	// A method without a static /api/{class}/{method} route must reach the same
	// dispatcher through both entry points as well.
	createExtra := map[string]string{
		"out_trade_no": "v2-suffix-create", "money": "1.00", "name": "suffix create",
		"type": "alipay", "clientip": "127.0.0.1", "notify_url": "https://merchant.example/notify",
	}
	pathCreate := v2Post(t, env, "/api/pay/create", v2Parameters(t, env, createExtra))
	v2ExpectOK(t, env, pathCreate)

	extra := map[string]string{}
	for key, value := range createExtra {
		extra[key] = value
	}
	extra["out_trade_no"] = "v2-suffix-create-suffix"
	suffixCreate := v2Post(t, env, "/api.php?s=pay/create", v2Parameters(t, env, extra))
	v2ExpectOK(t, env, suffixCreate)
	requireKeys(t, suffixCreate, "pay_type", "pay_info", "trade_no")
}

// TestV2RejectsBadSignature covers ApiHelper.php:59-63: any signature failure is
// re-thrown as code -3 and therefore unsigned.
func TestV2RejectsBadSignature(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("v2-badsign")

	tampered := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})
	tampered.Set("trade_no", tradeNo+"9")
	body := v2Post(t, env, "/api/pay/query", tampered)
	if code := v2ExpectRejected(t, body); code != -3 {
		t.Fatalf("tampered parameter code = %d, want -3: %v", code, body)
	}
	v2MustContain(t, body, "签名")

	garbage := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})
	garbage.Set("sign", "AAAA"+garbage.Get("sign")[4:])
	body = v2Post(t, env, "/api/pay/query", garbage)
	if code := v2ExpectRejected(t, body); code != -3 {
		t.Fatalf("corrupted sign code = %d, want -3: %v", code, body)
	}

	// sign_type must be RSA: a merchant that only holds a V2 credential cannot use
	// MD5 (ApiHelper.php:68-69), and the throw is re-coded as -3.
	//
	// The api.php selector is used on purpose: the shared /api/pay/query path is
	// dual stack and hands MD5 requests to the legacy V1 handler (see delegateV2 in
	// internal/compat/epay/payment/handler.go), which is a documented port decision
	// rather than upstream behaviour.
	md5typed := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})
	md5typed.Set("sign_type", "MD5")
	body = v2Post(t, env, "/api.php?s=pay/query", md5typed)
	if code := v2ExpectRejected(t, body); code != -3 {
		t.Fatalf("MD5 sign_type code = %d, want -3: %v", code, body)
	}

	// Missing sign is rejected by verifySign() before the key is even used
	// (Payment.php:39).
	unsigned := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})
	unsigned.Del("sign")
	body = v2Post(t, env, "/api/pay/query", unsigned)
	if code := v2ExpectRejected(t, body); code != -3 {
		t.Fatalf("missing sign code = %d, want -3: %v", code, body)
	}
	v2MustContain(t, body, "签名")
}

// TestV2RejectsMissingTimestamp covers ApiHelper.php:71: the timestamp is
// mandatory once API_INIT is defined, and the throw is wrapped as -3 by :59-63.
func TestV2RejectsMissingTimestamp(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("v2-notimestamp")

	values := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})
	values.Del("timestamp")
	body := v2Post(t, env, "/api/pay/query", values)
	if code := v2ExpectRejected(t, body); code != -3 {
		t.Fatalf("code = %d, want -3: %v", code, body)
	}
	v2MustContain(t, body, "时间戳")
}

// TestV2RejectsStaleTimestamp covers ApiHelper.php:72: |time()-timestamp| > 300
// is rejected on both sides of the window.
func TestV2RejectsStaleTimestamp(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("v2-stale")
	for _, offset := range []time.Duration{-301 * time.Second, 301 * time.Second, -3600 * time.Second, 3600 * time.Second} {
		values := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})
		values.Set("timestamp", strconv.FormatInt(time.Now().Add(offset).Unix(), 10))
		values.Set("sign", v2Sign(t, env, values))
		body := v2Post(t, env, "/api/pay/query", values)
		if code := v2ExpectRejected(t, body); code != -3 {
			t.Fatalf("offset %s code = %d, want -3: %v", offset, code, body)
		}
		v2MustContain(t, body, "时间戳")
	}

	// Inside the window the same request is accepted, which proves the boundary.
	values := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})
	values.Set("timestamp", strconv.FormatInt(time.Now().Add(-290*time.Second).Unix(), 10))
	values.Set("sign", v2Sign(t, env, values))
	v2ExpectOK(t, env, v2Post(t, env, "/api/pay/query", values))
}

// TestV2RejectsReplay sends the identical signed document twice. The second one is
// rejected before the handler runs; re-signing with a fresh timestamp is accepted
// again, so only the exact document is burned.
func TestV2RejectsReplay(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := v2CreatePendingOrder(t, env, "v2-replay")

	values := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})
	first := v2Post(t, env, "/api/pay/close", values)
	v2ExpectOK(t, env, first)
	if first["msg"] != "订单关闭成功" {
		t.Errorf("msg = %v, want 订单关闭成功", first["msg"])
	}

	replay := v2Post(t, env, "/api/pay/close", values)
	if code := v2ExpectRejected(t, replay); code != -3 {
		t.Fatalf("replay code = %d, want -3: %v", code, replay)
	}
	v2MustContain(t, replay, "重复请求")

	// A brand new signature is not a replay: it reaches the handler again. This port
	// closes idempotently, so re-closing a closed order is still code 0 — a
	// deliberate deviation from Order.php:145-146, which answers
	// "该订单状态不支持关闭！". What matters for the protocol is that the answer is
	// signed and that only the *identical* document was burned.
	fresh := v2ShiftTimestamp(t, env, v2Parameters(t, env, map[string]string{"trade_no": tradeNo}), -5*time.Second)
	body := v2Post(t, env, "/api/pay/close", fresh)
	if code := intOf(t, body["code"]); code != 0 {
		t.Fatalf("re-signed close code = %d, want the idempotent 0: %v", code, body)
	}
	v2ExpectSigned(t, env, body)
	if body["trade_no"] != tradeNo {
		t.Errorf("idempotent close returned %v, want %s", body["trade_no"], tradeNo)
	}
}

// TestV2RejectsUnknownRoute covers ApiHelper.php:36-41: an unknown class/method
// answers -5 and is never signed.
func TestV2RejectsUnknownRoute(t *testing.T) {
	env := newEnvironment(t)
	signed := v2Parameters(t, env, nil)

	for _, path := range []string{"/api/complain/image2", "/api/pay/unknown", "/api.php?s=pay/unknown", "/api.php?s=nope/nothing"} {
		body := v2Post(t, env, path, signed)
		if code := v2ExpectRejected(t, body); code != -5 {
			t.Fatalf("%s code = %d, want -5: %v", path, code, body)
		}
	}
}

// TestV2RejectsGETParameters covers ApiHelper.php:47-51: verify() reads $_POST
// only, so a GET request carries no parameters at all and is answered with -4.
//
// pay/submit is the single documented exception (Pay.php:12-18 reads $_GET first)
// and gets its own test below.
func TestV2RejectsGETParameters(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("v2-get")
	values := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})

	for _, target := range []string{"/api/pay/query?" + values.Encode(), "/api.php?s=pay/query&" + values.Encode()} {
		status, raw := env.do(http.MethodGet, target, nil, "")
		if status != http.StatusOK {
			t.Fatalf("GET %s = %d", target, status)
		}
		body := decodeJSON(t, raw)
		if code := v2ExpectRejected(t, body); code != -4 {
			t.Fatalf("GET %s code = %d, want -4: %v", target, code, body)
		}
		v2MustContain(t, body, "未传入任何参数")
	}

	// Missing pid with other parameters present is still the "no parameters"
	// branch (-4), not the "商户ID不能为空" branch (-1) of ApiHelper.php:54.
	withoutPID := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})
	withoutPID.Del("pid")
	body := v2Post(t, env, "/api/pay/query", withoutPID)
	if code := v2ExpectRejected(t, body); code != -4 {
		t.Fatalf("missing pid code = %d, want -4: %v", code, body)
	}

	// pid present but empty is the -1 branch.
	emptyPID := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})
	emptyPID.Set("pid", "")
	body = v2Post(t, env, "/api/pay/query", emptyPID)
	if code := v2ExpectRejected(t, body); code != -1 {
		t.Fatalf("empty pid code = %d, want -1: %v", code, body)
	}
}

// TestV2ApiPhpSuffixRejectsQueryOnlyParameters is the explicit contract for the
// api.php rewrite target: the s= selector may live in the query string (api.php:6-9
// reads $_GET['s']), but the signed parameters may not. ApiHelper.php:47-51 reads
// $_POST only, so a query-string-only call has no parameters at all and is answered
// with {"code":-4,"msg":"未传入任何参数"} before the signature is ever checked.
func TestV2ApiPhpSuffixRejectsQueryOnlyParameters(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("v2-suffix-query-only")
	values := v2Parameters(t, env, map[string]string{"trade_no": tradeNo})

	// Parameters in the query, empty body: rejected as "no parameters".
	status, raw := env.do(http.MethodGet, "/api.php?s=pay/query&"+values.Encode(), nil, "")
	if status != http.StatusOK {
		t.Fatalf("GET = %d, want the 200 business envelope", status)
	}
	body := decodeJSON(t, raw)
	if code := v2ExpectRejected(t, body); code != -4 {
		t.Fatalf("query-only parameters code = %d, want -4: %v", code, body)
	}
	v2MustContain(t, body, "未传入任何参数")

	// The very same parameters in the body are accepted, so -4 is about the
	// transport, not about the values.
	accepted := v2Post(t, env, "/api.php?s=pay/query", values)
	v2ExpectOK(t, env, accepted)
	if accepted["trade_no"] != tradeNo {
		t.Errorf("trade_no = %v, want %s", accepted["trade_no"], tradeNo)
	}

	// POST with the selector in the query and an empty body is the same -4 case.
	status, raw = env.do(http.MethodPost, "/api.php?s=pay/query", url.Values{}, "")
	if status != http.StatusOK {
		t.Fatalf("POST empty body = %d", status)
	}
	if code := v2ExpectRejected(t, decodeJSON(t, raw)); code != -4 {
		t.Fatalf("POST with empty body should be -4: %s", raw)
	}
}

// TestV2PayCreateSuccessSigned covers Pay.php:263-533 through
// Payment::echoJson (Payment.php:155-244): code=0 plus trade_no, pay_type and
// pay_info, signed only because the code is 0.
func TestV2PayCreateSuccessSigned(t *testing.T) {
	env := newEnvironment(t)
	body := v2Post(t, env, "/api/pay/create", v2Parameters(t, env, map[string]string{
		"out_trade_no": "v2-create-ok", "money": "1.00", "name": "create ok",
		"type": "alipay", "clientip": "127.0.0.1", "notify_url": "https://merchant.example/notify",
		"return_url": "https://merchant.example/return", "param": "attach-1",
	}))
	v2ExpectOK(t, env, body)
	requireKeys(t, body, "trade_no", "pay_type", "pay_info")
	if body["pay_type"] != "jump" {
		t.Errorf("pay_type = %v, want jump for the sandbox channel", body["pay_type"])
	}
	payInfo, ok := body["pay_info"].(string)
	if !ok || payInfo == "" {
		t.Errorf("pay_info = %v, want a payment URL or payload", body["pay_info"])
	}
	tradeNo, ok := body["trade_no"].(string)
	if !ok || tradeNo == "" {
		t.Fatalf("trade_no is empty: %v", body)
	}
	if !strings.Contains(payInfo, tradeNo) {
		t.Errorf("pay_info = %q, want it to reference the trade number %s", payInfo, tradeNo)
	}
}

// TestV2PayCreateFailureUnsigned covers Pay.php:313-331: parameter rejects use
// echojsonmsg, so a failed create is a bare code/msg — never signed.
func TestV2PayCreateFailureUnsigned(t *testing.T) {
	env := newEnvironment(t)
	base := map[string]string{
		"out_trade_no": "v2-create-bad", "money": "1.00", "name": "create bad",
		"type": "alipay", "clientip": "127.0.0.1", "notify_url": "https://merchant.example/notify",
	}

	cases := []struct {
		name   string
		mutate func(map[string]string)
		want   int
	}{
		{"zero amount", func(values map[string]string) { values["money"] = "0" }, -1},
		{"negative amount", func(values map[string]string) { values["money"] = "-1" }, -1},
		{"missing out_trade_no", func(values map[string]string) { delete(values, "out_trade_no") }, -1},
		{"missing notify_url", func(values map[string]string) { delete(values, "notify_url") }, -1},
		{"missing name", func(values map[string]string) { delete(values, "name") }, -1},
		{"missing clientip", func(values map[string]string) { delete(values, "clientip") }, -1},
		{"missing type", func(values map[string]string) { delete(values, "type") }, -1},
		{"bad out_trade_no", func(values map[string]string) { values["out_trade_no"] = "文 件" }, -1},
	}
	for _, testCase := range cases {
		extra := map[string]string{}
		for key, value := range base {
			extra[key] = value
		}
		testCase.mutate(extra)
		body := v2Post(t, env, "/api/pay/create", v2Parameters(t, env, extra))
		code := intOf(t, body["code"])
		if code != testCase.want {
			t.Errorf("%s: code = %d, want %d: %v", testCase.name, code, testCase.want, body)
		}
		v2ExpectUnsigned(t, body)
	}
}

// TestV2PaySubmitRedirects covers Pay.php:8-533: pay/submit is excluded from the
// shared verify list and answers HTML; this port redirects to the cashier instead,
// and unlike every other method it accepts its parameters in the query string
// (Pay.php:12-18).
func TestV2PaySubmitRedirects(t *testing.T) {
	env := newEnvironment(t)
	create := func(outTradeNo, money string) map[string]string {
		return map[string]string{
			"out_trade_no": outTradeNo, "money": money, "name": "submit",
			"type": "alipay", "notify_url": "https://merchant.example/notify",
			"return_url": "https://merchant.example/return",
		}
	}

	status, headers, raw := v2Send(t, env, http.MethodPost, "/api/pay/submit", v2Parameters(t, env, create("v2-submit-post", "1.00")))
	if status != http.StatusFound {
		t.Fatalf("POST submit = %d %s, want 302", status, raw)
	}
	location := headers.Get("Location")
	if !strings.Contains(location, "/cashier.php?trade_no=") {
		t.Fatalf("Location = %q, want the cashier redirect", location)
	}

	// The GET variant is accepted for pay/submit only, and the signature covers the
	// query parameters (Pay.php:12-18 uses $_GET, so getSignContent sees them).
	query := v2Parameters(t, env, create("v2-submit-get", "2.00"))
	status, _, raw = v2Send(t, env, http.MethodGet, "/api/pay/submit?"+query.Encode(), nil)
	if status != http.StatusFound {
		t.Fatalf("GET submit = %d %s, want 302", status, raw)
	}

	// Every other method rejects the same query-string transport with -4.
	status, _, raw = v2Send(t, env, http.MethodGet, "/api/pay/query?"+v2Parameters(t, env, map[string]string{"trade_no": "x"}).Encode(), nil)
	if status != http.StatusOK {
		t.Fatalf("GET query = %d %s", status, raw)
	}
	if code := v2ExpectRejected(t, decodeJSON(t, raw)); code != -4 {
		t.Fatalf("GET pay/query code = %d, want -4", code)
	}
}

// TestV2PaySubmitRejectsBadAmount keeps the failure branch of pay/submit honest:
// a zero amount is refused by Pay.php:59 with echojsonmsg, so it is unsigned.
func TestV2PaySubmitRejectsBadAmount(t *testing.T) {
	env := newEnvironment(t)
	body := v2Post(t, env, "/api/pay/submit", v2Parameters(t, env, map[string]string{
		"out_trade_no": "v2-submit-zero", "money": "0", "name": "submit zero",
		"type": "alipay", "notify_url": "https://merchant.example/notify",
	}))
	if code := v2ExpectRejected(t, body); code != -1 {
		t.Fatalf("code = %d, want -1: %v", code, body)
	}
}

// TestV2PayCloseSuccessAndFailure covers Pay.php:622-641 plus Order::close
// (Order.php:137-152): the empty selector is thrown (unsigned), while a missing or
// unclosable order is a returned array (signed).
func TestV2PayCloseSuccessAndFailure(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := v2CreatePendingOrder(t, env, "v2-close")

	success := v2Post(t, env, "/api/pay/close", v2Parameters(t, env, map[string]string{"trade_no": tradeNo}))
	v2ExpectOK(t, env, success)
	if success["trade_no"] != tradeNo {
		t.Errorf("trade_no = %v, want %s", success["trade_no"], tradeNo)
	}

	empty := v2Post(t, env, "/api/pay/close", v2Parameters(t, env, nil))
	if code := v2ExpectRejected(t, empty); code != -1 {
		t.Fatalf("empty selector code = %d, want -1: %v", code, empty)
	}
	v2MustContain(t, empty, "订单号不能为空")

	missing := v2Post(t, env, "/api/pay/close", v2Parameters(t, env, map[string]string{"trade_no": "2026010100000000000"}))
	if code := intOf(t, missing["code"]); code != -1 {
		t.Fatalf("missing order code = %d, want -1: %v", code, missing)
	}
	v2ExpectSigned(t, env, missing)
}

// TestV2PayRefundSuccessAndFailures covers Pay.php:559-595 and Order::refund
// (Order.php:65-134).
func TestV2PayRefundSuccessAndFailures(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("v2-refund")

	success := v2Post(t, env, "/api/pay/refund", v2Parameters(t, env, map[string]string{
		"trade_no": tradeNo, "out_refund_no": "v2-refund-1",
	}))
	v2ExpectOK(t, env, success)
	requireKeys(t, success, "refund_no", "out_refund_no", "trade_no", "out_trade_no", "money")
	if success["out_refund_no"] != "v2-refund-1" {
		t.Errorf("out_refund_no = %v, want v2-refund-1", success["out_refund_no"])
	}

	// The out_trade_no selector is resolved first and a miss is thrown
	// (Pay.php:570-573): unsigned.
	unsigned := v2Post(t, env, "/api/pay/refund", v2Parameters(t, env, map[string]string{
		"out_trade_no": "v2-refund-nope",
	}))
	if code := v2ExpectRejected(t, unsigned); code != -1 {
		t.Fatalf("unknown out_trade_no code = %d, want -1: %v", code, unsigned)
	}

	// An unpaid order is refused by Order::refund as a returned array
	// (Order.php:73-74): signed.
	pending := v2CreatePendingOrder(t, env, "v2-refund-unpaid")
	unpaid := v2Post(t, env, "/api/pay/refund", v2Parameters(t, env, map[string]string{"trade_no": pending}))
	if code := intOf(t, unpaid["code"]); code != -1 {
		t.Fatalf("unpaid refund code = %d, want -1: %v", code, unpaid)
	}
	v2ExpectSigned(t, env, unpaid)
	v2MustContain(t, unpaid, "未支付")

	// A malformed amount is thrown before the selector (Pay.php:565-566): unsigned.
	badMoney := v2Post(t, env, "/api/pay/refund", v2Parameters(t, env, map[string]string{
		"trade_no": tradeNo, "money": "abc",
	}))
	if code := v2ExpectRejected(t, badMoney); code != -1 {
		t.Fatalf("bad amount code = %d, want -1: %v", code, badMoney)
	}

	empty := v2Post(t, env, "/api/pay/refund", v2Parameters(t, env, nil))
	if code := v2ExpectRejected(t, empty); code != -1 {
		t.Fatalf("empty selector code = %d, want -1: %v", code, empty)
	}
}

// TestV2PayRefundQuerySuccessAndFailures covers Pay.php:597-620.
func TestV2PayRefundQuerySuccessAndFailures(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("v2-refundquery")
	refund := v2Post(t, env, "/api/pay/refund", v2Parameters(t, env, map[string]string{
		"trade_no": tradeNo, "out_refund_no": "v2-refundquery-1",
	}))
	v2ExpectOK(t, env, refund)
	refundNo := mustString(t, refund["refund_no"])

	success := v2Post(t, env, "/api/pay/refundquery", v2Parameters(t, env, map[string]string{"refund_no": refundNo}))
	v2ExpectOK(t, env, success)
	requireKeys(t, success, "refund_no", "out_refund_no", "money", "status", "addtime", "endtime")
	if success["refund_no"] != refundNo {
		t.Errorf("refund_no = %v, want %s", success["refund_no"], refundNo)
	}

	byOut := v2Post(t, env, "/api/pay/refundquery", v2Parameters(t, env, map[string]string{"out_refund_no": "v2-refundquery-1"}))
	v2ExpectOK(t, env, byOut)

	empty := v2Post(t, env, "/api/pay/refundquery", v2Parameters(t, env, nil))
	if code := v2ExpectRejected(t, empty); code != -1 {
		t.Fatalf("empty selector code = %d, want -1: %v", code, empty)
	}
	v2MustContain(t, empty, "退款单号不能为空")

	missing := v2Post(t, env, "/api/pay/refundquery", v2Parameters(t, env, map[string]string{"refund_no": "RF00000000000000000"}))
	if code := v2ExpectRejected(t, missing); code != -1 {
		t.Fatalf("missing refund code = %d, want -1: %v", code, missing)
	}
	v2MustContain(t, missing, "退款记录不存在")
}

// TestV2MerchantInfoSuccessAndFailure covers Merchant.php:8-23.
func TestV2MerchantInfoSuccessAndFailure(t *testing.T) {
	env := newEnvironment(t)
	env.createPaidOrder("v2-info")

	body := v2Post(t, env, "/api/merchant/info", v2Parameters(t, env, nil))
	v2ExpectOK(t, env, body)
	requireKeys(t, body, "pid", "status", "money", "frozen_money", "orders", "orders_today", "orders_lastday")
	if body["pid"] != strconv.FormatInt(env.pid, 10) {
		t.Errorf("pid = %v", body["pid"])
	}
	if intOf(t, body["orders"]) < 1 || intOf(t, body["orders_today"]) < 1 {
		t.Errorf("order counters = %v/%v, want at least the paid order", body["orders"], body["orders_today"])
	}

	unknown := v2Post(t, env, "/api/merchant/info", v2UnknownMerchantParameters(t, env, nil))
	if code := v2ExpectRejected(t, unknown); code != -1 {
		t.Fatalf("unknown merchant code = %d, want -1: %v", code, unknown)
	}
	v2MustContain(t, unknown, "商户不存在")
}

// TestV2MerchantOrdersSuccessAndFailure covers Merchant.php:25-49 including the
// limit cap of 50.
func TestV2MerchantOrdersSuccessAndFailure(t *testing.T) {
	env := newEnvironment(t)
	env.createPaidOrder("v2-orders-1")
	env.createPaidOrder("v2-orders-2")

	body := v2Post(t, env, "/api/merchant/orders", v2Parameters(t, env, map[string]string{"limit": "1"}))
	v2ExpectOK(t, env, body)
	requireKeys(t, body, "count", "data")
	data, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array: %v", body["data"])
	}
	if len(data) != 1 {
		t.Fatalf("limit=1 returned %d rows", len(data))
	}
	if intOf(t, body["count"]) < 2 {
		t.Errorf("count = %v, want at least 2", body["count"])
	}
	first, ok := data[0].(map[string]any)
	if !ok {
		t.Fatalf("row is not an object: %v", data[0])
	}
	requireKeys(t, first, "trade_no", "out_trade_no", "type", "pid", "addtime", "name", "money", "status")

	// The cap is 50: asking for 500 must not fail, and must not return more than
	// the account owns anyway.
	capped := v2Post(t, env, "/api/merchant/orders", v2Parameters(t, env, map[string]string{"limit": "500"}))
	v2ExpectOK(t, env, capped)

	badLimit := v2Post(t, env, "/api/merchant/orders", v2Parameters(t, env, map[string]string{"limit": "-1"}))
	if code := v2ExpectRejected(t, badLimit); code != -4 {
		t.Fatalf("limit=-1 code = %d, want -4: %v", code, badLimit)
	}

	unknown := v2Post(t, env, "/api/merchant/orders", v2UnknownMerchantParameters(t, env, nil))
	if code := v2ExpectRejected(t, unknown); code != -1 {
		t.Fatalf("unknown merchant code = %d, want -1: %v", code, unknown)
	}
}

// TestV2MerchantOrdersStatusQuirk pins the upstream quirk documented in
// tests/contracts/epay/manifest.yaml (known_quirk
// status_detected_in_POST_but_read_from_GET): Merchant.php:31-38 decides whether to
// filter with isset($queryArr['status']) — the signed POST payload — but then reads
// the value from $_GET. A status sent only in the POST body therefore filters on
// intval(”) = 0, i.e. unpaid orders, and never on the value the merchant sent.
func TestV2MerchantOrdersStatusQuirk(t *testing.T) {
	env := newEnvironment(t)
	paid := env.createPaidOrder("v2-quirk-paid")
	pending := v2CreatePendingOrder(t, env, "v2-quirk-pending")

	// POST-only status: presence triggers the filter, the value is intval('') = 0.
	byPostOnly := v2Post(t, env, "/api/merchant/orders", v2Parameters(t, env, map[string]string{"status": "1"}))
	v2ExpectOK(t, env, byPostOnly)
	for _, row := range v2Rows(t, byPostOnly) {
		if intOf(t, row["status"]) != 0 {
			t.Fatalf("POST-only status=1 filtered to %v, upstream filters on intval($_GET['status']) = 0: %v", row["status"], byPostOnly)
		}
		if row["trade_no"] == paid {
			t.Fatalf("POST-only status=1 returned the paid order, which upstream cannot do: %v", byPostOnly)
		}
	}

	// status in the query string together with its presence in the body is the
	// only combination that actually filters by value.
	byQuery := v2Post(t, env, "/api/merchant/orders?status=1", v2Parameters(t, env, map[string]string{"status": "1"}))
	v2ExpectOK(t, env, byQuery)
	rows := v2Rows(t, byQuery)
	if len(rows) != 1 || rows[0]["trade_no"] != paid {
		t.Fatalf("query status=1 returned %v, want exactly the paid order %s: %v", rows, paid, byQuery)
	}
	_ = pending

	// intval() never fails upstream: an unmapped status matches nothing instead of
	// raising an error.
	unmapped := v2Post(t, env, "/api/merchant/orders?status=9", v2Parameters(t, env, map[string]string{"status": "9"}))
	v2ExpectOK(t, env, unmapped)
	if rows := v2Rows(t, unmapped); len(rows) != 0 {
		t.Fatalf("status=9 returned %v, want no rows", rows)
	}
}

func v2Rows(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array: %v", body["data"])
	}
	rows := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("row is not an object: %v", item)
		}
		rows = append(rows, row)
	}
	return rows
}

// TestV2TransferSubmitSuccessAndFailures covers Transfer.php:17-40.
func TestV2TransferSubmitSuccessAndFailures(t *testing.T) {
	env := newEnvironment(t)
	// Fund the merchant so the payout passes the balance check.
	env.createPaidOrder("v2-transfer-fund")

	success := v2Post(t, env, "/api/transfer/submit", v2Parameters(t, env, map[string]string{
		"type": "alipay", "account": "payee@example.com", "name": "收款人",
		"money": "1.00", "out_biz_no": "v2-transfer-1",
	}))
	v2ExpectOK(t, env, success)
	requireKeys(t, success, "transfer_no", "out_transfer_no", "status", "money")
	if success["out_transfer_no"] != "v2-transfer-1" {
		t.Errorf("out_transfer_no = %v, want v2-transfer-1", success["out_transfer_no"])
	}
	transferNo := mustString(t, success["transfer_no"])

	// Repeating an out_biz_no with identical parameters is idempotent in this port
	// (transfer.Service.Create returns the existing order), so the answer is code 0
	// with the same transfer_no. Upstream Transfer.php:88-89 returns -1
	// "该交易号已存在，请更换交易号"; the idempotency is a deliberate, stronger
	// contract. The timestamp is shifted because a byte-identical document would be
	// stopped as a replay first.
	idempotent := v2Post(t, env, "/api/transfer/submit", v2ShiftTimestamp(t, env, v2Parameters(t, env, map[string]string{
		"type": "alipay", "account": "payee@example.com", "name": "收款人",
		"money": "1.00", "out_biz_no": "v2-transfer-1",
	}), -3*time.Second))
	v2ExpectOK(t, env, idempotent)
	if idempotent["transfer_no"] != transferNo {
		t.Errorf("idempotent submit returned %v, want the existing %s", idempotent["transfer_no"], transferNo)
	}

	// The same out_biz_no with different parameters is a business conflict, which
	// writeTransferError answers as a signed array.
	conflict := v2Post(t, env, "/api/transfer/submit", v2ShiftTimestamp(t, env, v2Parameters(t, env, map[string]string{
		"type": "alipay", "account": "payee@example.com", "name": "收款人",
		"money": "1.50", "out_biz_no": "v2-transfer-1",
	}), -6*time.Second))
	if code := intOf(t, conflict["code"]); code != -1 {
		t.Fatalf("conflicting out_biz_no code = %d, want -1: %v", code, conflict)
	}
	v2ExpectSigned(t, env, conflict)

	// Thrown parameter rejects (Transfer.php:24-31) stay unsigned.
	rejects := []struct {
		name  string
		extra map[string]string
	}{
		{"missing type", map[string]string{"account": "a@example.com", "money": "1.00", "out_biz_no": "v2-transfer-no-type"}},
		{"missing account", map[string]string{"type": "alipay", "money": "1.00", "out_biz_no": "v2-transfer-no-account"}},
		{"missing money", map[string]string{"type": "alipay", "account": "a@example.com", "out_biz_no": "v2-transfer-no-money"}},
		{"zero money", map[string]string{"type": "alipay", "account": "a@example.com", "money": "0", "out_biz_no": "v2-transfer-zero"}},
		{"bad out_biz_no", map[string]string{"type": "alipay", "account": "a@example.com", "money": "1.00", "out_biz_no": "bad no"}},
	}
	for _, reject := range rejects {
		body := v2Post(t, env, "/api/transfer/submit", v2Parameters(t, env, reject.extra))
		if code := v2ExpectRejected(t, body); code != -1 {
			t.Errorf("%s: code = %d, want -1: %v", reject.name, code, body)
		}
	}

	// Overdrawing the ledger balance is signed as well (Transfer.php:110-113).
	overdraw := v2Post(t, env, "/api/transfer/submit", v2Parameters(t, env, map[string]string{
		"type": "alipay", "account": "a@example.com", "money": "99999.00", "out_biz_no": "v2-transfer-overdraw",
	}))
	if code := intOf(t, overdraw["code"]); code != -1 {
		t.Fatalf("overdraw code = %d, want -1: %v", code, overdraw)
	}
	v2ExpectSigned(t, env, overdraw)

	// And the transfer is queryable by its biz_no.
	query := v2Post(t, env, "/api/transfer/query", v2Parameters(t, env, map[string]string{"biz_no": transferNo}))
	v2ExpectOK(t, env, query)
	if query["transfer_no"] != transferNo {
		t.Errorf("query trade_no = %v, want %s", query["transfer_no"], transferNo)
	}
}

// TestV2TransferQuerySuccessAndFailure covers Transfer.php:42-92.
func TestV2TransferQuerySuccessAndFailure(t *testing.T) {
	env := newEnvironment(t)
	env.createPaidOrder("v2-tquery-fund")
	created := v2Post(t, env, "/api/transfer/submit", v2Parameters(t, env, map[string]string{
		"type": "alipay", "account": "payee@example.com", "money": "1.00", "out_biz_no": "v2-tquery-1",
	}))
	v2ExpectOK(t, env, created)
	transferNo := mustString(t, created["transfer_no"])

	byOut := v2Post(t, env, "/api/transfer/query", v2Parameters(t, env, map[string]string{"out_biz_no": "v2-tquery-1"}))
	v2ExpectOK(t, env, byOut)
	if byOut["out_transfer_no"] != "v2-tquery-1" {
		t.Errorf("out_transfer_no = %v", byOut["out_transfer_no"])
	}

	empty := v2Post(t, env, "/api/transfer/query", v2Parameters(t, env, nil))
	if code := v2ExpectRejected(t, empty); code != -1 {
		t.Fatalf("empty selector code = %d, want -1: %v", code, empty)
	}
	v2MustContain(t, empty, "转账交易号不能为空")

	unknown := v2Post(t, env, "/api/transfer/query", v2Parameters(t, env, map[string]string{"biz_no": "TF0"}))
	if code := v2ExpectRejected(t, unknown); code != -1 {
		t.Fatalf("unknown transfer code = %d, want -1: %v", code, unknown)
	}
	v2MustContain(t, unknown, "转账交易号不存在")

	// The other merchant selector still works for a transfer that exists.
	byTransferNo := v2Post(t, env, "/api/transfer/query", v2Parameters(t, env, map[string]string{"transfer_no": transferNo}))
	v2ExpectOK(t, env, byTransferNo)
}

// TestV2TransferProofQuirk pins the second upstream quirk from the manifest
// (known_quirk out_biz_no_query_uses_biz_no_column): Transfer.php:106-108 looks the
// out_biz_no value up in the biz_no column, so a proof request carrying only
// out_biz_no can never match a real transfer.
func TestV2TransferProofQuirk(t *testing.T) {
	env := newEnvironment(t)
	env.createPaidOrder("v2-proof-fund")
	created := v2Post(t, env, "/api/transfer/submit", v2Parameters(t, env, map[string]string{
		"type": "alipay", "account": "payee@example.com", "money": "1.00", "out_biz_no": "v2-proof-1",
	}))
	v2ExpectOK(t, env, created)
	transferNo := mustString(t, created["transfer_no"])

	byBizNo := v2Post(t, env, "/api/transfer/proof", v2Parameters(t, env, map[string]string{"biz_no": transferNo}))
	v2ExpectOK(t, env, byBizNo)
	requireKeys(t, byBizNo, "proof_url", "transfer_no", "out_transfer_no", "status")

	// out_biz_no is compared against biz_no upstream, so this must miss.
	byOutBizNo := v2Post(t, env, "/api/transfer/proof", v2Parameters(t, env, map[string]string{"out_biz_no": "v2-proof-1"}))
	if code := v2ExpectRejected(t, byOutBizNo); code != -1 {
		t.Fatalf("out_biz_no proof code = %d, want -1: %v", code, byOutBizNo)
	}
	v2MustContain(t, byOutBizNo, "当前转账订单不存在")

	empty := v2Post(t, env, "/api/transfer/proof", v2Parameters(t, env, nil))
	if code := v2ExpectRejected(t, empty); code != -1 {
		t.Fatalf("empty selector code = %d, want -1: %v", code, empty)
	}
	v2MustContain(t, empty, "转账交易号不能为空")
}

// TestV2TransferBalanceSuccessAndFailure covers Transfer.php:119-142.
func TestV2TransferBalanceSuccessAndFailure(t *testing.T) {
	env := newEnvironment(t)
	env.createPaidOrder("v2-balance")

	body := v2Post(t, env, "/api/transfer/balance", v2Parameters(t, env, nil))
	v2ExpectOK(t, env, body)
	requireKeys(t, body, "available_money", "transfer_rate")
	if body["available_money"] != "10.00" {
		t.Errorf("available_money = %v, want 10.00", body["available_money"])
	}

	unknown := v2Post(t, env, "/api/transfer/balance", v2UnknownMerchantParameters(t, env, nil))
	if code := v2ExpectRejected(t, unknown); code != -1 {
		t.Fatalf("unknown merchant code = %d, want -1: %v", code, unknown)
	}
}

// TestV2PayCreateMethodHints covers the method/device parameters that Pay.php
// shares between mapi.php and pay/create (Epay/includes/lib/api/Pay.php:296-321):
// scan derives the wallet from the barcode prefix, jsapi demands sub_openid, and
// a jump device overrides the method.
func TestV2PayCreateMethodHints(t *testing.T) {
	env := newEnvironment(t)

	t.Run("scan-derives-wallet", func(t *testing.T) {
		outTradeNo := "v2-scan-" + randomSuffix(t)
		body := v2Post(t, env, "/api/pay/create", v2Parameters(t, env, map[string]string{
			"out_trade_no": outTradeNo, "money": "0.10", "name": "scan order",
			"clientip": "127.0.0.1", "notify_url": "https://merchant.example/notify",
			"method": "scan", "auth_code": "284567890123456789",
		}))
		v2ExpectOK(t, env, body)
		var order paymentmodel.Order
		if err := env.app.DB().Where("out_trade_no = ?", outTradeNo).First(&order).Error; err != nil {
			t.Fatalf("load order: %v", err)
		}
		if order.PaymentType != "alipay" {
			t.Fatalf("derived payment type = %q, want alipay (functions.php:553-575)", order.PaymentType)
		}
	})

	t.Run("jsapi-requires-sub-openid", func(t *testing.T) {
		body := v2Post(t, env, "/api/pay/create", v2Parameters(t, env, map[string]string{
			"out_trade_no": "v2-jsapi-bad-" + randomSuffix(t), "money": "0.10", "name": "jsapi",
			"clientip": "127.0.0.1", "notify_url": "https://merchant.example/notify",
			"type": "wxpay", "method": "jsapi",
		}))
		v2ExpectUnsigned(t, body)
		if code := v2ExpectRejected(t, body); code != -1 {
			t.Fatalf("code = %d, want -1", code)
		}
		v2MustContain(t, body, "sub_openid")
	})

	t.Run("jsapi-persists-buyer", func(t *testing.T) {
		outTradeNo := "v2-jsapi-" + randomSuffix(t)
		openID := "oUpF8uMuAJO_M2pxb1Q9zNjWeS6o"
		body := v2Post(t, env, "/api/pay/create", v2Parameters(t, env, map[string]string{
			"out_trade_no": outTradeNo, "money": "0.10", "name": "jsapi ok",
			"clientip": "127.0.0.1", "notify_url": "https://merchant.example/notify",
			"type": "wxpay", "method": "jsapi", "sub_openid": openID, "device": "wechat",
		}))
		v2ExpectOK(t, env, body)
		var order paymentmodel.Order
		if err := env.app.DB().Where("out_trade_no = ?", outTradeNo).First(&order).Error; err != nil {
			t.Fatalf("load order: %v", err)
		}
		if order.Buyer == nil || *order.Buyer != openID {
			t.Fatalf("order buyer = %v, want %s (Pay.php:425)", order.Buyer, openID)
		}
	})

	t.Run("jump-device-overrides-jsapi", func(t *testing.T) {
		body := v2Post(t, env, "/api/pay/create", v2Parameters(t, env, map[string]string{
			"out_trade_no": "v2-jump-" + randomSuffix(t), "money": "0.10", "name": "jump",
			"clientip": "127.0.0.1", "notify_url": "https://merchant.example/notify",
			"type": "alipay", "method": "jsapi", "device": "jump",
		}))
		v2ExpectOK(t, env, body)
	})
}
