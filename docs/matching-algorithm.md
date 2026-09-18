# Matching Algorithm

The matcher (`internal/matcher`) takes PSP and ledger records and returns typed
discrepancies. This document explains the algorithm and the rules behind each
decision.

## Overview

```
1. Index both sides by match_key.
2. Detect duplicate keys on each side.
3. For each non-duplicate key:
     - present only in PSP     -> MISSING_IN_LEDGER
     - present only in ledger  -> MISSING_IN_PSP
     - present in both         -> compare fields
4. Rank each finding's severity, escalating by monetary impact.
```

The whole matcher is a pure function — same inputs always produce the same
findings — which is what makes it exhaustively testable.

## Step 1 — Indexing

Both slices are grouped into `map[match_key][]Transaction`, preserving input
order within a key. `match_key` is populated by the ingest layer from whichever
column carries the shared reference (the PSP transaction id on the PSP side, the
`psp_reference` column on the ledger side).

## Step 2 — Duplicates short-circuit comparison

If a key appears more than once on a side, the pairing is ambiguous — which of
the two PSP rows should we compare against the single ledger row? Rather than
guess, the engine emits a `DUPLICATE_IN_PSP` / `DUPLICATE_IN_LEDGER` finding for
that key and does **not** also emit amount/fee/status findings for it. This keeps
findings unambiguous and avoids double-reporting. Duplicates are a real and
important reconciliation problem (double-settlement, double-booking), so they are
surfaced as their own type. Their monetary impact is the **sum of all
occurrences' amounts** (the full double-counted exposure, not just the first
row), and that aggregate runs through the same severity escalation — so two
duplicated large settlements can be reported as `critical`.

## Step 3 — One-sided keys

A key present on only one side means money is unaccounted for:

- **`MISSING_IN_LEDGER`** — the PSP says it moved money the ledger never
  recorded. Impact = the full PSP amount.
- **`MISSING_IN_PSP`** — the ledger booked a transaction the PSP never settled.
  Impact = the full ledger amount.

## Step 3 — Field comparison for matched pairs

For a key present once on each side, fields are compared in this order:

1. **Currency.** If currencies differ, emit `CURRENCY_MISMATCH` (always
   `critical`) and stop — comparing amounts across currencies is meaningless.
2. **Amount.** If gross amounts differ, emit `AMOUNT_MISMATCH` with
   impact = PSP − ledger.
3. **Fee.** If processor fees differ, emit `FEE_MISMATCH` with
   impact = PSP fee − ledger fee.
4. **Status.** If the normalized lifecycle status differs, emit
   `STATUS_MISMATCH`.

A single pair can produce several findings at once (e.g. both amount and status
differ) — each is reported separately so downstream triage sees the full picture.

## Step 4 — Severity

Base severity per type, then escalation by absolute monetary impact:

| Type | Base | Escalation |
|---|---|---|
| `CURRENCY_MISMATCH` | critical | — |
| `MISSING_IN_LEDGER` / `MISSING_IN_PSP` | high | → critical at/above the critical threshold |
| `DUPLICATE_IN_*` | high | → critical by aggregate impact (sum of all occurrences) |
| `AMOUNT_MISMATCH` | medium | → high / critical by impact |
| `STATUS_MISMATCH` | medium | — |
| `FEE_MISMATCH` | low | → high / critical by impact |

Thresholds (`HighImpactMinor`, `CriticalImpactMinor`) are configurable
(`--high`, `--critical`; defaults \$100 and \$1,000). Escalation never
de-escalates below the base severity.

## Determinism

Each discrepancy's `id` is `disc_` + a SHA-256 prefix of
`type|match_key|psp_external_id|ledger_external_id`. The same finding therefore
gets the same id across runs, so consumers can de-duplicate and track a finding
over time. Findings are sorted by severity, then descending absolute impact, then
type and id — a stable total order.

## Out of scope for v1

- **Fuzzy matching** (match on amount + timestamp window when references are
  missing). Higher recall, lower determinism — a separate opt-in strategy.
- **Many-to-one matching** (a settlement batch that nets several ledger entries).
- **Multi-currency settlement** within a single run.
