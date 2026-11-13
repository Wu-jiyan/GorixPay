package epayconformance

import (
	"bytes"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gorixpay/internal/compat/epay/signing"
)

// repoRoot walks up from this test file until it finds go.mod, so the tests do
// not depend on the working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller")
	}
	dir := filepath.Dir(file)
	for depth := 0; depth < 8; depth++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("go.mod not found above test file")
	return ""
}

// phpBinary returns the PHP CLI used as the upstream oracle. Tests that need it
// skip instead of failing when the machine has no PHP.
func phpBinary(t *testing.T) string {
	t.Helper()
	if configured := os.Getenv("EPAY_PHP_BIN"); configured != "" {
		return configured
	}
	binary, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php CLI unavailable, skipping upstream oracle: " + err.Error())
	}
	return binary
}

func scriptPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "tests", "conformance", "epay", "php", name)
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "tests", "contracts", "epay", "fixtures", name)
}

// runPHP feeds a JSON payload to a probe script and decodes its JSON reply.
func runPHP(t *testing.T, script string, payload any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(phpBinary(t), script)
	command.Stdin = bytes.NewReader(encoded)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("php %s failed: %v: %s", filepath.Base(script), err, stderr.String())
	}
	decoded := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("php %s returned %q: %v", filepath.Base(script), output, err)
	}
	return decoded
}

// TestPHPOracleSigningParity proves the Go signing package produces exactly the
// bytes the upstream PHP implementation produces. The PHP side runs the real
// function bodies copied from Epay/includes/lib/Payment.php:8-48.
func TestPHPOracleSigningParity(t *testing.T) {
	privatePEM, err := os.ReadFile(fixturePath(t, "test_only_rsa_private.pem"))
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := signing.ParseRSAPrivateKeyPEM(string(privatePEM))
	if err != nil {
		t.Fatal(err)
	}
	publicPEM, err := os.ReadFile(fixturePath(t, "test_only_rsa_public.pem"))
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := signing.ParseRSAPublicKeyPEM(string(publicPEM))
	if err != nil {
		t.Fatal(err)
	}

	cases := []map[string]any{
		{"pid": 1001, "out_trade_no": "GXP20261113001", "name": "兼容性测试商品", "money": "10.00", "sign_type": "MD5"},
		{"a": " ", "b": "  x  ", "c": "", "d": nil, "e": []string{"array", "ignored"}},
		{"zero": 0, "one": "1.00", "float": 1.5, "big": "100000.00"},
		{"A": "upper", "a": "lower", "z": "last"},
		{"sign": "ignored", "sign_type": "RSA", "amount": "0.01"},
		{},
	}
	const md5Key = "test-key"
	for index, params := range cases {
		php := runPHP(t, scriptPath(t, "sign_oracle.php"), map[string]any{
			"mode": "sign", "params": params, "md5key": md5Key,
			"private_key_file": fixturePath(t, "test_only_rsa_private.pem"),
			"public_key_file":  fixturePath(t, "test_only_rsa_public.pem"),
		})
		// PHP 7 returns false from substr('', 0, -1), so an empty parameter set
		// yields boolean false instead of "". Upstream still hashes it as "".
		phpCanonical := ""
		if text, ok := php["canonical"].(string); ok {
			phpCanonical = text
		}
		if got := signing.Canonical(params); got != phpCanonical {
			t.Errorf("case %d canonical:\n go = %q\nphp = %v", index, got, php["canonical"])
		}
		if got, want := signing.SignMD5(params, md5Key), php["md5"]; got != want {
			t.Errorf("case %d md5: go = %q php = %v", index, got, want)
		}
		goSignature, err := signing.SignRSA(params, privateKey)
		if err != nil {
			t.Fatal(err)
		}
		if goSignature != php["rsa"] {
			t.Errorf("case %d rsa: go = %q php = %v", index, goSignature, php["rsa"])
		}
		if php["php_verified"] != true {
			t.Errorf("case %d: PHP could not verify its own signature", index)
		}
		if !signing.VerifyRSA(params, publicKey, php["rsa"].(string)) {
			t.Errorf("case %d: Go could not verify the PHP signature", index)
		}
	}
}

// upstreamDir returns a local checkout of the upstream Epay PHP source. It is
// not part of this repository: point EPAY_UPSTREAM_DIR at one, or drop a
// checkout at <repo>/Epay. Tests that need it skip when it is absent.
func upstreamDir(t *testing.T) string {
	t.Helper()
	if configured := os.Getenv("EPAY_UPSTREAM_DIR"); configured != "" {
		return configured
	}
	return filepath.Join(repoRoot(t), "Epay")
}

