package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- controllable fake middleware for lifecycle tests ---

// controllableMiddleware lets a test deterministically block or drop
// responses. Response for each method call is driven by the `respond`
// callback, which receives the request ID and method and decides what to
// write (if anything).
type controllableMiddleware struct {
	// respond is invoked for every "method" msg after auth. Returning ok=false
	// tells the handler to simply not respond (simulates a hang). The response
	// bytes are written to the websocket verbatim.
	respond func(conn *websocket.Conn, method, id string) (ok bool)

	// onConnect, if set, runs after the initial connect handshake.
	onConnect func(conn *websocket.Conn)
}

func (m *controllableMiddleware) handler(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var req map[string]any
		if err := json.Unmarshal(msg, &req); err != nil {
			return
		}

		msgType, _ := req["msg"].(string)
		method, _ := req["method"].(string)
		id, _ := req["id"].(string)

		switch msgType {
		case "connect":
			resp, _ := json.Marshal(map[string]any{"msg": "connected", "session": "test"})
			_ = conn.WriteMessage(websocket.TextMessage, resp)

			if m.onConnect != nil {
				m.onConnect(conn)
			}

		case "method":
			if method == "auth.login_with_api_key" {
				resp, _ := json.Marshal(map[string]any{"msg": "result", "id": id, "result": true})
				_ = conn.WriteMessage(websocket.TextMessage, resp)

				continue
			}

			if m.respond != nil {
				_ = m.respond(conn, method, id)

				continue
			}

			// Default: echo an ok result.
			result, _ := json.Marshal(map[string]any{"ok": true})
			resp, _ := json.Marshal(map[string]any{"msg": "result", "id": id, "result": json.RawMessage(result)})
			_ = conn.WriteMessage(websocket.TextMessage, resp)
		}
	}
}

func startControllable(t *testing.T, m *controllableMiddleware) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(server.Close)

	return strings.TrimPrefix(server.URL, "http://")
}

// --- tests ---

// TestWS_PendingMapCleanupOnCtxCancel pins the invariant that a cancelled
// call removes its entry from the pending map. Without the defer cleanup,
// the map would accumulate entries forever and eventually deliver late
// responses to stale waiters.
func TestWS_PendingMapCleanupOnCtxCancel(t *testing.T) {
	t.Parallel()

	// Middleware never responds to method calls — waiters always cancel.
	m := &controllableMiddleware{
		respond: func(_ *websocket.Conn, _, _ string) bool { return false },
	}
	host := startControllable(t, m)

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)
	defer transport.Close()

	const calls = 50

	var wg sync.WaitGroup

	for range calls {
		wg.Add(1)

		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()

			var out map[string]any
			_ = transport.Call(ctx, "system.info", nil, &out)
		}()
	}

	wg.Wait()

	// Allow the cleanup defers to run.
	time.Sleep(50 * time.Millisecond)

	transport.pendingMu.Lock()
	remaining := len(transport.pending)
	transport.pendingMu.Unlock()

	assert.Equal(t, 0, remaining,
		"every cancelled call must remove its pending entry — leaked entries eventually deliver stale responses to dead waiters")
}

// TestWS_ReaderGoroutineExitsOnClose verifies Close triggers a clean reader
// exit: readerDone must close within a bounded time after Close returns.
func TestWS_ReaderGoroutineExitsOnClose(t *testing.T) {
	t.Parallel()

	m := &controllableMiddleware{}
	host := startControllable(t, m)

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)

	// Snapshot the readerDone before Close so we can observe it close.
	transport.connMu.RLock()
	readerDone := transport.readerDone
	transport.connMu.RUnlock()

	require.NoError(t, transport.Close())

	select {
	case <-readerDone:
		// Reader exited — expected.
	case <-time.After(2 * time.Second):
		t.Fatal("reader goroutine did not exit within 2s of Close()")
	}
}

