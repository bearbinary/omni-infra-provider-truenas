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

// --- Snapshot Tests ---

func TestCreateSnapshot_Success(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "zfs.snapshot.create", method)
		assert.Contains(t, string(params), "tank/omni-vms/test")
		assert.Contains(t, string(params), "omni-pre-upgrade")

		return nil, nil
	})

	err := c.CreateSnapshot(context.Background(), "tank/omni-vms/test", "omni-pre-upgrade-v1.12.5")
	require.NoError(t, err)
}

func TestListSnapshots_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "zfs.snapshot.query", method)

		return []Snapshot{
			{ID: "tank/omni-vms/test@snap1", Name: "snap1"},
			{ID: "tank/omni-vms/test@snap2", Name: "snap2"},
		}, nil
	})

	snaps, err := c.ListSnapshots(context.Background(), "tank/omni-vms/test")
	require.NoError(t, err)
	assert.Len(t, snaps, 2)
}

func TestDeleteSnapshot_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "zfs.snapshot.delete", method)

		return true, nil
	})

	err := c.DeleteSnapshot(context.Background(), "tank/omni-vms/test@snap1")
	require.NoError(t, err)
}

func TestDeleteSnapshot_NotFound(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, notFoundErr()
	})

	err := c.DeleteSnapshot(context.Background(), "tank/omni-vms/test@gone")
	require.NoError(t, err) // Idempotent
}

func TestRollbackSnapshot_Success(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "zfs.snapshot.rollback", method)
		assert.Contains(t, string(params), "tank/omni-vms/test@snap1")

		return true, nil
	})

	err := c.RollbackSnapshot(context.Background(), "tank/omni-vms/test@snap1")
	require.NoError(t, err)
}
