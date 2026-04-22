package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

	// wsMaxMessageBytes caps a single WebSocket frame. TrueNAS replies (even
	// large `pool.dataset.query` results) fit well under this ceiling; the cap
	// is a DoS guard against a malicious or compromised server that returns
	// multi-GiB frames to OOM the provider.
	wsMaxMessageBytes = 16 << 20
)

// isLoopbackHost returns true when host is a loopback literal or an IP that
// resolves to loopback. Used to quiet the cleartext-fallback warning on
// dev/CI setups, which legitimately use ws://127.0.0.1.
func isLoopbackHost(host string) bool {
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	if hostname == "localhost" {
		return true
	}

	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback()
	}

	return false
}

// validateHost rejects TRUENAS_HOST values that would let an attacker smuggle a
// different destination into the Bearer-token upload URL. Accepts bare host or
// host:port; rejects schemes, paths, user-info, query, and fragment components.
func validateHost(host string) error {
	if host == "" {
		return fmt.Errorf("host is empty")
	}

	if strings.ContainsAny(host, "/?#@ \t\r\n") {
		return fmt.Errorf("host %q must be a bare hostname or host:port (no scheme, path, or user-info)", host)
	}

	u, err := url.Parse("https://" + host)
	if err != nil {
		return fmt.Errorf("host %q is not a valid hostname: %w", host, err)
	}

	if u.Host != host {
		return fmt.Errorf("host %q normalized to %q — use a bare hostname or host:port", host, u.Host)
	}

	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("host %q must be a bare hostname or host:port", host)
	}

	// Extract hostname without port; allow IP literals and DNS labels.
	hostname := u.Hostname()
	if hostname == "" {
		return fmt.Errorf("host %q has no hostname component", host)
	}

	// IPv4/IPv6 literals are fine as-is.
	if net.ParseIP(hostname) != nil {
		return nil
	}

	// DNS labels: letters, digits, hyphens, dots. No underscores or other surprises.
	for _, r := range hostname {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.':
		default:
			return fmt.Errorf("host %q contains invalid character %q", host, r)
		}
	}

	return nil
}

// normalizeParams ensures params are sent as a JSON array (TrueNAS middleware expects positional params).
func normalizeParams(params any) any {
	if params == nil {
		return []any{}
	}

	switch params.(type) {
	case []any, []map[string]any, []string, []int:
		return params
	default:
		return []any{params}
	}
}

