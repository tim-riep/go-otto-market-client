package ottomarket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
)

// Amount is a monetary amount in cents.
//
// The OTTO Market API currently only allows the currency EUR, so amounts
// have at most two decimal places and are stored exactly as an integer
// number of cents instead of a floating point number. In JSON an Amount is
// a number with two decimal places, e.g. 12.34.
type Amount int64

// Cents returns a as a number of cents.
func (a Amount) Cents() int64 { return int64(a) }

// AmountFromCents returns the amount of the given number of cents.
func AmountFromCents(cents int64) Amount { return Amount(cents) }

// ParseAmount parses a decimal number such as "12.34", "-0.5" or "7".
// It fails if the value has more than two significant decimal places.
func ParseAmount(s string) (Amount, error) {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, fmt.Errorf("ottomarket: invalid amount %q", s)
	}
	r.Mul(r, big.NewRat(100, 1))
	if !r.IsInt() {
		return 0, fmt.Errorf("ottomarket: amount %q has more than two decimal places", s)
	}
	n := r.Num()
	if !n.IsInt64() {
		return 0, fmt.Errorf("ottomarket: amount %q out of range", s)
	}
	return Amount(n.Int64()), nil
}

// String formats a with two decimal places, e.g. "12.34" or "-0.50".
func (a Amount) String() string {
	c := int64(a)
	sign := ""
	var u uint64
	if c < 0 {
		sign = "-"
		u = uint64(-(c + 1)) + 1 // safe for math.MinInt64
	} else {
		u = uint64(c)
	}
	return fmt.Sprintf("%s%d.%02d", sign, u/100, u%100)
}

// Float64 returns a in euros as a floating point number, e.g. for display.
// Do not use it for calculations.
func (a Amount) Float64() float64 { return float64(a) / 100 }

// MarshalJSON implements json.Marshaler.
func (a Amount) MarshalJSON() ([]byte, error) { return []byte(a.String()), nil }

// UnmarshalJSON implements json.Unmarshaler. It accepts a JSON number
// with at most two significant decimal places.
func (a *Amount) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	if !json.Valid(b) || len(b) == 0 || b[0] == '"' || b[0] == '{' || b[0] == '[' {
		return fmt.Errorf("ottomarket: amount must be a JSON number, got %s", b)
	}
	v, err := ParseAmount(string(b))
	if err != nil {
		return err
	}
	*a = v
	return nil
}
