package client

import (
	"context"
	"encoding/json"
	"strings"
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
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		switch method {
		case "pool.query":
			return []map[string]any{
				{"id": 1, "name": "tank", "healthy": true, "status": "ONLINE", "size": int64(1000)},
				{"id": 2, "name": "fast", "healthy": true, "status": "ONLINE", "size": int64(2000)},
			}, nil
		case "pool.dataset.query":
			raw := string(params)
			if strings.Contains(raw, "tank") {
				return map[string]any{"available": map[string]any{"parsed": int64(500)}, "used": map[string]any{"parsed": int64(500)}}, nil
			}
			return map[string]any{"available": map[string]any{"parsed": int64(1500)}, "used": map[string]any{"parsed": int64(500)}}, nil
		}
		return nil, nil
	})

	pools, err := c.ListPools(context.Background())
	require.NoError(t, err)
	assert.Len(t, pools, 2)
	assert.Equal(t, "tank", pools[0].Name)
	assert.True(t, pools[0].Healthy)
	assert.Equal(t, int64(500), pools[0].Free)
	assert.Equal(t, int64(1500), pools[1].Free)
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
