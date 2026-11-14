package security

import (
	"strings"
	"testing"
)

func TestNewCaptchaDrawsFourAlphabetCharacters(t *testing.T) {
	generated, err := NewCaptcha()
	if err != nil {
		t.Fatal(err)
	}
	if len(generated.Code) != captchaLength {
		t.Fatalf("code=%q length=%d", generated.Code, len(generated.Code))
	}
	for _, char := range generated.Code {
		if !strings.ContainsRune(captchaAlphabet, char) {
			t.Fatalf("character %q is outside the alphabet", char)
		}
	}
	if strings.ContainsAny(generated.Code, "IO01") {
		t.Fatalf("ambiguous characters in %q", generated.Code)
	}
	if !strings.HasPrefix(generated.SVG, "<svg") || !strings.HasSuffix(generated.SVG, "</svg>") {
		t.Fatalf("svg=%q", generated.SVG[:min(len(generated.SVG), 40)])
	}
}

func TestNewCaptchaSVGPaintsGlyphsWithoutTextNodes(t *testing.T) {
	first, err := NewCaptcha()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCaptcha()
	if err != nil {
		t.Fatal(err)
	}
	if first.SVG == second.SVG {
		t.Fatal("two captcha images are identical")
	}
	// The answer must not be recoverable from the markup: no text nodes, and
	// none of the code's characters may appear as element content.
	if strings.Contains(first.SVG, "<text") {
		t.Fatal("svg uses <text> elements; the code would be readable by a script")
	}
	for _, char := range first.Code {
		if strings.Contains(first.SVG, ">"+string(char)+"<") {
			t.Fatalf("svg leaks glyph %q as text content", char)
		}
	}
	if !strings.Contains(first.SVG, "<rect ") {
		t.Fatal("svg paints no glyph pixels")
	}
	if !strings.Contains(first.SVG, "<line ") {
		t.Fatal("svg has no noise lines")
	}
	if strings.Count(first.SVG, "rotate(") != captchaLength {
		t.Fatalf("expected one rotation per character, got %d", strings.Count(first.SVG, "rotate("))
	}
}

func TestRandomIntStaysInRange(t *testing.T) {
	for index := 0; index < 50; index++ {
		value, err := randomInt(7)
		if err != nil {
			t.Fatal(err)
		}
		if value < 0 || value >= 7 {
			t.Fatalf("value=%d out of range", value)
		}
	}
	if value, err := randomInt(0); err != nil || value != 0 {
		t.Fatalf("zero limit value=%d err=%v", value, err)
	}
}
