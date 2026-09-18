package recon

import (
	"os"
	"testing"
	"time"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/ingest"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/matcher"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/model"
	"github.com/jatin-gl/payment-reconciliation-engine/pkg/money"
)

// fixedEngine returns an engine with deterministic clock and IDs for stable assertions.
func fixedEngine() *Engine {
	e := New(matcher.DefaultConfig())
	e.Now = func() time.Time { return time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC) }
	e.NewID = func() string { return "rpt_test" }
	return e
}

func p(key string, amount, fee int64, status model.Status) model.Transaction {
	return model.Transaction{MatchKey: key, ExternalID: "p-" + key, Source: model.SourcePSP,
		Amount: money.New(amount, "USD"), Fee: money.New(fee, "USD"), Status: status}
}
func l(key string, amount, fee int64, status model.Status) model.Transaction {
	return model.Transaction{MatchKey: key, ExternalID: "l-" + key, Source: model.SourceLedger,
		Amount: money.New(amount, "USD"), Fee: money.New(fee, "USD"), Status: status}
}

func TestReconcile_ReportShape(t *testing.T) {
	e := fixedEngine()
	report := e.Reconcile(
		[]model.Transaction{
			p("A", 10000, 300, model.StatusSettled), // clean
			p("B", 20000, 0, model.StatusSettled),   // amount mismatch
			p("C", 5000, 0, model.StatusSettled),    // missing in ledger
		},
		[]model.Transaction{
			l("A", 10000, 300, model.StatusSettled),
			l("B", 19000, 0, model.StatusSettled),
			l("D", 7500, 0, model.StatusSettled), // missing in psp
		},
	)

	if report.ReportID != "rpt_test" || report.ContractVersion != model.ContractVersion {
		t.Errorf("report metadata = %+v", report)
	}
	if report.Currency != "USD" {
		t.Errorf("currency = %q, want USD", report.Currency)
	}
	if report.Summary.PSPCount != 3 || report.Summary.LedgerCount != 3 {
		t.Errorf("counts = %+v", report.Summary)
	}
	if report.Summary.MatchedCount != 1 { // only A reconciles cleanly
		t.Errorf("matched = %d, want 1", report.Summary.MatchedCount)
	}
	if report.Summary.DiscrepancyCount != 3 {
		t.Errorf("discrepancies = %d, want 3 (%+v)", report.Summary.DiscrepancyCount, report.Discrepancies)
	}
	// Total money at risk = |20000-19000| + 5000 (C) + 7500 (D) = 13500
	if report.Summary.TotalMonetaryImpact.Amount() != 13500 {
		t.Errorf("total impact = %d, want 13500", report.Summary.TotalMonetaryImpact.Amount())
	}
}

func TestReconcile_SortedBySeverityThenImpact(t *testing.T) {
	e := fixedEngine()
	report := e.Reconcile(
		[]model.Transaction{
			p("small-fee", 10000, 999, model.StatusSettled),  // fee mismatch -> low
			p("big-missing", 500000, 0, model.StatusSettled), // missing -> critical (>= 100000)
		},
		[]model.Transaction{
			l("small-fee", 10000, 900, model.StatusSettled),
		},
	)
	if len(report.Discrepancies) < 2 {
		t.Fatalf("want >=2 discrepancies, got %+v", report.Discrepancies)
	}
	if report.Discrepancies[0].Severity != model.SeverityCritical {
		t.Errorf("most severe finding should sort first, got %s", report.Discrepancies[0].Severity)
	}
}

func TestReconcile_EmptyInputs(t *testing.T) {
	report := fixedEngine().Reconcile(nil, nil)
	if report.Summary.DiscrepancyCount != 0 || report.Summary.MatchedCount != 0 {
		t.Errorf("empty inputs should yield empty report, got %+v", report.Summary)
	}
}

// TestReconcile_FromTestdata is an end-to-end check across ingest + engine using
// the committed sample files, guarding the documented example in the README.
func TestReconcile_FromTestdata(t *testing.T) {
	pspFile, err := os.Open("../../testdata/psp_settlement.csv")
	if err != nil {
		t.Fatalf("open psp: %v", err)
	}
	defer pspFile.Close()
	ledgerFile, err := os.Open("../../testdata/internal_ledger.csv")
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer ledgerFile.Close()

	pspTxns, err := ingest.ParseCSV(pspFile, model.SourcePSP, ingest.DefaultPSPColumns(), ingest.DefaultStatusMap())
	if err != nil {
		t.Fatalf("parse psp: %v", err)
	}
	ledgerTxns, err := ingest.ParseCSV(ledgerFile, model.SourceLedger, ingest.DefaultLedgerColumns(), ingest.DefaultStatusMap())
	if err != nil {
		t.Fatalf("parse ledger: %v", err)
	}

	report := fixedEngine().Reconcile(pspTxns, ledgerTxns)

	// TXN-1001 matches cleanly; every other key contributes exactly one finding.
	if report.Summary.MatchedCount != 1 {
		t.Errorf("matched = %d, want 1", report.Summary.MatchedCount)
	}
	wantTypes := map[model.DiscrepancyType]bool{
		model.AmountMismatch:  false,
		model.MissingInLedger: false,
		model.MissingInPSP:    false,
		model.FeeMismatch:     false,
		model.StatusMismatch:  false,
		model.DuplicateInPSP:  false,
	}
	for _, d := range report.Discrepancies {
		if _, ok := wantTypes[d.Type]; ok {
			wantTypes[d.Type] = true
		}
	}
	for typ, seen := range wantTypes {
		if !seen {
			t.Errorf("expected a %s finding from testdata; report=%+v", typ, report.Discrepancies)
		}
	}
}
