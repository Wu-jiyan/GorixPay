package huifu

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

// keyMaterial bundles one RSA key pair in every shape the operator may paste.
type keyMaterial struct {
	private     *rsa.PrivateKey
	privatePEM  string
	privateBare string
	publicPEM   string
	publicBare  string
}

func newKeyMaterial(t *testing.T) keyMaterial {
	t.Helper()
	privateKey, err := signing.GenerateRSAKey()
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	privatePEM, err := signing.EncodeRSAPrivateKeyPEM(privateKey)
	if err != nil {
		t.Fatalf("encode private key: %v", err)
	}
	publicPEM, err := signing.EncodeRSAPublicKeyPEM(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("encode public key: %v", err)
	}
	return keyMaterial{
		private:     privateKey,
		privatePEM:  privatePEM,
		privateBare: bareBase64(privatePEM),
		publicPEM:   publicPEM,
		publicBare:  bareBase64(publicPEM),
	}
}

// bareBase64 strips the PEM armor and every line break, mimicking what Epay
// stores for the Huifu channel.
func bareBase64(pemValue string) string {
	lines := strings.Split(strings.TrimSpace(pemValue), "\n")
	var builder strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "-----") || strings.TrimSpace(line) == "" {
			continue
		}
		builder.WriteString(strings.TrimSpace(line))
	}
	return builder.String()
}

func TestCanonicalJSONMatchesPHPEncodingRules(t *testing.T) {
	data := map[string]json.RawMessage{
		"zeta":     rawValue("last"),
		"alpha":    rawValue("x/y"),
		"cn":       rawValue("中文<&>"),
		"quote":    rawValue(`he said "hi"\ok`),
		"empty":    rawValue(""),
		"numberly": json.RawMessage(`"0100"`),
	}
	want := `{"alpha":"x/y","cn":"中文<&>","empty":"","numberly":"0100","quote":"he said \"hi\"\\ok","zeta":"last"}`
	if got := canonicalJSON(data); got != want {
		t.Fatalf("canonical mismatch\n got=%s\nwant=%s", got, want)
	}
}

func TestCanonicalJSONKeepsNestedBytesUntouched(t *testing.T) {
	// A nested object with unsorted keys and a raw number must survive verbatim:
	// unmarshalling into map[string]any would reorder it and normalize the number.
	nested := json.RawMessage(`{"z":1,"a":"two"}`)
	data := map[string]json.RawMessage{
		"outer": nested,
		"a":     rawValue("first"),
	}
	want := `{"a":"first","outer":{"z":1,"a":"two"}}`
	if got := canonicalJSON(data); got != want {
		t.Fatalf("nested bytes changed\n got=%s\nwant=%s", got, want)
	}
}

func TestSignatureRoundTripDetectsTampering(t *testing.T) {
	keys := newKeyMaterial(t)
	data := map[string]json.RawMessage{"req_seq_id": rawValue("T1"), "trans_amt": rawValue("0.01")}

	signature, err := signData(keys.private, data)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyData(&keys.private.PublicKey, data, signature) {
		t.Fatal("signature does not verify")
	}
	data["trans_amt"] = rawValue("0.02")
	if verifyData(&keys.private.PublicKey, data, signature) {
		t.Fatal("tampered data still verifies")
	}
	if verifyData(&keys.private.PublicKey, data, "") {
		t.Fatal("empty signature accepted")
	}
}

func TestNormalizePEMAcceptsBothShapes(t *testing.T) {
	keys := newKeyMaterial(t)

	private, err := parsePrivateKeyPEM(keys.privateBare)
	if err != nil {
		t.Fatalf("bare private key rejected: %v", err)
	}
	if private.D.Cmp(keys.private.D) != 0 {
		t.Fatal("bare private key parsed into a different key")
	}
	for name, value := range map[string]string{
		"pem":          keys.privatePEM,
		"crlf-pem":     strings.ReplaceAll(keys.privatePEM, "\n", "\r\n"),
		"padded":       "  \n" + keys.privateBare + "\n  ",
		"broken-lines": wrapBase64(keys.privateBare, 40),
	} {
		t.Run("private-"+name, func(t *testing.T) {
			if _, err := parsePrivateKeyPEM(value); err != nil {
				t.Fatalf("rejected: %v", err)
			}
		})
	}
	for name, value := range map[string]string{
		"pem":      keys.publicPEM,
		"bare":     keys.publicBare,
		"crlf-pem": strings.ReplaceAll(keys.publicPEM, "\n", "\r\n"),
	} {
		t.Run("public-"+name, func(t *testing.T) {
			if _, err := parsePublicKeyPEM(value); err != nil {
				t.Fatalf("rejected: %v", err)
			}
		})
	}
	if _, err := parsePrivateKeyPEM("not a key"); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
		t.Fatalf("garbage private key error=%v", err)
	}
	if _, err := parsePublicKeyPEM(""); !errors.Is(err, pluginpayment.ErrInvalidConfiguration) {
		t.Fatalf("empty public key error=%v", err)
	}
	if normalizePEM("", "PUBLIC KEY") != "" {
		t.Fatal("empty value should normalize to empty")
	}
	if !strings.Contains(normalizePEM(keys.publicBare, "PUBLIC KEY"), "-----BEGIN PUBLIC KEY-----") {
		t.Fatal("bare key was not wrapped")
	}
}

