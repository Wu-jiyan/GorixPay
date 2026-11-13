package payment

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MaxUpstreamResponseBytes caps how much of an upstream response body a plugin
// buffers. Anything larger is treated as a misbehaving gateway.
const MaxUpstreamResponseBytes = 64 * 1024

// DefaultUpstreamTimeout is used when the caller passes no HTTP client to a
// plugin constructor. It keeps a hung gateway from pinning a request forever.
const DefaultUpstreamTimeout = 15 * time.Second

var amountPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// FormatAmount renders cents as a plain decimal string ("1234" -> "12.34"),
// the representation every upstream expects for money values.
func FormatAmount(amount int64) string {
	negative := amount < 0
	if negative {
		amount = -amount
	}
	text := strconv.FormatInt(amount/100, 10) + fmt.Sprintf(".%02d", amount%100)
	if negative {
		return "-" + text
	}
	return text
}

// ParseAmount converts a decimal amount string into cents. Fractional digits
// beyond two are rounded half up, which matches how the epay callback handler
// treats upstream money values.
func ParseAmount(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if !amountPattern.MatchString(trimmed) {
		return 0, fmt.Errorf("%w: invalid amount %q", ErrCallbackRejected, value)
	}
	rational, ok := new(big.Rat).SetString(trimmed)
	if !ok || rational.Sign() <= 0 {
		return 0, fmt.Errorf("%w: invalid amount %q", ErrCallbackRejected, value)
	}
	scaled := new(big.Rat).Mul(rational, big.NewRat(100, 1))
	quotient, remainder := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
	if new(big.Int).Lsh(remainder, 1).Cmp(scaled.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() || quotient.Sign() <= 0 {
		return 0, fmt.Errorf("%w: invalid amount %q", ErrCallbackRejected, value)
	}
	return quotient.Int64(), nil
}

// ParseCentsAmount converts an integer cents amount, accepting both a JSON
// number and its decimal string rendering.
func ParseCentsAmount(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("%w: missing amount", ErrCallbackRejected)
	}
	if parsed, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		if parsed <= 0 {
			return 0, fmt.Errorf("%w: non-positive amount %q", ErrCallbackRejected, value)
		}
		return parsed, nil
	}
	return ParseAmount(trimmed)
}

// MissingConfig reports an absent or blank required configuration key.
func MissingConfig(key string) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfiguration, key)
}

// RequireConfig returns a copy of config with the listed keys trimmed and
// verified non-empty. Values in the returned map are safe to use as-is.
func RequireConfig(config map[string]string, keys ...string) (map[string]string, error) {
	trimmed := make(map[string]string, len(config))
	for name, value := range config {
		trimmed[name] = strings.TrimSpace(value)
	}
	for _, key := range keys {
		if trimmed[key] == "" {
			return nil, MissingConfig(key)
		}
	}
	return trimmed, nil
}

// ConfigValue returns a trimmed configuration value, falling back when blank.
func ConfigValue(config map[string]string, key, fallback string) string {
	if value := strings.TrimSpace(config[key]); value != "" {
		return value
	}
	return fallback
}

// ReadUpstreamBody reads an upstream response with the shared size cap.
func ReadUpstreamBody(body io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, MaxUpstreamResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read upstream response: %w", err)
	}
	if len(raw) > MaxUpstreamResponseBytes {
		return nil, errors.New("upstream response exceeds limit")
	}
	return raw, nil
}

// NewHTTPClient builds the default upstream client used by plugin constructors.
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: DefaultUpstreamTimeout}
}

// OrDefaultClient substitutes the shared default client for a nil one.
func OrDefaultClient(client *http.Client) *http.Client {
	if client == nil {
		return NewHTTPClient()
	}
	return client
}

// StringMap converts a flat form or query map into the map[string]string shape
// used by callback results.
func StringMap(values map[string][]string) map[string]string {
	converted := make(map[string]string, len(values))
	for name, entries := range values {
		if len(entries) == 0 {
			converted[name] = ""
			continue
		}
		converted[name] = entries[0]
	}
	return converted
}

// RoundedTimestamp renders seconds-since-epoch, the format used by the WeChat
// Pay v3 Authorization header.
func RoundedTimestamp(at time.Time) string {
	return strconv.FormatInt(at.Unix(), 10)
}
