# What adversarial review found in a green codebase

I had a payment-reconciliation engine (Go) and its companion AI agent (Python)
passing every check I'd written: `go vet`, `staticcheck`, `gofmt`, the full test
suite under the race detector, `ruff`, `mypy`. Coverage was healthy — 90%+ on the
core packages. By every surface metric, done.

Then I ran the code through a round of deliberately **adversarial** review — the
mandate was "assume there are bugs and prove them," not "check that it looks
fine." It found real defects. Two were serious. None were caught by the green
suite, and *that fact is the most useful thing I learned*: high line-coverage over
paths that dodge the hard cases is exactly the kind of green that hides bugs.

This is a walk through the findings that mattered, how each was reproduced, and
what I changed. I'm writing it up because the bugs are instructive and because
"here's a defect an independent review found and here's how I fixed it" is a more
honest signal of engineering maturity than a wall of passing tests.

## 1. A data race on the shared engine (HIGH)

The HTTP server constructs one reconciliation engine and shares it across every
request — which is the right call, the engine is meant to be stateless. Except the
default report-ID generator wasn't:

```go
func New(cfg matcher.Config) *Engine {
    seq := 0
    return &Engine{
        // ...
        NewID: func() string {
            seq++ // read-modify-write on a captured int, no synchronization
            return fmt.Sprintf("rpt_%d_%d", time.Now().UnixNano(), seq)
        },
    }
}
```

Every `POST /v1/reconcile` calls `Reconcile`, which calls `NewID()`, which does
`seq++`. Two concurrent requests race on `seq` — a classic unsynchronized
read-modify-write — and can produce colliding report IDs, violating the "unique ID
per run" guarantee the data contract advertises.

Why the green `-race` suite never saw it: **no test ever exercised the concurrent
path.** The race detector only reports races it actually observes at runtime, and
every existing test called `Reconcile` from a single goroutine. The bug lived in
the gap between "tested with `-race`" and "tested *concurrently* with `-race`."

Reproduction — 50 goroutines against one shared engine:

```go
e := New(matcher.DefaultConfig())
var wg sync.WaitGroup
for i := 0; i < 50; i++ {
    wg.Add(1)
    go func() { defer wg.Done(); _ = e.Reconcile(psp, nil) }()
}
wg.Wait()
```

Under `-race` this fires immediately:

```
WARNING: DATA RACE
  Read at ... recon.New.func1() engine.go:30
  Previous write at ... recon.New.func1() engine.go:30
```

Fix — an atomic counter, plus a committed concurrency test so CI's `-race` now
actually covers the shared-engine path:

```go
var seq atomic.Int64
// ...
NewID: func() string {
    return fmt.Sprintf("rpt_%d_%d", time.Now().UnixNano(), seq.Add(1))
},
```

**Lesson:** "runs under `-race`" is not the same claim as "is race-free." The race
detector is only as good as the concurrency your tests actually create. If you
ship a shared, concurrently-accessed object, you need a test that hits it from
many goroutines — otherwise the tooling gives you false confidence.

## 2. Exact money that was 100× wrong for the yen (HIGH)

The whole point of the money type is exactness: every amount is an `int64` in the
currency's minor unit, never a float, because a one-cent drift in reconciliation
is a real finding, not a rounding artifact. I'd even made the *rendering* layer
currency-aware — it knew that JPY has zero decimal places and Bahraini dinar has
three:

```go
func Exponent(currency string) int { /* JPY -> 0, BHD -> 3, default 2 */ }
func (m Money) String() string      { /* uses Exponent */ }
```

But the *parsing* layer never got the memo:

```go
func ParseMinor(s string) (int64, error) {
    // ...
    total := major*100 + minor // always ×100, regardless of currency
    // ...
}
```

So a ¥5000 settlement row — where the yen *is* the minor unit — parsed to
`500000`, and then rendered back as `500000 JPY`. A silent **100× inflation** on
every zero-decimal-currency amount. And a 3-decimal currency was worse: `1.234`
for BHD tripped the "more than 2 decimal places" check and the file was **rejected
outright** — you couldn't ingest dinar at all.

The embarrassing part: in an earlier pass I'd "verified" the JPY case by eyeballing
the *format* — "yes, no decimal point, looks right" — and completely missed that
the *value* was 100× too large. Checking the shape of the output instead of the
number is exactly the kind of shallow verification that lets a bug survive.

Reproduction:

