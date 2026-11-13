package epay

import (
	"errors"
	"testing"
)

func TestParseMoneyCents(t *testing.T) {
	cases := []struct {
		value string
		cents int64
		err   error
	}{
		{value: "135.79", cents: 13579},
		{value: "20.50", cents: 2050},
		{value: "20.5", cents: 2050},
		{value: "20", cents: 2000},
		{value: "0.00", cents: 0},
		{value: "0", cents: 0},
		{value: "  7.10  ", cents: 710},
		{value: "+1.20", cents: 120},
		{value: "1.", cents: 100},
		{value: ".5", err: ErrMoneySyntax},
		{value: "", err: ErrMoneySyntax},
		{value: "1e3", err: ErrMoneySyntax},
		{value: "12.3.4", err: ErrMoneySyntax},
		{value: "1,000.00", err: ErrMoneySyntax},
		{value: "abc", err: ErrMoneySyntax},
		{value: "1.005", err: ErrMoneyPrecision},
		{value: "1.999", err: ErrMoneyPrecision},
		{value: "-1.00", cents: -100, err: ErrMoneyNegative},
		{value: "-0.00", cents: 0},
		{value: "99999999999999999999.00", err: ErrMoneyOverflow},
	}
	for _, testCase := range cases {
		cents, err := ParseMoneyCents(testCase.value)
		if testCase.err == nil {
			if err != nil {
				t.Fatalf("ParseMoneyCents(%q) error = %v, want nil", testCase.value, err)
			}
		} else if !errors.Is(err, testCase.err) {
			t.Fatalf("ParseMoneyCents(%q) error = %v, want %v", testCase.value, err, testCase.err)
		}
		if testCase.err != ErrMoneySyntax && testCase.err != ErrMoneyOverflow && cents != testCase.cents {
			t.Fatalf("ParseMoneyCents(%q) = %d, want %d", testCase.value, cents, testCase.cents)
		}
	}
}

func TestFormatCents(t *testing.T) {
	cases := map[int64]string{
		0:     "0.00",
		1:     "0.01",
		100:   "1.00",
		13579: "135.79",
		-2050: "-20.50",
	}
	for cents, want := range cases {
		if got := FormatCents(cents); got != want {
			t.Fatalf("FormatCents(%d) = %q, want %q", cents, got, want)
		}
	}
}

func TestMoneyReason(t *testing.T) {
	cases := map[error]string{
		ErrMoneyNegative:  "negative_amount",
		ErrMoneyPrecision: "amount_precision",
		ErrMoneyOverflow:  "amount_overflow",
		ErrMoneySyntax:    "amount_syntax",
	}
	for err, want := range cases {
		if got := moneyReason(err); got != want {
			t.Fatalf("moneyReason(%v) = %q, want %q", err, got, want)
		}
	}
}
