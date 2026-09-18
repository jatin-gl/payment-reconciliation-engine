# Discrepancy Data Contract (v1.0)

This document specifies the JSON the engine emits (`reconcile --format json`, or
`POST /v1/reconcile`). It is the stable interface consumed by downstream
systems — dashboards, alerting, and the companion
[`recon-dispute-agent`](https://github.com/jatin-gl/recon-dispute-agent), which
reads this exact shape.

The contract is versioned by `contract_version`. The current version is **1.0**.
Any breaking change (removing/renaming a field, changing a type, changing an enum
value's meaning) bumps the major version. Additive fields do not.

A canonical example is committed at [`example-report.json`](./example-report.json).

## Money is always integer minor units

Every monetary value is an object, never a float:

```json
{ "amount_minor": 21650, "currency": "USD" }
```

`amount_minor` is the value in the currency's minor unit (cents for USD/EUR,
paise for INR). `21650` with `"USD"` means **\$216.50**. Consumers must not parse
`amount_minor` as a decimal or apply floating-point math to it — the whole point
of the engine is that a one-cent drift is a real, detectable finding.

## Top-level object

| Field | Type | Notes |
|---|---|---|
| `report_id` | string | Unique id for this run |
| `contract_version` | string | Semantic version of this contract (`"1.0"`) |
| `generated_at` | string | RFC 3339 UTC timestamp |
| `currency` | string | ISO-4217 currency of the reconciliation |
| `summary` | object | Aggregate counts and totals (below) |
| `discrepancies` | array | Findings, ordered most-severe first |

### `summary`

| Field | Type | Notes |
|---|---|---|
| `psp_count` | int | Records read from the PSP settlement file |
| `ledger_count` | int | Records read from the internal ledger |
| `matched_count` | int | Keys present once on each side with no discrepancy |
| `discrepancy_count` | int | `len(discrepancies)` |
| `by_severity` | object | `{severity: count}` |
| `by_type` | object | `{type: count}` |
| `total_monetary_impact` | Money | Sum of absolute impacts, single currency |

### `discrepancies[]`

| Field | Type | Notes |
|---|---|---|
| `id` | string | Deterministic id — stable across identical runs, de-duplicable |
| `type` | string (enum) | See discrepancy types below |
| `severity` | string (enum) | `low` \| `medium` \| `high` \| `critical` |
| `match_key` | string | The shared business reference the records were matched on |
| `psp_record` | Transaction \| absent | The PSP record involved (omitted for one-sided ledger findings) |
| `ledger_record` | Transaction \| absent | The ledger record involved (omitted for one-sided PSP findings) |
| `monetary_impact` | Money | Signed money-at-risk this finding represents |
| `detail` | string | Human-readable one-line explanation |

### Transaction

| Field | Type | Notes |
|---|---|---|
| `match_key` | string | |
| `external_id` | string | Source system's own row id |
| `source` | string | `psp` \| `ledger` |
| `amount` | Money | Gross amount |
| `fee` | Money | Processor fee (often `0` on the ledger side) |
| `status` | string | Normalized: `pending` `authorized` `captured` `settled` `refunded` `chargeback` `failed` `unknown` |
| `raw_status` | string | The source's original, un-normalized status; omitted when the source had none. Distinguishes two records whose normalized `status` is both `unknown`. |
| `timestamp` | string | RFC 3339; omitted when the source had none |

## Discrepancy types

| `type` | Meaning | `monetary_impact` |
|---|---|---|
| `MISSING_IN_LEDGER` | PSP reported it; ledger never recorded it | full PSP amount |
| `MISSING_IN_PSP` | Ledger has it; PSP settlement never listed it | full ledger amount |
| `AMOUNT_MISMATCH` | Matched, gross amounts differ | PSP amount − ledger amount |
| `FEE_MISMATCH` | Matched, processor fee differs | PSP fee − ledger fee |
| `STATUS_MISMATCH` | Matched, lifecycle status differs | 0 |
| `CURRENCY_MISMATCH` | Matched, currency differs (always `critical`) | 0 |
| `DUPLICATE_IN_PSP` | Same key appears more than once in the PSP file | sum of all occurrences' amounts |
| `DUPLICATE_IN_LEDGER` | Same key appears more than once in the ledger | sum of all occurrences' amounts |

## Severity

Severity drives triage order (in the report and in the agent's work queue).
It is derived from the discrepancy type and, for monetary findings, escalated by
the absolute `monetary_impact` against configurable thresholds
(`--high`, `--critical`; defaults \$100 and \$1,000). Currency mismatches are
always `critical`.
