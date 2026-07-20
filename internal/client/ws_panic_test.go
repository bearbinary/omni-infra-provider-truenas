package client

import (
	"testing"
)

// TestWS_PanicIncrementsCounterBeforeRepanic pins the ordering contract
// that WSGoroutinePanics.Add(1) happens BEFORE panic(r) in each of the
// three recover blocks (close_wait, read_loop, close_conn). A refactor
// that reordered the metric increment after the re-panic would defeat
// the whole point of the goroutine-panic-recurrence alert — the alert
// is a lie if the counter never gets incremented on a real panic.
//
// STATUS: deferred. Asserting this contract from black-box tests requires
// inducing a panic in each of the three goroutine sites, which in turn
// needs a fault-injection hook inside ws.go (an unexported var func the
// test can swap for a `func() { panic("test") }` in each site). Adding
// those hooks is a small but real API-shape change to production code
// for the sake of a test, and the pre-existing recover blocks are hand-
// eye-verified (see internal/client/ws.go lines 270-290, 393-410,
// 435-450 — each block does Add(1) BEFORE panic(r), lint pin: analyzer
// under internal/hack/lintconcurrency).
//
// If the fault-injection hooks are added, the test body should:
//
//  1. For each site in {close_wait, read_loop, close_conn}:
//     a. Snapshot WSGoroutinePanics counter (via sdkmetric.ManualReader).
//     b. defer/recover the test-goroutine panic.
//     c. Trigger the hook.
//     d. Assert counter incremented and re-panic value matches.
//
// Tracked in docs/concurrency-patterns.md § Metric-name convention.
func TestWS_PanicIncrementsCounterBeforeRepanic(t *testing.T) {
	t.Skip("requires ws.go fault-injection hooks — see doc-comment on this test for the scaffolding required. The hand-verified contract is: WSGoroutinePanics.Add(1) must precede panic(r) in every recover block, and the lintconcurrency analyzer flags any function-level extraction of that ceremony as a rejected pattern.")
}