// TestWS_ReaderFailsAllPendingOnConnDrop verifies that killing the conn
// while calls are in-flight causes every waiter to return promptly instead
// of wedging indefinitely.
func TestWS_ReaderFailsAllPendingOnConnDrop(t *testing.T) {
	t.Parallel()

	var closeOnce sync.Once
	var closeConn atomic.Value // *websocket.Conn

	m := &controllableMiddleware{
		onConnect: func(conn *websocket.Conn) {
			closeConn.Store(conn)
		},
		respond: func(_ *websocket.Conn, _, _ string) bool {
			// Never respond — we'll drop the conn externally.
			return false
		},
	}
	host := startControllable(t, m)

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)
	defer transport.Close()

	// Start N concurrent calls that will block waiting for responses.
	const n = 20

	errs := make(chan error, n)

	for range n {
		go func() {
			var out map[string]any
			errs <- transport.Call(context.Background(), "system.info", nil, &out)
		}()
	}

	// Give calls a moment to register pending entries, then drop the server
	// side of the conn. The client reader will observe the read error and
	// fail all pending waiters.
	time.Sleep(100 * time.Millisecond)

	closeOnce.Do(func() {
		if c, ok := closeConn.Load().(*websocket.Conn); ok && c != nil {
			_ = c.Close()
		}
	})

	// All N waiters should return within a short window.
	deadline := time.After(5 * time.Second)

	for i := range n {
		select {
		case err := <-errs:
			require.Error(t, err, "call %d should have returned an error after conn drop", i)
		case <-deadline:
			t.Fatalf("only %d of %d calls returned after conn drop; reader did not fan out failure", i, n)
		}
	}
}

// TestWS_DefaultTimeoutFires pins the contract that a Call with no ctx
// deadline still has a 30s ceiling — not a parallel concern but a
// programmer-safety-net concern (otherwise a misbehaving server hangs the
// caller forever).
//
// We verify by using a server that never responds + a very short test
// override for the timeout. Since the 30s default is hard-coded, we can't
// tune it from outside — instead, exercise the cancellation path via a
// short ctx deadline and confirm the call returns the ctx error, not a
// "call timed out" message.
func TestWS_CtxDeadlineBeatsDefaultTimeout(t *testing.T) {
	t.Parallel()

	m := &controllableMiddleware{
		respond: func(_ *websocket.Conn, _, _ string) bool { return false },
	}
	host := startControllable(t, m)

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	start := time.Now()
	var out map[string]any
	err = transport.Call(ctx, "system.info", nil, &out)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded,
		"call should surface ctx.DeadlineExceeded, not the default 30s timeout")
	assert.Less(t, time.Since(start), 500*time.Millisecond,
		"call must not wait for the default 30s ceiling when ctx deadline is shorter")
}

// TestWS_OrphanResponseDoesNotPanic pushes a response with a non-existent
// request ID directly through the reader path. The reader must silently
// drop it (no pending entry to deliver to) rather than panic on nil send.
func TestWS_OrphanResponseDoesNotPanic(t *testing.T) {
	t.Parallel()

	// Middleware that, between auth and any real call, spontaneously pushes
	// a response with a fabricated ID. The reader should drop it.
	m := &controllableMiddleware{
		onConnect: func(_ *websocket.Conn) {
			// Nothing — the orphan push happens in respond().
		},
		respond: func(conn *websocket.Conn, _, id string) bool {
			// First push an orphan response the client has no pending entry for.
			orphan, _ := json.Marshal(map[string]any{"msg": "result", "id": "does-not-exist", "result": true})
			_ = conn.WriteMessage(websocket.TextMessage, orphan)

			// Then answer the real request.
			result, _ := json.Marshal(true)
			real_, _ := json.Marshal(map[string]any{"msg": "result", "id": id, "result": json.RawMessage(result)})
			_ = conn.WriteMessage(websocket.TextMessage, real_)

			return true
		},
	}
	host := startControllable(t, m)

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)
	defer transport.Close()

	var out bool
	err = transport.Call(context.Background(), "system.info", nil, &out)
	require.NoError(t, err, "orphan responses must not crash the reader or wedge the real call")
	assert.True(t, out)
}

