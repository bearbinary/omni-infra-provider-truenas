package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemVersion_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "system.version", method)

		return "TrueNAS-SCALE-25.04.0", nil
	})

	ver, err := c.SystemVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "TrueNAS-SCALE-25.04.0", ver)
}

func TestSystemVersion_OldVersion(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return "TrueNAS-SCALE-24.10.2", nil
	})

	ver, err := c.SystemVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "TrueNAS-SCALE-24.10.2", ver)
	// The version check logic is in main.go, not the client
}
