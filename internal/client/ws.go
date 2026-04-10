package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

const (
	maxReconnectAttempts = 3
	initialBackoff       = time.Second
	maxBackoff           = 30 * time.Second
	closeTimeout         = 10 * time.Second
	reconnectCooldown    = 30 * time.Second // Minimum time between reconnect bursts
)

// wsTransport implements Transport over a WebSocket connection to TrueNAS.
// Used for remote deployments where the Unix socket is not available.
// Requires an API key for authentication.
//
// TrueNAS uses a DDP-like WebSocket protocol (not pure JSON-RPC 2.0):
//   - Initial "connect" handshake required
//   - Messages use "msg" field ("method", "result", "connected")
//   - Method calls wrapped with "msg": "method"
type wsTransport struct {
	conn               *websocket.Conn
	apiKey             SecretString
	host               string
	insecureSkipVerify bool
	mu                 sync.Mutex
	wg                 sync.WaitGroup
	authed             bool
	lastReconnect      time.Time    // Circuit breaker: minimum time between reconnect bursts
	uploadClient       *http.Client // Reused for file uploads to benefit from connection pooling
}

// TrueNAS WebSocket message types.
type wsRequest struct {
	Msg     string   `json:"msg"`
	Method  string   `json:"method,omitempty"`
	ID      string   `json:"id,omitempty"`
	Params  any      `json:"params,omitempty"`
	Version string   `json:"version,omitempty"`
	Support []string `json:"support,omitempty"`
}

type wsResponse struct {
	Msg     string          `json:"msg"`
	ID      string          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *wsError        `json:"error,omitempty"`
	Session string          `json:"session,omitempty"`
}

type wsError struct {
	Error  int    `json:"error"`
	Reason string `json:"reason"`
}

// dialWebSocket establishes a WebSocket connection to TrueNAS, trying TLS first.
func dialWebSocket(host string, insecureSkipVerify bool) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecureSkipVerify, //nolint:gosec
		},
		HandshakeTimeout: 10 * time.Second,
	}

	url := fmt.Sprintf("wss://%s/websocket", host)

	conn, resp, err := dialer.Dial(url, nil)
	if err != nil {
		// Only fall back to unencrypted ws:// if TLS verification is explicitly disabled.
		// This prevents accidentally sending the API key over an unencrypted connection.
		if !insecureSkipVerify {
			statusInfo := ""
			if resp != nil {
				statusInfo = fmt.Sprintf(" (HTTP %d)", resp.StatusCode)
			}

			return nil, fmt.Errorf("failed to connect to %s%s: %w — if TrueNAS uses a self-signed cert, set TRUENAS_INSECURE_SKIP_VERIFY=true", host, statusInfo, err)
		}

		url = fmt.Sprintf("ws://%s/websocket", host)

		conn, resp, err = dialer.Dial(url, nil)
		if err != nil {
			statusInfo := ""
			if resp != nil {
				statusInfo = fmt.Sprintf(" (HTTP %d)", resp.StatusCode)
			}

			return nil, fmt.Errorf("failed to connect to %s%s: %w — is this TrueNAS SCALE 25.04+?", host, statusInfo, err)
		}
	}

	if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()

		return nil, fmt.Errorf("unexpected HTTP status %d from %s", resp.StatusCode, url)
	}

	return conn, nil
}

// newWSTransport creates a WebSocket transport and authenticates.
func newWSTransport(host string, apiKey SecretString, insecureSkipVerify bool) (*wsTransport, error) {
	conn, err := dialWebSocket(host, insecureSkipVerify)
	if err != nil {
		return nil, err
	}

	t := &wsTransport{
		conn:               conn,
		apiKey:             apiKey,
		host:               host,
		insecureSkipVerify: insecureSkipVerify,
		uploadClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: insecureSkipVerify, //nolint:gosec
				},
			},
			Timeout: 5 * time.Minute,
		},
	}

	if err := t.connect(); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("connect handshake failed: %w", err)
	}

	if err := t.authenticate(); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	return t, nil
}

func (t *wsTransport) Name() string {
	return "websocket"
}

// Close waits for in-flight calls to complete (up to 10s), then closes the connection.
func (t *wsTransport) Close() error {
	done := make(chan struct{})

	go func() {
		t.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(closeTimeout):
	}

	return t.conn.Close()
}

// reconnect closes the current connection and establishes a new one with exponential backoff.
// Must be called with t.mu held.
func (t *wsTransport) reconnect() error {
	// Circuit breaker: prevent rapid reconnect cycling under persistent failures.
	// If we reconnected recently, wait for the cooldown period first.
	if sinceLastReconnect := time.Since(t.lastReconnect); sinceLastReconnect < reconnectCooldown {
		wait := reconnectCooldown - sinceLastReconnect
		time.Sleep(wait)
	}

	t.lastReconnect = time.Now()

	if telemetry.WSReconnects != nil {
		telemetry.WSReconnects.Add(context.Background(), 1)
	}

	_ = t.conn.Close()
	t.authed = false

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

		conn, err := dialWebSocket(t.host, t.insecureSkipVerify)
		if err != nil {
			lastErr = err

			continue
		}

		t.conn = conn

		if err := t.connect(); err != nil {
			_ = t.conn.Close()
			lastErr = err

			continue
		}

		if err := t.authenticate(); err != nil {
			_ = t.conn.Close()
			lastErr = err

			continue
		}

		t.authed = true

		return nil
	}

	return fmt.Errorf("reconnect failed after %d attempts: %w", maxReconnectAttempts, lastErr)
}