// TestWS_ConcurrentCallRaceStress hammers the reader/writer split with
// many goroutines, cancellations, and short deadlines, combined with
// `-race` (enabled by the Makefile's go test target). The success signal
// is "no race detected + no deadlock".
func TestWS_ConcurrentCallRaceStress(t *testing.T) {
	t.Parallel()

	m := &controllableMiddleware{}
	host := startControllable(t, m)

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)
	defer transport.Close()

	// The point of the test is to exercise the reader/writer split under
	// concurrent cancellation, not to stress the race detector. Keep the call
	// count modest — 32 is enough to prove the pending map drains while
	// staying well under the wall-clock budget on loaded GHA runners, where
	// larger counts intermittently timed out (v0.15.1, v0.15.2 releases).
	const (
		goroutines = 4
		perG       = 8
	)

	var wg sync.WaitGroup

	for range goroutines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := range perG {
				// Alternate: short-but-achievable deadline vs. ample deadline.
				// Avoid sub-millisecond deadlines — they exercise the cancel
				// path so aggressively that the test becomes a runtime-scheduler
				// microbenchmark rather than a correctness check, and on a
				// loaded CI box the cleanup-goroutine pile-up can push the wall
				// time past the assertion window.
				d := 200 * time.Millisecond
				if i%4 == 0 {
					d = 5 * time.Millisecond
				}

				ctx, cancel := context.WithTimeout(context.Background(), d)

				var out map[string]any
				_ = transport.Call(ctx, "system.info", nil, &out)

				cancel()
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		// Pending map must drain after the storm.
		time.Sleep(200 * time.Millisecond)

		transport.pendingMu.Lock()
		remaining := len(transport.pending)
		transport.pendingMu.Unlock()

		assert.Equal(t, 0, remaining, "pending map leaked after stress; expected 0")
	case <-time.After(180 * time.Second):
		// Generous deadline so a slow CI runner under -race doesn't trigger
		// a false positive. The deadlock signal we care about is
		// "never finishes," not "finishes slowly."
		t.Fatal("stress test deadlocked — reader/writer split has a regression")
	}
}

