// Package money provides a currency-safe monetary value type backed by integer
// minor units (e.g. cents). Floating-point math is never used for money in this
// codebase: representing 0.1 + 0.2 in float64 yields 0.30000000000000004, which
// is unacceptable for reconciliation where a single-cent drift is a real finding.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Money is an immutable monetary amount in the minor unit of a currency
// (cents for USD/EUR, paise for INR, yen has no minor unit so Amount is whole).
// The zero value is a valid amount of 0 with an empty currency; use New to
// construct amounts that must carry a currency.
type Money struct {
	// Amount is the value in minor units. 10500 with currency "USD" is $105.00.
	amount int64
	// Currency is the ISO-4217 code, upper-cased. Empty is treated as "unknown"
	// and only compares equal to another empty currency.
	currency string
}

// New returns a Money value of amount minor units in the given ISO-4217 currency.
func New(amount int64, currency string) Money {
	return Money{amount: amount, currency: strings.ToUpper(strings.TrimSpace(currency))}
}

// Zero returns a zero amount in the given currency.
func Zero(currency string) Money { return New(0, currency) }

// Amount returns the value in minor units.
func (m Money) Amount() int64 { return m.amount }

// Currency returns the ISO-4217 currency code.
func (m Money) Currency() string { return m.currency }

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m.amount == 0 }

// SameCurrency reports whether two amounts share a currency.
func (m Money) SameCurrency(other Money) bool { return m.currency == other.currency }

// Add returns m+other. It returns an error if the currencies differ, because
// adding across currencies is a programming error, not a value to silently coerce.
func (m Money) Add(other Money) (Money, error) {
	if !m.SameCurrency(other) {
		return Money{}, fmt.Errorf("money: cannot add %s and %s", m.currency, other.currency)
	}
	return Money{amount: m.amount + other.amount, currency: m.currency}, nil
}

// Sub returns m-other, erroring on currency mismatch.
func (m Money) Sub(other Money) (Money, error) {
	if !m.SameCurrency(other) {
		return Money{}, fmt.Errorf("money: cannot subtract %s from %s", other.currency, m.currency)
	}
	return Money{amount: m.amount - other.amount, currency: m.currency}, nil
}

// Abs returns the absolute value.
func (m Money) Abs() Money {
	if m.amount < 0 {
		return Money{amount: -m.amount, currency: m.currency}
	}
	return m
}

// Equal reports whether two amounts are exactly equal in both value and currency.
func (m Money) Equal(other Money) bool {
	return m.amount == other.amount && m.currency == other.currency
}

// String renders the amount with two implied decimal places for the common case.
// It is a display helper, not a settlement format; downstream systems should read
// Amount() and Currency() directly.
func (m Money) String() string {
	sign := ""
	a := m.amount
	if a < 0 {
		sign = "-"
		a = -a
	}
	major := a / 100
	minor := a % 100
	return fmt.Sprintf("%s%d.%02d %s", sign, major, minor, m.currency)
}

// ParseMinor parses a decimal string like "105.00" into minor units. It supports
// an optional sign and up to two fractional digits. It intentionally rejects
// scientific notation and more than two decimals so that malformed settlement
// rows surface as errors instead of silently rounding.
func ParseMinor(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("money: empty amount")
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	major, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money: invalid amount %q: %w", s, err)
	}
	var minor int64
	if hasFrac {
		switch len(fracPart) {
		case 0:
			// "105." — treat as zero fractional part.
		case 1:
			d, err := strconv.ParseInt(fracPart, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("money: invalid fractional part %q: %w", fracPart, err)
			}
			minor = d * 10
		case 2:
			d, err := strconv.ParseInt(fracPart, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("money: invalid fractional part %q: %w", fracPart, err)
			}
			minor = d
		default:
			return 0, fmt.Errorf("money: amount %q has more than 2 decimal places", s)
		}
	}
	total := major*100 + minor
	if neg {
		total = -total
	}
	return total, nil
}
