package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateEncryptedZvol_Success(t *testing.T) {
	var receivedParams json.RawMessage

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.dataset.create", method)
		receivedParams = params

		return Dataset{ID: "tank/omni-vms/test-enc", Name: "test-enc", Type: "VOLUME"}, nil
	})

	ds, err := c.CreateEncryptedZvol(context.Background(), "tank/omni-vms/test-enc", 40, "my-secret-passphrase")
	require.NoError(t, err)
	assert.Equal(t, "VOLUME", ds.Type)

	// Verify encryption params were sent
	assert.Contains(t, string(receivedParams), `"encryption":true`)
	assert.Contains(t, string(receivedParams), `"aes-256-gcm"`)
	assert.Contains(t, string(receivedParams), `"my-secret-passphrase"`)
}

func TestUnlockDataset_Success(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.dataset.unlock", method)
		assert.Contains(t, string(params), "tank/omni-vms/test-enc")
		assert.Contains(t, string(params), "my-passphrase")

		return nil, nil
	})

	err := c.UnlockDataset(context.Background(), "tank/omni-vms/test-enc", "my-passphrase")
	require.NoError(t, err)
}

func TestLockDataset_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.dataset.lock", method)

		return nil, nil
	})

	err := c.LockDataset(context.Background(), "tank/omni-vms/test-enc")
	require.NoError(t, err)
}

func TestIsDatasetLocked_Locked(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return map[string]any{"locked": true}, nil
	})

	locked, err := c.IsDatasetLocked(context.Background(), "tank/omni-vms/test-enc")
	require.NoError(t, err)
	assert.True(t, locked)
}

func TestIsDatasetLocked_Unlocked(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return map[string]any{"locked": false}, nil
	})

	locked, err := c.IsDatasetLocked(context.Background(), "tank/omni-vms/test-enc")
	require.NoError(t, err)
	assert.False(t, locked)
}
