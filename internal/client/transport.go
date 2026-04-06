package client

import (
	"context"
	"io"
)

// Transport abstracts the JSON-RPC 2.0 communication layer.
// Implementations: Unix socket and WebSocket.
type Transport interface {
	// Call executes a JSON-RPC method. params and result are JSON-marshalable.
	Call(ctx context.Context, method string, params any, result any) error

	// UploadFile uploads a file to the TrueNAS filesystem.
	// Some methods (like filesystem.put) require pipe-based upload which
	// can't go through the standard Call interface.
	UploadFile(ctx context.Context, destPath string, data io.Reader, size int64) error

	// Name returns the transport identifier ("unix" or "websocket").
	Name() string

	// Close shuts down the connection.
	Close() error
}
