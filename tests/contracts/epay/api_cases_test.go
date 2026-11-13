package epay

import (
	"strings"
	"testing"
)

type apiCase struct {
	ID         string `yaml:"id"`
	Operation  string `yaml:"operation"`
	Outcome    string `yaml:"outcome"`
	ExpectCode int    `yaml:"expect_code"`
}

// TestAPICaseFixturesCoverEveryOperation mirrors the fixture coverage test used
// for requests.yaml/responses.yaml, but for the api.php (V1) and /api/:class/:method
// (V2) surfaces.
func TestAPICaseFixturesCoverEveryOperation(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cases := loadYAMLList[apiCase](t, "fixtures/api_cases.yaml")
	known := map[string]bool{"legacy/submit": true, "legacy/mapi": true}
	for operation := range manifest.Routes.LegacyAPI.Operations {
		known["legacy/"+operation] = true
	}
	for operation := range manifest.Routes.V2API.Operations {
		known["v2/"+operation] = true
	}
	outcomes := map[string]map[string]bool{}
	for _, testCase := range cases {
		if !known[testCase.Operation] {
			t.Errorf("fixture %s references unknown operation %s", testCase.ID, testCase.Operation)
			continue
		}
		if outcomes[testCase.Operation] == nil {
			outcomes[testCase.Operation] = map[string]bool{}
		}
		outcomes[testCase.Operation][testCase.Outcome] = true
		if strings.HasPrefix(testCase.Operation, "v2/") && testCase.Outcome == "success" && testCase.ExpectCode != 0 && testCase.ExpectCode != 302 {
			t.Errorf("fixture %s: V2 success codes are 0 or 302, got %d", testCase.ID, testCase.ExpectCode)
		}
		if strings.HasPrefix(testCase.Operation, "v2/") && testCase.Outcome == "failure" && testCase.ExpectCode >= 0 {
			t.Errorf("fixture %s: V2 failures use negative codes, got %d", testCase.ID, testCase.ExpectCode)
		}
	}
	for operation := range known {
		if !outcomes[operation]["success"] || !outcomes[operation]["failure"] {
			t.Errorf("%s requires success and failure cases", operation)
		}
	}
}

// TestV2OperationsDeclareEnvelopeAndStatusMapping keeps the V2 success code,
// pagination cap and numeric order status mapping shared by V1 order queries
// and V2 pay/query.
func TestV2OperationsDeclareEnvelopeAndStatusMapping(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	operations := manifest.Routes.V2API.Operations
	if operations["pay/create"].SuccessCode != 0 {
		t.Fatalf("pay/create success code = %d", operations["pay/create"].SuccessCode)
	}
	for _, operation := range []string{"pay/query", "pay/close", "pay/refund", "pay/refundquery", "merchant/info", "merchant/orders", "transfer/submit", "transfer/query", "transfer/proof", "transfer/balance"} {
		if _, ok := operations[operation]; !ok {
			t.Errorf("V2 operation %s missing from manifest", operation)
		}
	}
	if got := operations["merchant/orders"].MaxLimit; got != 50 {
		t.Fatalf("merchant/orders max limit = %d", got)
	}
	status := manifest.OrderStatus
	if status[0] != "unpaid" || status[1] != "paid" || status[2] != "refunded" {
		t.Fatalf("order status contract changed: %v", status)
	}
}
