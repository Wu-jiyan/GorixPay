package payment

import (
	"sort"
	"sync"
)

type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

func NewRegistry() *Registry {
	return &Registry{plugins: make(map[string]Plugin)}
}

func (registry *Registry) Register(plugin Plugin) error {
	if plugin == nil || !validMetadata(plugin.Metadata()) {
		return ErrInvalidPlugin
	}
	metadata := plugin.Metadata()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.plugins[metadata.Code]; exists {
		return ErrDuplicatePlugin
	}
	registry.plugins[metadata.Code] = plugin
	return nil
}

func (registry *Registry) Resolve(code string, capability Capability) (Plugin, error) {
	registry.mu.RLock()
	plugin, exists := registry.plugins[code]
	registry.mu.RUnlock()
	if !exists {
		return nil, ErrPluginNotFound
	}
	for _, current := range plugin.Metadata().Capabilities {
		if current == capability {
			return plugin, nil
		}
	}
	return nil, ErrUnsupportedCapability
}

// Lookup returns a registered plugin regardless of capability. Callers that
// need a capability-specific plugin must keep using Resolve.
func (registry *Registry) Lookup(code string) (Plugin, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	plugin, exists := registry.plugins[code]
	return plugin, exists
}

// List returns the metadata of every registered plugin ordered by code so the
// administration API and the contract tests see a stable sequence.
func (registry *Registry) List() []Metadata {
	registry.mu.RLock()
	items := make([]Metadata, 0, len(registry.plugins))
	for _, plugin := range registry.plugins {
		metadata := plugin.Metadata()
		metadata.Capabilities = append([]Capability{}, metadata.Capabilities...)
		items = append(items, metadata)
	}
	registry.mu.RUnlock()
	sort.Slice(items, func(left, right int) bool { return items[left].Code < items[right].Code })
	return items
}

// ConfigFields returns the published configuration schema of a plugin. A plugin
// that does not implement Configurable, or an unknown code, yields nil.
func (registry *Registry) ConfigFields(code string) []ConfigField {
	plugin, exists := registry.Lookup(code)
	if !exists {
		return nil
	}
	configurable, ok := plugin.(Configurable)
	if !ok {
		return nil
	}
	return configurable.ConfigFields()
}
