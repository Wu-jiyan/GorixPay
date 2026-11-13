package huifu

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"gorixpay/internal/compat/epay/signing"
	pluginpayment "gorixpay/internal/plugin/payment"
)

const (
	// DefaultBaseURL is the production Huifu Douggong gateway.
	DefaultBaseURL = "https://api.huifu.com"
	// jsPayPath is the aggregated (jspay) order endpoint.
	jsPayPath = "/v3/trade/payment/jspay"
	// successCode marks a successful business response.
	successCode = "00000100"
	// refundSuccessCode is the alternative success code some products return.
	refundSuccessCode = "00000000"
	// maxResponseBytes bounds an upstream response body.
	maxResponseBytes = 1 << 20
)

// Client speaks the Huifu Douggong protocol: JSON envelope, signature over a
// PHP-compatible canonical rendering of data, RSA-SHA256 PKCS#1 v1.5.
type Client struct {
	baseURL    string
	httpClient *http.Client
	sysID      string
	productID  string
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

type requestEnvelope struct {
	SysID     string                     `json:"sys_id"`
	ProductID string                     `json:"product_id"`
	Data      map[string]json.RawMessage `json:"data"`
	Sign      string                     `json:"sign"`
}

type responseEnvelope struct {
	Data map[string]json.RawMessage `json:"data"`
	Sign string                     `json:"sign"`
}

// newClient parses the channel configuration into a client. Keys are parsed
// eagerly so a misconfigured channel fails before any network call.
func newClient(config map[string]string, client *http.Client) (*Client, error) {
	resolved, err := pluginpayment.RequireConfig(config, "sys_id", "product_id", "private_key", "huifu_public_key")
	if err != nil {
		return nil, err
	}
	privateKey, err := parsePrivateKeyPEM(resolved["private_key"])
	if err != nil {
		return nil, err
	}
	publicKey, err := parsePublicKeyPEM(resolved["huifu_public_key"])
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL:    pluginpayment.ConfigValue(resolved, "api_url", DefaultBaseURL),
		httpClient: pluginpayment.OrDefaultClient(client),
		sysID:      resolved["sys_id"],
		productID:  resolved["product_id"],
		privateKey: privateKey,
		publicKey:  publicKey,
	}, nil
}

// pemBlockWidth is the line width PEM bodies conventionally use.
const pemBlockWidth = 64

// normalizePEM accepts both shapes channel operators paste in: a complete PEM
// block, or the bare base64 body Epay stores (1628 chars for a PKCS#8 private
// key, 392 for an SPKI public key). Bare bodies are re-wrapped every 64 columns
// and given the matching BEGIN/END lines.
func normalizePEM(value, blockType string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "BEGIN") {
		// Keep the operator's block, only normalizing line endings.
		return strings.ReplaceAll(trimmed, "\r\n", "\n")
	}
	compact := stripWhitespace(trimmed)
	if compact == "" {
		return ""
	}
	wrapped := wrapBase64(compact, pemBlockWidth)
	return "-----BEGIN " + blockType + "-----\n" + wrapped + "\n-----END " + blockType + "-----\n"
}

// stripWhitespace removes every space and line break from a base64 body.
func stripWhitespace(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		switch char {
		case ' ', '\t', '\r', '\n':
			continue
		}
		builder.WriteRune(char)
	}
	return builder.String()
}

// wrapBase64 breaks a base64 body into fixed width lines.
func wrapBase64(value string, width int) string {
	if width <= 0 {
		return value
	}
	var builder strings.Builder
	builder.Grow(len(value) + len(value)/width + 1)
	for index := 0; index < len(value); index += width {
		end := index + width
		if end > len(value) {
			end = len(value)
		}
		if index > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(value[index:end])
	}
	return builder.String()
}

// parsePrivateKeyPEM parses a configured merchant private key in either shape.
func parsePrivateKeyPEM(value string) (*rsa.PrivateKey, error) {
	key, err := signing.ParseRSAPrivateKeyPEM(normalizePEM(value, "PRIVATE KEY"))
	if err != nil {
		return nil, fmt.Errorf("%w: private_key", pluginpayment.ErrInvalidConfiguration)
	}
	return key, nil
}

// parsePublicKeyPEM parses a configured Huifu public key in either shape.
func parsePublicKeyPEM(value string) (*rsa.PublicKey, error) {
	key, err := signing.ParseRSAPublicKeyPEM(normalizePEM(value, "PUBLIC KEY"))
	if err != nil {
		return nil, fmt.Errorf("%w: huifu_public_key", pluginpayment.ErrInvalidConfiguration)
	}
	return key, nil
}

