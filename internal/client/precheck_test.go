package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolFreeSpace_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.query", method)

		return []map[string]any{
			{"name": "tank", "healthy": true, "free": int64(500 * 1024 * 1024 * 1024)},
		}, nil
	})

	free, err := c.PoolFreeSpace(context.Background(), "tank")
	require.NoError(t, err)
	assert.Equal(t, int64(500*1024*1024*1024), free)
}

func TestPoolFreeSpace_NotFound(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return []map[string]any{}, nil
	})

	_, err := c.PoolFreeSpace(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSystemMemoryAvailable_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "system.info", method)

		return map[string]any{"physmem": int64(32 * 1024 * 1024 * 1024)}, nil
	})

	mem, err := c.SystemMemoryAvailable(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(32*1024*1024*1024), mem)
}
