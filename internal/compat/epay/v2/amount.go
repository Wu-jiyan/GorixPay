package v2

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MaxAmountCents is the largest single amount accepted by the V2 protocol:
// 100000.00 CNY.
const MaxAmountCents int64 = 100000 * 100

// ErrInvalidAmount rejects zero, negative, scientific notation and amounts with
// more than two decimal places.
var ErrInvalidAmount = errors.New("invalid amount")

var amountPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,2})?$`)

// ParseAmount converts a CNY yuan string to cents. The value must be positive,
// use at most two decimal places and must not exceed MaxAmountCents.
func ParseAmount(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if !amountPattern.MatchString(trimmed) {
		return 0, ErrInvalidAmount
	}
	parts := strings.SplitN(trimmed, ".", 2)
	units, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, ErrInvalidAmount
	}
	var cents int64
	if len(parts) == 2 {
		fraction := parts[1]
		for len(fraction) < 2 {
			fraction += "0"
		}
		cents, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, ErrInvalidAmount
		}
	}
	if units > MaxAmountCents/100 {
		return 0, ErrInvalidAmount
	}
	total := units*100 + cents
	if total <= 0 || total > MaxAmountCents {
		return 0, ErrInvalidAmount
	}
	return total, nil
}

// FormatAmount renders cents as a two decimal yuan string ("0.01").
func FormatAmount(amount int64) string {
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	return fmt.Sprintf("%s%d.%02d", sign, amount/100, amount%100)
}
