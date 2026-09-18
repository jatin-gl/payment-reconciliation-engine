// Package contract defines the stable, versioned JSON wire format the engine
// emits. Keeping the wire types separate from the domain model lets the internal
// model evolve without silently breaking downstream consumers — most importantly
// the recon-dispute-agent, which parses this exact shape. Any change here is a
// data-contract change and must be reflected in docs/data-contract.md and the
// ContractVersion constant.
package contract

import (
	"time"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/model"
	"github.com/jatin-gl/payment-reconciliation-engine/pkg/money"
)

// Money is the wire representation of a monetary value: integer minor units plus
// an ISO-4217 currency. Consumers must never interpret amount_minor as a float.
type Money struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

func moneyDTO(m money.Money) Money {
	return Money{AmountMinor: m.Amount(), Currency: m.Currency()}
}

// Transaction is the wire form of a single source record.
type Transaction struct {
	MatchKey   string `json:"match_key"`
	ExternalID string `json:"external_id"`
	Source     string `json:"source"`
	Amount     Money  `json:"amount"`
	Fee        Money  `json:"fee"`
	Status     string `json:"status"`
	// RawStatus is the source's original, un-normalized status string. It lets a
	// consumer tell apart two records whose normalized status is both "unknown"
	// (e.g. "voided" vs "expired"). Omitted when the source had no status.
	RawStatus string `json:"raw_status,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

func transactionDTO(t *model.Transaction) *Transaction {
	if t == nil {
		return nil
	}
	dto := &Transaction{
		MatchKey:   t.MatchKey,
		ExternalID: t.ExternalID,
		Source:     string(t.Source),
		Amount:     moneyDTO(t.Amount),
		Fee:        moneyDTO(t.Fee),
		Status:     string(t.Status),
		RawStatus:  t.RawStatus,
	}
	if !t.Timestamp.IsZero() {
		dto.Timestamp = t.Timestamp.UTC().Format(time.RFC3339)
	}
	return dto
}

// Discrepancy is the wire form of a single finding.
type Discrepancy struct {
	ID             string       `json:"id"`
	Type           string       `json:"type"`
	Severity       string       `json:"severity"`
	MatchKey       string       `json:"match_key"`
	PSPRecord      *Transaction `json:"psp_record,omitempty"`
	LedgerRecord   *Transaction `json:"ledger_record,omitempty"`
	MonetaryImpact Money        `json:"monetary_impact"`
	Detail         string       `json:"detail"`
}

// Summary is the wire form of the aggregate view.
type Summary struct {
	PSPCount            int            `json:"psp_count"`
	LedgerCount         int            `json:"ledger_count"`
	MatchedCount        int            `json:"matched_count"`
	DiscrepancyCount    int            `json:"discrepancy_count"`
	BySeverity          map[string]int `json:"by_severity"`
	ByType              map[string]int `json:"by_type"`
	TotalMonetaryImpact Money          `json:"total_monetary_impact"`
}

// Report is the top-level wire object.
type Report struct {
	ReportID        string        `json:"report_id"`
	ContractVersion string        `json:"contract_version"`
	GeneratedAt     string        `json:"generated_at"`
	Currency        string        `json:"currency"`
	Summary         Summary       `json:"summary"`
	Discrepancies   []Discrepancy `json:"discrepancies"`
}

// FromReport converts a domain report into its wire representation.
func FromReport(r model.Report) Report {
	discs := make([]Discrepancy, 0, len(r.Discrepancies))
	for _, d := range r.Discrepancies {
		discs = append(discs, Discrepancy{
			ID:             d.ID,
			Type:           string(d.Type),
			Severity:       string(d.Severity),
			MatchKey:       d.MatchKey,
			PSPRecord:      transactionDTO(d.PSPRecord),
			LedgerRecord:   transactionDTO(d.LedgerRecord),
			MonetaryImpact: moneyDTO(d.MonetaryImpact),
			Detail:         d.Detail,
		})
	}

	return Report{
		ReportID:        r.ReportID,
		ContractVersion: r.ContractVersion,
		GeneratedAt:     r.GeneratedAt.UTC().Format(time.RFC3339),
		Currency:        r.Currency,
		Summary: Summary{
			PSPCount:            r.Summary.PSPCount,
			LedgerCount:         r.Summary.LedgerCount,
			MatchedCount:        r.Summary.MatchedCount,
			DiscrepancyCount:    r.Summary.DiscrepancyCount,
			BySeverity:          severityMap(r.Summary.BySeverity),
			ByType:              typeMap(r.Summary.ByType),
			TotalMonetaryImpact: moneyDTO(r.Summary.TotalMonetaryImpact),
		},
		Discrepancies: discs,
	}
}

func severityMap(in map[model.Severity]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[string(k)] = v
	}
	return out
}

func typeMap(in map[model.DiscrepancyType]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[string(k)] = v
	}
	return out
}
