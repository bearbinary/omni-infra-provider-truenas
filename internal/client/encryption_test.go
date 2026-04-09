package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateEncryptedZvol_Success(t *testing.T) {
	t.Parallel()

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
	assert.Contains(t, string(receivedParams), `"AES-256-GCM"`)
	assert.Contains(t, string(receivedParams), `"my-secret-passphrase"`)
}

func TestCreateEncryptedZvol_InheritEncryptionFalse(t *testing.T) {
	t.Parallel()

	var receivedParams json.RawMessage

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		receivedParams = params
		return Dataset{ID: "tank/test"}, nil
	})

	_, err := c.CreateEncryptedZvol(context.Background(), "tank/test", 10, "pass")
	require.NoError(t, err)

	// TrueNAS 25.10+ requires inherit_encryption: false when encryption is enabled
	assert.Contains(t, string(receivedParams), `"inherit_encryption":false`)
}

func TestCreateEncryptedZvol_UserPropertiesListFormat(t *testing.T) {
	t.Parallel()

	var receivedParams json.RawMessage

	c := newMockClient(t, func(_ string, params json.RawMessage) (any, *jsonRPCError) {
		receivedParams = params
		return Dataset{ID: "tank/test"}, nil
	})

	props := []UserProperty{
		{Key: "org.omni:managed", Value: "true"},
		{Key: "org.omni:passphrase", Value: "secret123"},
	}

	_, err := c.CreateEncryptedZvol(context.Background(), "tank/test", 10, "pass", props)
	require.NoError(t, err)

	// TrueNAS 25.10+ expects user_properties as list of {key, value} objects
	assert.Contains(t, string(receivedParams), `"key":"org.omni:managed"`)
	assert.Contains(t, string(receivedParams), `"value":"true"`)
	assert.Contains(t, string(receivedParams), `"key":"org.omni:passphrase"`)
	assert.Contains(t, string(receivedParams), `"value":"secret123"`)
}

func TestGetDatasetUserProperty_Found(t *testing.T) {
	t.Parallel()

	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.dataset.query", method)
		return map[string]any{
			"user_properties": map[string]any{
				"org.omni:passphrase": map[string]any{"value": "stored-secret-abc"},
				"org.omni:managed":    map[string]any{"value": "true"},
			},
		}, nil
	})

	val, err := c.GetDatasetUserProperty(context.Background(), "tank/test", "org.omni:passphrase")
	require.NoError(t, err)
	assert.Equal(t, "stored-secret-abc", val)
}

func TestGetDatasetUserProperty_NotFound(t *testing.T) {
	t.Parallel()

	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return map[string]any{
			"user_properties": map[string]any{
				"org.omni:managed": map[string]any{"value": "true"},
			},
		}, nil
	})

	val, err := c.GetDatasetUserProperty(context.Background(), "tank/test", "org.omni:passphrase")
	require.NoError(t, err)
	assert.Empty(t, val, "should return empty string when property doesn't exist")
}

func TestGetDatasetUserProperty_EmptyProperties(t *testing.T) {
	t.Parallel()

	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return map[string]any{
			"user_properties": map[string]any{},
		}, nil
	})

	val, err := c.GetDatasetUserProperty(context.Background(), "tank/test", "org.omni:passphrase")
	require.NoError(t, err)
	assert.Empty(t, val)
}

func TestUnlockDataset_Success(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.dataset.lock", method)

		return nil, nil
	})

	err := c.LockDataset(context.Background(), "tank/omni-vms/test-enc")
	require.NoError(t, err)
}

func TestIsDatasetLocked_Locked(t *testing.T) {
	t.Parallel()

	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return map[string]any{"locked": true}, nil
	})

	locked, err := c.IsDatasetLocked(context.Background(), "tank/omni-vms/test-enc")
	require.NoError(t, err)
	assert.True(t, locked)
}

func TestIsDatasetLocked_Unlocked(t *testing.T) {
	t.Parallel()

	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return map[string]any{"locked": false}, nil
	})

	locked, err := c.IsDatasetLocked(context.Background(), "tank/omni-vms/test-enc")
	require.NoError(t, err)
	assert.False(t, locked)
}

func TestUserProperty_ListFormat(t *testing.T) {
	t.Parallel()

	props := OmniManagedProperties("req-123")
	props = append(props, UserProperty{Key: "org.omni:passphrase", Value: "secret"})

	data, err := json.Marshal(props)
	require.NoError(t, err)

	// Should be a JSON array of objects, not a map
	assert.Contains(t, string(data), `[{`)
	assert.Contains(t, string(data), `"key":"org.omni:managed"`)
	assert.Contains(t, string(data), `"key":"org.omni:passphrase"`)
	assert.Contains(t, string(data), `"value":"secret"`)
}
