// Package model defines the core domain types for reconciliation: the payment
// records that flow in from each source, the discrepancies the engine detects,
// and the report that aggregates them.
package model

import (
	"time"

	"github.com/jatin-gl/payment-reconciliation-engine/pkg/money"
)

// Source identifies which system a record came from.
type Source string

const (
	// SourcePSP is the payment service provider / gateway settlement file.
	SourcePSP Source = "psp"
	// SourceLedger is the merchant's internal ledger.
	SourceLedger Source = "ledger"
)

// Status is the normalized lifecycle state of a payment. Different providers use
// different vocabularies; ingest adapters map them onto this closed set so the
// matcher can compare statuses meaningfully.
type Status string

const (
	StatusPending  Status = "pending"
	StatusCaptured Status = "captured"
	StatusSettled  Status = "settled"
	StatusRefunded Status = "refunded"
	StatusFailed   Status = "failed"
	StatusUnknown  Status = "unknown"
)

// Transaction is a single payment record from one source.
type Transaction struct {
	// MatchKey is the shared business reference used to line records up across
	// the two sources (e.g. the merchant order reference the PSP echoes back).
	MatchKey string
	// ExternalID is the source system's own row identifier, retained for tracing.
	ExternalID string
	Source     Source
	// Amount is the gross transaction amount.
	Amount money.Money
	// Fee is the processor fee. The ledger side often reports this as zero.
	Fee    money.Money
	Status Status
	// Timestamp is when the source system recorded the transaction.
	Timestamp time.Time
	// Raw preserves the original parsed row so a human (or the downstream AI
	// agent) can audit exactly what the source reported.
	Raw map[string]string
}

// Net returns Amount minus Fee. It errors only on a currency mismatch between
// the two, which would indicate a malformed record rather than a real net value.
func (t Transaction) Net() (money.Money, error) {
	return t.Amount.Sub(t.Fee)
}
