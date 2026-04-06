package client

import (
	"encoding/json"
	"sync/atomic"
)

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	ID      int64  `json:"id"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is the error object in a JSON-RPC 2.0 response.
type jsonRPCError struct {
	Code    int             `json:"error"`
	Message string          `json:"reason"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// requestIDCounter generates unique request IDs.
var requestIDCounter atomic.Int64

func nextRequestID() int64 {
	return requestIDCounter.Add(1)
}

// toAPIError converts a JSON-RPC error to an APIError.
func toAPIError(e *jsonRPCError) *APIError {
	return &APIError{
		Code:    e.Code,
		Message: e.Message,
	}
}
