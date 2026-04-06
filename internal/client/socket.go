package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// socketTransport implements Transport over the TrueNAS middleware Unix socket.
// No authentication is required — the socket is trusted for local processes.
type socketTransport struct {
	socketPath string
	mu         sync.Mutex
	wg         sync.WaitGroup
}

// socketAvailable checks if the middleware Unix socket exists and is connectable.
func socketAvailable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	// Check it's a socket
	return info.Mode().Type()&os.ModeSocket != 0
}

// newSocketTransport creates a transport that communicates over the middleware Unix socket.
func newSocketTransport(path string) (*socketTransport, error) {
	// Verify we can connect
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to middleware socket at %s: %w", path, err)
	}
	conn.Close()

	return &socketTransport{socketPath: path}, nil
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

	return nil
}

// Call sends a JSON-RPC request over a new Unix socket connection and reads the response.
// Each call opens a fresh connection — the middleware expects this pattern.
func (t *socketTransport) Call(ctx context.Context, method string, params any, result any) error {
	t.wg.Add(1)
	defer t.wg.Done()

	t.mu.Lock()
	defer t.mu.Unlock()

	conn, err := net.Dial("unix", t.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to middleware socket: %w", err)
	}
	defer conn.Close()

	// Set deadline from context
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline) //nolint:errcheck
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		ID:      nextRequestID(),
		Params:  normalizeParams(params),
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.Error != nil {
		return toAPIError(resp.Error)
	}

	if result != nil && resp.Result != nil {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("failed to unmarshal result: %w", err)
		}
	}

	return nil
}

// UploadFile writes a file directly to the filesystem via the socket.
// When running on the TrueNAS host, we can write files directly since we
// have local filesystem access.
func (t *socketTransport) UploadFile(ctx context.Context, destPath string, data io.Reader, _ int64) error {
	content, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("failed to read upload data: %w", err)
	}

	return os.WriteFile(destPath, content, 0o755)
}

// normalizeParams ensures params are sent as a JSON array (TrueNAS middleware expects positional params).
func normalizeParams(params any) any {
	if params == nil {
		return []any{}
	}

	// If already a slice, use as-is
	switch params.(type) {
	case []any, []map[string]any, []string, []int:
		return params
	default:
		// Wrap single value in array
		return []any{params}
	}
}
