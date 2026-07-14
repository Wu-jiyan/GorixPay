package epay

import "testing"

func TestCanonicalContentBoundaries(t *testing.T) {
	params := map[string]any{
		"z": "last", "sign": "ignored", "empty": "", "arr": []string{"ignored"},
		"sign_type": "RSA", "A": "upper", "a": "lower", "amount": "1.00",
		"zero": 0, "space": " x ", "nil": nil, "whitespace": "   ",
	}
	want := "A=upper&a=lower&amount=1.00&space= x &z=last&zero=0"
	if got := CanonicalContent(params); got != want {
		t.Fatalf("canonical content = %q, want %q", got, want)
	}
}

func TestCanonicalContentUsesPHPScalarFormatting(t *testing.T) {
	params := map[string]any{"false": false, "true": true, "float": 1.5}
	if got, want := CanonicalContent(params), "float=1.5&true=1"; got != want {
		t.Fatalf("canonical content = %q, want %q", got, want)
	}
}

func TestMD5EmptyContentVector(t *testing.T) {
	if got, want := MD5Sign(map[string]any{}, "123"), "202cb962ac59075b964b07152d234b70"; got != want {
		t.Fatalf("MD5 signature = %q, want %q", got, want)
	}
}

func TestMD5PreservesValueWhitespace(t *testing.T) {
	params := map[string]any{"name": " 商品 ", "zero": "0"}
	if got, want := MD5Sign(params, "key"), "5ac3e59446641b8bd12477dfbf4c2fc8"; got != want {
		t.Fatalf("MD5 signature = %q, want %q", got, want)
	}
}