func TestJsonObjectSortsAndKeepsSlash(t *testing.T) {
	got := jsonObject(map[string]string{"subject": "订单/A&B", "buyer_id": "2088"})
	want := `{"buyer_id":"2088","subject":"订单/A&B"}`
	if got != want {
		t.Fatalf("jsonObject=%s want=%s", got, want)
	}
}

func TestPostRejectsTamperedResponse(t *testing.T) {
	keys := newKeyMaterial(t)
	stranger := newKeyMaterial(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		var envelope requestEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !verifyData(&keys.private.PublicKey, envelope.Data, envelope.Sign) {
			t.Errorf("request signature does not verify")
		}
		responseData := map[string]json.RawMessage{"resp_code": rawValue(successCode), "qr_code": rawValue("https://qr.example/x")}
		// Signed by the wrong key: the client must refuse it.
		signature, err := signData(stranger.private, responseData)
		if err != nil {
			t.Errorf("sign response: %v", err)
		}
		body, err := marshalNoEscape(map[string]any{"data": responseData, "sign": signature})
		if err != nil {
			t.Errorf("marshal response: %v", err)
		}
		_, _ = writer.Write(body)
	}))
	defer server.Close()

	client := &Client{
		baseURL: server.URL, httpClient: server.Client(), sysID: "sys", productID: "prod",
		privateKey: keys.private, publicKey: &keys.private.PublicKey,
	}
	_, err := client.post(context.Background(), jsPayPath, map[string]json.RawMessage{"req_seq_id": rawValue("T1")})
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("error=%v", err)
	}
}

func TestPostRejectsGarbageAndEmptyResponses(t *testing.T) {
	keys := newKeyMaterial(t)
	for name, body := range map[string]string{
		"not-json":    "oops",
		"empty-data":  `{"data":{},"sign":"x"}`,
		"missing-sig": `{"data":{"resp_code":"00000100"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(body))
			}))
			defer server.Close()
			client := &Client{
				baseURL: server.URL, httpClient: server.Client(), sysID: "s", productID: "p",
				privateKey: keys.private, publicKey: &keys.private.PublicKey,
			}
			if _, err := client.post(context.Background(), jsPayPath, map[string]json.RawMessage{"a": rawValue("b")}); err == nil {
				t.Fatalf("accepted %q", body)
			}
		})
	}
}

func TestBusinessErrorMessages(t *testing.T) {
	if err := businessError(map[string]json.RawMessage{
		"resp_code": rawValue(successCode), "resp_desc": rawValue("下单成功"),
	}); err != nil {
		t.Fatalf("success code rejected: %v", err)
	}
	if err := businessError(map[string]json.RawMessage{"resp_code": rawValue(refundSuccessCode)}); err != nil {
		t.Fatalf("refund success code rejected: %v", err)
	}
	err := businessError(map[string]json.RawMessage{
		"resp_code": rawValue("00000101"), "resp_desc": rawValue("交易失败"), "bank_message": rawValue("余额不足"),
	})
	if err == nil || !strings.Contains(err.Error(), "00000101") || !strings.Contains(err.Error(), "交易失败") || !strings.Contains(err.Error(), "余额不足") {
		t.Fatalf("error=%v", err)
	}
	// bank_message already contained in resp_desc must not be duplicated.
	err = businessError(map[string]json.RawMessage{
		"resp_code": rawValue("22000001"), "resp_desc": rawValue("卡号错误"), "bank_message": rawValue("卡号错误"),
	})
	if err == nil || strings.Count(err.Error(), "卡号错误") != 1 {
		t.Fatalf("error=%v", err)
	}
}

func TestRawStringNormalizesScalars(t *testing.T) {
	data := map[string]json.RawMessage{
		"text":   rawValue("hello"),
		"null":   json.RawMessage("null"),
		"number": json.RawMessage("100"),
		"nested": json.RawMessage(`{"a":"b"}`),
	}
	if got := rawString(data, "text"); got != "hello" {
		t.Fatalf("text=%q", got)
	}
	if got := rawString(data, "null"); got != "" {
		t.Fatalf("null=%q", got)
	}
	if got := rawString(data, "number"); got != "100" {
		t.Fatalf("number=%q", got)
	}
	if got := rawString(data, "missing"); got != "" {
		t.Fatalf("missing=%q", got)
	}
}
