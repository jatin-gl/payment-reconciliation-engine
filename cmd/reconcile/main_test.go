package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/contract"
)

const (
	pspFixture    = "../../testdata/psp_settlement.csv"
	ledgerFixture = "../../testdata/internal_ledger.csv"
)

func TestRun_TextOutput(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--psp", pspFixture, "--ledger", ledgerFixture, "--format", "text"}, &out)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	text := out.String()
	for _, want := range []string{"Discrepancies:    6", "DUPLICATE_IN_PSP", "Money at risk:    306.50 USD"} {
		if !strings.Contains(text, want) {
			t.Errorf("text output missing %q:\n%s", want, text)
		}
	}
}

func TestRun_JSONOutput(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--psp", pspFixture, "--ledger", ledgerFixture, "--format", "json"}, &out)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var report contract.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if report.ContractVersion != "1.0" || report.Summary.DiscrepancyCount != 6 {
		t.Errorf("unexpected report summary: %+v", report.Summary)
	}
}

func TestRun_FailOnDiscrepancyReturnsSentinel(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--psp", pspFixture, "--ledger", ledgerFixture, "--fail-on-discrepancy"}, &out)
	if !errors.Is(err, errDiscrepancies) {
		t.Fatalf("expected errDiscrepancies sentinel, got %v", err)
	}
}

func TestRun_MissingRequiredFlags(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--psp", pspFixture}, &out); err == nil {
		t.Fatal("missing --ledger should error")
	}
}

func TestRun_UnknownFormat(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--psp", pspFixture, "--ledger", ledgerFixture, "--format", "xml"}, &out)
	if err == nil {
		t.Fatal("unknown format should error")
	}
}

func TestRun_UnreadableInput(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--psp", "does-not-exist.csv", "--ledger", ledgerFixture}, &out)
	if err == nil {
		t.Fatal("missing input file should error")
	}
}

// writeCleanPair writes a PSP and ledger CSV that reconcile with zero
// discrepancies (one matching transaction on each side) and returns their paths.
func writeCleanPair(t *testing.T) (pspPath, ledgerPath string) {
	t.Helper()
	dir := t.TempDir()
	pspPath = filepath.Join(dir, "psp.csv")
	ledgerPath = filepath.Join(dir, "ledger.csv")
	psp := "transaction_id,amount,fee,currency,status,settled_at\nTXN-1,100.00,3.00,USD,settled,2026-01-15T10:00:00Z\n"
	ledger := "psp_reference,entry_id,amount,fee,currency,status,booked_at\nTXN-1,L-1,100.00,3.00,USD,settled,2026-01-15\n"
	if err := os.WriteFile(pspPath, []byte(psp), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, []byte(ledger), 0o600); err != nil {
		t.Fatal(err)
	}
	return pspPath, ledgerPath
}

func TestRun_CleanMatchReportsZeroDiscrepancies(t *testing.T) {
	psp, ledger := writeCleanPair(t)
	var out bytes.Buffer
	if err := run([]string{"--psp", psp, "--ledger", ledger}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Discrepancies:    0") || !strings.Contains(text, "fully reconciled") {
		t.Errorf("expected a clean report, got:\n%s", text)
	}
}

func TestRun_CleanMatchDoesNotTripFailOnDiscrepancy(t *testing.T) {
	psp, ledger := writeCleanPair(t)
	var out bytes.Buffer
	if err := run([]string{"--psp", psp, "--ledger", ledger, "--fail-on-discrepancy"}, &out); err != nil {
		t.Fatalf("clean match must not return the discrepancy sentinel: %v", err)
	}
}

func TestRun_HelpExitsZero(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--help"}, &out); err != nil {
		t.Errorf("--help should return nil (exit 0), got %v", err)
	}
}
