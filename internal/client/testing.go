package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// MockHandler is a function that receives a JSON-RPC method and params, and returns a result or error.
// Exported for use in other packages' tests.
type MockHandler func(method string, params json.RawMessage) (any, error)

// MockTransport implements Transport for testing.
type MockTransport struct {
	Handler MockHandler
}

// Name implements Transport.
func (t *MockTransport) Name() string { return "mock" }

// Close implements Transport.
func (t *MockTransport) Close() error { return nil }

// UploadFile implements Transport.
func (t *MockTransport) UploadFile(_ context.Context, _ string, _ io.Reader, _ int64) error {
	return nil
}

// Call implements Transport.
func (t *MockTransport) Call(_ context.Context, method string, params any, result any) error {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("mock: failed to marshal params: %w", err)
	}

	resp, rpcErr := t.Handler(method, rawParams)
	if rpcErr != nil {
		return rpcErr
	}

	if result != nil && resp != nil {
		data, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("mock: failed to marshal response: %w", err)
		}

		if err := json.Unmarshal(data, result); err != nil {
			return fmt.Errorf("mock: failed to unmarshal into result: %w", err)
		}
	}

	return nil
}

// NewMockClient creates a Client backed by a MockTransport for testing.
func NewMockClient(handler MockHandler) *Client {
	return newClient(&MockTransport{Handler: handler}, defaultMaxConcurrentCalls)
}
