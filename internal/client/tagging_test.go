package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOmniManagedProperties(t *testing.T) {
	props := OmniManagedProperties("test-request-123")

	assert.Equal(t, "true", props["org.omni:managed"])
	assert.Equal(t, "truenas", props["org.omni:provider"])
	assert.Equal(t, "test-request-123", props["org.omni:request-id"])
}

func TestCreateZvol_WithProperties(t *testing.T) {
	var receivedParams json.RawMessage

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.dataset.create", method)
		receivedParams = params

		return Dataset{ID: "tank/test", Type: "VOLUME"}, nil
	})

	props := OmniManagedProperties("req-abc")
	_, err := c.CreateZvol(context.Background(), "tank/test", 10, props)
	require.NoError(t, err)

	assert.Contains(t, string(receivedParams), `"org.omni:managed"`)
	assert.Contains(t, string(receivedParams), `"org.omni:provider"`)
	assert.Contains(t, string(receivedParams), `"req-abc"`)
}

func TestCreateZvol_WithoutProperties(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return Dataset{ID: "tank/test", Type: "VOLUME"}, nil
	})

	// No properties — should still work (backward compatible)
	_, err := c.CreateZvol(context.Background(), "tank/test", 10)
	require.NoError(t, err)
}
