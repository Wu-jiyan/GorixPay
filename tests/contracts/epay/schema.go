package epay

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	Version                  int                 `yaml:"version"`
	SourceRoot               string              `yaml:"source_root"`
	CompatibilityPolicy      string              `yaml:"compatibility_policy"`
	Signing                  Signing             `yaml:"signing"`
	Routes                   Routes              `yaml:"routes"`
	Notifications            Notifications       `yaml:"notifications"`
	PaymentEntry             PaymentEntry        `yaml:"payment_entry"`
	OrderStatus              map[int]string      `yaml:"order_status"`
	CompatibilityQuirks      []string            `yaml:"compatibility_quirks"`
	CompatibilityDisposition map[string][]string `yaml:"compatibility_disposition"`
	sources                  []SourceReference
}

type Signing struct {
	Canonicalization Canonicalization `yaml:"canonicalization"`
	MD5              MD5Signing       `yaml:"md5"`
	RSA              RSASigning       `yaml:"rsa"`
	Vectors          []SigningVector  `yaml:"vectors"`
}

type Canonicalization struct {
	Sort                    string   `yaml:"sort"`
	Separator               string   `yaml:"separator"`
	ExcludeKeys             []string `yaml:"exclude_keys"`
	ExcludeValues           []string `yaml:"exclude_values"`
	PreserveValueWhitespace bool     `yaml:"preserve_value_whitespace"`
}

type MD5Signing struct {
	Formula                    string `yaml:"formula"`
	DefaultWhenSignTypeMissing bool   `yaml:"default_when_sign_type_missing"`
}

type RSASigning struct {
	Algorithm string `yaml:"algorithm"`
	Encoding  string `yaml:"encoding"`
	KeySize   int    `yaml:"key_size"`
}

type SigningVector struct {
	ID          string         `yaml:"id"`
	MerchantKey string         `yaml:"merchant_key"`
	Params      map[string]any `yaml:"params"`
	SignContent string         `yaml:"sign_content"`
	Signature   string         `yaml:"signature"`
}

type Routes struct {
	LegacySubmit RouteGroup `yaml:"legacy_submit"`
	LegacyMAPI   RouteGroup `yaml:"legacy_mapi"`
	LegacyAPI    APIGroup   `yaml:"legacy_api"`
	V2API        APIGroup   `yaml:"v2_api"`
}

type RouteGroup struct {
	Path        string            `yaml:"path"`
	Methods     []string          `yaml:"methods"`
	Required    []string          `yaml:"required"`
	Optional    []string          `yaml:"optional"`
	Conditional map[string]string `yaml:"conditional"`
	Codes       map[string]int    `yaml:"codes"`
	Success     map[string]any    `yaml:"success"`
}

type APIGroup struct {
	Path        string                  `yaml:"path"`
	PathPattern string                  `yaml:"path_pattern"`
	Operations  map[string]APIOperation `yaml:"operations"`
}

type APIOperation struct {
	Method      string            `yaml:"method"`
	Methods     []string          `yaml:"methods"`
	Required    []string          `yaml:"required"`
	Optional    []string          `yaml:"optional"`
	Conditional map[string]string `yaml:"conditional"`
	MaxLimit    int               `yaml:"max_limit"`
	SuccessCode int               `yaml:"success_code"`
}

type Notifications struct {
	MerchantTransport      string              `yaml:"merchant_transport"`
	SuccessBodyContainsAny []string            `yaml:"success_body_contains_any"`
	V0                     NotificationVersion `yaml:"v0"`
	V1                     NotificationVersion `yaml:"v1"`
	Retry                  NotificationRetry   `yaml:"retry"`
}

type NotificationVersion struct {
	SignType string   `yaml:"sign_type"`
	Fields   []string `yaml:"fields"`
}

type NotificationRetry struct {
	ApproximateMinutes []int `yaml:"approximate_minutes_after_initial_failure"`
	TerminalValue      int   `yaml:"terminal_notify_value"`
	BatchSize          int   `yaml:"batch_size"`
}

type PaymentEntry struct {
	PathPattern   string   `yaml:"path_pattern"`
	CommonActions []string `yaml:"common_actions"`
}

type SourceReference struct {
	Raw  string
	Path string
	Line int
}

var sourcePattern = regexp.MustCompile(`^([^:]+):(\d+)$`)

func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	manifest.sources, err = collectSources(&root)
	if err != nil {
		return nil, err
	}
	if err := manifest.validate(); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (m *Manifest) Sources() []SourceReference {
	return append([]SourceReference(nil), m.sources...)
}

func (m *Manifest) validate() error {
	if m.Version <= 0 || m.SourceRoot == "" || m.CompatibilityPolicy == "" {
		return fmt.Errorf("manifest header fields are required")
	}
	if m.Routes.LegacySubmit.Path == "" || m.Routes.LegacyMAPI.Path == "" || len(m.Routes.LegacyAPI.Operations) == 0 || len(m.Routes.V2API.Operations) == 0 {
		return fmt.Errorf("route entrypoints are required")
	}
	if len(m.Notifications.V0.Fields) == 0 || len(m.Notifications.V1.Fields) == 0 || len(m.CompatibilityQuirks) == 0 || len(m.CompatibilityDisposition) == 0 {
		return fmt.Errorf("notification and compatibility contracts are required")
	}
	if len(m.sources) == 0 {
		return fmt.Errorf("source references are required")
	}
	return nil
}

func collectSources(node *yaml.Node) ([]SourceReference, error) {
	var result []SourceReference
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Value == "source" {
				values := value.Content
				if value.Kind == yaml.ScalarNode {
					values = []*yaml.Node{value}
				}
				for _, item := range values {
					match := sourcePattern.FindStringSubmatch(item.Value)
					if match == nil || filepath.IsAbs(match[1]) || strings.Contains(filepath.ToSlash(match[1]), "../") {
						return nil, fmt.Errorf("invalid source reference %q", item.Value)
					}
					line, _ := strconv.Atoi(match[2])
					result = append(result, SourceReference{Raw: item.Value, Path: filepath.FromSlash(match[1]), Line: line})
				}
			}
			children, err := collectSources(value)
			if err != nil {
				return nil, err
			}
			result = append(result, children...)
		}
	} else {
		for _, child := range node.Content {
			children, err := collectSources(child)
			if err != nil {
				return nil, err
			}
			result = append(result, children...)
		}
	}
	return result, nil
}