// TestWS_CloseDuringReconnectDoesNotBlockPastGrace pins the contract that
// Close returns within a bounded budget even when reconnect() is holding
// connMu.Lock across a dial retry cycle. reconnect can hold connMu.Lock
// for up to (initialBackoff × 1 + 2 + 4 + 3 × HandshakeTimeout) ≈ 37s in
// the worst case — Close waiting behind that lock is a hang from the
// operator's perspective.
//
// Budget: 5s. Rationale: initialBackoff (1s) + one HandshakeTimeout (10s
// under the pessimistic case, but in practice the redial fails immediately
// against a closed server) should not exceed the grace budget. If this
// test surfaces a real latency-past-budget, it is skipped and the contract
// question is documented in the skip message rather than silently mutated.
func TestWS_CloseDuringReconnectDoesNotBlockPastGrace(t *testing.T) {
	t.Parallel()

	// Middleware that accepts the initial handshake, then on the first
	// method call, closes the underlying conn to trigger reconnect. All
	// subsequent connect attempts are refused at the TCP layer by taking
	// the server down.
	var serverRef atomic.Pointer[httptest.Server]
	var connCount atomic.Int32

	m := &controllableMiddleware{
		respond: func(conn *websocket.Conn, _, _ string) bool {
			// Drop the current server so subsequent redial attempts get
			// connection-refused, forcing reconnect into backoff while
			// still holding connMu.Lock.
			if s := serverRef.Load(); s != nil {
				go s.Close()
			}
			_ = conn.Close()
			return false
		},
		onConnect: func(_ *websocket.Conn) {
			connCount.Add(1)
		},
	}

	server := httptest.NewServer(http.HandlerFunc(m.handler))
	serverRef.Store(server)
	// No t.Cleanup(server.Close) — respond() already closes it. Adding
	// t.Cleanup would double-close and panic in httptest.

	host := strings.TrimPrefix(server.URL, "http://")

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)

	// Fire a call that triggers reconnect. It runs in a goroutine so we
	// can race Close against it.
	callDone := make(chan struct{})
	go func() {
		defer close(callDone)
		var out map[string]any
		_ = transport.Call(context.Background(), "system.info", nil, &out)
	}()

	// Wait for reconnect() to be underway. A short sleep is fine here:
	// the call above hits the server, the server closes both directions,
	// the reader errors and Call sees a connection error → reconnect().
	time.Sleep(100 * time.Millisecond)

	// Now race Close against a reconnect that is holding connMu.Lock.
	closeReturned := make(chan error, 1)
	closeStart := time.Now()
	go func() {
		closeReturned <- transport.Close()
	}()

	const grace = 5 * time.Second

	select {
	case cErr := <-closeReturned:
		elapsed := time.Since(closeStart)
		t.Logf("Close returned in %s (err=%v)", elapsed, cErr)
		assert.Less(t, elapsed, grace,
			"Close must return within grace budget even while reconnect holds connMu.Lock")
	case <-time.After(grace):
		// Reaching this branch is the failure mode: reconnect() takes
		// connMu.Lock for the duration of backoff + up to 3 dial retries
		// (~37s worst case), and Close() waits behind that lock. The
		// contract we assert is `elapsed < grace`; if Close is still
		// blocked at grace, that is a regression, not an expected skip.
		//
		// The previous t.Skip("A5 — …") here hid the regression as a
		// silent PASS. Un-skipping means when the underlying reconnect-
		// holds-connMu-across-backoff issue surfaces, this test fails
		// loudly instead of the operator only finding out post-deploy.
		//
		// KNOWN CAVEAT: on the current implementation this test passes
		// consistently on a warm machine (Close returns in ~5ms) because
		// the fake middleware closes the current server immediately,
		// which unblocks reconnect's dial before the connMu.Lock holder
		// can hit the retry loop. If a future refactor makes reconnect
		// truly block across dial retries, this branch will trip. Do
		// not re-add t.Skip — fix the reconnect-holds-connMu contract
		// instead (see docs/concurrency-patterns.md).
		elapsed := time.Since(closeStart)
		t.Errorf("A5 regression: Close blocked for %s (want < %s) — reconnect is holding connMu.Lock across backoff/dial retries", elapsed, grace)
	}

	<-callDone
}

// TestWS_CallAfterCloseReturnsErrTransportClosed pins the contract that
// callers can errors.Is(err, ErrTransportClosed) rather than substring-
// matching on the error message. The three post-Close reject sites in ws.go
// previously each hand-rolled fmt.Errorf("transport is closed") — one
// keystroke away from drift.
func TestWS_CallAfterCloseReturnsErrTransportClosed(t *testing.T) {
	t.Parallel()

	m := &controllableMiddleware{}
	host := startControllable(t, m)

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)

	require.NoError(t, transport.Close())

	var out map[string]any
	err = transport.Call(context.Background(), "system.info", nil, &out)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTransportClosed),
		"Call after Close must return ErrTransportClosed sentinel (got %v)", err)
}

// TestWS_UploadFileAfterCloseReturnsErrTransportClosed pins the same
// contract on the UploadFile side.
func TestWS_UploadFileAfterCloseReturnsErrTransportClosed(t *testing.T) {
	t.Parallel()

	m := &controllableMiddleware{}
	host := startControllable(t, m)

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)

	require.NoError(t, transport.Close())

	payload := bytes.NewReader([]byte("hello"))
	err = transport.UploadFile(context.Background(), "/mnt/test/file", payload, int64(payload.Len()))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTransportClosed),
		"UploadFile after Close must return ErrTransportClosed sentinel (got %v)", err)
}

