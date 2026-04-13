package client

import "encoding/json"

// jsonRPCError is the error object in a JSON-RPC 2.0 response.
// Used by the mock transport in tests.
type jsonRPCError struct {
	Code    int             `json:"error"`
	Message string          `json:"reason"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// toAPIError converts a JSON-RPC error to an APIError.
func toAPIError(e *jsonRPCError) *APIError {
	return &APIError{
		Code:    e.Code,
		Message: e.Message,
	}
}
