package ingest

import (
	"strings"
	"testing"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/model"
)

func TestParseCSV_PSPHappyPath(t *testing.T) {
	in := `transaction_id,amount,fee,currency,status,settled_at
TXN-1,105.00,3.00,USD,settled,2026-01-15T10:00:00Z
TXN-2,50.00,1.50,USD,paid,2026-01-15T11:00:00Z
`
	got, err := ParseCSV(strings.NewReader(in), model.SourcePSP, DefaultPSPColumns(), DefaultStatusMap())
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	first := got[0]
	if first.MatchKey != "TXN-1" || first.Amount.Amount() != 10500 || first.Fee.Amount() != 300 {
		t.Errorf("row 0 = %+v", first)
	}
	if first.Status != model.StatusSettled {
		t.Errorf("status = %q, want settled", first.Status)
	}
	// "paid" must normalize to captured via the default status map.
	if got[1].Status != model.StatusCaptured {
		t.Errorf("row 1 status = %q, want captured", got[1].Status)
	}
	if first.Timestamp.IsZero() {
		t.Error("timestamp should be parsed")
	}
}

func TestParseCSV_LedgerLayout(t *testing.T) {
	in := `psp_reference,entry_id,amount,fee,currency,status,booked_at
TXN-1,L-1,105.00,3.00,USD,settled,2026-01-15
`
	got, err := ParseCSV(strings.NewReader(in), model.SourceLedger, DefaultLedgerColumns(), DefaultStatusMap())
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if got[0].MatchKey != "TXN-1" || got[0].ExternalID != "L-1" {
		t.Errorf("row = %+v", got[0])
	}
	if got[0].Source != model.SourceLedger {
		t.Errorf("source = %q, want ledger", got[0].Source)
	}
}

func TestParseCSV_MalformedAmountIsHardError(t *testing.T) {
	in := `transaction_id,amount,fee,currency,status,settled_at
TXN-1,not-a-number,3.00,USD,settled,2026-01-15T10:00:00Z
`
	_, err := ParseCSV(strings.NewReader(in), model.SourcePSP, DefaultPSPColumns(), DefaultStatusMap())
	if err == nil {
		t.Fatal("malformed amount must be an error, not a silent zero")
	}
	if !strings.Contains(err.Error(), "row 2") {
		t.Errorf("error should identify the offending row, got: %v", err)
	}
}

func TestParseCSV_MissingRequiredColumn(t *testing.T) {
	in := `transaction_id,fee,currency,status
TXN-1,3.00,USD,settled
`
	_, err := ParseCSV(strings.NewReader(in), model.SourcePSP, DefaultPSPColumns(), DefaultStatusMap())
	if err == nil {
		t.Fatal("missing amount column must error")
	}
}

func TestParseCSV_UnknownStatusFallsBack(t *testing.T) {
	in := `transaction_id,amount,fee,currency,status,settled_at
TXN-1,105.00,3.00,USD,teleported,2026-01-15T10:00:00Z
`
	got, err := ParseCSV(strings.NewReader(in), model.SourcePSP, DefaultPSPColumns(), DefaultStatusMap())
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if got[0].Status != model.StatusUnknown {
		t.Errorf("unmapped status should fall back to unknown, got %q", got[0].Status)
	}
}

func TestParseCSV_OptionalFeeAndTimestampOmitted(t *testing.T) {
	// A minimal layout without fee or timestamp columns should still parse.
	cm := ColumnMap{MatchKey: "ref", ExternalID: "ref", Amount: "amt", Currency: "ccy"}
	in := `ref,amt,ccy
TXN-1,10.00,USD
`
	got, err := ParseCSV(strings.NewReader(in), model.SourcePSP, cm, DefaultStatusMap())
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if got[0].Fee.Amount() != 0 || !got[0].Timestamp.IsZero() {
		t.Errorf("optional fields should default cleanly, got %+v", got[0])
	}
}
