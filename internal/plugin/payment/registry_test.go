package payment

import (
	"context"
	"errors"
	"testing"
)

type fakePlugin struct {
	metadata Metadata
	action   Action
}

func (plugin fakePlugin) Metadata() Metadata { return plugin.metadata }

func (plugin fakePlugin) CreatePayment(context.Context, CreateRequest) (Action, error) {
	return plugin.action, nil
}

func TestRegistryRegistersAndResolvesPlugin(t *testing.T) {
	registry := NewRegistry()
	plugin := fakePlugin{metadata: Metadata{
		Code: "mock", Name: "Mock", Capabilities: []Capability{CapabilityPayment, CapabilityRefund},
	}}
	if err := registry.Register(plugin); err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.Resolve("mock", CapabilityPayment)
	if err != nil || resolved.Metadata().Code != "mock" {
		t.Fatalf("resolve: %+v err=%v", resolved, err)
	}
	if _, err := registry.Resolve("mock", CapabilityTransfer); !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("unsupported capability error=%v", err)
	}
	if err := registry.Register(plugin); !errors.Is(err, ErrDuplicatePlugin) {
		t.Fatalf("duplicate register error=%v", err)
	}
}

func TestRegistryRejectsInvalidMetadata(t *testing.T) {
	registry := NewRegistry()
	for _, metadata := range []Metadata{
		{},
		{Code: "Mock Invalid", Name: "Mock", Capabilities: []Capability{CapabilityPayment}},
		{Code: "mock", Name: "Mock"},
		{Code: "mock", Name: "Mock", Capabilities: []Capability{"unknown"}},
	} {
		if err := registry.Register(fakePlugin{metadata: metadata}); !errors.Is(err, ErrInvalidPlugin) {
			t.Fatalf("metadata %+v error=%v", metadata, err)
		}
	}
}

func TestActionValidation(t *testing.T) {
	valid := []Action{
		{Type: ActionJump, URL: "https://pay.example/redirect"},
		{Type: ActionHTML, Data: "<form></form>"},
		{Type: ActionQRCode, URL: "https://pay.example/code"},
		{Type: ActionURLScheme, URL: "weixin://pay/test"},
		{Type: ActionJSAPI, Data: map[string]any{"appId": "test"}},
		{Type: ActionApp, Data: map[string]any{"orderInfo": "test"}},
		{Type: ActionScan, Data: map[string]any{"status": "success"}},
		{Type: ActionWXPlugin, Data: map[string]any{"path": "test"}},
		{Type: ActionWXApp, Data: map[string]any{"prepay_id": "test"}},
	}
	for _, action := range valid {
		if err := action.Validate(); err != nil {
			t.Errorf("action %s: %v", action.Type, err)
		}
	}
	for _, action := range []Action{{}, {Type: ActionJump}, {Type: ActionHTML}, {Type: "unknown", URL: "https://pay.example"}} {
		if err := action.Validate(); !errors.Is(err, ErrInvalidAction) {
			t.Errorf("invalid action %+v error=%v", action, err)
		}
	}
}