```go
v, _ := ParseMinor("5000")          // JPY row for ¥5000
// v == 500000  -> renders "500000 JPY"  (should be 5000)
_, err := ParseMinor("1.234")       // BHD
// err: "more than 2 decimal places"  -> 3-decimal currency can't be ingested
```

Fix — make parsing currency/exponent-aware and thread the currency through
ingest, scaling by `10^Exponent(currency)`, tolerating trailing zeros beyond the
currency's precision but rejecting significant digits:

```go
func ParseMinor(s, currency string) (int64, error) {
    exp := Exponent(currency)
    // ...scale by 10^exp, validate fractional length against exp...
}
```

Now ¥5000 parses to `5000`, and BHD `1.234` parses to `1234`.

**Lesson two-for-one.** First: a half-applied fix is a bug generator — making the
*display* currency-aware while leaving *parsing* hard-coded created an internal
inconsistency that was worse than either being uniformly naive. Consistency across
layers matters. Second: verify the *value*, not the *shape*. "It renders without a
decimal point" is not the same test as "the number is correct."

## 3. The `int64` boundary (LOW, but real)

`Abs()` and `String()` both broke at `math.MinInt64`, which has no positive
`int64` counterpart:

```go
New(math.MinInt64, "USD").Abs().Amount() // -9223372036854775808  (still negative!)
New(math.MinInt64, "USD").String()       // "--92233720368547758.-8 USD"  (garbage)
```

`Abs` is used in severity classification and sorting, so a `MinInt64` impact would
mis-sort and mis-escalate. The parser's overflow guard prevents reaching this via
CSV, but `Money` values are constructed in other places, so it's a latent trap.
Fixed by saturating `Abs` at `MaxInt64` and rendering `String` from the decimal
string of the value rather than negating it (which can't overflow).

**Lesson:** signed integer boundaries (`MinInt64`, overflow on `+`/`-`/`*`) are the
first place to probe any integer-based numeric type. They're the edges tests
rarely visit and the exact spots where "obviously correct" arithmetic isn't.

## 4. The CLI crashed on real-world input (Python side)

Two uncaught tracebacks in the agent's CLI, both on inputs a real user hits:

- **A UTF-8 BOM.** The CLI read a file, then handed the content to a loader that
  decided "is this JSON or a file path?" by checking whether it starts with `{`. A
  BOM (`﻿`) isn't stripped by `lstrip()`, so BOM-prefixed content failed that
  check, got treated as a *path*, and the multi-KB JSON string was passed to
  `open()` → `OSError: File name too long` → uncaught traceback. Excel and Windows
  exports emit BOMs constantly.
- **A non-UTF-8 stdout.** The text output uses `⚠` and `✓` glyphs. Under an ASCII
  stdout (`PYTHONIOENCODING=ascii`, some redirected pipes), `print` raised
  `UnicodeEncodeError` — no output, nonzero exit, traceback.

Both were fixed by parsing the already-read content directly instead of routing it
back through a path heuristic (the double-interpretation was the deeper smell),
stripping a leading BOM, and reconfiguring stdout to UTF-8 with a byte-level
fallback.

**Lesson:** the edges of a CLI are input encoding and output encoding, and both are
where "works on my machine" quietly fails on someone else's. A loader that can
interpret its argument two different ways ("maybe JSON, maybe a path") is a design
smell — the ambiguity is where the bug hid.

## The meta-lesson

Every one of these passed a green suite with good coverage. The pattern connecting
them:

- The **data race** was invisible because coverage measured *lines executed*, not
  *concurrency exercised*.
- The **money bug** was invisible because the test checked output *shape*, not
  *value*, and never fed a non-2-decimal currency through the parse layer.
- The **`MinInt64`** bugs were invisible because no test visited the boundary.
- The **CLI crashes** were invisible because no test fed a BOM or a non-UTF-8
  stdout.

High line-coverage tells you which code *ran*, not whether it ran on the inputs
that break it. The bugs live in the cases the happy-path tests are structurally
designed to avoid: concurrency, numeric boundaries, currency edge cases, encoding
edge cases. Adversarial review — someone actively trying to construct the input
that breaks it — finds those in a way "does it pass CI?" never will.

After the fixes, the suites are green again — but now with a concurrency test, a
currency round-trip test, boundary tests, and CLI-encoding tests. The green means
more than it did before, because it now covers the paths that were actually
dangerous. That's the difference between coverage as a number and coverage as
evidence.
