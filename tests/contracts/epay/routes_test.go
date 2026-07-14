package epay

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type requestFixture struct {
	ID        string         `yaml:"id"`
	Operation string         `yaml:"operation"`
	Outcome   string         `yaml:"outcome"`
	Method    string         `yaml:"method"`
	Query     map[string]any `yaml:"query"`
	Form      map[string]any `yaml:"form"`
}

type responseFixture struct {
	ID        string `yaml:"id"`
	Operation string `yaml:"operation"`
	Outcome   string `yaml:"outcome"`
	Code      *int   `yaml:"code"`
	Format    string `yaml:"format"`
	Signed    bool   `yaml:"signed"`
}

func TestFixtureCoverageForEveryOperation(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	requests := loadYAMLList[requestFixture](t, "fixtures/requests.yaml")
	responses := loadYAMLList[responseFixture](t, "fixtures/responses.yaml")
	operations := []string{"legacy/submit", "legacy/mapi"}
	for operation := range manifest.Routes.LegacyAPI.Operations {
		operations = append(operations, "legacy/"+operation)
	}
	for operation := range manifest.Routes.V2API.Operations {
		operations = append(operations, "v2/"+operation)
	}
	for _, operation := range operations {
		assertOutcomes(t, operation, requests)
		assertOutcomes(t, operation, responses)
	}
}

func TestRouteContractBoundaries(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Routes.LegacySubmit.Path != "/submit.php" || manifest.Routes.LegacyMAPI.Path != "/mapi.php" {
		t.Fatal("legacy payment paths changed")
	}
	if got := manifest.Routes.LegacyAPI.Operations["settle"].MaxLimit; got != 50 {
		t.Fatalf("legacy settle max limit = %d", got)
	}
	if got := manifest.Routes.V2API.Operations["merchant/orders"].MaxLimit; got != 50 {
		t.Fatalf("merchant orders max limit = %d", got)
	}
	create := manifest.Routes.V2API.Operations["pay/create"]
	if create.Conditional["sub_openid"] != "required_when_method_jsapi" || create.Conditional["auth_code"] != "required_when_method_scan" {
		t.Fatal("pay/create conditional fields changed")
	}
}

func loadYAMLList[T any](t *testing.T, path string) []T {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result []T
	if err := yaml.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertOutcomes[T interface {
	requestFixture | responseFixture
}](t *testing.T, operation string, fixtures []T) {
	t.Helper()
	found := map[string]bool{}
	for _, fixture := range fixtures {
		var currentOperation, outcome string
		switch value := any(fixture).(type) {
		case requestFixture:
			currentOperation, outcome = value.Operation, value.Outcome
		case responseFixture:
			currentOperation, outcome = value.Operation, value.Outcome
		}
		if currentOperation == operation {
			found[outcome] = true
		}
	}
	if !found["success"] || !found["failure"] {
		t.Errorf("%s requires success and failure fixtures", operation)
	}
}
