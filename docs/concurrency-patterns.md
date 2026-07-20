# Concurrency patterns

This project has been bitten by concurrency bugs that survived unit tests
and only surfaced in production — most recently the `wsTransport` WaitGroup
reuse panic (see `CHANGELOG.md` → *WebSocket `Close()` no longer races with
in-flight `Call`/`UploadFile`*). This doc names the three patterns we accept
for goroutine-owning types and the ones we reject, so future changes have a
default rather than reinventing lifecycle plumbing per package.

The default rule: **if a struct declares a `sync.WaitGroup` field, the same
package MUST ship a `*_lifecycle_test.go`, `*_stress_test.go`, or
`*_race_test.go`** exercising Close-during-work and repeat-Close scenarios.
`make lint-concurrency` enforces this in CI.

## Approved patterns

### Pattern 1 — `context.Context` cancellation (preferred)

When the semantic is "signal every worker to stop and refuse new work,"
prefer `context.Context` over a bespoke `closed bool + sync.Mutex`. `ctx.Err()`
is cheap and race-free by construction; there is no ordering dance between
"flip the flag" and "wait for in-flight."

```go
type Server struct {
    ctx    context.Context
    cancel context.CancelFunc
}

func (s *Server) Do(ctx context.Context, req Request) error {
    if err := s.ctx.Err(); err != nil {
        return err // server closed
    }
    return s.work(ctx, req)
}

func (s *Server) Close() {
    s.cancel()
}
```

Every worker `select { case <-ctx.Done(): return; ... }`. There is no
`sync.WaitGroup` and no `Add`-after-`Wait` race because there is no `Add`.

Use this for any new goroutine-owning type unless one of the alternatives
below is genuinely a better fit.

### Pattern 2 — `errgroup.Group` for scoped fan-out

`golang.org/x/sync/errgroup` wraps `sync.WaitGroup` with the correct
ordering (all `Add`s happen before `Wait`) and adds first-error propagation
and shared-ctx cancellation. Prefer this over hand-rolled WaitGroup any time
you have a bounded set of goroutines that must all complete before the
parent function returns.

```go
g, ctx := errgroup.WithContext(ctx)
for _, task := range tasks {
    task := task
    g.Go(func() error { return task.run(ctx) })
}
return g.Wait()
```

### Pattern 3 — `sync.WaitGroup` + colocated `closed` flag under one mutex

For the "unbounded incoming work + a single Close that must drain
in-flight" shape (the `wsTransport` shape) where callers pass their own
`ctx` and we need a transport-level close signal outliving any single
call's ctx:

```go
// Guarded by connMu:
closed bool
wg     sync.WaitGroup

// Close writer:      w.Lock() → closed=true → w.Unlock() → wg.Wait()
// Adder callsite:    r.RLock() → if closed { r.RUnlock(); return } → wg.Add(1) → r.RUnlock()
```

For the full ordering rationale, see the doc-comment on `wsTransport.Close`
in `internal/client/ws.go` — that comment is authoritative; this doc
paraphrases. Regression tests:
`internal/client/ws_lifecycle_test.go` (`TestWS_CloseDoesNotRaceWithConcurrentCalls`,
`TestWS_CloseIsIdempotent`, `TestWS_ConcurrentCloseAllWaitForDrain`).

## Rejected patterns

- **`atomic.Bool` for the `closed` flag + `sync.WaitGroup`.** Atomics let
  the check pass, then let `Close` win the race to `Wait` before the
  concurrent `Add` lands. The whole point of Pattern 3 is that the mutex
  imposes an ordering the atomic cannot.
- **Bare `sync.WaitGroup` with no `closed` guard.** Callers can `Add`
  after `Wait` returns; `sync: WaitGroup is reused before previous Wait
  has returned` panic follows. This was the bug Emil reported.
- **Extracted `t.safeGo()` wrapper.** Hides the `Add`/`Done` pairing from
  the caller's frame. Keeps the ceremony visible per call site instead —
  the noise is load-bearing. If you catch yourself extracting this, revisit.
- **`sync.Once` alone for idempotent `Close`.** `Once` guarantees the body
  runs once but does nothing about concurrent callers observing the
  in-progress close. Pair with a `closed` flag when idempotency matters.

## Testing goroutine-owning types

Every package with a `sync.WaitGroup` field must ship at least one of:

- `*_lifecycle_test.go` — Close-under-work, repeat-Close, Close-during-*
  scenarios.
- `*_stress_test.go` — high-concurrency workload with `-race`.
- `*_race_test.go` — targeted regression pin for a specific race.

