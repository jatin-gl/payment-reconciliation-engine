// Package ingest parses raw source files (PSP settlement exports, internal
// ledger dumps) into the normalized model.Transaction form the engine consumes.
//
// CSV layouts differ per provider, so parsing is driven by a ColumnMap that
// names which header each field lives under. Amounts are parsed straight into
// integer minor units; a row with a malformed amount is a hard error, never a
// silent zero, because a dropped or mis-parsed amount would hide a real finding.
package ingest

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/model"
	"github.com/jatin-gl/payment-reconciliation-engine/pkg/money"
)

// ColumnMap maps model fields to the CSV header names in a particular file.
// Amount and Currency are required; Fee, Status, and Timestamp are optional and
// skipped when their header name is empty.
type ColumnMap struct {
	MatchKey   string
	ExternalID string
	Amount     string
	Fee        string
	Currency   string
	Status     string
	Timestamp  string
}

// DefaultPSPColumns is a reasonable default layout for a PSP settlement export.
func DefaultPSPColumns() ColumnMap {
	return ColumnMap{
		MatchKey:   "transaction_id",
		ExternalID: "transaction_id",
		Amount:     "amount",
		Fee:        "fee",
		Currency:   "currency",
		Status:     "status",
		Timestamp:  "settled_at",
	}
}

// DefaultLedgerColumns is a reasonable default layout for an internal ledger export.
func DefaultLedgerColumns() ColumnMap {
	return ColumnMap{
		MatchKey:   "psp_reference",
		ExternalID: "entry_id",
		Amount:     "amount",
		Fee:        "fee",
		Currency:   "currency",
		Status:     "status",
		Timestamp:  "booked_at",
	}
}

// DefaultStatusMap normalizes common provider status vocabularies onto model.Status.
func DefaultStatusMap() map[string]model.Status {
	return map[string]model.Status{
		"pending":    model.StatusPending,
		"authorized": model.StatusAuthorized,
		"captured":   model.StatusCaptured,
		"paid":       model.StatusCaptured,
		"settled":    model.StatusSettled,
		"payout":     model.StatusSettled,
		"refunded":   model.StatusRefunded,
		"refund":     model.StatusRefunded,
		"chargeback": model.StatusChargeback,
		"failed":     model.StatusFailed,
		"declined":   model.StatusFailed,
	}
}

// timestampLayouts are tried in order; the first that parses wins.
var timestampLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// ParseCSV reads a CSV from r and returns the parsed transactions. The first row
// must be a header. Any row-level error is returned with its 1-based row number
// so the operator can find the offending line in the source file.
func ParseCSV(r io.Reader, source model.Source, cm ColumnMap, statusMap map[string]model.Status) ([]model.Transaction, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	// Strip a leading UTF-8 BOM that Excel/Windows exports commonly prepend to
	// the first cell; without this the first column name never matches its mapping.
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}
	col := make(map[string]int, len(header))
	for i, h := range header {
		name := strings.TrimSpace(h)
		if _, dup := col[name]; dup {
			// A duplicate header would silently shadow one column with another and
			// mis-parse amounts; fail loudly instead.
			return nil, fmt.Errorf("duplicate column %q in header", name)
		}
		col[name] = i
	}

	// Required columns must be present up front, so a misconfigured mapping fails
	// loudly on the first read rather than per-row.
	for name, want := range map[string]string{"amount": cm.Amount, "currency": cm.Currency, "match_key": cm.MatchKey} {
		if _, ok := col[want]; !ok {
			return nil, fmt.Errorf("required %s column %q not found; header has %v", name, want, header)
		}
	}

	var out []model.Transaction
	rowNum := 1 // header was row 1
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		rowNum++
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", rowNum, err)
		}
		txn, err := rowToTransaction(rec, col, source, cm, statusMap)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", rowNum, err)
		}
		out = append(out, txn)
	}
	return out, nil
}

func rowToTransaction(rec []string, col map[string]int, source model.Source, cm ColumnMap, statusMap map[string]model.Status) (model.Transaction, error) {
	get := func(header string) string {
		idx, ok := col[header]
		if !ok || idx >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[idx])
	}

	matchKey := get(cm.MatchKey)
	if matchKey == "" {
		// A blank reference cannot be reconciled against the other side; treating
		// several blank keys as "duplicates of empty" would be misleading.
		return model.Transaction{}, fmt.Errorf("empty match key")
	}

	currency := get(cm.Currency)
	if currency == "" {
		return model.Transaction{}, fmt.Errorf("empty currency")
	}

	amountMinor, err := money.ParseMinor(get(cm.Amount), currency)
	if err != nil {
		return model.Transaction{}, fmt.Errorf("amount: %w", err)
	}

	fee := money.Zero(currency)
	if cm.Fee != "" {
		if raw := get(cm.Fee); raw != "" {
			feeMinor, err := money.ParseMinor(raw, currency)
			if err != nil {
				return model.Transaction{}, fmt.Errorf("fee: %w", err)
			}
			fee = money.New(feeMinor, currency)
		}
	}

	rawStatus := get(cm.Status)
	status := model.StatusUnknown
	if cm.Status != "" {
		if s, ok := statusMap[strings.ToLower(rawStatus)]; ok {
			status = s
		}
	}

	var ts time.Time
	if cm.Timestamp != "" {
		if raw := get(cm.Timestamp); raw != "" {
			ts, err = parseTimestamp(raw)
			if err != nil {
				return model.Transaction{}, fmt.Errorf("timestamp: %w", err)
			}
		}
	}

	raw := make(map[string]string, len(col))
	for h, idx := range col {
		if idx < len(rec) {
			raw[h] = strings.TrimSpace(rec[idx])
		}
	}

	return model.Transaction{
		MatchKey:   matchKey,
		ExternalID: get(cm.ExternalID),
		Source:     source,
		Amount:     money.New(amountMinor, currency),
		Fee:        fee,
		Status:     status,
		RawStatus:  rawStatus,
		Timestamp:  ts,
		Raw:        raw,
	}, nil
}

func parseTimestamp(s string) (time.Time, error) {
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", s)
}