// wsTransport implements Transport over a WebSocket connection to TrueNAS.
// Used for all deployments (local and remote) since TrueNAS 25.10.
// Requires an API key for authentication.
//
// TrueNAS uses a DDP-like WebSocket protocol (not pure JSON-RPC 2.0):
//   - Initial "connect" handshake required
//   - Messages use "msg" field ("method", "result", "connected")
//   - Method calls wrapped with "msg": "method"
//
// Concurrency model (v0.15+):
//   - A single reader goroutine owns all reads from the websocket.Conn and
//     demultiplexes responses to per-request channels keyed by request ID.
//   - writeMu serializes writes (gorilla/websocket forbids concurrent writes).
//   - connMu guards connection swap on reconnect; read-held for call path,
//     write-held for reconnect.
//
// This replaces the earlier design where one big mutex wrapped both the
// write and read, causing cascading timeouts when one slow call blocked
// others, and leaving context cancellation unable to unblock waiters.
type wsTransport struct {
	connMu sync.RWMutex
	conn   *websocket.Conn
	authed bool

	// writeSem is a 1-slot semaphore gating writes to conn (gorilla/websocket
	// forbids concurrent writes). Implemented as a channel rather than a
	// sync.Mutex so acquisition can honor ctx cancellation in a single
	// select without spawning a goroutine per Call.
	writeSem chan struct{}

	// Pending-call registry. Keyed by request ID; each entry is a buffered
	// (cap=1) channel that the reader goroutine sends the full response on.
	pendingMu sync.Mutex
	pending   map[string]chan *wsResponse

	// Reader goroutine lifecycle. Re-initialized on every reconnect.
	readerDone chan struct{}
	readerErr  error // guarded by pendingMu

	apiKey             SecretString
	host               string
	insecureSkipVerify bool
	wg                 sync.WaitGroup
	lastReconnect      time.Time    // Circuit breaker: minimum time between reconnect bursts
	uploadClient       *http.Client // Reused for file uploads to benefit from connection pooling
	closed             bool         // guarded by connMu (write side)
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
	if err := validateHost(host); err != nil {
		return nil, err
	}

	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecureSkipVerify, //nolint:gosec
		},
		HandshakeTimeout: 10 * time.Second,
	}

	wsURL := fmt.Sprintf("wss://%s/websocket", host)

	conn, resp, err := dialer.Dial(wsURL, nil)
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

		// Cleartext fallback — loudly warn so an operator who set
		// TRUENAS_INSECURE_SKIP_VERIFY=true for a dev cert issue does not
		// silently downgrade a production deploy. The API key and control-plane
		// traffic are now transiting cleartext. Suppressed for loopback because
		// dev/CI setups hit ws://127.0.0.1 legitimately.
		if !isLoopbackHost(host) {
			slog.Warn("truenas websocket falling back to unencrypted ws:// — API key is sent in cleartext",
				slog.String("host", host),
				slog.String("remediation", "unset TRUENAS_INSECURE_SKIP_VERIFY or fix the TLS cert"),
			)
		}

		wsURL = fmt.Sprintf("ws://%s/websocket", host)

		conn, resp, err = dialer.Dial(wsURL, nil)
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

		return nil, fmt.Errorf("unexpected HTTP status %d from %s", resp.StatusCode, wsURL)
	}

	conn.SetReadLimit(wsMaxMessageBytes)

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
		pending:            make(map[string]chan *wsResponse),
		writeSem:           make(chan struct{}, 1),
		uploadClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: insecureSkipVerify, //nolint:gosec
				},
			},
			// Never auto-follow redirects: a malicious or misconfigured server
			// could 3xx us into re-sending the Bearer token to a different host.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Timeout: 5 * time.Minute,
		},
	}

	// Handshake + auth use synchronous reads under deadline before the reader
	// goroutine takes over. Once the reader is started, all reads go through
	// the demultiplexer.
	if err := t.handshakeAndAuth(); err != nil {
		_ = conn.Close()

		return nil, err
	}

	t.startReader()

	return t, nil
}

// handshakeAndAuth runs the initial DDP connect + auth.login_with_api_key
// sequence on a freshly-dialed connection. Must be called before the reader
// goroutine is started.
func (t *wsTransport) handshakeAndAuth() error {
	if err := t.connect(); err != nil {
		return fmt.Errorf("connect handshake failed: %w", err)
	}

	if err := t.authenticate(); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	return nil
}

// startReader launches the goroutine that owns all reads on the current
// connection. Responses are demuxed to pending entries by request ID;
// connection errors fail every in-flight caller and set readerErr. Must be
// called after a successful handshake, with no prior reader running.
func (t *wsTransport) startReader() {
	t.readerDone = make(chan struct{})

	go t.readLoop(t.conn, t.readerDone)
}

// readLoop is the reader goroutine. It exits when the connection returns an
// error (typically: peer closed, Close() called, or network fault) and fails
// all pending calls with that error. A new reader is started on reconnect.
func (t *wsTransport) readLoop(conn *websocket.Conn, done chan struct{}) {
	defer close(done)

	for {
		var resp wsResponse
		if err := conn.ReadJSON(&resp); err != nil {
			t.pendingMu.Lock()
			t.readerErr = err

			for id, ch := range t.pending {
				// Non-blocking send: the cap-1 channel guarantees delivery
				// unless a response was already delivered, in which case the
				// waiter has already consumed it.
				select {
				case ch <- &wsResponse{ID: id, Error: &wsError{Error: -1, Reason: "connection lost: " + err.Error()}}:
				default:
				}
			}

			t.pendingMu.Unlock()

			return
		}

		t.pendingMu.Lock()
		ch, ok := t.pending[resp.ID]
		t.pendingMu.Unlock()

		if !ok {
			// Orphan response (late arrival after ctx-cancel); drop silently.
			continue
		}

		select {
		case ch <- &resp:
		default:
			// Channel full means a duplicate response for this ID — drop.
		}
	}
}

