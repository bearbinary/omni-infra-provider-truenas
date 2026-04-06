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

func TestStress_ConcurrentProvisionSimulation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const numVMs = 20
	const maxConcurrent = 4

	var (
		active    atomic.Int32
		maxActive atomic.Int32
		completed atomic.Int32
	)

	c := newClient(&MockTransport{
		Handler: func(method string, _ json.RawMessage) (any, error) {
			cur := active.Add(1)

			for {
				old := maxActive.Load()
				if cur <= old || maxActive.CompareAndSwap(old, cur) {
					break
				}
			}

			// Simulate varying API response times
			time.Sleep(time.Duration(10+cur%5) * time.Millisecond)
			active.Add(-1)
			completed.Add(1)

			switch method {
			case methodVMCreate:
				return VM{ID: int(completed.Load()), Name: "test"}, nil
			case "pool.dataset.create":
				return Dataset{ID: "tank/test", Type: "VOLUME"}, nil
			case "vm.device.create":
				return Device{ID: 1}, nil
			default:
				return map[string]any{"ok": true}, nil
			}
		},
	}, maxConcurrent)

	var wg sync.WaitGroup
	errors := make([]error, numVMs)

	for i := range numVMs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			ctx := context.Background()

			// Simulate a provision sequence: create zvol, create VM, add devices
			var ds Dataset
			if err := c.call(ctx, "pool.dataset.create", nil, &ds); err != nil {
				errors[idx] = err

				return
			}

			var vm VM
			if err := c.call(ctx, methodVMCreate, nil, &vm); err != nil {
				errors[idx] = err

				return
			}

			var dev Device
			if err := c.call(ctx, "vm.device.create", nil, &dev); err != nil {
				errors[idx] = err

				return
			}
		}(i)
	}

	wg.Wait()

	for i, err := range errors {
		require.NoError(t, err, "VM %d should provision without error", i)
	}

	assert.Equal(t, int32(numVMs*3), completed.Load(), "all API calls should complete")
	assert.LessOrEqual(t, int(maxActive.Load()), maxConcurrent,
		"concurrent calls should not exceed rate limit")
	t.Logf("Peak concurrent calls: %d (limit: %d)", maxActive.Load(), maxConcurrent)
}

func TestStress_RapidPingFlood(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	var callCount atomic.Int32

	c := newClient(&MockTransport{
		Handler: func(_ string, _ json.RawMessage) (any, error) {
			callCount.Add(1)

			return map[string]any{"version": "test"}, nil
		},
	}, 8)

	const numPings = 100

	var wg sync.WaitGroup
	for range numPings {
		wg.Add(1)
		go func() {
			defer wg.Done()

			err := c.Ping(context.Background())
			require.NoError(t, err)
		}()
	}

	wg.Wait()
	assert.Equal(t, int32(numPings), callCount.Load())
}
