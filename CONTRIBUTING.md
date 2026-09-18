# Contributing

Thanks for your interest in the project. This is a portfolio/reference codebase,
but it is built to production standards and contributions that keep it there are
welcome.

## Development setup

Requires Go 1.23+.

```bash
git clone https://github.com/jatin-gl/payment-reconciliation-engine
cd payment-reconciliation-engine
make test        # run the suite with the race detector
make run-example # reconcile the committed sample files
```

## Before opening a PR

Run the full local gate — CI runs the same checks:

```bash
make fmt   # gofmt
make lint  # go vet + staticcheck
make test  # go test ./... -race
```

## Standards

- **No floating-point money.** All monetary values go through `pkg/money`
  (integer minor units). A PR that introduces `float64` for an amount will not
  be merged.
- **The matcher stays pure.** `internal/matcher` must not perform I/O, read the
  clock, or use randomness — that is what keeps it exhaustively testable.
- **Every new discrepancy type or field is a contract change.** Update
  `internal/contract`, `docs/data-contract.md`, and bump `ContractVersion` if the
  change is breaking.
- **Tests accompany behavior.** New matching rules need table-driven tests;
  new endpoints need `httptest` coverage.
- Keep changes gofmt-clean and vet-clean.

## Commit messages

Use clear, imperative subject lines (e.g. "Add fee-tolerance matching option").
Explain the *why* in the body when it isn't obvious from the diff.
