package model

import "github.com/jatin-gl/payment-reconciliation-engine/pkg/money"

// DiscrepancyType enumerates the kinds of mismatch the engine can detect. The
// values are stable strings because they are part of the data contract consumed
// by downstream systems (dashboards, the AI dispute-resolution agent).
type DiscrepancyType string

const (
	// MissingInLedger: the PSP reported a transaction the ledger never recorded.
	MissingInLedger DiscrepancyType = "MISSING_IN_LEDGER"
	// MissingInPSP: the ledger has a transaction the PSP settlement never listed.
	MissingInPSP DiscrepancyType = "MISSING_IN_PSP"
	// AmountMismatch: both sides matched on key but the gross amounts differ.
	AmountMismatch DiscrepancyType = "AMOUNT_MISMATCH"
	// FeeMismatch: both sides matched but the reported processor fee differs.
	FeeMismatch DiscrepancyType = "FEE_MISMATCH"
	// StatusMismatch: both sides matched but the lifecycle status differs.
	StatusMismatch DiscrepancyType = "STATUS_MISMATCH"
	// CurrencyMismatch: both sides matched but the currency differs — always high risk.
	CurrencyMismatch DiscrepancyType = "CURRENCY_MISMATCH"
	// DuplicateInPSP: the same match key appears more than once on the PSP side.
	DuplicateInPSP DiscrepancyType = "DUPLICATE_IN_PSP"
	// DuplicateInLedger: the same match key appears more than once in the ledger.
	DuplicateInLedger DiscrepancyType = "DUPLICATE_IN_LEDGER"
)

// Severity ranks how much attention a discrepancy warrants. It is derived from
// the type and the monetary impact by the detector, and drives triage ordering
// both in the report and in the downstream agent's work queue.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Discrepancy is a single reconciliation finding.
type Discrepancy struct {
	// ID is a deterministic identifier derived from the type and match key, so
	// the same finding across runs is stable and de-duplicable downstream.
	ID       string
	Type     DiscrepancyType
	Severity Severity
	MatchKey string
	// PSPRecord and LedgerRecord are the records involved. Either may be nil for
	// one-sided findings (a MISSING_IN_LEDGER has no ledger record).
	PSPRecord    *Transaction
	LedgerRecord *Transaction
	// MonetaryImpact is the signed money-at-risk this finding represents. For a
	// missing transaction it is the full amount; for an amount mismatch it is the
	// difference (PSP minus ledger).
	MonetaryImpact money.Money
	// Detail is a human-readable one-line explanation.
	Detail string
}
