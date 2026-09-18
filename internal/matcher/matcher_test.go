package matcher

import (
	"strings"
	"testing"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/model"
	"github.com/jatin-gl/payment-reconciliation-engine/pkg/money"
)

// txn is a compact test constructor.
func txn(source model.Source, key string, extID string, amountMinor, feeMinor int64, currency string, status model.Status) model.Transaction {
	return model.Transaction{
		MatchKey:   key,
		ExternalID: extID,
		Source:     source,
		Amount:     money.New(amountMinor, currency),
		Fee:        money.New(feeMinor, currency),
		Status:     status,
	}
}

func psp(key, extID string, amount, fee int64) model.Transaction {
	return txn(model.SourcePSP, key, extID, amount, fee, "USD", model.StatusSettled)
}
func ledger(key, extID string, amount, fee int64) model.Transaction {
	return txn(model.SourceLedger, key, extID, amount, fee, "USD", model.StatusSettled)
}

// findByType returns the first discrepancy of the given type, or nil.
func findByType(discs []model.Discrepancy, t model.DiscrepancyType) *model.Discrepancy {
	for i := range discs {
		if discs[i].Type == t {
			return &discs[i]
		}
	}
	return nil
}

func TestReconcile_CleanMatchProducesNothing(t *testing.T) {
	pspSide := []model.Transaction{psp("TXN-1", "p1", 10000, 300)}
	ledgerSide := []model.Transaction{ledger("TXN-1", "l1", 10000, 300)}
	got := Reconcile(pspSide, ledgerSide, DefaultConfig())
	if len(got) != 0 {
		t.Fatalf("clean match should produce no discrepancies, got %d: %+v", len(got), got)
	}
}

func TestReconcile_MissingInLedger(t *testing.T) {
	got := Reconcile([]model.Transaction{psp("TXN-1", "p1", 10000, 0)}, nil, DefaultConfig())
	d := findByType(got, model.MissingInLedger)
	if d == nil {
		t.Fatal("expected MISSING_IN_LEDGER")
	}
	if d.PSPRecord == nil || d.LedgerRecord != nil {
		t.Error("MISSING_IN_LEDGER should carry the PSP record and no ledger record")
	}
	if d.MonetaryImpact.Amount() != 10000 {
		t.Errorf("impact = %d, want 10000", d.MonetaryImpact.Amount())
	}
}

func TestReconcile_MissingInPSP(t *testing.T) {
	got := Reconcile(nil, []model.Transaction{ledger("TXN-9", "l9", 5000, 0)}, DefaultConfig())
	d := findByType(got, model.MissingInPSP)
	if d == nil {
		t.Fatal("expected MISSING_IN_PSP")
	}
	if d.LedgerRecord == nil || d.PSPRecord != nil {
		t.Error("MISSING_IN_PSP should carry the ledger record and no PSP record")
	}
}

func TestReconcile_AmountMismatchAndImpact(t *testing.T) {
	got := Reconcile(
		[]model.Transaction{psp("TXN-1", "p1", 10500, 0)},
		[]model.Transaction{ledger("TXN-1", "l1", 10000, 0)},
		DefaultConfig(),
	)
	d := findByType(got, model.AmountMismatch)
	if d == nil {
		t.Fatal("expected AMOUNT_MISMATCH")
	}
	if d.MonetaryImpact.Amount() != 500 { // PSP minus ledger
		t.Errorf("impact = %d, want 500", d.MonetaryImpact.Amount())
	}
}

func TestReconcile_FeeMismatch(t *testing.T) {
	got := Reconcile(
		[]model.Transaction{psp("TXN-1", "p1", 10000, 350)},
		[]model.Transaction{ledger("TXN-1", "l1", 10000, 300)},
		DefaultConfig(),
	)
	if findByType(got, model.FeeMismatch) == nil {
		t.Fatalf("expected FEE_MISMATCH, got %+v", got)
	}
	if findByType(got, model.AmountMismatch) != nil {
		t.Error("amounts are equal; should not report AMOUNT_MISMATCH")
	}
}

