// Package money holds the arithmetic shared by the fee, rebate and profit
// sharing code. Amounts are always integer fen (cents).
package money

// BasisPoints is the denominator of every rate in this system: 10000bp = 100%.
const BasisPoints int64 = 10000

// ApplyBasisPoints applies a basis-point rate to an amount, rounding down to the
// fen, without ever overflowing.
//
// The obvious amount*rate/10000 wraps around int64 for amounts above roughly
// 9.2e14 fen, which is how a 100% rate on a huge amount came out as 0 (or as a
// negative payout). Splitting the amount into whole thousands and a remainder
// keeps every intermediate product below 1e8 while giving the exact same floor
// division result.
func ApplyBasisPoints(amount int64, rateBp int) int64 {
	if amount <= 0 || rateBp <= 0 {
		return 0
	}
	rate := int64(rateBp)
	if rate > BasisPoints {
		rate = BasisPoints
	}
	return amount/BasisPoints*rate + amount%BasisPoints*rate/BasisPoints
}
