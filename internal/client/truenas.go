// Package client implements a Go client for the TrueNAS SCALE JSON-RPC 2.0 API.
//
// This client does NOT support the legacy REST v2.0 API. It requires TrueNAS SCALE 25.04+.
//
// Connects via WebSocket (wss://<host>/websocket) with API key authentication.
// TrueNAS 25.10 removed implicit authentication on the Unix socket, so all
// transports now require an API key — there's no longer a zero-auth path.
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
	// Host is the TrueNAS hostname or IP (e.g., "truenas.local" or "192.168.1.100"). Required.
	Host string

	// APIKey is the TrueNAS API key. Required.
	APIKey string

	// InsecureSkipVerify disables TLS certificate verification for WebSocket connections.
	InsecureSkipVerify bool

	// MaxConcurrentCalls limits concurrent API calls to TrueNAS.
	// Prevents overwhelming the middleware during large scale-ups.
	// Defaults to 8 if not set.
	MaxConcurrentCalls int
}

const defaultMaxConcurrentCalls = 8

// New creates a new TrueNAS API client that connects via WebSocket.
// Host and APIKey are both required. TrueNAS 25.10 removed implicit
// authentication on the Unix socket, so an API key is required in all cases.
func New(cfg Config) (*Client, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("TRUENAS_HOST is required")
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("TRUENAS_API_KEY is required")
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
	ErrCodeNotFound      = 2  // ENOENT — resource does not exist
	ErrCodeInvalid       = 11 // EINVAL — invalid argument (also used for "already exists" in some contexts)
	ErrCodeNoMemory      = 12 // ENOMEM — host cannot guarantee guest memory (vm.start)
	ErrCodeDenied        = 13 // EACCES — permission denied
	ErrCodeExists        = 17 // EEXIST — resource already exists
	ErrCodeNoSpace       = 28 // ENOSPC — no space left on device
	ErrCodeMatchNotFound = 22 // EINVAL repurposed — `query` with `{"get": true}` found zero rows (TrueNAS `MatchNotFound()`)
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
	case ErrCodeNoMemory:
		return "TrueNAS host is out of free RAM — cannot guarantee memory for this VM. " +
			"Stop another guest, reduce this MachineClass memory, or add host RAM"
	case ErrCodeDenied:
		return "TrueNAS permission denied — check API key permissions"
	case ErrCodeInvalid:
		if strings.Contains(apiErr.Message, "nic_attach") || strings.Contains(apiErr.Message, "NIC") {
			return fmt.Sprintf("network interface not found on TrueNAS: %s", apiErr.Message)
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
//
// Catches both:
//   - code 2 (ENOENT): the classic "not found" from direct lookup endpoints.
//   - code 22 with "MatchNotFound": TrueNAS `query` methods with `{"get": true}`
//     return code 22 when the filter matches zero rows. Observed on
//     `vm.query`, `pool.dataset.query`, `disk.query` and others — the
//     middleware reuses EINVAL (22) for "no match" rather than ENOENT, so a
//     strict `Code == 2` check leaves Deprovision (and other cleanup paths)
//     stuck forever when the VM was already deleted externally.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}

	if apiErr.Code == ErrCodeNotFound {
		return true
	}

	if apiErr.Code == ErrCodeMatchNotFound && strings.Contains(apiErr.Message, "MatchNotFound") {
		return true
	}

	return false
}

// IsNoMemory returns true if the error indicates the TrueNAS host could not
// guarantee memory for a guest VM. This is the runtime ENOMEM produced by
// vm.start when the host has insufficient free RAM (other VMs already
// committed it, ARC won't release fast enough, etc.) — distinct from a
// MachineClass that simply requests more memory than the host has total,
// which the provider's pre-flight check rejects before VM creation.
//
// Matches both the structured code (12 / ENOMEM) and the message-based
// fallback because TrueNAS sometimes returns the libvirt-relayed string
// without a clean error code.
func IsNoMemory(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}

	if apiErr.Code == ErrCodeNoMemory {
		return true
	}

	return strings.Contains(apiErr.Message, "[ENOMEM]") ||
		strings.Contains(apiErr.Message, "Cannot guarantee memory")
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