// clientProbe runs the upstream EpayCore client SDK (Epay/plugins/epay/inc/
// EpayCore.class.php) against the live test server.
func (env *environment) clientProbe(action string, extra map[string]any) map[string]any {
	env.t.Helper()
	upstream := upstreamDir(env.t)
	sdk := filepath.Join(upstream, "plugins", "epay", "inc", "EpayCore.class.php")
	if _, err := os.Stat(sdk); err != nil {
		env.t.Skip("upstream Epay source not found, set EPAY_UPSTREAM_DIR to a checkout: " + err.Error())
	}
	payload := map[string]any{
		"action": action, "upstream_dir": upstream,
		"apiurl": env.url + "/", "pid": env.pid, "key": env.md5Key,
	}
	for key, value := range extra {
		payload[key] = value
	}
	return runPHP(env.t, scriptPath(env.t, "client_probe.php"), payload)
}

// TestRealClientSDKCreatesOrder drives mapi.php through the real 彩虹易支付
// client SDK: the SDK signs and curls, the server must answer code=1.
func TestRealClientSDKCreatesOrder(t *testing.T) {
	env := newEnvironment(t)
	outTradeNo := "SDK-" + randomSuffix(t)
	result := env.clientProbe("apiPay", map[string]any{
		"params": map[string]string{
			"pid":  strconv.FormatInt(env.pid, 10),
			"type": "alipay", "out_trade_no": outTradeNo,
			"notify_url": env.merchantNotifyBase + "/notify", "return_url": env.merchantNotifyBase + "/return",
			"name": "客户端中文商品", "money": "10.00", "clientip": "127.0.0.1",
		},
	})
	response, ok := result["response"].(map[string]any)
	if !ok {
		t.Fatalf("client response = %v", result)
	}
	if code := intOf(t, response["code"]); code != 1 {
		t.Fatalf("SDK apiPay code = %d (%v)", code, response)
	}
	if tradeNo := mustString(t, response["trade_no"]); tradeNo == "" {
		t.Fatalf("SDK apiPay trade_no empty: %v", response)
	}
}

// TestRealClientSDKPayLink verifies the SDK generated GET link is accepted by
// submit.php and that the page hands the browser to the cashier.
func TestRealClientSDKPayLink(t *testing.T) {
	env := newEnvironment(t)
	result := env.clientProbe("payLink", map[string]any{
		"params": map[string]string{
			"pid":  strconv.FormatInt(env.pid, 10),
			"type": "alipay", "out_trade_no": "SDK-LINK-" + randomSuffix(t),
			"notify_url": env.merchantNotifyBase + "/notify", "return_url": env.merchantNotifyBase + "/return",
			"name": "链接支付商品", "money": "1.00", "clientip": "127.0.0.1",
		},
	})
	link := mustString(t, result["url"])
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	status, body := env.do(http.MethodGet, parsed.RequestURI(), nil, "")
	if status != http.StatusOK {
		t.Fatalf("submit.php status = %d (%s)", status, body)
	}
	if !strings.Contains(string(body), "cashier.php") {
		t.Fatalf("submit.php did not redirect to the cashier: %s", body)
	}
}

// TestRealClientSDKQueriesOrder exercises api.php?act=order through the SDK.
func TestRealClientSDKQueriesOrder(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("SDK-QUERY-" + randomSuffix(t))
	result := env.clientProbe("queryOrder", map[string]any{"trade_no": tradeNo})
	response, ok := result["response"].(map[string]any)
	if !ok {
		t.Fatalf("client response = %v", result)
	}
	if code := intOf(t, response["code"]); code != 1 {
		t.Fatalf("SDK queryOrder code = %d (%v)", code, response)
	}
	if got := mustString(t, response["trade_no"]); got != tradeNo {
		t.Fatalf("SDK queryOrder trade_no = %q, want %q", got, tradeNo)
	}
	if status := intOf(t, response["status"]); status != 1 {
		t.Fatalf("SDK queryOrder status = %d, want paid (1)", status)
	}
}

// TestRealClientSDKRefundsOrder exercises api.php?act=refund through the SDK.
func TestRealClientSDKRefundsOrder(t *testing.T) {
	env := newEnvironment(t)
	tradeNo := env.createPaidOrder("SDK-REFUND-" + randomSuffix(t))
	result := env.clientProbe("refund", map[string]any{
		"trade_no": tradeNo, "refund_no": "RF-" + randomSuffix(t), "money": "1.00",
	})
	response, ok := result["response"].(map[string]any)
	if !ok {
		t.Fatalf("client response = %v", result)
	}
	if code := intOf(t, response["code"]); code != 0 {
		t.Fatalf("SDK refund code = %d, want 0 (Epay/api.php:116-146): %v", code, response)
	}
}