// TestWS_CloseDoesNotRaceWithConcurrentUploads mirrors
// TestWS_CloseDoesNotRaceWithConcurrentCalls on the UploadFile side. Same
// hazard, same fix location, same failure mode ("panic: sync: WaitGroup is
// reused before previous Wait has returned") — but the Call side test alone
// cannot cover the UploadFile branch that also does wg.Add(1) under the
// same closed-check protocol. Uses a TLS server that answers /websocket
// (for auth handshake) and /_upload/ (200 OK, drain body) so the upload
// completes successfully in the non-Close path.
func TestWS_CloseDoesNotRaceWithConcurrentUploads(t *testing.T) {
	t.Parallel()

	m := &controllableMiddleware{}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "_upload") {
			// Drain then 200 — mimics TrueNAS accepting the multipart upload.
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
			return
		}
		m.handler(w, r)
	}))
	t.Cleanup(server.Close)

	host := strings.TrimPrefix(server.URL, "https://")

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)

	var wg sync.WaitGroup

	// 4 uploaders in tight loop.
	for range 4 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				payload := bytes.NewReader([]byte("hello"))
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				upErr := transport.UploadFile(ctx, "/mnt/tank/iso/x.iso", payload, int64(payload.Len()))
				cancel()

				if upErr != nil {
					// Any error (ErrTransportClosed, ctx, upload rejected on
					// half-closed transport) means Close has landed and we
					// should stop.
					return
				}
			}
		}()
	}

	// 4 Close racers.
	for range 4 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			assert.NoError(t, transport.Close())
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Close/Upload race did not converge — possible deadlock regression")
	}
}

// TestWS_CloseDoesNotRaceWithConcurrentCalls pins the fix for a real
// production crash: "panic: sync: WaitGroup is reused before previous Wait
// has returned" inside wsTransport.Close(). It reproduced because Close set
// `closed` only after wg.Wait() returned, so Call/UploadFile could still
// wg.Add(1) at the exact moment Wait's internal counter reached zero —
// exactly the interleaving sync.WaitGroup forbids. The fix flips `closed`
// under connMu's write lock before calling Wait, and Call/UploadFile check
// `closed` under the read lock before calling Add, so an Add already past
// the check must land before Close's write lock (and therefore its Wait)
// can proceed. This test hammers concurrent Call and concurrent Close
// together; a regression panics the whole test binary within a handful of
// iterations, with or without -race.
func TestWS_CloseDoesNotRaceWithConcurrentCalls(t *testing.T) {
	t.Parallel()

	// The Add-during-Wait race is probabilistic. A single-shot invocation
	// can pass under a lucky scheduler even against the pre-fix code. The
	// former 20× inner loop was removed once `make test-stress` gained a
	// -count=30 outer driver — single source of truth for iteration lives
	// in the Makefile, so this test body runs once per `go test` invocation
	// and the stress job pays for the repetition.
	m := &controllableMiddleware{}
	host := startControllable(t, m)

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)

	var wg sync.WaitGroup

	// Keep issuing calls until the transport reports closed.
	for range 8 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				var out map[string]any
				callErr := transport.Call(ctx, "system.info", nil, &out)
				cancel()

				if callErr != nil {
					return
				}
			}
		}()
	}

	// Race multiple concurrent Close callers — Close must be idempotent
	// and must never panic no matter how many goroutines race it.
	for range 4 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			assert.NoError(t, transport.Close())
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	// Deadlock timer tightened 30s → 5s. Under the pre-fix code a
	// panic surfaces well within 5s; a latency regression that
	// pushes Close past 5s (e.g. wg.Wait blocking behind a stuck
	// in-flight call) will now trip the timer instead of hiding
	// under a 30s ceiling.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close/Call race did not converge — possible deadlock or latency regression")
	}
}

