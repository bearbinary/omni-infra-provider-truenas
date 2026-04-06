package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPing_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "system.info", method)

		return map[string]any{"version": "TrueNAS-SCALE-25.04"}, nil
	})

	err := c.Ping(context.Background())
	require.NoError(t, err)
}

func TestPing_Failure(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, &jsonRPCError{Code: ErrCodeDenied, Message: "permission denied"}
	})

	err := c.Ping(context.Background())
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, ErrCodeDenied, apiErr.Code)
}

func TestIsNotFound(t *testing.T) {
	assert.True(t, IsNotFound(&APIError{Code: ErrCodeNotFound}))
	assert.False(t, IsNotFound(&APIError{Code: ErrCodeExists}))
	assert.False(t, IsNotFound(assert.AnError))
}

func TestIsAlreadyExists(t *testing.T) {
	assert.True(t, IsAlreadyExists(&APIError{Code: ErrCodeExists}))
	assert.False(t, IsAlreadyExists(&APIError{Code: ErrCodeNotFound}))
	assert.False(t, IsAlreadyExists(assert.AnError))
}

func TestTransportName(t *testing.T) {
	c := newMockClient(t, nil)
	assert.Equal(t, "mock", c.TransportName())
}