// TestNotificationSignatureVerifiesInPHP captures the asynchronous notification
// we send and has the merchant SDK verify it exactly like a real merchant would.
func TestNotificationSignatureVerifiesInPHP(t *testing.T) {
	phpBinary(t) // skip early when PHP is unavailable
	env := newEnvironment(t)
	var mutex sync.Mutex
	var captured url.Values
	var method string
	receiver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		captured = request.URL.Query()
		method = request.Method
		mutex.Unlock()
		_, _ = writer.Write([]byte("success"))
	}))
	defer receiver.Close()

	values := env.signedValues("SDK-NOTIFY-"+randomSuffix(t), map[string]string{"type": "alipay"})
	values.Set("notify_url", receiver.URL+"/notify")
	signV1(t, values, env.md5Key)
	status, body := env.form("/mapi.php", values)
	if status != http.StatusOK || intOf(t, body["code"]) != 1 {
		t.Fatalf("mapi = %d %v", status, body)
	}
	env.paySandboxOrder(mustString(t, body["trade_no"]))

	deadline := time.Now().Add(10 * time.Second)
	for {
		mutex.Lock()
		received := captured
		httpMethod := method
		mutex.Unlock()
		if len(received) > 0 {
			if httpMethod != http.MethodGet {
				t.Fatalf("notification method = %s, want GET (Epay/includes/functions.php:522)", httpMethod)
			}
			params := map[string]string{}
			for name := range received {
				params[name] = received.Get(name)
			}
			result := env.clientProbe("verifyNotify", map[string]any{"params": params})
			if result["verified"] != true {
				t.Fatalf("merchant SDK rejected our notification: %v", params)
			}
			if received.Get("trade_status") != "TRADE_SUCCESS" {
				t.Fatalf("trade_status = %q", received.Get("trade_status"))
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no notification delivered within 10s")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestV2ResponseSignatureVerifiesInPHP asks PHP to verify the platform
// signature on a V2 response, proving the response signing is standard
// SHA256withRSA over the documented canonical string.
func TestV2ResponseSignatureVerifiesInPHP(t *testing.T) {
	privatePEM, err := os.ReadFile(fixturePath(t, "test_only_rsa_private.pem"))
	if err != nil {
		t.Fatal(err)
	}
	_ = privatePEM
	env := newEnvironment(t)
	outTradeNo := "V2-SIG-" + randomSuffix(t)
	env.createPaidOrder(outTradeNo)

	merchantKey, err := signing.ParseRSAPrivateKeyPEM(env.rsaPrivatePEM)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{
		"pid": env.pid, "out_trade_no": outTradeNo,
		"timestamp": time.Now().Unix(), "sign_type": "RSA",
	}
	signature, err := signing.SignRSA(params, merchantKey)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{
		"pid": {fmt.Sprint(env.pid)}, "out_trade_no": {outTradeNo},
		"timestamp": {fmt.Sprint(params["timestamp"])}, "sign_type": {"RSA"}, "sign": {signature},
	}
	status, body := env.form("/api/pay/query", form)
	if status != http.StatusOK {
		t.Fatalf("pay/query status = %d %v", status, body)
	}
	if code := intOf(t, body["code"]); code != 0 {
		t.Fatalf("pay/query code = %d (%v)", code, body)
	}
	responseSign := mustString(t, body["sign"])
	if body["sign_type"] != "RSA" {
		t.Fatalf("sign_type = %v", body["sign_type"])
	}

	platformPEM, err := os.ReadFile(fixturePath(t, "test_only_rsa_public.pem"))
	if err != nil {
		t.Fatal(err)
	}
	_ = platformPEM
	platformKeyPEM := filepath.Join(t.TempDir(), "platform.pem")
	if err := os.WriteFile(platformKeyPEM, []byte(env.platformPublicPEM), 0o600); err != nil {
		t.Fatal(err)
	}

	canonical := runPHP(t, scriptPath(t, "sign_oracle.php"), map[string]any{
		"mode": "sign", "params": body,
	})
	verified := runPHP(t, scriptPath(t, "sign_oracle.php"), map[string]any{
		"mode": "verify", "canonical": canonical["canonical"], "sign": responseSign,
		"public_key_file": platformKeyPEM,
	})
	if verified["verified"] != true {
		t.Fatalf("PHP rejected the V2 response signature: canonical=%v sign=%s", canonical["canonical"], responseSign)
	}

	// The Go verifier must agree with PHP on the same canonical string.
	platformKey, err := signing.ParseRSAPublicKeyPEM(env.platformPublicPEM)
	if err != nil {
		t.Fatal(err)
	}
	var check rsa.PublicKey = *platformKey
	_ = check
	if !signing.VerifyRSA(body, platformKey, responseSign) {
		t.Fatal("Go rejected the V2 response signature")
	}
}

// TestBrowserReturnCarriesVerifiableSignature proves the browser flow ends on
// the merchant return_url with the same signed query string the merchant SDK
// verifies (Epay/includes/functions.php:519-531, EpayCore::verifyReturn).
func TestBrowserReturnCarriesVerifiableSignature(t *testing.T) {
	phpBinary(t)
	env := newEnvironment(t)
	returnURL := "https://merchant.example/return-page"
	values := env.signedValues("RT-"+randomSuffix(t), map[string]string{
		"type": "alipay", "return_url": returnURL,
	})
	status, body := env.form("/mapi.php", values)
	if status != http.StatusOK || intOf(t, body["code"]) != 1 {
		t.Fatalf("mapi = %d %v", status, body)
	}
	tradeNo := mustString(t, body["trade_no"])

	// Before payment the browser is kept on the cashier.
	location := env.redirect("/pay/return/" + tradeNo + "/")
	if !strings.Contains(location, "cashier.php") {
		t.Fatalf("unpaid return redirect = %q, want the cashier", location)
	}
	if checkpay := env.redirect("/pay/checkpay/" + tradeNo + "/"); !strings.Contains(checkpay, "cashier.php") {
		t.Fatalf("unpaid checkpay redirect = %q, want the cashier", checkpay)
	}

	env.paySandboxOrder(tradeNo)
	location = env.redirect("/pay/return/" + tradeNo + "/")
	if !strings.HasPrefix(location, returnURL+"?") {
		t.Fatalf("paid return redirect = %q, want %s?<signed params>", location, returnURL)
	}
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{}
	for key := range parsed.Query() {
		params[key] = parsed.Query().Get(key)
	}
	if params["trade_status"] != "TRADE_SUCCESS" || params["trade_no"] != tradeNo {
		t.Fatalf("return params = %v", params)
	}
	result := env.clientProbe("verifyReturn", map[string]any{"params": params})
	if result["verified"] != true {
		t.Fatalf("merchant SDK rejected the return signature: %v", params)
	}

	// A return_url that already carries a query string gets the signed
	// parameters appended with "&" (Epay/includes/functions.php:525-530). Note
	// that the merchant's own parameter then participates in $_GET and breaks
	// EpayCore::verifyReturn — the reference implementation behaves the same way,
	// which is why the signed set is only asserted here.
	queryReturnURL := "https://merchant.example/return-page?from=shop"
	queryValues := env.signedValues("RTQ-"+randomSuffix(t), map[string]string{
		"type": "alipay", "return_url": queryReturnURL,
	})
	status, queryBody := env.form("/mapi.php", queryValues)
	if status != http.StatusOK || intOf(t, queryBody["code"]) != 1 {
		t.Fatalf("mapi with query return_url = %d %v", status, queryBody)
	}
	queryTradeNo := mustString(t, queryBody["trade_no"])
	env.paySandboxOrder(queryTradeNo)
	queryLocation := env.redirect("/pay/return/" + queryTradeNo + "/")
	if !strings.HasPrefix(queryLocation, queryReturnURL+"&") {
		t.Fatalf("return redirect = %q, want %s&<signed params>", queryLocation, queryReturnURL)
	}
	if !strings.Contains(queryLocation, "&sign=") {
		t.Fatalf("return redirect %q lost the signature", queryLocation)
	}

	// /pay/submit/{trade_no}/ is the payurl fallback upstream returns for
	// jsapi/app/scan orders (Epay/includes/lib/Payment.php:301).
	if submitLocation := env.redirect("/pay/submit/" + tradeNo + "/"); !strings.Contains(submitLocation, "/pay/return/") {
		t.Fatalf("submit redirect = %q, want the return redirect for a paid order", submitLocation)
	}
	// /pay/ok/{trade_no}/ renders the success page when no return_url is set.
	okStatus, okBody := env.do(http.MethodGet, "/pay/ok/"+tradeNo+"/", nil, "")
	if okStatus != http.StatusOK || !strings.Contains(string(okBody), tradeNo) {
		t.Fatalf("ok page = %d %s", okStatus, okBody)
	}
}

// redirect issues a GET and returns the Location header without following it.
func (env *environment) redirect(path string) string {
	env.t.Helper()
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Get(env.url + path)
	if err != nil {
		env.t.Fatalf("GET %s: %v", path, err)
	}
	defer response.Body.Close()
	location := response.Header.Get("Location")
	if response.StatusCode != http.StatusFound || location == "" {
		env.t.Fatalf("GET %s = %d, location %q", path, response.StatusCode, location)
	}
	return location
}