The default stress-test shape:

```go
func TestFoo_CloseDoesNotRaceWithConcurrentBar(t *testing.T) {
    // Iterate 20× — a WaitGroup race can pass single-run under a lucky
    // scheduler. 20 iterations catches Emil-class regressions >99%.
    for iter := range 20 {
        t.Run(fmt.Sprintf("iter-%d", iter), func(t *testing.T) {
            // fresh instance per iter
            // spawn N workers doing tight-loop work
            // spawn M Close goroutines racing against the workers
            // deadline the whole test (e.g. 5s) so a genuine deadlock
            // fails loudly rather than hanging CI
        })
    }
}
```

CI hooks that enforce this:

- `make test` uses `-race` — catches actual races. `sync.WaitGroup`
  misuse patterns that `staticcheck` can spot statically (SA2000 family)
  are enforced via `.golangci.yml` (single owner; the earlier `-vet=all`
  in `go test` was removed to avoid a two-config drift risk).
- `make test-stress` iterates the concurrency-heavy packages 30× with
  `-race` — used as a pre-release gate for changes touching
  goroutine-owning packages. No `-run` filter: iteration is the source
  of truth.
- `make lint-concurrency` runs the go/analysis analyzer under
  `internal/hack/lintconcurrency` — enforces the "every WaitGroup /
  errgroup.Group owner has a lifecycle test" rule, and rejects any
  function literally named `safeGo`.
- `.github/workflows/ci.yaml` runs all three: `test` and
  `lint-concurrency` on every push; `race-stress` on push-to-main and
  on PRs whose diff touches concurrency-heavy paths (paths-filter, not
  a label).
- Prometheus alert rules are unit-tested via `promtool test rules
  deploy/observability/alerts/*_test.yml` — a typo in a label matcher
  now fails CI instead of shipping a silent no-op alert.

## Runtime observability

Even with all of the above, a novel race can slip in. Ship telemetry that
turns "silent crash-loop" into "warn log + counter + page":

- `defer recover()` on every long-lived goroutine, with a
  `truenas.<subsystem>.goroutine_panics{site=...}` counter incremented
  before the re-panic. `wsTransport` does this for its three lifecycle
  goroutines (`close_wait`, `read_loop`, `close_conn`).
- Prometheus alert on `increase(truenas_ws_goroutine_panics_total[5m]) > 0`
  — pages immediately on recurrence. Alert on
  `changes(truenas_provider_start_time_seconds[5m]) >= 2` as a companion to
  catch sub-export crashes where the panic counter never gets exported.
  (The gauge is provider-owned and OTLP-pushed; the Prometheus-client
  `process_start_time_seconds` series never exists in this pipeline.)

The recover-log-metric block is inline per goroutine site — see
*Rejected patterns* for why we do NOT extract a shared `safeGo` helper.

**Caveat: release externally-visible state before re-panicking.**
If the recovering goroutine owns a lease, session, or lock on a peer
system, release that state INSIDE the recover block before calling
`panic(r)`. Example: a singleton-lease goroutine's recover block would
need `s.releaseLease(ctx)` before the re-panic. The re-panic is the
right death signal; the leaked state is not.

### Metric-name convention

Every long-lived-goroutine owner MUST register its panic counter under
the shape:

```
truenas.<subsystem>.goroutine_panics   (Int64Counter, label: site)
```

The panic-recurrence alert filters by metric name, so a new subsystem
that ships a differently-named counter is a silent no-op from an alerting
perspective. When adding a new subsystem:

1. Register the counter in `internal/telemetry/metrics.go` under the
   `truenas.<subsystem>.goroutine_panics` name.
2. Update
   `deploy/observability/alerts/truenas-provider.rules.yml` to add a
   corresponding `TrueNAS<Subsystem>GoroutinePanicRecurrence` alert.
3. Do NOT add the panic value or stack as a metric label — those are
   unbounded strings and would explode TSDB cardinality on any
   recurring panic. The panic value belongs in the structured log line
   at the recover site (`slog.Any("panic", r)` or `zap.Any("panic", r)`),
   not the metric.

## References

- `CHANGELOG.md` → *Reliability* → WebSocket Close race entry
- `internal/client/ws.go` — Pattern 3 reference implementation
- `internal/client/ws_lifecycle_test.go` — stress-test conventions
- `internal/hack/lintconcurrency/` — the go/analysis linter
- `deploy/observability/alerts/truenas-provider.rules.yml` — recurrence alerts
- `deploy/observability/alerts/truenas-provider_test.yml` — promtool unit tests
