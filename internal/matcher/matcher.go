// Package matcher contains the reconciliation matching engine: given payment
// records from two sources, it lines them up by their shared match key and
// classifies every difference into a typed, severity-ranked discrepancy.
//
// Design notes:
//
//   - Matching is key-based. The caller is responsible for populating
//     Transaction.MatchKey with a reference that is meaningful across both
//     sources (typically the PSP transaction id the ledger stores as an external
//     reference). Fuzzy/amount-window matching is intentionally out of scope for
//     v1 — it trades determinism for recall and belongs behind a separate,
//     explicitly-opt-in strategy.
//   - Duplicates short-circuit field comparison. If a key appears more than once
//     on a side, the pairing is ambiguous, so the engine emits a DUPLICATE_* find
//     for that key and does not also emit amount/fee/status mismatches for it.
//   - Money is never compared as float. See pkg/money.
package matcher

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/model"
	"github.com/jatin-gl/payment-reconciliation-engine/pkg/money"
)

// Config tunes severity classification. Thresholds are in minor units of the
// reconciliation currency (e.g. cents). The zero value is usable but treats
// every monetary finding as high; prefer DefaultConfig.
type Config struct {
	// HighImpactMinor: monetary impact at or above this is at least High.
	HighImpactMinor int64
	// CriticalImpactMinor: monetary impact at or above this is Critical.
	CriticalImpactMinor int64
}

// DefaultConfig returns sensible thresholds: $100 for high, $1,000 for critical.
func DefaultConfig() Config {
	return Config{HighImpactMinor: 10_000, CriticalImpactMinor: 100_000}
}

// Reconcile compares PSP and ledger records and returns the detected
// discrepancies. Clean matches produce no discrepancy. The result is not sorted;
// the engine layer orders it for reporting.
func Reconcile(psp, ledger []model.Transaction, cfg Config) []model.Discrepancy {
	pspByKey := index(psp)
	ledgerByKey := index(ledger)

	var out []model.Discrepancy

	// Duplicate detection first, so we can skip field comparison for ambiguous keys.
	dupPSP := duplicateKeys(pspByKey)
	dupLedger := duplicateKeys(ledgerByKey)
	for key := range dupPSP {
		recs := pspByKey[key]
		impact := sumAmounts(recs) // full exposure, not just the first occurrence
		out = append(out, newDiscrepancy(model.DuplicateInPSP,
			severityForImpact(model.SeverityHigh, impact, cfg), key,
			&recs[0], nil, impact,
			fmt.Sprintf("match key %q appears %d times in PSP settlement (total %s)", key, len(recs), impact)))
	}
	for key := range dupLedger {
		recs := ledgerByKey[key]
		impact := sumAmounts(recs)
		out = append(out, newDiscrepancy(model.DuplicateInLedger,
			severityForImpact(model.SeverityHigh, impact, cfg), key,
			nil, &recs[0], impact,
			fmt.Sprintf("match key %q appears %d times in ledger (total %s)", key, len(recs), impact)))
	}

	// One-sided and field-level comparisons over the union of keys.
	for key, pspRecs := range pspByKey {
		if dupPSP[key] {
			continue
		}
		p := pspRecs[0]
		ledgerRecs, ok := ledgerByKey[key]
		if !ok {
			out = append(out, newDiscrepancy(model.MissingInLedger, severityForImpact(model.SeverityHigh, p.Amount, cfg),
				key, &p, nil, p.Amount,
				fmt.Sprintf("PSP transaction %s (%s) has no ledger entry", p.ExternalID, p.Amount)))
			continue
		}
		if dupLedger[key] {
			continue // ledger side ambiguous; already reported as duplicate
		}
		out = append(out, compareFields(p, ledgerRecs[0], cfg)...)
	}

	for key, ledgerRecs := range ledgerByKey {
		if dupLedger[key] {
			continue
		}
		if _, ok := pspByKey[key]; ok {
			continue // already handled in the PSP pass
		}
		l := ledgerRecs[0]
		out = append(out, newDiscrepancy(model.MissingInPSP, severityForImpact(model.SeverityHigh, l.Amount, cfg),
			key, nil, &l, l.Amount,
			fmt.Sprintf("ledger transaction %s (%s) has no PSP settlement entry", l.ExternalID, l.Amount)))
	}

	return out
}

