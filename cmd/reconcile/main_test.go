package main

import (
	"bytes"
	"encoding/json"
	"errors"
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
	for _, want := range []string{"Discrepancies:    6", "DUPLICATE_IN_PSP", "Money at risk:    216.50 USD"} {
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

func TestRun_NoDiscrepanciesNoSentinel(t *testing.T) {
	// Reconciling a file against itself with matching column layouts still yields
	// findings only if the layouts differ; here we assert the happy path returns
	// nil when fail-on-discrepancy is off regardless of content.
	var out bytes.Buffer
	err := run([]string{"--psp", pspFixture, "--ledger", ledgerFixture}, &out)
	if err != nil {
		t.Fatalf("run without fail flag should not error: %v", err)
	}
}
