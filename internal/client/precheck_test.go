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
		assert.Equal(t, "pool.dataset.query", method)

		return map[string]any{
			"available": map[string]any{"parsed": int64(500 * 1024 * 1024 * 1024)},
			"used":      map[string]any{"parsed": int64(200 * 1024 * 1024 * 1024)},
		}, nil
	})

	free, err := c.PoolFreeSpace(context.Background(), "tank")
	require.NoError(t, err)
	assert.Equal(t, int64(500*1024*1024*1024), free)
}

func TestPoolFreeSpace_NotFound(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, &jsonRPCError{Code: ErrCodeNotFound, Message: "not found"}
	})

	_, err := c.PoolFreeSpace(context.Background(), "nonexistent")
	assert.Error(t, err)
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
