// Package sandbox implements the local sandbox payment channel used to exercise
// the full payment loop (submit -> cashier -> sandbox pay -> callback -> notify)
// without an upstream acquirer. It is only registered when the sandbox is
// enabled and the runtime is not production.
package sandbox

import (
	"context"
	"errors"
	"net/url"
	"strings"

	pluginpayment "gorixpay/internal/plugin/payment"
)

var (
	ErrMissingTradeNo = errors.New("sandbox payment requires a trade number")
	ErrMissingSiteURL = errors.New("sandbox payment requires an absolute site URL")
)

const sandboxPath = "/pay/sandbox/"

type Plugin struct{}

func New() *Plugin {
	return &Plugin{}
}

func (*Plugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{
		Code:         "sandbox",
		Name:         "沙箱支付",
		Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment},
	}
}

// CreatePayment returns a jump action to the sandbox confirmation page. The
// public site URL is injected by the orchestrator through CreateRequest.Extra;
// when it is absent the notify URL's scheme and host are used as a fallback so
// the action is always absolute.
func (*Plugin) CreatePayment(_ context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	tradeNo := strings.TrimSpace(request.TradeNo)
	if tradeNo == "" {
		return pluginpayment.Action{}, ErrMissingTradeNo
	}
	base, err := siteBaseURL(request)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	action := pluginpayment.Action{
		Type: pluginpayment.ActionJump,
		URL:  base + sandboxPath + url.PathEscape(tradeNo),
	}
	if err := action.Validate(); err != nil {
		return pluginpayment.Action{}, err
	}
	return action, nil
}

func siteBaseURL(request pluginpayment.CreateRequest) (string, error) {
	if candidate, ok := parseBaseURL(request.Extra["site_url"], true); ok {
		return candidate, nil
	}
	if candidate, ok := parseBaseURL(request.NotifyURL, false); ok {
		return candidate, nil
	}
	return "", ErrMissingSiteURL
}

// parseBaseURL validates an absolute HTTP(S) URL and returns its base. The
// configured site URL keeps its path prefix; the notify URL fallback only needs
// scheme and host, matching the documented degradation rule.
func parseBaseURL(raw string, keepPath bool) (string, bool) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", false
	}
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false
	}
	if !keepPath {
		return parsed.Scheme + "://" + parsed.Host, true
	}
	return parsed.Scheme + "://" + parsed.Host + strings.TrimRight(parsed.Path, "/"), true
}
