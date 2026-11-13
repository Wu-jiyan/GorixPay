package epay

import (
	"errors"
	"strconv"
	"strings"
)

// Money parsing errors. They are wrapped with the offending value so reports can
// show which row is broken, while callers still classify the problem with
// errors.Is.
var (
	ErrMoneySyntax    = errors.New("invalid decimal amount")
	ErrMoneyPrecision = errors.New("amount has more than two decimal places")
	ErrMoneyNegative  = errors.New("amount is negative")
	ErrMoneyOverflow  = errors.New("amount is out of range")
)

// maxMoneyUnits bounds the integer part so units*100 cannot overflow int64.
const maxMoneyUnits = (1 << 62) / 100

// ParseMoneyCents converts an Epay decimal string ("135.79") into integer cents.
//
// Epay stores money in MySQL DECIMAL(10,2); the driver hands those columns to
// database/sql as strings, so the migration never goes through float64 and no
// rounding can happen behind our back. Parsing is strict on purpose: a value
// that is not a plain decimal (scientific notation, thousands separators, empty)
// is a data problem the operator has to see, not something to coerce.
//
// When the value is negative the returned cents are still the parsed value, so
// callers can report the magnitude; the error tells them it must not be migrated.
func ParseMoneyCents(value string) (int64, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return 0, ErrMoneySyntax
	}
	negative := false
	switch raw[0] {
	case '-':
		negative = true
		raw = raw[1:]
	case '+':
		raw = raw[1:]
	}
	if raw == "" {
		return 0, ErrMoneySyntax
	}
	unitsPart := raw
	fractionPart := ""
	if index := strings.IndexByte(raw, '.'); index >= 0 {
		unitsPart = raw[:index]
		fractionPart = raw[index+1:]
		if strings.ContainsRune(fractionPart, '.') {
			return 0, ErrMoneySyntax
		}
	}
	if unitsPart == "" || !allDigits(unitsPart) || !allDigits(fractionPart) {
		return 0, ErrMoneySyntax
	}
	if len(fractionPart) > 2 {
		return 0, ErrMoneyPrecision
	}
	for len(fractionPart) < 2 {
		fractionPart += "0"
	}
	units, err := strconv.ParseInt(unitsPart, 10, 64)
	if err != nil || units > maxMoneyUnits {
		return 0, ErrMoneyOverflow
	}
	fraction, err := strconv.ParseInt(fractionPart, 10, 64)
	if err != nil {
		return 0, ErrMoneyOverflow
	}
	cents := units*100 + fraction
	if negative {
		if cents == 0 {
			return 0, nil
		}
		return -cents, ErrMoneyNegative
	}
	return cents, nil
}

// FormatCents renders integer cents the way Epay writes them, for report output.
func FormatCents(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return sign + strconv.FormatInt(cents/100, 10) + "." + leftPad2(cents%100)
}

func allDigits(value string) bool {
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func leftPad2(value int64) string {
	text := strconv.FormatInt(value, 10)
	if len(text) >= 2 {
		return text
	}
	return "0" + text
}

// moneyReason maps a ParseMoneyCents error onto the report reason code.
func moneyReason(err error) string {
	switch {
	case errors.Is(err, ErrMoneyNegative):
		return "negative_amount"
	case errors.Is(err, ErrMoneyPrecision):
		return "amount_precision"
	case errors.Is(err, ErrMoneyOverflow):
		return "amount_overflow"
	default:
		return "amount_syntax"
	}
}
