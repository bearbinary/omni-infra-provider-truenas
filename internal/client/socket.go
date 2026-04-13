package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

// socketTransport implements Transport over the TrueNAS middleware Unix socket
// using WebSocket protocol. No authentication is required — the socket is
// trusted for local processes.
//
// TrueNAS 25.10+ uses WebSocket over the Unix socket (same DDP-like protocol
// as the remote WebSocket transport). The Python midclt client connects via
// ws+unix:// — we do the same with gorilla/websocket and a custom Unix dialer.
type socketTransport struct {
	conn          *websocket.Conn
	socketPath    string
	mu            sync.Mutex
	wg            sync.WaitGroup
	lastReconnect time.Time
}

// socketAvailable checks if the middleware Unix socket exists and is connectable.
func socketAvailable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.Mode().Type()&os.ModeSocket != 0
}

// dialUnixWebSocket establishes a WebSocket connection over a Unix socket.
func dialUnixWebSocket(socketPath string) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		NetDial: func(_, _ string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 10*time.Second)
		},
		HandshakeTimeout: 10 * time.Second,
	}

	// The URL path must be /api/current for TrueNAS 25.10+ JSON-RPC 2.0.
	// The hostname is ignored for Unix sockets — "localhost" is a dummy.
	conn, _, err := dialer.Dial("ws://localhost/api/current", nil)
	if err != nil {
		return nil, fmt.Errorf("websocket handshake over unix socket failed: %w", err)
	}

	return conn, nil
}

// newSocketTransport creates a transport that communicates via WebSocket over
// the middleware Unix socket. Performs the DDP connect handshake but skips
// authentication (Unix socket is trusted).
func newSocketTransport(path string) (*socketTransport, error) {
	conn, err := dialUnixWebSocket(path)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to middleware socket at %s: %w", path, err)
	}

	t := &socketTransport{
		conn:       conn,
		socketPath: path,
	}

	if err := t.connect(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("connect handshake failed: %w", err)
	}

	return t, nil
}

func (t *socketTransport) Name() string {
	return "unix"
}

func (t *socketTransport) Close() error {
	done := make(chan struct{})

	go func() {
		t.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}

	return t.conn.Close()
}

// connect sends the initial DDP connect handshake (same as wsTransport).
func (t *socketTransport) connect() error {
	t.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck

	if err := t.conn.WriteJSON(wsRequest{
		Msg:     "connect",
		Version: "1",
		Support: []string{"1"},
	}); err != nil {
		return fmt.Errorf("failed to send connect: %w", err)
	}

	t.conn.SetReadDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck

	var resp wsResponse
	if err := t.conn.ReadJSON(&resp); err != nil {
		return fmt.Errorf("failed to read connect response: %w", err)
	}

	if resp.Msg != "connected" {
		return fmt.Errorf("unexpected connect response: %s", resp.Msg)
	}

	// Clear deadlines for normal operation
	t.conn.SetReadDeadline(time.Time{})  //nolint:errcheck
	t.conn.SetWriteDeadline(time.Time{}) //nolint:errcheck

	return nil
}

// reconnect closes the current connection and establishes a new one.
func (t *socketTransport) reconnect() error {
	if sinceLastReconnect := time.Since(t.lastReconnect); sinceLastReconnect < reconnectCooldown {
		wait := reconnectCooldown - sinceLastReconnect
		time.Sleep(wait)
	}

	t.lastReconnect = time.Now()

	if telemetry.WSReconnects != nil {
		telemetry.WSReconnects.Add(context.Background(), 1)
	}

	_ = t.conn.Close()

	var lastErr error
	backoff := initialBackoff

	for attempt := range maxReconnectAttempts {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}

		conn, err := dialUnixWebSocket(t.socketPath)
		if err != nil {
			lastErr = err
			continue
		}

		t.conn = conn

		if err := t.connect(); err != nil {
			_ = t.conn.Close()
			lastErr = err
			continue
		}

		return nil
	}

	return fmt.Errorf("reconnect failed after %d attempts: %w", maxReconnectAttempts, lastErr)
}

// Call sends a method call over the WebSocket-over-Unix-socket connection.
// On connection failure, attempts to reconnect and retry once.
func (t *socketTransport) Call(ctx context.Context, method string, params any, result any) error {
	t.wg.Add(1)
	defer t.wg.Done()

	t.mu.Lock()
	defer t.mu.Unlock()

	err := t.doCall(ctx, method, params, result)
	if err == nil {
		return nil
	}

	var apiErr *APIError
	if isAPIError(err, &apiErr) {
		return err
	}

	if reconnErr := t.reconnect(); reconnErr != nil {
		return errors.Join(
			fmt.Errorf("call failed: %w", err),
			fmt.Errorf("reconnect failed: %w", reconnErr),
		)
	}

	return t.doCall(ctx, method, params, result)
}

// doCall performs a single call without reconnect logic.
func (t *socketTransport) doCall(ctx context.Context, method string, params any, result any) error {
	reqID := nextWSRequestID()

	req := wsRequest{
		Msg:    "method",
		Method: method,
		ID:     reqID,
		Params: normalizeParams(params),
	}

	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}

	t.conn.SetWriteDeadline(deadline) //nolint:errcheck
	t.conn.SetReadDeadline(deadline)  //nolint:errcheck

	if err := t.conn.WriteJSON(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	for {
		var resp wsResponse
		if err := t.conn.ReadJSON(&resp); err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		if resp.ID != reqID {
			continue
		}

		if resp.Error != nil {
			return &APIError{
				Code:    resp.Error.Error,
				Message: resp.Error.Reason,
			}
		}

		if result != nil && resp.Result != nil {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("failed to unmarshal result: %w", err)
			}
		}

		return nil
	}
}

// UploadFile writes a file directly to the filesystem.
// When running on the TrueNAS host, we have local filesystem access via the
// mounted volumes, so we stream directly to disk to avoid buffering entire
// ISOs in memory.
func (t *socketTransport) UploadFile(_ context.Context, destPath string, data io.Reader, _ int64) error {
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create file %q: %w", destPath, err)
	}

	if _, err := io.Copy(f, data); err != nil {
		_ = f.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("failed to write file %q: %w", destPath, err)
	}

	return f.Close()
}

// normalizeParams ensures params are sent as a JSON array (TrueNAS middleware expects positional params).
func normalizeParams(params any) any {
	if params == nil {
		return []any{}
	}

	switch params.(type) {
	case []any, []map[string]any, []string, []int:
		return params
	default:
		return []any{params}
	}
}