// canonicalJSON renders data the way PHP json_encode(JSON_UNESCAPED_SLASHES|
// JSON_UNESCAPED_UNICODE) renders a ksort-ed array: keys in byte order, no HTML
// escaping, no slash escaping, nested values kept as their original bytes.
func canonicalJSON(data map[string]json.RawMessage) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(jsonString(key))
		builder.WriteByte(':')
		builder.Write(data[key])
	}
	builder.WriteByte('}')
	return builder.String()
}

// jsonString renders a Go string as a JSON string literal without the HTML
// escaping json.Marshal applies by default (<, >, & must stay literal).
func jsonString(value string) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		// Encoding a Go string cannot fail; fall back to the quoted literal.
		return strconv.Quote(value)
	}
	return strings.TrimRight(buffer.String(), "\n")
}

// jsonObject renders a nested JSON object string. Huifu expects these as
// double-encoded strings (alipay_data, wx_data, risk_check_data, ...).
func jsonObject(fields map[string]string) string {
	raw := make(map[string]json.RawMessage, len(fields))
	for name, value := range fields {
		raw[name] = json.RawMessage(jsonString(value))
	}
	return canonicalJSON(raw)
}

// marshalNoEscape encodes the request envelope without HTML escaping so the
// bytes on the wire match the bytes that were signed.
func marshalNoEscape(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

// signCanonical produces the base64 RSA-SHA256 signature of a signed string.
func signCanonical(privateKey *rsa.PrivateKey, canonical string) (string, error) {
	if privateKey == nil {
		return "", errors.New("missing huifu private key")
	}
	digest := sha256.Sum256([]byte(canonical))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

// verifyCanonical checks a base64 RSA-SHA256 signature over an exact string.
func verifyCanonical(publicKey *rsa.PublicKey, canonical, signature string) bool {
	if publicKey == nil {
		return false
	}
	trimmed := strings.TrimSpace(signature)
	if trimmed == "" {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return false
	}
	digest := sha256.Sum256([]byte(canonical))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], decoded) == nil
}

// verifyData verifies a signature over the canonical rendering of data.
func verifyData(publicKey *rsa.PublicKey, data map[string]json.RawMessage, signature string) bool {
	return verifyCanonical(publicKey, canonicalJSON(data), signature)
}

// signData signs the canonical rendering of data.
func signData(privateKey *rsa.PrivateKey, data map[string]json.RawMessage) (string, error) {
	return signCanonical(privateKey, canonicalJSON(data))
}

// rawString returns a field as a Go string. JSON null and non-string scalars are
// normalized instead of failing the whole notification.
func rawString(data map[string]json.RawMessage, key string) string {
	raw, exists := data[key]
	if !exists {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "null" || trimmed == "" {
		return ""
	}
	return strings.Trim(trimmed, `"`)
}

// post sends one signed request and returns the verified data object.
func (client *Client) post(ctx context.Context, path string, data map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	signature, err := signData(client.privateKey, data)
	if err != nil {
		return nil, fmt.Errorf("sign huifu request: %w", err)
	}
	body, err := marshalNoEscape(requestEnvelope{
		SysID: client.sysID, ProductID: client.productID, Data: data, Sign: signature,
	})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(client.baseURL, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call huifu gateway: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read huifu response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return nil, errors.New("huifu response exceeds limit")
	}
	var decoded responseEnvelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("invalid huifu response (status %d): %w", response.StatusCode, err)
	}
	if len(decoded.Data) == 0 {
		return nil, fmt.Errorf("huifu response carries no data (status %d)", response.StatusCode)
	}
	if !verifyData(client.publicKey, decoded.Data, decoded.Sign) {
		return nil, errors.New("huifu response signature verification failed")
	}
	return decoded.Data, nil
}

// businessError maps resp_code/resp_desc onto a Go error, appending bank_message
// when the gateway supplied one.
func businessError(data map[string]json.RawMessage) error {
	code := rawString(data, "resp_code")
	if code == successCode || code == refundSuccessCode {
		return nil
	}
	description := rawString(data, "resp_desc")
	if bank := rawString(data, "bank_message"); bank != "" && !strings.Contains(description, bank) {
		description = strings.TrimSpace(description + " " + bank)
	}
	if description == "" {
		description = "unknown error"
	}
	return fmt.Errorf("huifu rejected the request: %s %s", code, description)
}
