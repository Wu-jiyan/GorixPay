package epay

import (
	"strings"
	"testing"
)

type notificationFixture struct {
	ID           string   `yaml:"id"`
	Version      string   `yaml:"version"`
	SignType     string   `yaml:"sign_type"`
	Fields       []string `yaml:"fields"`
	ResponseBody string   `yaml:"response_body"`
	HTTPStatus   int      `yaml:"http_status"`
	Accepted     bool     `yaml:"accepted"`
	RetryMinutes []int    `yaml:"retry_minutes"`
}

type paymentActionFixture struct {
	ID       string `yaml:"id"`
	Action   string `yaml:"action"`
	Category string `yaml:"category"`
}

func TestNotificationContracts(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := loadYAMLList[notificationFixture](t, "fixtures/notifications.yaml")
	accepted := map[string]bool{}
	rejectedOK := false
	rejectedEmpty200 := false
	versions := map[string]bool{}
	for _, fixture := range fixtures {
		versions[fixture.Version] = true
		if fixture.Accepted {
			accepted[fixture.ResponseBody] = true
		}
		if fixture.ResponseBody == "ok" && !fixture.Accepted {
			rejectedOK = true
		}
		if fixture.ResponseBody == "" && fixture.HTTPStatus == 200 && !fixture.Accepted {
			rejectedEmpty200 = true
		}
	}
	for _, body := range manifest.Notifications.SuccessBodyContainsAny {
		if !accepted[body] {
			t.Errorf("missing accepted notification body %q", body)
		}
	}
	if !versions["v0"] || !versions["v1"] || !rejectedOK || !rejectedEmpty200 {
		t.Fatal("notification fixtures do not cover versions and response boundaries")
	}
	if got := manifest.Notifications.Retry.ApproximateMinutes; !equalInts(got, []int{1, 3, 19, 55, 115}) {
		t.Fatalf("retry minutes = %v", got)
	}
}

func TestPaymentActionCoverage(t *testing.T) {
	fixtures := loadYAMLList[paymentActionFixture](t, "fixtures/payment_actions.yaml")
	found := map[string]bool{}
	categories := map[string]bool{}
	for _, fixture := range fixtures {
		found[fixture.Action] = true
		categories[fixture.Category] = true
	}
	for _, action := range []string{"jump", "html", "qrcode", "urlscheme", "jsapi", "app", "scan", "wxplugin", "wxapp"} {
		if !found[action] {
			t.Errorf("missing payment action %s", action)
		}
	}
	for _, category := range []string{"success_fail", "ok_fail", "xml", "json"} {
		if !categories[category] {
			t.Errorf("missing plugin acknowledgement category %s", category)
		}
	}
}

func notificationAccepted(body string) bool {
	return strings.Contains(body, "success") || strings.Contains(body, "SUCCESS") || strings.Contains(body, "Success")
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
