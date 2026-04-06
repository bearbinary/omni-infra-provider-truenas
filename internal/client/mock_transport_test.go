package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
)

// mockHandler is a function that receives a JSON-RPC method and params, and returns a result or error.
type mockHandler func(method string, params json.RawMessage) (any, *jsonRPCError)

// mockTransport implements Transport for unit testing.
type mockTransport struct {
	handler mockHandler
}

func (t *mockTransport) Name() string { return "mock" }
func (t *mockTransport) Close() error { return nil }
func (t *mockTransport) UploadFile(_ context.Context, _ string, _ io.Reader, _ int64) error {
	return nil
}

func (t *mockTransport) Call(_ context.Context, method string, params any, result any) error {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("mock: failed to marshal params: %w", err)
	}

	resp, rpcErr := t.handler(method, rawParams)
	if rpcErr != nil {
		return toAPIError(rpcErr)
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

// newMockClient creates a Client backed by a mock transport for testing.
func newMockClient(t *testing.T, handler mockHandler) *Client {
	t.Helper()

	return newClient(&mockTransport{handler: handler}, defaultMaxConcurrentCalls)
}

// notFoundErr returns a JSON-RPC error for resource not found.
func notFoundErr() *jsonRPCError {
	return &jsonRPCError{Code: ErrCodeNotFound, Message: "not found"}
}

// alreadyExistsErr returns a JSON-RPC error for resource already exists.
func alreadyExistsErr() *jsonRPCError {
	return &jsonRPCError{Code: ErrCodeExists, Message: "already exists"}
}
