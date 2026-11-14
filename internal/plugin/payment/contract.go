package payment

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
)

var (
	ErrInvalidPlugin         = errors.New("invalid payment plugin")
	ErrDuplicatePlugin       = errors.New("duplicate payment plugin")
	ErrPluginNotFound        = errors.New("payment plugin not found")
	ErrUnsupportedCapability = errors.New("payment plugin capability is unsupported")
	ErrInvalidAction         = errors.New("invalid payment action")
	// ErrInvalidConfiguration marks a channel configuration that is missing a
	// field the plugin cannot work without.
	ErrInvalidConfiguration = errors.New("invalid channel configuration")
	// ErrCallbackRejected marks an asynchronous notification that failed
	// signature verification or carried an unusable payload.
	ErrCallbackRejected = errors.New("payment callback rejected")
)

type Capability string

const (
	CapabilityPayment  Capability = "payment"
	CapabilityQuery    Capability = "query"
	CapabilityClose    Capability = "close"
	CapabilityRefund   Capability = "refund"
	CapabilityTransfer Capability = "transfer"
)

type Metadata struct {
	Code         string
	Name         string
	Capabilities []Capability
}

type CreateRequest struct {
	TradeNo       string
	Amount        int64
	Currency      string
	Description   string
	PaymentType   string
	NotifyURL     string
	ReturnURL     string
	ClientIP      string
	Method        string
	Extra         map[string]string
	Configuration map[string]string
}

type Plugin interface {
	Metadata() Metadata
	CreatePayment(context.Context, CreateRequest) (Action, error)
}

// ConfigValidator is implemented by plugins that can reject an incomplete
// channel configuration (for example a channel without a signing key) before an
// order or a callback is attempted. Registry.ValidateConfiguration is the entry
// point the channel administration can call when it saves a channel.
type ConfigValidator interface {
	ValidateConfiguration(config map[string]string) error
}

type ActionType string

const (
	ActionJump      ActionType = "jump"
	ActionHTML      ActionType = "html"
	ActionQRCode    ActionType = "qrcode"
	ActionURLScheme ActionType = "urlscheme"
	ActionJSAPI     ActionType = "jsapi"
	ActionApp       ActionType = "app"
	ActionScan      ActionType = "scan"
	ActionWXPlugin  ActionType = "wxplugin"
	ActionWXApp     ActionType = "wxapp"
)

type Action struct {
	Type ActionType
	URL  string
	Data any
}

func (action Action) Validate() error {
	switch action.Type {
	case ActionJump:
		parsed, err := url.ParseRequestURI(action.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return ErrInvalidAction
		}
	case ActionQRCode:
		// A QR payload is not always an http URL: WeChat Pay hands out
		// weixin://wxpay/bizpayurl?pr=... and other acquirers use their own
		// schemes, so any absolute URI is accepted here.
		parsed, err := url.ParseRequestURI(action.URL)
		if err != nil || parsed.Scheme == "" {
			return ErrInvalidAction
		}
		if (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host == "" {
			return ErrInvalidAction
		}
	case ActionURLScheme:
		parsed, err := url.ParseRequestURI(action.URL)
		if err != nil || parsed.Scheme == "" {
			return ErrInvalidAction
		}
	case ActionHTML:
		if action.Data == nil || action.Data == "" {
			return ErrInvalidAction
		}
	case ActionJSAPI, ActionApp, ActionScan, ActionWXPlugin, ActionWXApp:
		if action.Data == nil {
			return ErrInvalidAction
		}
	default:
		return ErrInvalidAction
	}
	return nil
}

// ConfigField describes one key of a plugin's channel configuration so the
// administration UI can render a form without hard-coded knowledge of the
// plugin. Secret fields are never echoed back by the API.
type ConfigField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}

// Field types understood by the administration UI.
const (
	ConfigFieldText     = "text"
	ConfigFieldPassword = "password"
	ConfigFieldURL      = "url"
	ConfigFieldTextarea = "textarea"
	ConfigFieldSelect   = "select"
)

// Configurable is the optional capability of a plugin that publishes the shape
// of its channel configuration. Plugins that predate it keep working untouched.
type Configurable interface {
	ConfigFields() []ConfigField
}

// CallbackResult is the normalized outcome of an upstream notification. Amount
// is expressed in the smallest currency unit (cents) like every other amount in
// the platform, so the handler can compare it with the stored order directly.
type CallbackResult struct {
	TradeNo        string
	ChannelTradeNo string
	Amount         int64
	Paid           bool
	Raw            map[string]string
	// Ack is the exact body the upstream expects to stop retrying. Empty means
	// the generic "success" acknowledgement. Gateways with a proprietary
	// acknowledgement (Huifu wants RECV_ORD_ID_<trade_no>) set it here.
	Ack string
}

// CronHook is the optional capability upstream ticks through cron.php's plugin
// branch: every registered plugin that implements it is called periodically by
// the caller's scheduler (GorixPay does not start a ticker itself).
type CronHook interface {
	Cron(ctx context.Context) error
}

// CallbackVerifier is implemented by plugins that can authenticate and decode
// asynchronous notifications. The handler hands over the untouched request so
// each plugin keeps using its own header or form conventions; the raw body is
// still readable because the handler rewinds it.
type CallbackVerifier interface {
	VerifyCallback(ctx context.Context, r *http.Request, config map[string]string) (CallbackResult, error)
}

func validMetadata(metadata Metadata) bool {
	if !regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`).MatchString(metadata.Code) || metadata.Name == "" || len(metadata.Capabilities) == 0 {
		return false
	}
	seen := make(map[Capability]bool, len(metadata.Capabilities))
	for _, capability := range metadata.Capabilities {
		switch capability {
		case CapabilityPayment, CapabilityQuery, CapabilityClose, CapabilityRefund, CapabilityTransfer:
		default:
			return false
		}
		if seen[capability] {
			return false
		}
		seen[capability] = true
	}
	return true
}
