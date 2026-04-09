package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Zvol Resize Tests ---

func TestGetZvolSize_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.dataset.query", method)

		return map[string]any{
			"volsize": map[string]any{"parsed": int64(40 * 1024 * 1024 * 1024)},
		}, nil
	})

	size, err := c.GetZvolSize(context.Background(), "tank/omni-vms/test")
	require.NoError(t, err)
	assert.Equal(t, int64(40*1024*1024*1024), size)
}

func TestResizeZvol_Success(t *testing.T) {
	var updatedSize int64

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.dataset.update", method)

		var p []json.RawMessage
		json.Unmarshal(params, &p) //nolint:errcheck

		var opts map[string]any
		json.Unmarshal(p[1], &opts) //nolint:errcheck
		updatedSize = int64(opts["volsize"].(float64))

		return nil, nil
	})

	err := c.ResizeZvol(context.Background(), "tank/omni-vms/test", 80)
	require.NoError(t, err)
	assert.Equal(t, int64(80*1024*1024*1024), updatedSize)
}
