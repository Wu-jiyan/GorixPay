package payment

import (
	"context"
	"errors"
	"testing"
)

// validatingPlugin implements ConfigValidator and records what it was asked.
type validatingPlugin struct {
	failure error
	seen    map[string]string
}

func (plugin *validatingPlugin) Metadata() Metadata {
	return Metadata{Code: "validating", Name: "validating", Capabilities: []Capability{CapabilityPayment}}
}

func (*validatingPlugin) CreatePayment(context.Context, CreateRequest) (Action, error) {
	return Action{Type: ActionJump, URL: "https://pay.example/validating"}, nil
}

func (plugin *validatingPlugin) ValidateConfiguration(config map[string]string) error {
	plugin.seen = config
	return plugin.failure
}

func TestRegistryValidateConfiguration(t *testing.T) {
	registry := NewRegistry()
	validating := &validatingPlugin{}
	if err := registry.Register(validating); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(plainPlugin{code: "plain"}); err != nil {
		t.Fatal(err)
	}

	config := map[string]string{"key": "channel-key"}
	if err := registry.ValidateConfiguration("validating", config); err != nil {
		t.Fatalf("valid configuration rejected: %v", err)
	}
	if validating.seen["key"] != "channel-key" {
		t.Fatalf("plugin did not receive the configuration: %+v", validating.seen)
	}

	// Plugins without the capability accept anything, so the call stays optional.
	if err := registry.ValidateConfiguration("plain", map[string]string{}); err != nil {
		t.Fatalf("capability-free plugin rejected a configuration: %v", err)
	}

	validating.failure = ErrInvalidConfiguration
	if err := registry.ValidateConfiguration("validating", config); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("error=%v", err)
	}
	if err := registry.ValidateConfiguration("unknown", config); !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("unknown plugin error=%v", err)
	}
}
