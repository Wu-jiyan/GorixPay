package money

import (
	"math"
	"testing"
)

func TestApplyBasisPointsMatchesExactDivision(t *testing.T) {
	cases := []struct {
		amount int64
		rateBp int
		want   int64
	}{
		{10000, 10000, 10000},
		{10000, 250, 250},
		{10001, 250, 250},
		{9999, 100, 99},
		{1, 10000, 1},
		{1, 5000, 0},
		{12345, 3333, 4114},
		{0, 500, 0},
		{500, 0, 0},
		{500, -10, 0},
		// A rate above 100% is clamped, never applied.
		{10000, 20000, 10000},
	}
	for _, testCase := range cases {
		if got := ApplyBasisPoints(testCase.amount, testCase.rateBp); got != testCase.want {
			t.Fatalf("ApplyBasisPoints(%d, %d) = %d, want %d", testCase.amount, testCase.rateBp, got, testCase.want)
		}
	}
}

// TestApplyBasisPointsDoesNotOverflow pins the exact bug the audit reproduced:
// amount*rate/10000 wrapped to 0 for a 100% rate on a very large amount.
func TestApplyBasisPointsDoesNotOverflow(t *testing.T) {
	huge := int64(math.MaxInt64 / 100)
	if got := ApplyBasisPoints(huge, 10000); got != huge {
		t.Fatalf("ApplyBasisPoints(%d, 10000) = %d, want %d", huge, got, huge)
	}
	if got := ApplyBasisPoints(huge, 5000); got != huge/2 {
		t.Fatalf("ApplyBasisPoints(%d, 5000) = %d, want %d", huge, got, huge/2)
	}
	if got := ApplyBasisPoints(math.MaxInt64, 10000); got != math.MaxInt64 {
		t.Fatalf("ApplyBasisPoints(MaxInt64, 10000) = %d, want MaxInt64", got)
	}
	// Whatever the input, a payout can never exceed the amount or go negative.
	amounts := []int64{1, 999, 10000, 123456789, math.MaxInt64 - 1, math.MaxInt64}
	rates := []int{0, 1, 250, 5000, 9999, 10000, 10001, 1 << 20}
	for _, amount := range amounts {
		for _, rate := range rates {
			got := ApplyBasisPoints(amount, rate)
			if got < 0 || got > amount {
				t.Fatalf("ApplyBasisPoints(%d, %d) = %d, outside [0, amount]", amount, rate, got)
			}
		}
	}
}
