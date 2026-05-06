package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHealthz_Healthy(t *testing.T) {
	t.Parallel()

	s := NewServer(func(_ context.Context) error {
		return nil
	}, zap.NewNop())
	s.refresh(context.Background()) // populate cache before assertions

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp healthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
	assert.Empty(t, resp.Error)
	assert.NotEmpty(t, resp.LastOK)
}

func TestHealthz_Unhealthy(t *testing.T) {
	t.Parallel()

	s := NewServer(func(_ context.Context) error {
		return fmt.Errorf("TrueNAS API unreachable")
	}, zap.NewNop())
	s.refresh(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp healthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "error", resp.Status)
	// Error is intentionally generic — detailed reason is logged server-side
	// to avoid leaking pool names / IPs to unauthenticated /healthz callers.
	assert.NotEmpty(t, resp.Error)
	assert.NotContains(t, resp.Error, "unreachable",
		"raw upstream error must not leak via the health endpoint")
}

func TestHealthz_TracksLastOK(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	s := NewServer(func(_ context.Context) error {
		n := callCount.Add(1)
		if n > 1 {
			return fmt.Errorf("failed")
		}
		return nil
	}, zap.NewNop())

	// First refresh succeeds
	s.refresh(context.Background())

	req1 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w1 := httptest.NewRecorder()
	s.handleHealthz(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	var resp1 healthResponse
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &resp1))
	lastOK := resp1.LastOK

	// Second refresh fails; lastOK must be preserved.
	s.refresh(context.Background())

	req2 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w2 := httptest.NewRecorder()
	s.handleHealthz(w2, req2)
	assert.Equal(t, http.StatusServiceUnavailable, w2.Code)

	var resp2 healthResponse
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp2))
	assert.Equal(t, "error", resp2.Status)
	assert.Equal(t, lastOK, resp2.LastOK, "lastOK should be preserved from the successful call")
}

// TestHealthz_HandlerDoesNotCallChecker pins the amplification fix: a probe
// storm against /healthz must NOT translate into a probe storm against the
// TrueNAS backend. The handler now reads cached state; only the background
// refresh loop calls the checker.
func TestHealthz_HandlerDoesNotCallChecker(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	s := NewServer(func(_ context.Context) error {
		callCount.Add(1)
		return nil
	}, zap.NewNop())

	// One refresh to populate the cache. After this no further checker
	// calls should happen even under heavy handler traffic.
	s.refresh(context.Background())
	require.Equal(t, int32(1), callCount.Load(), "exactly one backend call after one refresh")

	for range 100 {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		s.handleHealthz(w, req)
	}

	assert.Equal(t, int32(1), callCount.Load(),
		"100 handler invocations must not call the checker — that was the amplification primitive")
}

// TestHealthz_BackgroundRefreshUpdatesState verifies the goroutine actually
// observes state changes. The background loop runs the checker every
// refreshInterval; we set a tight interval and watch for a transition.
func TestHealthz_BackgroundRefreshUpdatesState(t *testing.T) {
	t.Parallel()

	var healthy atomic.Bool

	s := NewServer(func(_ context.Context) error {
		if healthy.Load() {
			return nil
		}
		return fmt.Errorf("not yet")
	}, zap.NewNop(),
		WithRefreshInterval(20*time.Millisecond),
		WithCheckerTimeout(100*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.refresh(ctx) // initial — fails
	go s.refreshLoop(ctx)

	// Flip the upstream to healthy and wait for the loop to observe it.
	healthy.Store(true)

	require.Eventually(t, func() bool {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		s.handleHealthz(w, req)
		return w.Code == http.StatusOK
	}, time.Second, 10*time.Millisecond,
		"background refresh must observe the upstream becoming healthy")
}

// TestHealthz_PoolNameNotLeaked pins the scrubbing contract: an upstream
// error carrying reconnaissance data (pool names, internal IPs, request
// IDs) must NOT appear in the /healthz response body. Operators get the
// detail via server logs; unauthenticated callers see only a generic
// "check failed" message.
func TestHealthz_PoolNameNotLeaked(t *testing.T) {
	t.Parallel()

	// Scenarios that have actually shown up in the provider's logs over
	// v0.13–v0.14. Each embeds either a pool name, an IP, or a request ID.
	leakyErrors := []string{
		`pool "ssd-prod-secrets" not found on TrueNAS`,
		`connection refused dialing 10.0.42.17:80`,
		`vm.query for request-id 11111111-2222-3333-4444-555555555555 failed`,
		`Invalid VM name: omni_prod_secrets_cluster`,
	}

	for _, upstream := range leakyErrors {
		upstream := upstream
		t.Run(upstream, func(t *testing.T) {
			t.Parallel()

			s := NewServer(func(_ context.Context) error {
				return fmt.Errorf("%s", upstream)
			}, zap.NewNop())
			s.refresh(context.Background())

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			w := httptest.NewRecorder()
			s.handleHealthz(w, req)

			require.Equal(t, http.StatusServiceUnavailable, w.Code)

			var resp healthResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

			assert.NotContains(t, resp.Error, upstream,
				"upstream error text must not leak to unauthenticated /healthz callers")
			// The response must still indicate "something is wrong" or
			// observability pipelines can't tell healthy from unhealthy.
			assert.Equal(t, "error", resp.Status)
			assert.NotEmpty(t, resp.Error)
		})
	}
}

func TestReadyz_SameAsHealthz(t *testing.T) {
	t.Parallel()

	s := NewServer(func(_ context.Context) error {
		return nil
	}, zap.NewNop())
	s.refresh(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
