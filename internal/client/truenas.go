// Package client implements a Go client for the TrueNAS SCALE JSON-RPC 2.0 API.
//
// This client does NOT support the legacy REST v2.0 API. It requires TrueNAS SCALE 25.04+.
//
// Two transports are supported, auto-detected in this order:
//  1. Unix socket (/var/run/middleware/middlewared.sock) — no API key required, most secure
//  2. WebSocket (wss://<host>/websocket) — requires API key
package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

// Client wraps the TrueNAS JSON-RPC 2.0 API.
type Client struct {
	transport Transport
	semaphore chan struct{} // Limits concurrent API calls to prevent overwhelming TrueNAS
}

// Config holds TrueNAS connection parameters.
type Config struct {
	// Host is the TrueNAS hostname or IP (e.g., "truenas.local" or "192.168.1.100").
	// Not used when connecting via Unix socket.
	Host string

	// APIKey is the TrueNAS API key. Required for WebSocket transport.
	// Not needed when connecting via Unix socket.
	APIKey string

	// InsecureSkipVerify disables TLS certificate verification for WebSocket connections.
	InsecureSkipVerify bool

	// SocketPath overrides the default Unix socket path.
	// Defaults to /var/run/middleware/middlewared.sock.
	SocketPath string

	// MaxConcurrentCalls limits concurrent API calls to TrueNAS.
	// Prevents overwhelming the middleware during large scale-ups.
	// Defaults to 8 if not set.
	MaxConcurrentCalls int
}

const defaultMaxConcurrentCalls = 8

// DefaultSocketPath is the standard location of the TrueNAS middleware Unix socket.
const DefaultSocketPath = "/var/run/middleware/middlewared.sock"

// New creates a new TrueNAS API client.
// It auto-detects the best available transport:
//  1. Unix socket (if available) — zero-config, no API key needed
//  2. WebSocket — requires Host and APIKey
func New(cfg Config) (*Client, error) {
	socketPath := cfg.SocketPath
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}

	// Try Unix socket first (most secure, no API key needed)
	if socketAvailable(socketPath) {
		t, err := newSocketTransport(socketPath)
		if err != nil {
			return nil, fmt.Errorf("failed to connect via unix socket: %w", err)
		}

		return newClient(t, cfg.MaxConcurrentCalls), nil
	}

	// Fall back to WebSocket
	if cfg.Host == "" {
		return nil, fmt.Errorf("no unix socket at %s and TRUENAS_HOST not set — cannot connect to TrueNAS", socketPath)
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("WebSocket transport requires TRUENAS_API_KEY")
	}

	t, err := newWSTransport(cfg.Host, NewSecretString(cfg.APIKey), cfg.InsecureSkipVerify)
	if err != nil {
		return nil, fmt.Errorf("failed to connect via websocket: %w", err)
	}

	return newClient(t, cfg.MaxConcurrentCalls), nil
}

func newClient(t Transport, maxConcurrent int) *Client {
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrentCalls
	}

	return &Client{
		transport: t,
		semaphore: make(chan struct{}, maxConcurrent),
	}
}

// TransportName returns which transport is active ("unix" or "websocket").
func (c *Client) TransportName() string {
	return c.transport.Name()
}

// Close shuts down the transport connection.
func (c *Client) Close() error {
	return c.transport.Close()
}

var tracer = otel.Tracer("truenas-client")

// call executes a JSON-RPC method and decodes the result.
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	// Record queue depth before acquiring
	if telemetry.RateLimitQueueSize != nil {
		telemetry.RateLimitQueueSize.Record(ctx, int64(len(c.semaphore)))
	}

	// Rate limit: acquire semaphore slot
	select {
	case c.semaphore <- struct{}{}:
		defer func() { <-c.semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}

	ctx, span := tracer.Start(ctx, "truenas."+method,
		trace.WithAttributes(attribute.String("rpc.method", method)),
	)
	defer span.End()

	start := time.Now()

	err := c.transport.Call(ctx, method, params, result)

	duration := time.Since(start).Seconds()
	if telemetry.APICallDuration != nil {
		telemetry.APICallDuration.Record(ctx, duration,
			telemetry.WithMethod(method),
		)
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	return err
}

// Ping checks if the TrueNAS API is reachable.
func (c *Client) Ping(ctx context.Context) error {
	var info map[string]any

	return c.call(ctx, "system.info", nil, &info)
}

// APIError represents an error response from the TrueNAS JSON-RPC API.
type APIError struct {
	Code    int    `json:"error"`
	Message string `json:"reason"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("truenas api error (code %d): %s", e.Code, e.Message)
}

// Common TrueNAS middleware error codes.
const (
	ErrCodeNotFound = 2  // ENOENT — resource does not exist
	ErrCodeExists   = 17 // EEXIST — resource already exists
	ErrCodeInvalid  = 11 // EINVAL — invalid argument (also used for "already exists" in some contexts)
	ErrCodeDenied   = 13 // EACCES — permission denied
	ErrCodeNoSpace  = 28 // ENOSPC — no space left on device
)

// UserFriendlyError returns a human-readable error message for common TrueNAS errors.
// Used to set meaningful status messages on MachineRequestStatus in Omni.
func UserFriendlyError(err error) string {
	if err == nil {
		return ""
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		// Check for connection errors
		msg := err.Error()
		if strings.Contains(msg, "reconnect failed") || strings.Contains(msg, "failed to connect") {
			return "TrueNAS is unreachable — check network connectivity and that TrueNAS is running"
		}

		if strings.Contains(msg, "authentication") || strings.Contains(msg, "auth") {
			return "TrueNAS authentication failed — check TRUENAS_API_KEY"
		}

		return msg
	}

	switch apiErr.Code {
	case ErrCodeNoSpace:
		return "TrueNAS pool is full — free up space or use a different pool"
	case ErrCodeDenied:
		return "TrueNAS permission denied — check API key permissions"
	case ErrCodeInvalid:
		if strings.Contains(apiErr.Message, "nic_attach") || strings.Contains(apiErr.Message, "NIC") {
			return fmt.Sprintf("NIC attach target not found on TrueNAS: %s", apiErr.Message)
		}

		if strings.Contains(apiErr.Message, "name") {
			return fmt.Sprintf("Invalid VM name: %s", apiErr.Message)
		}

		return fmt.Sprintf("Invalid configuration: %s", apiErr.Message)
	default:
		return apiErr.Error()
	}
}

// IsNotFound returns true if the error indicates the resource was not found.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == ErrCodeNotFound
	}

	return false
}

// IsAlreadyExists returns true if the error indicates the resource already exists.
// TrueNAS uses multiple error codes with "already exists" in the message:
// EEXIST (17), EINVAL (11), and EFAULT (14) for datasets.
func IsAlreadyExists(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.Code == ErrCodeExists {
			return true
		}

		if containsAlreadyExists(apiErr.Message) {
			return true
		}
	}

	return false
}

func containsAlreadyExists(msg string) bool {
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "Already exists")
}