func TestReconcile_StatusMismatch(t *testing.T) {
	p := txn(model.SourcePSP, "TXN-1", "p1", 10000, 0, "USD", model.StatusSettled)
	l := txn(model.SourceLedger, "TXN-1", "l1", 10000, 0, "USD", model.StatusRefunded)
	got := Reconcile([]model.Transaction{p}, []model.Transaction{l}, DefaultConfig())
	if findByType(got, model.StatusMismatch) == nil {
		t.Fatalf("expected STATUS_MISMATCH, got %+v", got)
	}
}

func TestReconcile_CurrencyMismatchIsCriticalAndStops(t *testing.T) {
	p := txn(model.SourcePSP, "TXN-1", "p1", 10000, 0, "USD", model.StatusSettled)
	l := txn(model.SourceLedger, "TXN-1", "l1", 9500, 0, "EUR", model.StatusRefunded)
	got := Reconcile([]model.Transaction{p}, []model.Transaction{l}, DefaultConfig())
	d := findByType(got, model.CurrencyMismatch)
	if d == nil {
		t.Fatal("expected CURRENCY_MISMATCH")
	}
	if d.Severity != model.SeverityCritical {
		t.Errorf("currency mismatch severity = %s, want critical", d.Severity)
	}
	// When currency differs, amount/status comparisons are meaningless and must
	// be suppressed.
	if findByType(got, model.AmountMismatch) != nil || findByType(got, model.StatusMismatch) != nil {
		t.Errorf("currency mismatch should suppress other field findings, got %+v", got)
	}
}

func TestReconcile_MultipleFieldMismatchesInOnePair(t *testing.T) {
	p := txn(model.SourcePSP, "TXN-1", "p1", 10500, 350, "USD", model.StatusSettled)
	l := txn(model.SourceLedger, "TXN-1", "l1", 10000, 300, "USD", model.StatusRefunded)
	got := Reconcile([]model.Transaction{p}, []model.Transaction{l}, DefaultConfig())
	for _, typ := range []model.DiscrepancyType{model.AmountMismatch, model.FeeMismatch, model.StatusMismatch} {
		if findByType(got, typ) == nil {
			t.Errorf("expected %s in %+v", typ, got)
		}
	}
}

func TestReconcile_DuplicatesSuppressFieldComparison(t *testing.T) {
	pspSide := []model.Transaction{
		psp("TXN-1", "p1", 10000, 0),
		psp("TXN-1", "p2", 20000, 0), // duplicate key on PSP side
	}
	ledgerSide := []model.Transaction{ledger("TXN-1", "l1", 10000, 0)}
	got := Reconcile(pspSide, ledgerSide, DefaultConfig())
	if findByType(got, model.DuplicateInPSP) == nil {
		t.Fatalf("expected DUPLICATE_IN_PSP, got %+v", got)
	}
	// The ambiguous key must not also yield amount/missing findings.
	if findByType(got, model.AmountMismatch) != nil || findByType(got, model.MissingInLedger) != nil {
		t.Errorf("duplicate key should suppress pairwise findings, got %+v", got)
	}
}

func TestSeverityForImpact(t *testing.T) {
	cfg := Config{HighImpactMinor: 10_000, CriticalImpactMinor: 100_000}
	tests := []struct {
		name   string
		base   model.Severity
		amount int64
		want   model.Severity
	}{
		{"below high stays base", model.SeverityMedium, 5_000, model.SeverityMedium},
		{"at high escalates", model.SeverityMedium, 10_000, model.SeverityHigh},
		{"at critical escalates", model.SeverityLow, 100_000, model.SeverityCritical},
		{"never de-escalates below base", model.SeverityHigh, 100, model.SeverityHigh},
		{"negative impact uses absolute value", model.SeverityLow, -100_000, model.SeverityCritical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := severityForImpact(tt.base, money.New(tt.amount, "USD"), cfg)
			if got != tt.want {
				t.Errorf("severityForImpact(%s, %d) = %s, want %s", tt.base, tt.amount, got, tt.want)
			}
		})
	}
}

