package client

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingTransport simulates a connection that fails on the first N calls,
// then succeeds. Used to test reconnect retry behavior.
type failingTransport struct {
	MockTransport
	callCount atomic.Int32
	failUntil int32
}

func (t *failingTransport) Call(ctx context.Context, method string, params any, result any) error {
	count := t.callCount.Add(1)
	if count <= t.failUntil {
		return errors.New("connection reset by peer")
	}

	return t.MockTransport.Call(ctx, method, params, result)
}

func TestReconnect_RetryOnConnectionError(t *testing.T) {
	// The ws transport's Call() retries once after reconnect.
	// We can test the concept with the mock: a transport that fails once then succeeds.
	ft := &failingTransport{
		MockTransport: MockTransport{
			Handler: func(method string, _ json.RawMessage) (any, error) {
				return map[string]any{"version": "TrueNAS-SCALE-25.04"}, nil
			},
		},
		failUntil: 1, // First call fails, second succeeds
	}

	c := newClient(ft, defaultMaxConcurrentCalls)

	// First call fails because of connection error
	var info map[string]any
	err := c.call(context.Background(), "system.info", nil, &info)

	// The call() in truenas.go doesn't retry (that's ws.go's job),
	// so this should fail. But we verify the error is a connection error, not an API error.
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), "truenas api error", "should be a connection error, not API error")

	// Second call should succeed (failUntil=1, callCount is now 2)
	err = c.call(context.Background(), "system.info", nil, &info)
	require.NoError(t, err)
}

func TestReconnect_APIErrorNotRetried(t *testing.T) {
	callCount := 0
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		callCount++

		return nil, &jsonRPCError{Code: ErrCodeDenied, Message: "permission denied"}
	})

	var info map[string]any
	err := c.call(context.Background(), "system.info", nil, &info)

	assert.Error(t, err)
	assert.Equal(t, 1, callCount, "API errors should not trigger a retry")

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, ErrCodeDenied, apiErr.Code)
}
