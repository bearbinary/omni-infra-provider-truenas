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

	"go.uber.org/zap"
)

// Checker defines the health check function.
type Checker func(ctx context.Context) error

// Server runs an HTTP health check endpoint.
type Server struct {
	checker Checker
	logger  *zap.Logger
	mu      sync.RWMutex
	lastErr error
	lastOK  time.Time
}

// NewServer creates a new health check HTTP server.
func NewServer(checker Checker, logger *zap.Logger) *Server {
	return &Server{
		checker: checker,
		logger:  logger.Named("health"),
	}
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
	}

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

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	err := s.checker(ctx)

	s.mu.Lock()
	s.lastErr = err
	if err == nil {
		s.lastOK = time.Now()
	}
	s.mu.Unlock()

	resp := healthResponse{
		Status: "ok",
	}

	if err != nil {
		// Return a generic status to unauthenticated callers. The underlying
		// error includes pool names, IPs, and request-ID fragments that are
		// reconnaissance data for an attacker who reaches /healthz via a
		// misconfigured Service or Ingress. Detail stays in server-side logs.
		s.logger.Warn("health check failed", zap.Error(err))

		resp.Status = "error"
		resp.Error = "check failed (see provider logs)"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	s.mu.RLock()
	if !s.lastOK.IsZero() {
		resp.LastOK = s.lastOK.Format(time.RFC3339)
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

type healthResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	LastOK string `json:"last_ok,omitempty"`
}
