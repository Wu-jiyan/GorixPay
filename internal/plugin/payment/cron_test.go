package payment

import (
	"context"
	"errors"
	"testing"
)

// cronPlugin implements both Plugin and CronHook.
type cronPlugin struct {
	code    string
	calls   int
	failure error
	panicOn bool
}

func (plugin *cronPlugin) Metadata() Metadata {
	return Metadata{Code: plugin.code, Name: plugin.code, Capabilities: []Capability{CapabilityPayment}}
}

func (*cronPlugin) CreatePayment(context.Context, CreateRequest) (Action, error) {
	return Action{Type: ActionJump, URL: "https://pay.example/cron"}, nil
}

func (plugin *cronPlugin) Cron(context.Context) error {
	plugin.calls++
	if plugin.panicOn {
		panic("plugin exploded")
	}
	return plugin.failure
}

// plainPlugin implements only Plugin, like the chat channels.
type plainPlugin struct {
	code string
}

func (plugin plainPlugin) Metadata() Metadata {
	return Metadata{Code: plugin.code, Name: plugin.code, Capabilities: []Capability{CapabilityPayment}}
}

func (plainPlugin) CreatePayment(context.Context, CreateRequest) (Action, error) {
	return Action{Type: ActionJump, URL: "https://pay.example/plain"}, nil
}

func TestCronHooksOnlyReturnsImplementationsInRegistrationOrder(t *testing.T) {
	registry := NewRegistry()
	first := &cronPlugin{code: "alpha"}
	second := &cronPlugin{code: "beta"}
	for _, plugin := range []Plugin{first, plainPlugin{code: "gamma"}, second} {
		if err := registry.Register(plugin); err != nil {
			t.Fatalf("register %s: %v", plugin.Metadata().Code, err)
		}
	}
	hooks := registry.CronHooks()
	if len(hooks) != 2 {
		t.Fatalf("hooks=%d", len(hooks))
	}
	if hooks[0] != CronHook(first) || hooks[1] != CronHook(second) {
		t.Fatalf("hooks are not in registration order: %T %T", hooks[0], hooks[1])
	}
}

func TestCronHooksOnEmptyAndHooklessRegistry(t *testing.T) {
	empty := NewRegistry().CronHooks()
	if empty == nil {
		t.Fatal("empty registry returned a nil slice")
	}
	if len(empty) != 0 {
		t.Fatalf("empty registry hooks=%d", len(empty))
	}
	registry := NewRegistry()
	if err := registry.Register(plainPlugin{code: "plain"}); err != nil {
		t.Fatal(err)
	}
	if hooks := registry.CronHooks(); len(hooks) != 0 {
		t.Fatalf("hookless registry hooks=%d", len(hooks))
	}
}

func TestRunCronHooksCollectsFailuresAndRecoversPanics(t *testing.T) {
	ok := &cronPlugin{code: "ok"}
	broken := &cronPlugin{code: "broken", failure: errors.New("channel unreachable")}
	exploding := &cronPlugin{code: "exploding", panicOn: true}
	failures := RunCronHooks(context.Background(), []CronHook{ok, broken, nil, exploding})
	if len(failures) != 2 {
		t.Fatalf("failures=%v", failures)
	}
	if !errors.Is(failures[0], broken.failure) {
		t.Fatalf("first failure=%v", failures[0])
	}
	if failures[1] == nil {
		t.Fatal("panic was not converted into an error")
	}
	// Every hook ran: the panic did not stop the loop or crash the caller.
	if ok.calls != 1 || broken.calls != 1 || exploding.calls != 1 {
		t.Fatalf("calls=%d/%d/%d", ok.calls, broken.calls, exploding.calls)
	}
	if len(RunCronHooks(context.Background(), nil)) != 0 {
		t.Fatal("empty hook list produced failures")
	}
}

func TestRegistryStillRejectsDuplicateCodesAfterOrderTracking(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(&cronPlugin{code: "dup"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(&cronPlugin{code: "dup"}); !errors.Is(err, ErrDuplicatePlugin) {
		t.Fatalf("duplicate error=%v", err)
	}
	if hooks := registry.CronHooks(); len(hooks) != 1 {
		t.Fatalf("hooks=%d", len(hooks))
	}
}
