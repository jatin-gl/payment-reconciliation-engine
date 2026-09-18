package money

import (
	"math"
	"testing"
)

func TestParseMinor(t *testing.T) {
	tests := []struct {
		in       string
		currency string
		want     int64
		wantErr  bool
	}{
		// 2-decimal (USD).
		{"105.00", "USD", 10500, false},
		{"105", "USD", 10500, false},
		{"0.01", "USD", 1, false},
		{"0.1", "USD", 10, false},
		{"-4.50", "USD", -450, false},
		{"+12.34", "USD", 1234, false},
		{"105.", "USD", 10500, false},
		{"1000000.99", "USD", 100000099, false},
		{"", "USD", 0, true},
		{"abc", "USD", 0, true},
		{"1.234", "USD", 0, true},                 // more than 2 decimals must error, not round
		{"1.2.3", "USD", 0, true},                 // Cut splits on the first '.', so "2.3" trips the >2-decimals check
		{"1e3", "USD", 0, true},                   // scientific notation rejected
		{"100000000000000000.00", "USD", 0, true}, // overflows int64 -> must error, never wrap
		{"92233720368547758.08", "USD", 0, true},  // just past MaxInt64 minor units
		// 0-decimal (JPY): the value is unchanged; a significant fractional digit is an error.
		{"5000", "JPY", 5000, false},
		{"105.00", "JPY", 105, false}, // trailing zeros tolerated
		{"105.5", "JPY", 0, true},     // significant fractional digit for a 0-decimal currency
		// 3-decimal (BHD).
		{"1.234", "BHD", 1234, false},
		{"1.5", "BHD", 1500, false}, // right-padded to 3 digits
		{"1.2345", "BHD", 0, true},  // too many decimals for a 3-decimal currency
	}
	for _, tt := range tests {
		got, err := ParseMinor(tt.in, tt.currency)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseMinor(%q, %q) = %d, want error", tt.in, tt.currency, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMinor(%q, %q) unexpected error: %v", tt.in, tt.currency, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMinor(%q, %q) = %d, want %d", tt.in, tt.currency, got, tt.want)
		}
	}
}

func TestAddSubCurrencyGuard(t *testing.T) {
	usd := New(10000, "USD")
	eur := New(5000, "EUR")

	if _, err := usd.Add(eur); err == nil {
		t.Fatal("Add across currencies should error")
	}
	if _, err := usd.Sub(eur); err == nil {
		t.Fatal("Sub across currencies should error")
	}

	sum, err := usd.Add(New(500, "usd")) // lower-case currency should normalize
	if err != nil {
		t.Fatalf("Add same currency: %v", err)
	}
	if sum.Amount() != 10500 || sum.Currency() != "USD" {
		t.Fatalf("Add = %v, want 105.00 USD", sum)
	}
}

func TestAbsAndEqual(t *testing.T) {
	neg := New(-450, "USD")
	if got := neg.Abs(); got.Amount() != 450 {
		t.Errorf("Abs = %d, want 450", got.Amount())
	}
	if !New(100, "USD").Equal(New(100, "USD")) {
		t.Error("equal amounts should be Equal")
	}
	if New(100, "USD").Equal(New(100, "EUR")) {
		t.Error("same amount, different currency should not be Equal")
	}
}

func TestString(t *testing.T) {
	tests := map[Money]string{
		New(10500, "USD"): "105.00 USD",
		New(-450, "EUR"):  "-4.50 EUR",
		New(5, "USD"):     "0.05 USD",
		New(0, "INR"):     "0.00 INR",
		// Zero-decimal currency: minor units are whole units.
		New(100, "JPY"):  "100 JPY",
		New(5000, "KRW"): "5000 KRW",
		// Three-decimal currency.
		New(1234, "BHD"): "1.234 BHD",
		New(-1, "BHD"):   "-0.001 BHD",
	}
	for m, want := range tests {
		if got := m.String(); got != want {
			t.Errorf("%d %s .String() = %q, want %q", m.Amount(), m.Currency(), got, want)
		}
	}
}

func TestExponent(t *testing.T) {
	cases := map[string]int{"USD": 2, "eur": 2, "JPY": 0, "KRW": 0, "BHD": 3, "ZZZ": 2}
	for ccy, want := range cases {
		if got := Exponent(ccy); got != want {
			t.Errorf("Exponent(%q) = %d, want %d", ccy, got, want)
		}
	}
}

func TestAddSubOverflow(t *testing.T) {
	max := New(math.MaxInt64, "USD")
	if _, err := max.Add(New(1, "USD")); err == nil {
		t.Error("Add overflow should error")
	}
	min := New(math.MinInt64, "USD")
	if _, err := min.Sub(New(1, "USD")); err == nil {
		t.Error("Sub overflow should error")
	}
	// A normal add near zero must still succeed.
	if _, err := New(100, "USD").Add(New(200, "USD")); err != nil {
		t.Errorf("normal Add should not error: %v", err)
	}
}
