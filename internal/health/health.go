// Package health provides an HTTP health check endpoint.
// Enables proper Kubernetes liveness/readiness probes that verify
// actual TrueNAS connectivity, not just process liveness.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

// Checker defines the health check function.
type Checker func(ctx context.Context) error

// Default cadence for the background refresh loop. Two seconds is short
// enough that a Kubernetes liveness probe (default 10s period) sees a
// recent verdict, but long enough that a probe storm — e.g. an exposed
// service hit from outside the cluster — collapses to one backend call
// per refreshInterval rather than one call per request.
const (
	defaultRefreshInterval = 2 * time.Second
	defaultCheckerTimeout  = 10 * time.Second
)

// healthTracer wraps each background refresh in a span so a slow TrueNAS
// round-trip on the health path appears in trace search alongside other
// provider operations. The previous per-request handler implicitly
// nested the checker call under the inbound request's parent span; the
// background-refresh model loses that nesting, and a span on `refresh`
// is what restores observability of the actual upstream call.
var healthTracer = otel.Tracer("truenas-health")

// Server runs an HTTP health check endpoint.
//
// The endpoint is unauthenticated by design (Kubernetes kubelet probes
// don't carry credentials), so the handler must remain cheap. Each request
// previously triggered a fresh TrueNAS WebSocket round-trip, which gave a
// caller who reached the listener through a misconfigured Service or
// Ingress an amplification primitive. The Server caches the most recent
// checker result and refreshes it on a fixed interval; handlers do an
// O(1) read.
type Server struct {
	checker         Checker
	logger          *zap.Logger
	refreshInterval time.Duration
	checkerTimeout  time.Duration

	mu      sync.RWMutex
	lastOK  time.Time
	lastErr error
}

// Option configures a Server. Functional options keep the common
// `NewServer(checker, logger)` call identical while letting callers
// (especially tests) override knobs without reaching into private
// struct fields.
type Option func(*Server)

// WithRefreshInterval overrides the background-refresh cadence.
func WithRefreshInterval(d time.Duration) Option {
	return func(s *Server) { s.refreshInterval = d }
}

// WithCheckerTimeout overrides the per-checker timeout. Must be ≤
// refreshInterval to avoid silent freshness degradation under upstream
// slowdowns.
func WithCheckerTimeout(d time.Duration) Option {
	return func(s *Server) { s.checkerTimeout = d }
}

// NewServer creates a new health check HTTP server.
func NewServer(checker Checker, logger *zap.Logger, opts ...Option) *Server {
	s := &Server{
		checker:         checker,
		logger:          logger.Named("health"),
		refreshInterval: defaultRefreshInterval,
		checkerTimeout:  defaultCheckerTimeout,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Run starts the HTTP server on the given address. Blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleHealthz) // Same check for both

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		// WriteTimeout caps the slow-client variant of the same probe-storm
		// DoS — without it, a caller that holds the connection open while
		// reading slowly can pin server resources indefinitely.
		WriteTimeout: 10 * time.Second,
		// IdleTimeout closes idle keep-alive connections so a flood of
		// short probes doesn't leave a tail of half-open sockets.
		IdleTimeout: 120 * time.Second,
	}

	// Populate the cache before the listener accepts traffic so the very
	// first request gets a real verdict instead of "no data yet". If the
	// initial check fails, that's also a real verdict — better to report
	// 503 than to flap "ok" for one tick.
	s.refresh(ctx)

	// Boot-time breadcrumb so the operator can tell from logs alone
	// whether the listener came up against a healthy or unhealthy
	// backend. Without this, a clean start with TrueNAS down looks
	// identical (in logs) to a clean start with TrueNAS up — only the
	// HTTP responses differ, and those aren't always captured.
	s.mu.RLock()
	initialErr := s.lastErr
	s.mu.RUnlock()
	if initialErr == nil {
		s.logger.Info("initial health check ok")
	} else {
		s.logger.Warn("initial health check failed; serving 503 until next refresh",
			zap.Error(initialErr),
		)
	}

	go s.refreshLoop(ctx)

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		srv.Shutdown(shutdownCtx) //nolint:errcheck
	}()

	s.logger.Info("health endpoint listening", zap.String("addr", addr))

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

// refreshLoop drives s.refresh on a fixed interval until ctx is cancelled.
// Runs in its own goroutine so a slow backend can't block handler reads.
func (s *Server) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(s.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refresh(ctx)
		}
	}
}

// refresh runs the checker once and caches the result. The lock is held
// only across the cache update — the checker itself runs unlocked so a
// slow TrueNAS round-trip can't stall handler reads.
//
// Wraps the checker call in an OTel span so a slow upstream round-trip
// shows up in trace search; increments HealthCheckErrors so the existing
// `TrueNASHealthCheckFailing` Prometheus alert can fire on transient
// failures (the previous design only incremented the counter inside
// specific checker subpaths in cmd/.../main.go, leaving WebSocket /
// timeout failures invisible to the alert).
func (s *Server) refresh(ctx context.Context) {
	refreshCtx, cancel := context.WithTimeout(ctx, s.checkerTimeout)
	defer cancel()

	refreshCtx, span := healthTracer.Start(refreshCtx, "health.refresh")
	defer span.End()

	err := s.checker(refreshCtx)

	s.mu.Lock()
	s.lastErr = err
	if err == nil {
		s.lastOK = time.Now()
	}
	s.mu.Unlock()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "health check failed")

		if telemetry.HealthCheckErrors != nil {
			telemetry.HealthCheckErrors.Add(refreshCtx, 1)
		}

		// The underlying error includes pool names, IPs, and request-ID
		// fragments that are reconnaissance data for an attacker who
		// reaches /healthz via a misconfigured Service or Ingress.
		// Detail stays in server-side logs only; the handler scrubs it
		// from the response body.
		s.logger.Warn("health check failed", zap.Error(err))
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	err := s.lastErr
	lastOK := s.lastOK
	s.mu.RUnlock()

	resp := healthResponse{
		Status: "ok",
	}

	if err != nil {
		// Return a generic status to unauthenticated callers. The underlying
		// error includes pool names, IPs, and request-ID fragments that are
		// reconnaissance data for an attacker who reaches /healthz via a
		// misconfigured Service or Ingress. Detail stays in server-side logs.
		resp.Status = "error"
		resp.Error = "check failed (see provider logs)"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if !lastOK.IsZero() {
		resp.LastOK = lastOK.Format(time.RFC3339)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

type healthResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	LastOK string `json:"last_ok,omitempty"`
}
