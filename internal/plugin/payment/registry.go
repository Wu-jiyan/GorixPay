package payment

import "sync"

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
