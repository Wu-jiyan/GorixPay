package netguard

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestValidateURLRejectsInternalTargets(t *testing.T) {
	blockedTargets := []string{
		"http://127.0.0.1:8080/notify",
		"http://127.1.2.3/notify",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/notify",
		"http://0.0.0.0:8080/notify",
		"http://localhost/notify",
		"http://pay.localhost/notify",
		"http://10.0.0.5/notify",
		"http://192.168.1.10/notify",
		"http://172.16.4.4/notify",
		"http://100.64.0.9/notify",
		"http://user:pass@merchant.example/notify",
		"javascript:alert(1)",
		"file:///etc/passwd",
		"notify.example/path",
	}
	for _, target := range blockedTargets {
		t.Run(target, func(t *testing.T) {
			parsed, err := url.Parse(target)
			if err != nil {
				t.Fatalf("test target %q does not parse: %v", target, err)
			}
			if err := ValidateURL(parsed); !errors.Is(err, ErrBlockedTarget) {
				t.Fatalf("ValidateURL(%q) = %v, want %v", target, err, ErrBlockedTarget)
			}
		})
	}
}

func TestValidateURLAllowsPublicTargets(t *testing.T) {
	allowed := []string{
		"https://merchant.example/notify",
		"http://merchant.example:8080/notify?x=1",
		"https://8.8.8.8/notify",
		"https://shop.example.com/pay/return",
	}
	for _, target := range allowed {
		t.Run(target, func(t *testing.T) {
			parsed, err := url.Parse(target)
			if err != nil {
				t.Fatalf("test target %q does not parse: %v", target, err)
			}
			if err := ValidateURL(parsed); err != nil {
				t.Fatalf("ValidateURL(%q) = %v, want nil", target, err)
			}
		})
	}
}

// TestValidateURLWithAllowPrivateRelaxesInternalRangesOnly pins the rule that
// makes the switch safe to hand to operators: a same-host or LAN receiver
// becomes reachable, and the metadata/link-local classes stay refused.
func TestValidateURLWithAllowPrivateRelaxesInternalRangesOnly(t *testing.T) {
	cases := []struct {
		target       string
		allowPrivate bool
		wantBlocked  bool
	}{
		{"http://192.168.1.10/notify", true, false},
		{"http://10.1.2.3/notify", true, false},
		{"http://127.0.0.1:8081/notify", true, false},
		{"http://localhost/notify", true, false},
		{"http://192.168.1.10/notify", false, true},
		{"http://127.0.0.1/notify", false, true},
		{"http://169.254.169.254/latest/meta-data/", true, true},
		{"http://[fe80::1]/notify", true, true},
		{"http://100.64.0.9/notify", true, true},
	}
	for _, testCase := range cases {
		parsed, err := url.Parse(testCase.target)
		if err != nil {
			t.Fatalf("test target %q does not parse: %v", testCase.target, err)
		}
		err = ValidateURLWith(parsed, testCase.allowPrivate)
		if gotBlocked := errors.Is(err, ErrBlockedTarget); gotBlocked != testCase.wantBlocked {
			t.Fatalf("ValidateURLWith(%q, allowPrivate=%v) = %v, wantBlocked=%v", testCase.target, testCase.allowPrivate, err, testCase.wantBlocked)
		}
	}
}

func TestBlockedClassification(t *testing.T) {
	cases := []struct {
		address      string
		allowPrivate bool
		want         bool
	}{
		{"127.0.0.1", false, true},
		{"127.0.0.1", true, false},
		{"::1", true, false},
		{"0.0.0.0", false, true},
		{"0.0.0.0", true, true},
		{"169.254.169.254", true, true},
		{"10.0.0.1", false, true},
		{"10.0.0.1", true, false},
		{"172.16.0.1", true, false},
		{"192.168.0.1", true, false},
		{"100.64.0.1", true, true},
		{"198.18.0.1", true, true},
		{"fd00::1", false, true},
		{"fd00::1", true, false},
		{"93.184.216.34", false, false},
	}
	for _, testCase := range cases {
		ip := net.ParseIP(testCase.address)
		if ip == nil {
			t.Fatalf("%q does not parse", testCase.address)
		}
		if got := blocked(ip, testCase.allowPrivate); got != testCase.want {
			t.Fatalf("blocked(%s, allowPrivate=%v) = %v, want %v", testCase.address, testCase.allowPrivate, got, testCase.want)
		}
	}
}

// TestControlRejectsLoopbackDial exercises the exact hook net/http calls before
// it connects, which is also what catches a public hostname that resolves to an
// internal address.
func TestControlRejectsLoopbackDial(t *testing.T) {
	if err := control(false)("tcp", "127.0.0.1:8080", nil); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("loopback dial error = %v, want %v", err, ErrBlockedTarget)
	}
	if err := control(false)("tcp", "[::1]:8080", nil); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("ipv6 loopback dial error = %v, want blocked", err)
	}
	if err := control(true)("tcp", "[::1]:8080", nil); err != nil {
		t.Fatalf("allowPrivate loopback dial error = %v, want nil", err)
	}
	if err := control(true)("tcp", "169.254.169.254:80", nil); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("allowPrivate metadata dial error = %v, want blocked", err)
	}
	if err := control(false)("tcp", "10.1.2.3:80", nil); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("private dial error = %v, want blocked", err)
	}
	if err := control(true)("tcp", "10.1.2.3:80", nil); err != nil {
		t.Fatalf("allowPrivate private dial error = %v, want nil", err)
	}
	if err := control(false)("tcp", "93.184.216.34:443", nil); err != nil {
		t.Fatalf("public dial error = %v, want nil", err)
	}
}

func TestClientRefusesToConnectToLoopback(t *testing.T) {
	var reached bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		reached = true
		_, _ = writer.Write([]byte("success"))
	}))
	defer server.Close()
	client := Client(Options{Timeout: 2 * time.Second})
	response, err := client.Get(server.URL + "/notify")
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("guarded client reached a loopback server")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("error = %v, want a blocked-target error", err)
	}
	if reached {
		t.Fatal("loopback server received the request")
	}
}

func TestClientSetsTimeoutAndRedirectLimit(t *testing.T) {
	client := Client(Options{})
	if client.Timeout != DefaultTimeout {
		t.Fatalf("default timeout = %v, want %v", client.Timeout, DefaultTimeout)
	}
	custom := Client(Options{Timeout: 3 * time.Second})
	if custom.Timeout != 3*time.Second {
		t.Fatalf("custom timeout = %v", custom.Timeout)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/next", nil)
	if err := client.CheckRedirect(request, make([]*http.Request, maxRedirects)); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("redirect limit error = %v, want %v", err, ErrBlockedTarget)
	}
}
