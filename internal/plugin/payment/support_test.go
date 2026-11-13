package payment

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatAmount(t *testing.T) {
	for value, want := range map[int64]string{0: "0.00", 5: "0.05", 100: "1.00", 1234: "12.34", -1: "-0.01"} {
		if got := FormatAmount(value); got != want {
			t.Errorf("FormatAmount(%d) = %q, want %q", value, got, want)
		}
	}
}

func TestParseAmount(t *testing.T) {
	for value, want := range map[string]int64{"12.34": 1234, "0.01": 1, "1": 100, "12.345": 1235, " 7.5 ": 750} {
		got, err := ParseAmount(value)
		if err != nil || got != want {
			t.Errorf("ParseAmount(%q) = %d, %v; want %d", value, got, err, want)
		}
	}
	for _, value := range []string{"", "0.00", "abc", "12.", "-1.00", "1e3", ".5"} {
		if _, err := ParseAmount(value); !errors.Is(err, ErrCallbackRejected) {
			t.Errorf("ParseAmount(%q) error = %v", value, err)
		}
	}
}

func TestParseCentsAmountAcceptsNumberAndDecimal(t *testing.T) {
	for value, want := range map[string]int64{"1234": 1234, "12.34": 1234, " 600 ": 600} {
		got, err := ParseCentsAmount(value)
		if err != nil || got != want {
			t.Errorf("ParseCentsAmount(%q) = %d, %v; want %d", value, got, err, want)
		}
	}
	for _, value := range []string{"", "0", "-5"} {
		if _, err := ParseCentsAmount(value); err == nil {
			t.Errorf("ParseCentsAmount(%q) accepted", value)
		}
	}
}

func TestRequireConfigTrimsAndRejectsBlankKeys(t *testing.T) {
	trimmed, err := RequireConfig(map[string]string{"a": "  value  "}, "a")
	if err != nil || trimmed["a"] != "value" {
		t.Fatalf("trimmed=%+v err=%v", trimmed, err)
	}
	if _, err := RequireConfig(map[string]string{"a": "   "}, "a"); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("blank value error=%v", err)
	}
	if _, err := RequireConfig(nil, "b"); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("missing key error=%v", err)
	}
}

func TestReadUpstreamBodyEnforcesLimit(t *testing.T) {
	if _, err := ReadUpstreamBody(strings.NewReader(strings.Repeat("x", MaxUpstreamResponseBytes+1))); err == nil {
		t.Fatal("oversized body accepted")
	}
	raw, err := ReadUpstreamBody(strings.NewReader("ok"))
	if err != nil || string(raw) != "ok" {
		t.Fatalf("raw=%q err=%v", raw, err)
	}
}

func TestConfigValueFallsBackOnBlank(t *testing.T) {
	config := map[string]string{"gateway_url": "  ", "app_id": " https://example.test "}
	if got := ConfigValue(config, "gateway_url", "default"); got != "default" {
		t.Fatalf("blank fallback = %q", got)
	}
	if got := ConfigValue(config, "app_id", "default"); got != "https://example.test" {
		t.Fatalf("trimmed value = %q", got)
	}
}
