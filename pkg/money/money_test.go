package money

import "testing"

func TestParseMinor(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"105.00", 10500, false},
		{"105", 10500, false},
		{"0.01", 1, false},
		{"0.1", 10, false},
		{"-4.50", -450, false},
		{"+12.34", 1234, false},
		{"105.", 10500, false},
		{"1000000.99", 100000099, false},
		{"", 0, true},
		{"abc", 0, true},
		{"1.234", 0, true}, // more than 2 decimals must error, not round
		{"1.2.3", 0, true}, // ParseInt on "2.3" fails
		{"1e3", 0, true},   // scientific notation rejected
	}
	for _, tt := range tests {
		got, err := ParseMinor(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseMinor(%q) = %d, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMinor(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMinor(%q) = %d, want %d", tt.in, got, tt.want)
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
	}
	for m, want := range tests {
		if got := m.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", m.Amount(), got, want)
		}
	}
}
