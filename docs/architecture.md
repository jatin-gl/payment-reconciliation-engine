# Architecture

The engine is a small, layered pipeline. Each layer has one job and depends only
on the layers below it, so any stage can be tested in isolation and swapped
without touching the others.

```
                 ┌──────────────┐        ┌──────────────┐
   PSP CSV  ─────▶│              │        │              │
                  │  ingest      │──────▶ │   matcher    │──────▶ discrepancies
 Ledger CSV ─────▶│  (parse)     │  []Txn │  (classify)  │  []Discrepancy
                 └──────────────┘        └──────────────┘
                                                 │
                                                 ▼
                                          ┌──────────────┐
                                          │    recon     │  sort + summarize
                                          │  (engine)    │──────▶ model.Report
                                          └──────────────┘
                                                 │
                                                 ▼
                                          ┌──────────────┐
                                          │  contract    │  domain → stable JSON
                                          └──────────────┘
                                            ▲          ▲
                                    cmd/reconcile   internal/api
                                       (CLI)         (HTTP)
```

## Packages

| Package | Responsibility |
|---|---|
| `pkg/money` | Currency-safe integer-minor-unit money type. No floats, ever. Reusable outside this project. |
| `internal/model` | Domain types: `Transaction`, `Discrepancy`, `Report`, and the enums. No I/O. |
| `internal/ingest` | Parse source CSVs into `[]model.Transaction` via a configurable `ColumnMap`. Malformed amounts are hard errors. |
| `internal/matcher` | The matching engine. Pure function `Reconcile([]Txn, []Txn, Config) []Discrepancy`. No I/O, no clock. |
| `internal/recon` | Orchestration: drives the matcher, sorts findings, builds the summary, stamps report metadata. Clock and id generator are injectable for deterministic tests. |
| `internal/contract` | Domain → stable, versioned JSON wire types. Isolating this lets the domain evolve without breaking downstream consumers. |
| `internal/api` | HTTP surface: `/healthz` and `POST /v1/reconcile`. Thin — parses uploads, calls `recon`, serializes via `contract`. |
| `cmd/reconcile` | CLI entrypoint. |
| `cmd/server` | HTTP server entrypoint with graceful shutdown. |

## Design choices

- **Money never touches float.** Every amount is `int64` minor units plus a
  currency code. Adding across currencies is an error, not a silent coercion.
  See [`pkg/money`](../pkg/money/money.go).
- **The matcher is a pure function.** It takes slices in and returns findings
  out — no files, no time, no randomness. That makes the core logic exhaustively
  table-testable (see `matcher_test.go`).
- **Wire types are separate from domain types.** `internal/contract` is the only
  place JSON tags live, so the internal model can change freely while the
  published contract stays stable and explicitly versioned.
- **Determinism is designed in.** Discrepancy ids are a hash of
  `(type, match_key, external ids)`, so the same finding is stable across runs
  and de-duplicable downstream. The report clock and id generator are injected,
  so tests assert on exact output.

## Matching strategy

v1 uses **exact key-based matching**: records line up on a shared business
reference (`match_key`). This is deterministic and explainable — every finding
points to a specific key. Fuzzy/amount-window matching trades determinism for
recall and is intentionally left as a separate, opt-in strategy for a later
version rather than being the silent default. See
[matching-algorithm.md](./matching-algorithm.md).
