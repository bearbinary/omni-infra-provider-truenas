package client

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimit_ConcurrentCalls(t *testing.T) {
	const maxConcurrent = 3

	var (
		active    atomic.Int32
		maxActive atomic.Int32
	)

	c := newClient(&MockTransport{
		Handler: func(method string, _ json.RawMessage) (any, error) {
			cur := active.Add(1)

			// Track peak concurrency
			for {
				old := maxActive.Load()
				if cur <= old || maxActive.CompareAndSwap(old, cur) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond) // Simulate API work
			active.Add(-1)

			return map[string]any{"ok": true}, nil
		},
	}, maxConcurrent)

	// Fire 10 concurrent calls
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			var result map[string]any

			err := c.call(context.Background(), "test.method", nil, &result)
			require.NoError(t, err)
		}()
	}

	wg.Wait()

	assert.LessOrEqual(t, int(maxActive.Load()), maxConcurrent,
		"peak concurrent calls should not exceed maxConcurrent=%d", maxConcurrent)
}

func TestRateLimit_ContextCancellation(t *testing.T) {
	// Create a client with only 1 slot
	c := newClient(&MockTransport{
		Handler: func(method string, _ json.RawMessage) (any, error) {
			time.Sleep(500 * time.Millisecond) // Hold the slot
			return nil, nil
		},
	}, 1)

	// Start a call that holds the slot
	go func() {
		c.call(context.Background(), "test.hold", nil, nil) //nolint:errcheck
	}()

	time.Sleep(10 * time.Millisecond) // Let it acquire

	// Try another call with a short timeout — should fail with context error
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.call(ctx, "test.blocked", nil, nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
