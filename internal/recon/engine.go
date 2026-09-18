// Package recon orchestrates a reconciliation run: it drives the matcher, then
// aggregates the findings into a sorted, summarized report.
package recon

import (
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/matcher"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/model"
	"github.com/jatin-gl/payment-reconciliation-engine/pkg/money"
)

// Engine runs reconciliations. Now and NewID are injectable so runs are
// deterministic under test; the zero value is not usable — construct with New.
//
// An Engine built with New is safe for concurrent use by multiple goroutines
// (the HTTP server shares a single Engine across requests): its config and
// clock are read-only after construction, and the default id generator uses an
// atomic counter. If you inject a custom NewID, make it concurrency-safe too.
type Engine struct {
	Config matcher.Config
	Now    func() time.Time
	NewID  func() string
}

// New returns an Engine with the given config and wall-clock defaults.
func New(cfg matcher.Config) *Engine {
	var seq atomic.Int64
	return &Engine{
		Config: cfg,
		Now:    time.Now,
		NewID: func() string {
			return fmt.Sprintf("rpt_%d_%d", time.Now().UnixNano(), seq.Add(1))
		},
	}
}

// Reconcile compares the two record sets and returns a fully populated report.
func (e *Engine) Reconcile(psp, ledger []model.Transaction) model.Report {
	discrepancies := matcher.Reconcile(psp, ledger, e.Config)
	sortDiscrepancies(discrepancies)

	currency := inferCurrency(psp, ledger)
	report := model.Report{
		ReportID:        e.NewID(),
		ContractVersion: model.ContractVersion,
		GeneratedAt:     e.Now().UTC(),
		Currency:        currency,
		Discrepancies:   discrepancies,
		Summary:         buildSummary(psp, ledger, discrepancies, currency),
	}
	return report
}

func buildSummary(psp, ledger []model.Transaction, discs []model.Discrepancy, currency string) model.Summary {
	bySeverity := map[model.Severity]int{}
	byType := map[model.DiscrepancyType]int{}
	total := money.Zero(currency)
	discKeys := map[string]bool{}

	for _, d := range discs {
		bySeverity[d.Severity]++
		byType[d.Type]++
		discKeys[d.MatchKey] = true
		if d.MonetaryImpact.Currency() == currency {
			// Sum absolute impact; cross-currency findings (should be rare) are
			// excluded from the single-currency total to avoid meaningless math.
			if sum, err := total.Add(d.MonetaryImpact.Abs()); err == nil {
				total = sum
			}
		}
	}

	return model.Summary{
		PSPCount:            len(psp),
		LedgerCount:         len(ledger),
		MatchedCount:        matchedCount(psp, ledger, discKeys),
		DiscrepancyCount:    len(discs),
		BySeverity:          bySeverity,
		ByType:              byType,
		TotalMonetaryImpact: total,
	}
}

// matchedCount is the number of business keys that appear exactly once on each
// side and produced no discrepancy — the cleanly reconciled transactions.
func matchedCount(psp, ledger []model.Transaction, discKeys map[string]bool) int {
	pspKeys := keyCounts(psp)
	ledgerKeys := keyCounts(ledger)
	n := 0
	for key, c := range pspKeys {
		if c == 1 && ledgerKeys[key] == 1 && !discKeys[key] {
			n++
		}
	}
	return n
}

func keyCounts(txns []model.Transaction) map[string]int {
	m := make(map[string]int, len(txns))
	for _, t := range txns {
		m[t.MatchKey]++
	}
	return m
}

func inferCurrency(psp, ledger []model.Transaction) string {
	if len(psp) > 0 {
		return psp[0].Amount.Currency()
	}
	if len(ledger) > 0 {
		return ledger[0].Amount.Currency()
	}
	return ""
}

var severityRank = map[model.Severity]int{
	model.SeverityCritical: 0,
	model.SeverityHigh:     1,
	model.SeverityMedium:   2,
	model.SeverityLow:      3,
}

// sortDiscrepancies orders findings most-actionable first: by severity, then by
// larger absolute monetary impact, then by type and ID for a stable total order.
func sortDiscrepancies(d []model.Discrepancy) {
	sort.SliceStable(d, func(i, j int) bool {
		if severityRank[d[i].Severity] != severityRank[d[j].Severity] {
			return severityRank[d[i].Severity] < severityRank[d[j].Severity]
		}
		ii, jj := d[i].MonetaryImpact.Abs().Amount(), d[j].MonetaryImpact.Abs().Amount()
		if ii != jj {
			return ii > jj
		}
		if d[i].Type != d[j].Type {
			return d[i].Type < d[j].Type
		}
		return d[i].ID < d[j].ID
	})
}
