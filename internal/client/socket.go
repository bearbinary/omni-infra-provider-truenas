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
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

// socketTransport implements Transport over the TrueNAS middleware Unix socket
// using WebSocket with JSON-RPC 2.0 protocol. No authentication is required —
// the socket is trusted for local processes.
//
// TrueNAS 25.10+ uses the JSONRPCClient protocol over the Unix socket:
//   - WebSocket connection to the socket file (no HTTP path needed)
//   - Pure JSON-RPC 2.0 messages (NOT the DDP protocol used by the remote /websocket endpoint)
//   - No connect handshake — just send {"jsonrpc":"2.0","method":...} directly
//   - No authentication needed for local Unix socket
type socketTransport struct {
	conn          *websocket.Conn
	socketPath    string
	mu            sync.Mutex
	wg            sync.WaitGroup
	lastReconnect time.Time
}

// socketRequestID generates unique request IDs for socket transport calls.
var socketRequestID atomic.Int64

func nextSocketRequestID() string {
	return fmt.Sprintf("%d", socketRequestID.Add(1))
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

	// The hostname is ignored for Unix sockets — "localhost" is a dummy.
	// The Python ws+unix:// scheme sends no HTTP path; gorilla maps "/" to the root.
	conn, _, err := dialer.Dial("ws://localhost/", nil)
	if err != nil {
		return nil, fmt.Errorf("websocket handshake over unix socket failed: %w", err)
	}

	return conn, nil
}

// newSocketTransport creates a transport that communicates via WebSocket over
// the middleware Unix socket using JSON-RPC 2.0. No handshake or auth needed.
func newSocketTransport(path string) (*socketTransport, error) {
	conn, err := dialUnixWebSocket(path)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to middleware socket at %s: %w", path, err)
	}

	return &socketTransport{
		conn:       conn,
		socketPath: path,
	}, nil
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

		return nil
	}

	return fmt.Errorf("reconnect failed after %d attempts: %w", maxReconnectAttempts, lastErr)
}

// Call sends a JSON-RPC 2.0 method call over the WebSocket-over-Unix-socket.
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

// jsonRPC2Request is a JSON-RPC 2.0 request (used by the Unix socket transport).
type jsonRPC2Request struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	ID      string `json:"id"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPC2Response is a JSON-RPC 2.0 response (used by the Unix socket transport).
type jsonRPC2Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPC2Error  `json:"error,omitempty"`
}

// jsonRPC2Error is a JSON-RPC 2.0 error object.
type jsonRPC2Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// doCall performs a single JSON-RPC 2.0 call without reconnect logic.
func (t *socketTransport) doCall(ctx context.Context, method string, params any, result any) error {
	reqID := nextSocketRequestID()

	req := jsonRPC2Request{
		JSONRPC: "2.0",
		Method:  method,
		ID:      reqID,
		Params:  normalizeParams(params),
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
		var resp jsonRPC2Response
		if err := t.conn.ReadJSON(&resp); err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		// Skip messages that don't match our request ID (e.g., job updates)
		if resp.ID != reqID {
			continue
		}

		if resp.Error != nil {
			return &APIError{
				Code:    resp.Error.Code,
				Message: resp.Error.Message,
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
