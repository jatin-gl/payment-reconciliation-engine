package contract

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/model"
	"github.com/jatin-gl/payment-reconciliation-engine/pkg/money"
)

func TestFromReport_WireShape(t *testing.T) {
	psp := &model.Transaction{
		MatchKey: "TXN-1", ExternalID: "p1", Source: model.SourcePSP,
		Amount: money.New(10500, "USD"), Fee: money.New(300, "USD"), Status: model.StatusSettled,
		Timestamp: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}
	ledger := &model.Transaction{
		MatchKey: "TXN-1", ExternalID: "l1", Source: model.SourceLedger,
		Amount: money.New(10000, "USD"), Fee: money.New(300, "USD"), Status: model.StatusSettled,
	}
	report := model.Report{
		ReportID:        "rpt_1",
		ContractVersion: model.ContractVersion,
		GeneratedAt:     time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC),
		Currency:        "USD",
		Summary: model.Summary{
			PSPCount: 1, LedgerCount: 1, MatchedCount: 0, DiscrepancyCount: 1,
			BySeverity:          map[model.Severity]int{model.SeverityMedium: 1},
			ByType:              map[model.DiscrepancyType]int{model.AmountMismatch: 1},
			TotalMonetaryImpact: money.New(500, "USD"),
		},
		Discrepancies: []model.Discrepancy{{
			ID: "disc_abc", Type: model.AmountMismatch, Severity: model.SeverityMedium,
			MatchKey: "TXN-1", PSPRecord: psp, LedgerRecord: ledger,
			MonetaryImpact: money.New(500, "USD"), Detail: "amount mismatch",
		}},
	}

	dto := FromReport(report)
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Round-trip into a generic map to assert on the exact JSON keys the agent reads.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"report_id", "contract_version", "generated_at", "currency", "summary", "discrepancies"} {
		if _, ok := m[key]; !ok {
			t.Errorf("wire report missing key %q", key)
		}
	}

	d := m["discrepancies"].([]any)[0].(map[string]any)
	if d["type"] != "AMOUNT_MISMATCH" {
		t.Errorf("type = %v, want AMOUNT_MISMATCH", d["type"])
	}
	impact := d["monetary_impact"].(map[string]any)
	if impact["amount_minor"].(float64) != 500 || impact["currency"] != "USD" {
		t.Errorf("monetary_impact = %v", impact)
	}
	// Timestamp present on psp_record, omitted on ledger_record (zero value).
	pspRec := d["psp_record"].(map[string]any)
	if pspRec["timestamp"] != "2026-01-15T10:00:00Z" {
		t.Errorf("psp timestamp = %v", pspRec["timestamp"])
	}
	ledgerRec := d["ledger_record"].(map[string]any)
	if _, ok := ledgerRec["timestamp"]; ok {
		t.Error("zero ledger timestamp should be omitted from the wire form")
	}
}
