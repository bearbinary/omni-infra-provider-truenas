//go:build integration

package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract tests verify that the TrueNAS JSON-RPC API methods we depend on
// exist and return the expected structure. These catch breaking API changes
// between TrueNAS versions.

func TestContract_SystemInfo(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	var info map[string]any
	err := c.Ping(ctx)
	require.NoError(t, err, "system.info should exist and be callable")

	// Verify system.info returns expected fields
	err = c.call(ctx, "system.info", nil, &info)
	require.NoError(t, err)
	assert.Contains(t, info, "version", "system.info should return 'version' field")
	assert.Contains(t, info, "hostname", "system.info should return 'hostname' field")
	assert.Contains(t, info, "physmem", "system.info should return 'physmem' field")
}

func TestContract_PoolQuery(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	var pools []map[string]any
	err := c.call(ctx, "pool.query", nil, &pools)
	require.NoError(t, err, "pool.query should exist and be callable")
	require.NotEmpty(t, pools, "should have at least one pool")

	pool := pools[0]
	assert.Contains(t, pool, "name", "pool should have 'name' field")
	assert.Contains(t, pool, "healthy", "pool should have 'healthy' field")
}

func TestContract_VMQuery(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	var vms []map[string]any
	err := c.call(ctx, "vm.query", nil, &vms)
	require.NoError(t, err, "vm.query should exist and be callable")
	// May be empty if no VMs exist — that's fine
}

func TestContract_VMDeviceNICAttachChoices(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	choices, err := c.NICAttachChoices(ctx)
	require.NoError(t, err, "vm.device.nic_attach_choices should exist and be callable")
	assert.NotEmpty(t, choices, "should have at least one NIC attach choice")
}

func TestContract_FilesystemStat(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	// /mnt always exists on TrueNAS
	var stat map[string]any
	err := c.call(ctx, "filesystem.stat", []any{"/mnt"}, &stat)
	require.NoError(t, err, "filesystem.stat should exist and be callable")
	assert.Contains(t, stat, "realpath", "filesystem.stat should return 'realpath' field")
	assert.Contains(t, stat, "type", "filesystem.stat should return 'type' field")
	assert.Contains(t, stat, "size", "filesystem.stat should return 'size' field")
}

func TestContract_FilesystemListdir(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	var entries []map[string]any
	err := c.call(ctx, "filesystem.listdir", []any{"/mnt"}, &entries)
	require.NoError(t, err, "filesystem.listdir should exist and be callable")

	if len(entries) > 0 {
		assert.Contains(t, entries[0], "name", "listdir entry should have 'name' field")
		assert.Contains(t, entries[0], "path", "listdir entry should have 'path' field")
		assert.Contains(t, entries[0], "type", "listdir entry should have 'type' field")
	}
}

func TestContract_DatasetQuery(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	var datasets []map[string]any
	filter := []any{
		[]any{[]any{"id", "=", pool}},
	}
	err := c.call(ctx, "pool.dataset.query", filter, &datasets)
	require.NoError(t, err, "pool.dataset.query should exist and be callable")
	require.NotEmpty(t, datasets, "should find the pool dataset")

	ds := datasets[0]
	assert.Contains(t, ds, "id", "dataset should have 'id' field")
	assert.Contains(t, ds, "name", "dataset should have 'name' field")
	assert.Contains(t, ds, "type", "dataset should have 'type' field")
}

func TestContract_ZFSSnapshotQuery(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	var snaps []map[string]any
	err := c.call(ctx, "zfs.snapshot.query", nil, &snaps)
	require.NoError(t, err, "zfs.snapshot.query should exist and be callable")
	// May be empty — that's fine, we just need the method to exist
}
