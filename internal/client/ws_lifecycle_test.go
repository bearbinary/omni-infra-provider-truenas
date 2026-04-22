package client

import (
	"context"
	"encoding/json"
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
