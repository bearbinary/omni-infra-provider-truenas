package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetHostInfo_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "system.info", method)

		return map[string]any{
			"hostname": "truenas.local",
			"version":  "TrueNAS-SCALE-25.04",
			"physmem":  int64(32 * 1024 * 1024 * 1024),
			"cores":    12,
		}, nil
	})

	info, err := c.GetHostInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "truenas.local", info.Hostname)
	assert.Equal(t, 12, info.Cores)
	assert.Equal(t, int64(32*1024*1024*1024), info.Physmem)
}

func TestListPools_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.query", method)

		return []map[string]any{
			{"id": 1, "name": "tank", "healthy": true, "status": "ONLINE", "size": int64(1000), "free": int64(500), "allocated": int64(500)},
			{"id": 2, "name": "fast", "healthy": true, "status": "ONLINE", "size": int64(2000), "free": int64(1500), "allocated": int64(500)},
		}, nil
	})

	pools, err := c.ListPools(context.Background())
	require.NoError(t, err)
	assert.Len(t, pools, 2)
	assert.Equal(t, "tank", pools[0].Name)
	assert.True(t, pools[0].Healthy)
}

func TestListDisks_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "disk.query", method)

		return []map[string]any{
			{"name": "sda", "serial": "ABC123", "size": int64(1000000), "type": "SSD", "pool": "tank"},
		}, nil
	})

	disks, err := c.ListDisks(context.Background())
	require.NoError(t, err)
	assert.Len(t, disks, 1)
	assert.Equal(t, "sda", disks[0].Name)
}