// TestWS_CloseIsIdempotent pins the sequential idempotency contract that
// wsTransport.Close short-circuits on the second and subsequent calls with
// no error. No race here — this is the plain-old "operator shutdown +
// deferred cleanup + finaliser" three-Close pattern.
func TestWS_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	m := &controllableMiddleware{}
	host := startControllable(t, m)

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)

	require.NoError(t, transport.Close(), "first Close")
	require.NoError(t, transport.Close(), "second Close (idempotent)")
	require.NoError(t, transport.Close(), "third Close (idempotent)")
}

// TestWS_ConcurrentCloseAllWaitForDrain documents the semantic that only
// the FIRST Close waits for in-flight Call/UploadFile to drain; second
// and subsequent concurrent Close callers short-circuit on the closed
// flag and return immediately with no error. This is intentional: the
// drain contract belongs to the first Close, and racing callers should
// not each pay a 10s grace budget.
//
// If a future refactor wants "every Close waits for drain," it must:
//  1. Flip this test's expectation.
//  2. Document the change in the Close doc comment.
//  3. Justify the O(N * closeTimeout) worst case that comes with it.
func TestWS_ConcurrentCloseAllWaitForDrain(t *testing.T) {
	t.Parallel()

	// Middleware that blocks method-call responses until a channel is
	// signalled, then returns an ok result.
	release := make(chan struct{})

	m := &controllableMiddleware{
		respond: func(conn *websocket.Conn, _, id string) bool {
			<-release
			result, _ := json.Marshal(map[string]any{"ok": true})
			resp, _ := json.Marshal(map[string]any{"msg": "result", "id": id, "result": json.RawMessage(result)})
			_ = conn.WriteMessage(websocket.TextMessage, resp)
			return true
		},
	}
	host := startControllable(t, m)

	transport, err := newWSTransport(host, NewSecretString("test-key"), true)
	require.NoError(t, err)

	// Start N slow calls that will block until release.
	const n = 4

	callDone := make(chan struct{}, n)

	for range n {
		go func() {
			var out map[string]any
			_ = transport.Call(context.Background(), "system.info", nil, &out)
			callDone <- struct{}{}
		}()
	}

	// Give calls a beat to Add(1) to the WaitGroup.
	time.Sleep(100 * time.Millisecond)

	// Start 4 concurrent Close goroutines, recording return times.
	const closers = 4

	closeReturned := make(chan time.Time, closers)

	for range closers {
		go func() {
			_ = transport.Close()
			closeReturned <- time.Now()
		}()
	}

	// Give closers a beat to enter Close and race for the connMu.Lock.
	time.Sleep(100 * time.Millisecond)

	// Now release the in-flight calls and record when the FIRST call
	// returns (which is what the FIRST Close's wg.Wait is waiting for).
	close(release)

	callReturnTimes := make([]time.Time, 0, n)
	for range n {
		<-callDone
		callReturnTimes = append(callReturnTimes, time.Now())
	}

	// The FIRST Close (the one that flipped t.closed = true) must return
	// AFTER all in-flight calls have returned. Subsequent concurrent
	// Closes are allowed to short-circuit early.
	closeReturnTimes := make([]time.Time, 0, closers)
	for range closers {
		closeReturnTimes = append(closeReturnTimes, <-closeReturned)
	}

	// The last-returning Close is the one that took the write lock and
	// waited on wg.Wait. It must be AFTER all in-flight calls returned.
	var latestClose time.Time
	for _, ct := range closeReturnTimes {
		if ct.After(latestClose) {
			latestClose = ct
		}
	}

	var latestCall time.Time
	for _, ct := range callReturnTimes {
		if ct.After(latestCall) {
			latestCall = ct
		}
	}

	assert.True(t, !latestClose.Before(latestCall),
		"the drain-owning Close (last return) must be at-or-after all in-flight calls returned; latestClose=%s latestCall=%s",
		latestClose, latestCall)
}
