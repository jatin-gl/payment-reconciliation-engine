package model

import (
	"time"

	"github.com/jatin-gl/payment-reconciliation-engine/pkg/money"
)

// ContractVersion is the semantic version of the discrepancy data contract
// emitted by the engine and consumed by downstream systems. Bump the major
// version on any breaking change to the wire format.
const ContractVersion = "1.0"

// Summary is the aggregate view of a reconciliation run.
type Summary struct {
	PSPCount            int
	LedgerCount         int
	MatchedCount        int
	DiscrepancyCount    int
	BySeverity          map[Severity]int
	ByType              map[DiscrepancyType]int
	TotalMonetaryImpact money.Money
}

// Report is the full result of a reconciliation run: the summary plus every
// discrepancy, ordered by the engine from most to least severe.
type Report struct {
	ReportID        string
	ContractVersion string
	GeneratedAt     time.Time
	Currency        string
	Summary         Summary
	Discrepancies   []Discrepancy
}