// registerPending allocates a channel for a request ID and adds it to the
// pending map. Returns the channel + a cleanup closure.
func (t *wsTransport) registerPending(reqID string) (<-chan *wsResponse, func()) {
	ch := make(chan *wsResponse, 1)

	t.pendingMu.Lock()
	t.pending[reqID] = ch
	t.pendingMu.Unlock()

	return ch, func() {
		t.pendingMu.Lock()
		delete(t.pending, reqID)
		t.pendingMu.Unlock()
	}
}

func (t *wsTransport) Name() string {
	return "websocket"
}

// Close waits for in-flight calls to complete (up to 10s), then closes the connection.
// The final close itself is bounded so a half-open TCP (no RST, no FIN) can't wedge
// shutdown — we set SO_LINGER=0 and then run Close under a short deadline.
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

	t.connMu.Lock()
	t.closed = true
	conn := t.conn
	t.connMu.Unlock()

	// Drop unsent bytes immediately (SO_LINGER=0) on the underlying TCP socket
	// so a dead peer doesn't block the Close call.
	if underlying := conn.UnderlyingConn(); underlying != nil {
		if tcpConn, ok := underlying.(*net.TCPConn); ok {
			_ = tcpConn.SetLinger(0)
		}
	}

	closeDone := make(chan error, 1)

	go func() {
		closeDone <- conn.Close()
	}()

	select {
	case err := <-closeDone:
		return err
	case <-time.After(closeTimeout):
		return fmt.Errorf("websocket close timed out after %s", closeTimeout)
	}
}

// reconnect closes the current connection, waits for the reader goroutine to
// drain (failing all pending calls), and establishes a new connection with
// exponential backoff. Takes connMu write lock for the duration so no caller
// can observe a half-transition.
func (t *wsTransport) reconnect() error {
	t.connMu.Lock()
	defer t.connMu.Unlock()

	if t.closed {
		return fmt.Errorf("transport is closed")
	}

	// Circuit breaker: prevent rapid reconnect cycling under persistent failures.
	if sinceLastReconnect := time.Since(t.lastReconnect); sinceLastReconnect < reconnectCooldown {
		wait := reconnectCooldown - sinceLastReconnect
		time.Sleep(wait)
	}

	t.lastReconnect = time.Now()

	if telemetry.WSReconnects != nil {
		telemetry.WSReconnects.Add(context.Background(), 1)
	}

	// Close old conn. The reader goroutine will observe the error, fail all
	// pending calls, and exit.
	oldConn := t.conn
	oldReaderDone := t.readerDone
	_ = oldConn.Close()
	t.authed = false

	if oldReaderDone != nil {
		<-oldReaderDone
	}

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

		if err := t.handshakeAndAuth(); err != nil {
			_ = t.conn.Close()
			lastErr = err

			continue
		}

		// Reset the reader error before starting a fresh reader so new calls
		// don't inherit the previous connection's failure.
		t.pendingMu.Lock()
		t.readerErr = nil
		t.pendingMu.Unlock()

		t.startReader()

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
		// Sanitize: TrueNAS middleware has historically echoed request params
		// back in error reasons. If the Reason string contains a long
		// alphanumeric substring that resembles an API key, redact it before
		// wrapping into an error that may be logged upstream.
		return fmt.Errorf("auth error: %s", redactLikelySecrets(resp.Error.Reason))
	}

	var result bool
	if err := json.Unmarshal(resp.Result, &result); err != nil || !result {
		return fmt.Errorf("authentication rejected — check TRUENAS_API_KEY")
	}

	t.authed = true

	return nil
}

// wsRequestCounter is the monotonic source of JSON-RPC request IDs. Uses
// atomic.Int64 instead of a mutex because each Call issues exactly one
// increment — the mutex-plus-counter form was a net loss under the race
// detector and a wash in production.
var wsRequestCounter atomic.Int64

func nextWSRequestID() string {
	return strconv.FormatInt(wsRequestCounter.Add(1), 10)
}

