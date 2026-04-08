package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testZvolPath = "tank/omni-vms/test-1"

func TestCreateZvol(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.dataset.create", method)
		assert.Contains(t, string(params), testZvolPath)
		assert.Contains(t, string(params), `"VOLUME"`)

		return Dataset{ID: testZvolPath, Name: "test-1", Type: "VOLUME"}, nil
	})

	ds, err := c.CreateZvol(context.Background(), testZvolPath, 40)
	require.NoError(t, err)
	assert.Equal(t, "VOLUME", ds.Type)
}

func TestEnsureDataset_AlreadyExists(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, alreadyExistsErr()
	})

	err := c.EnsureDataset(context.Background(), "tank/talos-iso")
	require.NoError(t, err)
}

func TestDeleteDataset_Success(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.dataset.delete", method)
		assert.Contains(t, string(params), testZvolPath)

		return true, nil
	})

	err := c.DeleteDataset(context.Background(), testZvolPath)
	require.NoError(t, err)
}

func TestDeleteDataset_NotFound(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, notFoundErr()
	})

	err := c.DeleteDataset(context.Background(), "tank/omni-vms/gone")
	require.NoError(t, err)
}

func TestFileExists_True(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "filesystem.stat", method)

		return map[string]any{"name": "test.iso", "size": 1024}, nil
	})

	exists, err := c.FileExists(context.Background(), "/mnt/tank/talos-iso/test.iso")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestFileExists_False(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, notFoundErr()
	})

	exists, err := c.FileExists(context.Background(), "/mnt/tank/talos-iso/missing.iso")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestPoolExists_True(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.query", method)

		return []map[string]any{{"id": 1, "name": "tank"}}, nil
	})

	exists, err := c.PoolExists(context.Background(), "tank")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPoolExists_False(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return []map[string]any{}, nil
	})

	exists, err := c.PoolExists(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestNetworkInterfaceValid_True(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.device.nic_attach_choices", method)

		return map[string]string{
			"br100":   "br100",
			"vlan666": "vlan666",
			"enp5s0":  "enp5s0",
		}, nil
	})

	valid, err := c.NetworkInterfaceValid(context.Background(), "vlan666")
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestNetworkInterfaceValid_False(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return map[string]string{
			"br100": "br100",
		}, nil
	})

	valid, err := c.NetworkInterfaceValid(context.Background(), "vlan999")
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestSHA256Dedup(t *testing.T) {
	url1 := "https://factory.talos.dev/image/abc/v1.8.0/nocloud-amd64.iso"
	url2 := "https://factory.talos.dev/image/def/v1.8.0/nocloud-amd64.iso"

	hash1 := sha256.Sum256([]byte(url1))
	hash2 := sha256.Sum256([]byte(url2))
	hash1Again := sha256.Sum256([]byte(url1))

	id1 := hex.EncodeToString(hash1[:])
	id2 := hex.EncodeToString(hash2[:])
	id1Again := hex.EncodeToString(hash1Again[:])

	assert.Equal(t, id1, id1Again)
	assert.NotEqual(t, id1, id2)
}
