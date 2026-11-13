package plugins

import (
	"os"

	"gopkg.in/yaml.v3"
)

// ConfigField is one declared configuration key of a plugin.
type ConfigField struct {
	Key      string `yaml:"key"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Secret   bool   `yaml:"secret"`
}

// Plugin describes the contract a shipped plugin must honour.
type Plugin struct {
	Code             string            `yaml:"code"`
	Name             string            `yaml:"name"`
	Capabilities     []string          `yaml:"capabilities"`
	CallbackVerifier bool              `yaml:"callback_verifier"`
	Upstream         map[string]string `yaml:"upstream"`
	ActionTypes      []string          `yaml:"action_types"`
	ConfigFields     []ConfigField     `yaml:"config_fields"`
	AmountUnit       string            `yaml:"amount_unit"`
}

// AdminRoutes names the administration endpoints the manifest pins down.
type AdminRoutes struct {
	Plugins     string `yaml:"plugins"`
	ChannelTest string `yaml:"channel_test"`
}

// Manifest is the whole contract file.
type Manifest struct {
	Version       int         `yaml:"version"`
	CallbackRoute string      `yaml:"callback_route"`
	AdminRoutes   AdminRoutes `yaml:"admin_routes"`
	Plugins       []Plugin    `yaml:"plugins"`
}

// LoadManifest reads and parses the contract manifest.
func LoadManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Find returns the declaration of a plugin code.
func (manifest Manifest) Find(code string) (Plugin, bool) {
	for _, plugin := range manifest.Plugins {
		if plugin.Code == code {
			return plugin, true
		}
	}
	return Plugin{}, false
}
