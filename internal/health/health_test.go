package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHealthz_Healthy(t *testing.T) {
	t.Parallel()

	s := NewServer(func(_ context.Context) error {
		return nil
	}, zap.NewNop())

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

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp healthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "error", resp.Status)
	assert.Contains(t, resp.Error, "unreachable")
}

func TestHealthz_TracksLastOK(t *testing.T) {
	t.Parallel()

	callCount := 0
	s := NewServer(func(_ context.Context) error {
		callCount++
		if callCount > 1 {
			return fmt.Errorf("failed")
		}
		return nil
	}, zap.NewNop())

	// First call succeeds
	req1 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w1 := httptest.NewRecorder()
	s.handleHealthz(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	var resp1 healthResponse
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	lastOK := resp1.LastOK

	// Second call fails but lastOK is preserved
	req2 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w2 := httptest.NewRecorder()
	s.handleHealthz(w2, req2)
	assert.Equal(t, http.StatusServiceUnavailable, w2.Code)

	var resp2 healthResponse
	json.Unmarshal(w2.Body.Bytes(), &resp2)
	assert.Equal(t, "error", resp2.Status)
	assert.Equal(t, lastOK, resp2.LastOK, "lastOK should be preserved from the successful call")
}

func TestReadyz_SameAsHealthz(t *testing.T) {
	t.Parallel()

	s := NewServer(func(_ context.Context) error {
		return nil
	}, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