// Call sends a method call over the WebSocket and awaits the response.
// On connection failure, attempts to reconnect and retry once.
//
// Unlike the pre-v0.15 implementation, this Call blocks only on its own
// pending channel + its own ctx — it does NOT serialize behind a call-global
// mutex, so a slow call on one goroutine can't stall unrelated calls on
// other goroutines, and ctx cancellation unblocks the waiter immediately.
func (t *wsTransport) Call(ctx context.Context, method string, params any, result any) error {
	t.wg.Add(1)
	defer t.wg.Done()

	// Fast-path: try once, reconnect + retry on connection errors.
	err := t.doCall(ctx, method, params, result)
	if err == nil {
		return nil
	}

	// API errors (from TrueNAS middleware) are not retryable.
	var apiErr *APIError
	if isAPIError(err, &apiErr) {
		return err
	}

	// ctx cancellations are not retryable either.
	if ctx.Err() != nil {
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

// doCall performs a single WebSocket call against the currently-held
// connection without reconnect logic. Writes are serialized by writeMu;
// reads are demultiplexed by the reader goroutine.
func (t *wsTransport) doCall(ctx context.Context, method string, params any, result any) error {
	reqID := nextWSRequestID()

	respCh, cleanup := t.registerPending(reqID)
	defer cleanup()

	req := wsRequest{
		Msg:    "method",
		Method: method,
		ID:     reqID,
		Params: normalizeParams(params),
	}

	if err := t.writeRequest(ctx, req); err != nil {
		return err
	}

	// Default deadline mirrors the legacy behavior so callers without a ctx
	// deadline still get a 30s ceiling instead of hanging indefinitely.
	var timeoutCh <-chan time.Time
	if _, ok := ctx.Deadline(); !ok {
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	// Snapshot the current reader's done channel. If reconnect happens while
	// we're waiting, the snapshot closes and we return promptly (with the
	// reader error) so the caller can retry against the new connection.
	t.connMu.RLock()
	readerDone := t.readerDone
	t.connMu.RUnlock()

	select {
	case resp := <-respCh:
		return t.handleResponse(resp, result)
	case <-ctx.Done():
		return ctx.Err()
	case <-timeoutCh:
		return fmt.Errorf("call %q timed out after 30s", method)
	case <-readerDone:
		t.pendingMu.Lock()
		readerErr := t.readerErr
		t.pendingMu.Unlock()

		if readerErr != nil {
			return fmt.Errorf("connection lost during call: %w", readerErr)
		}

		return fmt.Errorf("connection lost during call")
	}
}

// writeRequest serializes a JSON-RPC request onto the current connection
// using a 1-slot semaphore (writeSem). Acquisition honors ctx cancellation
// via a single select — no goroutine spawned, no heap allocation for the
// common uncontended path. Release is a single channel receive.
func (t *wsTransport) writeRequest(ctx context.Context, req wsRequest) error {
	select {
	case t.writeSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	defer func() { <-t.writeSem }()

	t.connMu.RLock()
	conn := t.conn
	authed := t.authed
	t.connMu.RUnlock()

	if !authed {
		return fmt.Errorf("not authenticated")
	}

	writeDeadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(writeDeadline) {
		writeDeadline = d
	}

	_ = conn.SetWriteDeadline(writeDeadline)

	if err := conn.WriteJSON(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	return nil
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

		// Build the filesystem.put envelope via json.Marshal rather than
		// string formatting; %q produces Go-quoted strings which diverge from
		// JSON string rules for some Unicode code points. destPath is
		// provisioner-sourced today, but belt-and-braces — never hand-roll JSON.
		dataJSON, merr := json.Marshal(map[string]any{
			"method": "filesystem.put",
			"params": []any{destPath, map[string]any{"mode": 493}},
		})
		if merr != nil {
			pw.CloseWithError(merr)

			return
		}

		if _, err = dataPart.Write(dataJSON); err != nil {
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

	// Build the URL via net/url to make structural guarantees explicit —
	// Host was checked by validateHost at construction so this is redundant,
	// but hand-formatted URLs have bitten us before (fmt.Sprintf + unvalidated
	// host = bearer exfil). Never again.
	uploadURL := (&url.URL{Scheme: "https", Host: t.host, Path: "/_upload/"}).String()

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