func TestDiscrepancyID_DeterministicAndStable(t *testing.T) {
	run := func() string {
		d := Reconcile(
			[]model.Transaction{psp("TXN-1", "p1", 10500, 0)},
			[]model.Transaction{ledger("TXN-1", "l1", 10000, 0)},
			DefaultConfig(),
		)
		return d[0].ID
	}
	first, second := run(), run()
	if first != second {
		t.Errorf("discrepancy IDs must be deterministic across identical runs: %q != %q", first, second)
	}
}

func TestReconcile_DuplicateInLedgerAggregatesExposure(t *testing.T) {
	// Two ledger rows for one key: exposure is the SUM, and the aggregate crosses
	// the critical threshold even though neither row alone would.
	ledgerSide := []model.Transaction{
		ledger("TXN-1", "l1", 60000, 0),
		ledger("TXN-1", "l2", 60000, 0),
	}
	got := Reconcile(nil, ledgerSide, DefaultConfig())
	d := findByType(got, model.DuplicateInLedger)
	if d == nil {
		t.Fatalf("expected DUPLICATE_IN_LEDGER, got %+v", got)
	}
	if d.MonetaryImpact.Amount() != 120000 {
		t.Errorf("impact = %d, want 120000 (sum of both rows, not just the first)", d.MonetaryImpact.Amount())
	}
	if d.Severity != model.SeverityCritical {
		t.Errorf("severity = %s, want critical (aggregate exposure >= threshold)", d.Severity)
	}
}

func unmapped(source model.Source, extID, rawStatus string) model.Transaction {
	return model.Transaction{
		MatchKey: "TXN-1", ExternalID: extID, Source: source,
		Amount: money.New(10000, "USD"), Fee: money.New(0, "USD"),
		Status: model.StatusUnknown, RawStatus: rawStatus,
	}
}

func TestReconcile_TwoDifferentUnmappedStatusesMismatch(t *testing.T) {
	got := Reconcile(
		[]model.Transaction{unmapped(model.SourcePSP, "p", "voided")},
		[]model.Transaction{unmapped(model.SourceLedger, "l", "expired")},
		DefaultConfig(),
	)
	d := findByType(got, model.StatusMismatch)
	if d == nil {
		t.Fatalf("two different unmapped statuses must mismatch, got %+v", got)
	}
	// The human-facing detail must show the raw statuses, not two "unknown"s.
	if !strings.Contains(d.Detail, "voided") || !strings.Contains(d.Detail, "expired") {
		t.Errorf("detail should surface raw statuses, got %q", d.Detail)
	}
}

func TestReconcile_AuthorizedVsCapturedMismatch(t *testing.T) {
	// An auth hold is materially different from a capture; they must not be
	// collapsed into the same normalized status.
	p := txn(model.SourcePSP, "TXN-1", "p", 10000, 0, "USD", model.StatusAuthorized)
	l := txn(model.SourceLedger, "TXN-1", "l", 10000, 0, "USD", model.StatusCaptured)
	got := Reconcile([]model.Transaction{p}, []model.Transaction{l}, DefaultConfig())
	if findByType(got, model.StatusMismatch) == nil {
		t.Fatalf("authorized vs captured must be a status mismatch, got %+v", got)
	}
}

func TestReconcile_SameUnmappedStatusNoMismatch(t *testing.T) {
	got := Reconcile(
		[]model.Transaction{unmapped(model.SourcePSP, "p", "held")},
		[]model.Transaction{unmapped(model.SourceLedger, "l", "held")},
		DefaultConfig(),
	)
	if findByType(got, model.StatusMismatch) != nil {
		t.Errorf("identical unmapped statuses should not mismatch, got %+v", got)
	}
}
