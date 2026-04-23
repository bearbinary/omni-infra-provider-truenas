package autoscaler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	truenasclient "github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

// TestTrueNASCapacityAdapter_PoolFreeBytes verifies the adapter routes
// pool.query responses to the correct pool by name and surfaces a
// clear error when the operator points at a pool that doesn't exist
// on the host. Uses NewMockClient so no real TrueNAS connection is
// needed.
func TestTrueNASCapacityAdapter_PoolFreeBytes(t *testing.T) {
	t.Parallel()

	const gib = int64(1024 * 1024 * 1024)

	t.Run("returns free bytes for named pool", func(t *testing.T) {
		t.Parallel()

		c := truenasclient.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
			switch method {
			case "pool.query":
				return []map[string]any{
					{"id": 1, "name": "tank", "healthy": true, "status": "ONLINE", "size": 10 * gib},
					{"id": 2, "name": "fast", "healthy": true, "status": "ONLINE", "size": 20 * gib},
				}, nil
			case "pool.dataset.query":
				// Simulate the root-dataset query ListPools runs per pool.
				// Report 100 GiB available on `tank`.
				return map[string]any{
					"available": map[string]any{"parsed": 100 * gib},
					"used":      map[string]any{"parsed": 50 * gib},
				}, nil
			}

			return nil, nil
		})

		adapter := NewTrueNASCapacityAdapter(c)

		free, err := adapter.PoolFreeBytes(context.Background(), "tank")
		require.NoError(t, err)
		assert.Equal(t, 100*gib, free)
	})

	t.Run("pool not found surfaces as a clear error", func(t *testing.T) {
		t.Parallel()

		c := truenasclient.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
			switch method {
			case "pool.query":
				return []map[string]any{
					{"id": 1, "name": "tank", "healthy": true, "status": "ONLINE", "size": 10 * gib},
				}, nil
			case "pool.dataset.query":
				return map[string]any{
					"available": map[string]any{"parsed": gib},
					"used":      map[string]any{"parsed": gib},
				}, nil
			}

			return nil, nil
		})

		adapter := NewTrueNASCapacityAdapter(c)

		_, err := adapter.PoolFreeBytes(context.Background(), "missing-pool")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing-pool")
		assert.Contains(t, err.Error(), "not found")
	})
}

// TestTrueNASCapacityAdapter_HostFreeMemoryBytes pins the intentional
// not-implemented behavior. When the TrueNAS system.mem_info wrapper
// lands, this test flips to an actual assertion — until then, operator-
// facing docs direct users to `autoscale-min-host-mem-gib=0` and this
// test guards against an accidental silent-zero implementation that
// would always satisfy the memory gate.
func TestTrueNASCapacityAdapter_HostFreeMemoryBytes(t *testing.T) {
	t.Parallel()

	c := truenasclient.NewMockClient(func(_ string, _ json.RawMessage) (any, error) {
		t.Fatal("HostFreeMemoryBytes should not issue an RPC until implemented")
		return nil, nil
	})

	adapter := NewTrueNASCapacityAdapter(c)

	_, err := adapter.HostFreeMemoryBytes(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHostMemNotImplemented),
		"callers must be able to errors.Is for the sentinel")
	assert.Contains(t, err.Error(), "autoscale-min-host-mem-gib=0",
		"error message must tell operators how to disable the check until the wrapper lands")
}

// TestTrueNASCapacityAdapter_SatisfiesInterface is a compile-time check:
// if a future refactor breaks the CapacityQuery contract, this file
// won't build. Runtime assertion belt and suspenders — Go's type
// system would already catch most of it at build time.
func TestTrueNASCapacityAdapter_SatisfiesInterface(_ *testing.T) {
	var _ CapacityQuery = (*TrueNASCapacityAdapter)(nil)
}
