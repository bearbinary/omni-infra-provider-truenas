package client

import (
	"encoding/json"
	"testing"
)

// mockHandler is used by client-internal tests that need to return typed API errors.
// It wraps the exported MockHandler by converting *jsonRPCError to *APIError.
type mockHandler func(method string, params json.RawMessage) (any, *jsonRPCError)

// newMockClient creates a Client backed by the exported MockTransport, adapting
// the mockHandler signature (returns *jsonRPCError) to MockHandler (returns error).
// This keeps all existing client-internal tests working without changes.
func newMockClient(t *testing.T, handler mockHandler) *Client {
	t.Helper()

	return NewMockClient(func(method string, params json.RawMessage) (any, error) {
		resp, rpcErr := handler(method, params)
		if rpcErr != nil {
			return nil, toAPIError(rpcErr)
		}

		return resp, nil
	})
}

// notFoundErr returns a JSON-RPC error for resource not found.
func notFoundErr() *jsonRPCError {
	return &jsonRPCError{Code: ErrCodeNotFound, Message: "not found"}
}

// alreadyExistsErr returns a JSON-RPC error for resource already exists.
func alreadyExistsErr() *jsonRPCError {
	return &jsonRPCError{Code: ErrCodeExists, Message: "already exists"}
}
