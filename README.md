# Payment Reconciliation Engine

[![CI](https://github.com/jatin-gl/payment-reconciliation-engine/actions/workflows/ci.yml/badge.svg)](https://github.com/jatin-gl/payment-reconciliation-engine/actions/workflows/ci.yml)
[![Go 1.23+](https://img.shields.io/badge/go-1.23%2B-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

A fast, deterministic reconciliation engine that compares a **PSP / gateway
settlement file** against an **internal ledger**, matches transactions on a
shared reference, and reports every discrepancy — missing transactions, amount
and fee drift, status mismatches, currency mismatches, and duplicates — ranked
by severity and money at risk.

It ships as both a **CLI** (drop into a nightly cron / CI job) and an **HTTP
service**, and emits a stable, versioned JSON [data contract](docs/data-contract.md)
designed for downstream automation. Its companion project,
[**recon-dispute-agent**](https://github.com/jatin-gl/recon-dispute-agent), is an
AI agent that consumes this output and investigates each discrepancy.

> **Why this exists.** Reconciliation is where payments quietly break: a
> settlement lands a cent short, a refund never books, a webhook double-fires and
> a charge settles twice. This engine turns two files into an actionable,
> ranked list of exactly what doesn't line up — with money handled as integer
> minor units so a one-cent difference is a real finding, never a rounding
> artifact.

---

## Highlights

- **Correct money handling.** Every amount is `int64` minor units + ISO-4217
  currency ([`pkg/money`](pkg/money/money.go)). No floats touch money anywhere in
  the codebase; cross-currency arithmetic is an error, not a silent coercion.
- **Eight discrepancy types**, each severity-ranked and escalated by monetary
  impact against configurable thresholds.
- **Deterministic & explainable.** Exact key-based matching; every finding has a
  stable id and points at a specific transaction reference.
- **CLI + REST**, sharing one engine. The CLI can gate a job with a non-zero exit
  code when discrepancies are found.
- **Stable versioned JSON contract** ([spec](docs/data-contract.md)) — the
  integration point for dashboards, alerting, and the AI dispute agent.
- **Tested to production standards**: table-driven unit tests, an
  ingest→engine→API integration path over committed fixtures, `-race`, and CI
  that enforces gofmt, `go vet`, and `staticcheck`.

## Quickstart

Requires Go 1.23+.

```bash
git clone https://github.com/jatin-gl/payment-reconciliation-engine
cd payment-reconciliation-engine

# Reconcile the committed sample files
make run-example
```

Output:

```
Reconciliation report rpt_… (contract v1.0)

  PSP records:      7
  Ledger records:   6
  Cleanly matched:  1
  Discrepancies:    6
  Money at risk:    306.50 USD

Findings (most severe first):
  [high    ] DUPLICATE_IN_PSP   TXN-1007   match key "TXN-1007" appears 2 times in PSP settlement (total 180.00 USD)
  [high    ] MISSING_IN_PSP     TXN-1004   ledger transaction L-4 (75.00 USD) has no PSP settlement entry
  [high    ] MISSING_IN_LEDGER  TXN-1003   PSP transaction TXN-1003 (50.00 USD) has no ledger entry
  [medium  ] AMOUNT_MISMATCH    TXN-1002   amount mismatch: PSP 200.00 USD vs ledger 199.00 USD
  [medium  ] STATUS_MISMATCH    TXN-1006   status mismatch: PSP "settled" vs ledger "refunded"
  [low     ] FEE_MISMATCH       TXN-1005   fee mismatch: PSP 5.00 USD vs ledger 4.50 USD
```

## CLI

```bash
go build -o bin/reconcile ./cmd/reconcile

bin/reconcile \
  --psp    testdata/psp_settlement.csv \
  --ledger testdata/internal_ledger.csv \
  --format json \
  --out    report.json
```

| Flag | Default | Description |
|---|---|---|
| `--psp` | — | Path to the PSP settlement CSV (required) |
| `--ledger` | — | Path to the internal ledger CSV (required) |
| `--format` | `text` | `text` or `json` |
| `--out` | stdout | Write output to this file |
| `--high` | `10000` | High-impact threshold, minor units (\$100) |
| `--critical` | `100000` | Critical-impact threshold, minor units (\$1,000) |
| `--fail-on-discrepancy` | `false` | Exit with code `2` if any discrepancy is found — gate a cron/CI job |

Exit codes: `0` clean, `1` usage/runtime error, `2` discrepancies found (with `--fail-on-discrepancy`).

## HTTP service

```bash
make run-server               # listens on :8080

curl -s http://localhost:8080/healthz

curl -s -X POST http://localhost:8080/v1/reconcile \
  -F psp=@testdata/psp_settlement.csv \
  -F ledger=@testdata/internal_ledger.csv | jq .summary
```

| Method & path | Description |
|---|---|
| `GET /healthz` | Liveness + contract version |
| `POST /v1/reconcile` | multipart form with `psp` and `ledger` CSV file fields → JSON report |

### Docker

```bash
docker build -t payment-reconciliation-engine .
docker run -p 8080:8080 payment-reconciliation-engine
```

The image is a static binary on `distroless/static` running as non-root.

## Input format

Two CSVs with sensible default column layouts (configurable in
[`internal/ingest`](internal/ingest/csv.go)):

**PSP settlement** — `transaction_id, amount, fee, currency, status, settled_at`
**Internal ledger** — `psp_reference, entry_id, amount, fee, currency, status, booked_at`

The ledger's `psp_reference` is the shared key the two sides are matched on.
Provider status vocabularies (`paid`, `authorized`, `chargeback`, …) are
normalized onto a common set. A malformed amount is a hard error with its row
number — never a silent zero.

## Output

A stable, versioned JSON document. Full specification in
[docs/data-contract.md](docs/data-contract.md); a canonical example in
[docs/example-report.json](docs/example-report.json). Money is always
`{ "amount_minor": 21650, "currency": "USD" }` — integer minor units.

## How it works

Ingest → match → summarize → serialize, as a layered pipeline where the matching
core is a pure, exhaustively-tested function. See
[docs/architecture.md](docs/architecture.md) and
[docs/matching-algorithm.md](docs/matching-algorithm.md).

## Testing

```bash
make test    # go test ./... -race
make lint    # go vet + staticcheck
make cover   # writes coverage.html
```

The suite covers the money type's parsing edge cases, every discrepancy type via
table-driven matcher tests, severity escalation, CSV ingest error paths, an
ingest→engine→API integration path over the committed fixtures, the CLI
(text/json output and exit codes), and the JSON wire shape. CI additionally runs
`gofmt`, `go vet`, and `staticcheck`.

## Companion project

[**recon-dispute-agent**](https://github.com/jatin-gl/recon-dispute-agent) — an
AI agentic workflow (Claude + tool-calling) that ingests this engine's report,
investigates each discrepancy with tools, classifies the root cause, and proposes
a resolution with a verification loop. The two compose in one pipeline:

```bash
reconcile --psp settlement.csv --ledger ledger.csv --format json | recon-agent -
```

## License

MIT — see [LICENSE](LICENSE).