// connect sends the initial DDP connect handshake.
func (t *wsTransport) connect() error {
	t.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck

	if err := t.conn.WriteJSON(wsRequest{
		Msg:     "connect",
		Version: "1",
		Support: []string{"1"},
	}); err != nil {
		return fmt.Errorf("failed to send connect: %w", err)
	}

	t.conn.SetReadDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck

	var resp wsResponse
	if err := t.conn.ReadJSON(&resp); err != nil {
		return fmt.Errorf("failed to read connect response: %w", err)
	}

	if resp.Msg != "connected" {
		return fmt.Errorf("unexpected connect response: %s", resp.Msg)
	}

	return nil
}

// authenticate sends the auth.login_with_api_key method.
func (t *wsTransport) authenticate() error {
	t.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck

	if err := t.conn.WriteJSON(wsRequest{
		Msg:    "method",
		Method: "auth.login_with_api_key",
		ID:     "auth",
		Params: []any{t.apiKey.Reveal()},
	}); err != nil {
		return fmt.Errorf("failed to send auth request: %w", err)
	}

	t.conn.SetReadDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck

	var resp wsResponse
	if err := t.conn.ReadJSON(&resp); err != nil {
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	// Clear deadlines for normal operation
	t.conn.SetReadDeadline(time.Time{})  //nolint:errcheck
	t.conn.SetWriteDeadline(time.Time{}) //nolint:errcheck

	if resp.Error != nil {
		return fmt.Errorf("auth error: %s", resp.Error.Reason)
	}

	var result bool
	if err := json.Unmarshal(resp.Result, &result); err != nil || !result {
		return fmt.Errorf("authentication rejected — check TRUENAS_API_KEY")
	}

	t.authed = true

	return nil
}

var wsRequestCounter int64
var wsRequestMu sync.Mutex

func nextWSRequestID() string {
	wsRequestMu.Lock()
	defer wsRequestMu.Unlock()

	wsRequestCounter++

	return fmt.Sprintf("%d", wsRequestCounter)
}

// Call sends a method call over the WebSocket and reads the response.
// On connection failure, attempts to reconnect and retry once.
func (t *wsTransport) Call(ctx context.Context, method string, params any, result any) error {
	t.wg.Add(1)
	defer t.wg.Done()

	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.authed {
		if err := t.reconnect(); err != nil {
			return fmt.Errorf("not authenticated and reconnect failed: %w", err)
		}
	}

	err := t.doCall(ctx, method, params, result)
	if err == nil {
		return nil
	}

	// If the call failed due to a connection issue, try to reconnect and retry once.
	// API errors (from TrueNAS) are not retryable.
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

// doCall performs a single WebSocket call without reconnect logic.
func (t *wsTransport) doCall(ctx context.Context, method string, params any, result any) error {
	reqID := nextWSRequestID()

	req := wsRequest{
		Msg:    "method",
		Method: method,
		ID:     reqID,
		Params: normalizeParams(params),
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
		var resp wsResponse
		if err := t.conn.ReadJSON(&resp); err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		if resp.ID != reqID {
			continue
		}

		return t.handleResponse(&resp, result)
	}
}

func (t *wsTransport) handleResponse(resp *wsResponse, result any) error {
	if resp.Error != nil {
		return &APIError{
			Code:    resp.Error.Error,
			Message: resp.Error.Reason,
		}
	}

	if result != nil && resp.Result != nil {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("failed to unmarshal result: %w", err)
		}
	}

	return nil
}

// isAPIError checks if the error is a TrueNAS API error (not a connection error).
func isAPIError(err error, target **APIError) bool {
	if err == nil {
		return false
	}

	var apiErr *APIError

	if errors.As(err, &apiErr) { //nolint:govet
		if target != nil {
			*target = apiErr
		}

		return true
	}

	return false
}

// UploadFile uploads a file via the REST upload endpoint.
// filesystem.put requires pipe-based upload which isn't available over WebSocket calls,
// so we fall back to the HTTP multipart upload endpoint.
func (t *wsTransport) UploadFile(ctx context.Context, destPath string, data io.Reader, size int64) error {
	t.wg.Add(1)
	defer t.wg.Done()

	ctx, span := tracer.Start(ctx, "truenas.upload_file",
		trace.WithAttributes(
			attribute.String("file.path", destPath),
			attribute.Int64("file.size", size),
		),
	)
	defer span.End()

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer func() { _ = pw.Close() }()

		dataPart, err := writer.CreateFormField("data")
		if err != nil {
			pw.CloseWithError(err)

			return
		}

		dataJSON := fmt.Sprintf(`{"method": "filesystem.put", "params": [%q, {"mode": 493}]}`, destPath)
		if _, err = dataPart.Write([]byte(dataJSON)); err != nil {
			pw.CloseWithError(err)

			return
		}

		filePart, err := writer.CreateFormFile("file", "upload")
		if err != nil {
			pw.CloseWithError(err)

			return
		}

		if _, err = io.Copy(filePart, data); err != nil {
			pw.CloseWithError(err)

			return
		}

		_ = writer.Close()
	}()

	uploadURL := fmt.Sprintf("https://%s/_upload/", t.host)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, pr)
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.apiKey.Reveal())
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := t.uploadClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Limit error body read to 1MB to prevent OOM from malicious/broken server
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		err := fmt.Errorf("upload failed: status %d: %s", resp.StatusCode, string(body))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return err
	}

	if size > 0 && telemetry.ISOUploadBytes != nil {
		telemetry.ISOUploadBytes.Add(ctx, size)
	}

	span.SetStatus(codes.Ok, "")

	return nil
}
