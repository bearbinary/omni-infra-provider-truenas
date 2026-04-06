package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
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
	apiKey             string
	host               string
	insecureSkipVerify bool
	mu                 sync.Mutex
	authed             bool
}

// TrueNAS WebSocket message types.
type wsRequest struct {
	Msg     string `json:"msg"`
	Method  string `json:"method,omitempty"`
	ID      string `json:"id,omitempty"`
	Params  any    `json:"params,omitempty"`
	Version string `json:"version,omitempty"`
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

// newWSTransport creates a WebSocket transport and authenticates.
func newWSTransport(host, apiKey string, insecureSkipVerify bool) (*wsTransport, error) {
	url := fmt.Sprintf("wss://%s/websocket", host)

	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecureSkipVerify, //nolint:gosec
		},
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.Dial(url, nil)
	if err != nil {
		// Try without TLS as fallback
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
		conn.Close()

		return nil, fmt.Errorf("unexpected HTTP status %d from %s", resp.StatusCode, url)
	}

	t := &wsTransport{
		conn:               conn,
		apiKey:             apiKey,
		host:               host,
		insecureSkipVerify: insecureSkipVerify,
	}

	// TrueNAS requires a "connect" handshake before any method calls
	if err := t.connect(); err != nil {
		conn.Close()

		return nil, fmt.Errorf("connect handshake failed: %w", err)
	}

	// Authenticate with API key
	if err := t.authenticate(); err != nil {
		conn.Close()

		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	return t, nil
}

func (t *wsTransport) Name() string {
	return "websocket"
}

func (t *wsTransport) Close() error {
	return t.conn.Close()
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
		Params: []any{t.apiKey},
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

	// Check the result is true
	var result bool
	if err := json.Unmarshal(resp.Result, &result); err != nil || !result {
		return fmt.Errorf("authentication rejected — check TRUENAS_API_KEY")
	}

	t.authed = true

	return nil
}

// requestID generates string IDs for the DDP protocol.
var wsRequestCounter int64
var wsRequestMu sync.Mutex

func nextWSRequestID() string {
	wsRequestMu.Lock()
	defer wsRequestMu.Unlock()

	wsRequestCounter++

	return fmt.Sprintf("%d", wsRequestCounter)
}

// Call sends a method call over the WebSocket and reads the response.
func (t *wsTransport) Call(ctx context.Context, method string, params any, result any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.authed {
		return fmt.Errorf("not authenticated")
	}

	reqID := nextWSRequestID()

	req := wsRequest{
		Msg:    "method",
		Method: method,
		ID:     reqID,
		Params: normalizeParams(params),
	}

	// Set deadlines from context or default 30s timeout
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}

	t.conn.SetWriteDeadline(deadline) //nolint:errcheck
	t.conn.SetReadDeadline(deadline)  //nolint:errcheck

	if err := t.conn.WriteJSON(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	// Read responses until we get one matching our request ID.
	// TrueNAS may send subscription events on the same connection.
	for {
		var resp wsResponse
		if err := t.conn.ReadJSON(&resp); err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		if resp.ID != reqID {
			// Skip subscription events or responses to other requests
			continue
		}

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
}

// UploadFile uploads a file via the REST upload endpoint.
// filesystem.put requires pipe-based upload which isn't available over WebSocket calls,
// so we fall back to the HTTP multipart upload endpoint.
func (t *wsTransport) UploadFile(ctx context.Context, destPath string, data io.Reader, size int64) error {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()

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

		writer.Close()
	}()

	uploadURL := fmt.Sprintf("https://%s/_upload/", t.host)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, pr)
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: t.insecureSkipVerify, //nolint:gosec
			},
		},
		Timeout: 5 * time.Minute, // ISOs can be large
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("upload failed: status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
