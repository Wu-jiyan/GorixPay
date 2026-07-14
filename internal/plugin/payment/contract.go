package payment

import (
	"context"
	"errors"
	"net/url"
	"regexp"
)

var (
	ErrInvalidPlugin         = errors.New("invalid payment plugin")
	ErrDuplicatePlugin       = errors.New("duplicate payment plugin")
	ErrPluginNotFound        = errors.New("payment plugin not found")
	ErrUnsupportedCapability = errors.New("payment plugin capability is unsupported")
	ErrInvalidAction         = errors.New("invalid payment action")
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
	case ActionJump, ActionQRCode:
		parsed, err := url.ParseRequestURI(action.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
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