// compareFields emits the field-level discrepancies for a matched pair. A pair
// can produce several findings at once (e.g. both amount and fee differ).
func compareFields(p, l model.Transaction, cfg Config) []model.Discrepancy {
	var out []model.Discrepancy

	// Currency mismatch is checked first and is always critical: comparing
	// amounts across currencies is meaningless, so we stop at the currency finding.
	if p.Amount.Currency() != l.Amount.Currency() {
		out = append(out, newDiscrepancy(model.CurrencyMismatch, model.SeverityCritical, p.MatchKey,
			&p, &l, money.Zero(p.Amount.Currency()),
			fmt.Sprintf("currency mismatch: PSP %s vs ledger %s", p.Amount.Currency(), l.Amount.Currency())))
		return out
	}

	if !p.Amount.Equal(l.Amount) {
		impact, _ := p.Amount.Sub(l.Amount) // same currency guaranteed above
		out = append(out, newDiscrepancy(model.AmountMismatch, severityForImpact(model.SeverityMedium, impact, cfg),
			p.MatchKey, &p, &l, impact,
			fmt.Sprintf("amount mismatch: PSP %s vs ledger %s", p.Amount, l.Amount)))
	}

	if !p.Fee.Equal(l.Fee) {
		impact, _ := p.Fee.Sub(l.Fee)
		out = append(out, newDiscrepancy(model.FeeMismatch, severityForImpact(model.SeverityLow, impact, cfg),
			p.MatchKey, &p, &l, impact,
			fmt.Sprintf("fee mismatch: PSP %s vs ledger %s", p.Fee, l.Fee)))
	}

	// Compare effective status, not just the normalized enum: two *different*
	// unmapped provider statuses (e.g. "voided" vs "expired") both normalize to
	// StatusUnknown and would otherwise be treated as equal, hiding a real
	// lifecycle divergence.
	if effectiveStatus(p) != effectiveStatus(l) {
		out = append(out, newDiscrepancy(model.StatusMismatch, model.SeverityMedium, p.MatchKey,
			&p, &l, money.Zero(p.Amount.Currency()),
			fmt.Sprintf("status mismatch: PSP %q vs ledger %q", displayStatus(p), displayStatus(l))))
	}

	return out
}

// effectiveStatus returns a comparable status string: the normalized status when
// known, or a raw-tagged value when the source status was unmapped.
func effectiveStatus(t model.Transaction) string {
	if t.Status == model.StatusUnknown && t.RawStatus != "" {
		return "raw:" + strings.ToLower(t.RawStatus)
	}
	return string(t.Status)
}

// displayStatus renders a human-readable status: the raw source string when the
// status was unmapped, otherwise the normalized value.
func displayStatus(t model.Transaction) string {
	if t.Status == model.StatusUnknown && t.RawStatus != "" {
		return t.RawStatus
	}
	return string(t.Status)
}

// sumAmounts totals the gross amounts of the given records. In a single-currency
// reconciliation they share a currency; a cross-currency record is skipped rather
// than producing a bogus sum. It represents the full exposure of a duplicate key.
func sumAmounts(recs []model.Transaction) money.Money {
	if len(recs) == 0 {
		return money.Money{}
	}
	total := recs[0].Amount
	for _, r := range recs[1:] {
		if sum, err := total.Add(r.Amount); err == nil {
			total = sum
		}
	}
	return total
}

// severityForImpact escalates a base severity according to the absolute monetary
// impact and the configured thresholds. It never de-escalates below the base.
func severityForImpact(base model.Severity, impact money.Money, cfg Config) model.Severity {
	abs := impact.Abs().Amount()
	switch {
	case cfg.CriticalImpactMinor > 0 && abs >= cfg.CriticalImpactMinor:
		return model.SeverityCritical
	case cfg.HighImpactMinor > 0 && abs >= cfg.HighImpactMinor:
		return maxSeverity(base, model.SeverityHigh)
	default:
		return base
	}
}

var severityRank = map[model.Severity]int{
	model.SeverityLow:      0,
	model.SeverityMedium:   1,
	model.SeverityHigh:     2,
	model.SeverityCritical: 3,
}

func maxSeverity(a, b model.Severity) model.Severity {
	if severityRank[a] >= severityRank[b] {
		return a
	}
	return b
}

// index groups records by match key, preserving input order within a key.
func index(txns []model.Transaction) map[string][]model.Transaction {
	m := make(map[string][]model.Transaction, len(txns))
	for _, t := range txns {
		m[t.MatchKey] = append(m[t.MatchKey], t)
	}
	return m
}

func duplicateKeys(byKey map[string][]model.Transaction) map[string]bool {
	dups := make(map[string]bool)
	for key, recs := range byKey {
		if len(recs) > 1 {
			dups[key] = true
		}
	}
	return dups
}

// newDiscrepancy builds a Discrepancy with a deterministic ID so the same finding
// is stable across runs and de-duplicable by downstream consumers.
func newDiscrepancy(typ model.DiscrepancyType, sev model.Severity, key string,
	psp, ledger *model.Transaction, impact money.Money, detail string) model.Discrepancy {
	return model.Discrepancy{
		ID:             discrepancyID(typ, key, psp, ledger),
		Type:           typ,
		Severity:       sev,
		MatchKey:       key,
		PSPRecord:      psp,
		LedgerRecord:   ledger,
		MonetaryImpact: impact,
		Detail:         detail,
	}
}

func discrepancyID(typ model.DiscrepancyType, key string, psp, ledger *model.Transaction) string {
	var pspID, ledgerID string
	if psp != nil {
		pspID = psp.ExternalID
	}
	if ledger != nil {
		ledgerID = ledger.ExternalID
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s", typ, key, pspID, ledgerID)))
	return "disc_" + hex.EncodeToString(sum[:6])
}
