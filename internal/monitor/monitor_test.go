package monitor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()

	return logger
}

func TestMonitorRun_CancelsOnContext(t *testing.T) {
	c := client.NewMockClient(func(_ string, _ json.RawMessage) (any, error) {
		return map[string]any{"hostname": "test", "cores": 4, "physmem": int64(32 * 1024 * 1024 * 1024)}, nil
	})

	m := New(c, Config{Interval: time.Hour}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()

	// Let it collect once
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not exit after context cancellation")
	}
}

func TestPoolSelector_ExplicitPool(t *testing.T) {
	ps := NewPoolSelector(nil, testLogger())

	pool, err := ps.SelectPool(context.Background(), "tank")
	require.NoError(t, err)
	assert.Equal(t, "tank", pool)
}

func TestPoolSelector_AutoSelect_MostFreeSpace(t *testing.T) {
	c := client.NewMockClient(func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "pool.query":
			return []map[string]any{
				{"id": 1, "name": "small", "healthy": true, "status": "ONLINE", "size": 200 * 1024 * 1024 * 1024},
				{"id": 2, "name": "large", "healthy": true, "status": "ONLINE", "size": 1000 * 1024 * 1024 * 1024},
				{"id": 3, "name": "degraded", "healthy": false, "status": "DEGRADED", "size": 2000 * 1024 * 1024 * 1024},
			}, nil
		case "pool.dataset.query":
			// Return usable space based on which pool is being queried
			raw := string(params)
			if strings.Contains(raw, "small") {
				return map[string]any{"available": map[string]any{"parsed": 100 * 1024 * 1024 * 1024}, "used": map[string]any{"parsed": 100 * 1024 * 1024 * 1024}}, nil
			}
			if strings.Contains(raw, "large") {
				return map[string]any{"available": map[string]any{"parsed": 500 * 1024 * 1024 * 1024}, "used": map[string]any{"parsed": 500 * 1024 * 1024 * 1024}}, nil
			}
			if strings.Contains(raw, "degraded") {
				return map[string]any{"available": map[string]any{"parsed": 900 * 1024 * 1024 * 1024}, "used": map[string]any{"parsed": 1100 * 1024 * 1024 * 1024}}, nil
			}
			return map[string]any{"available": map[string]any{"parsed": 0}, "used": map[string]any{"parsed": 0}}, nil
		}

		return nil, nil
	})

	ps := NewPoolSelector(c, testLogger())

	pool, err := ps.SelectPool(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "large", pool, "should select healthy pool with most free space, not degraded")
}

func TestPoolSelector_NoHealthyPools(t *testing.T) {
	c := client.NewMockClient(func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "pool.query":
			return []map[string]any{
				{"id": 1, "name": "faulted", "healthy": false, "status": "FAULTED", "size": 100},
			}, nil
		case "pool.dataset.query":
			return map[string]any{"available": map[string]any{"parsed": 100}, "used": map[string]any{"parsed": 0}}, nil
		}

		return nil, nil
	})

	ps := NewPoolSelector(c, testLogger())

	_, err := ps.SelectPool(context.Background(), "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no healthy pools")
}

func TestCollectHostInfo(t *testing.T) {
	c := client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		if method == "system.info" {
			return map[string]any{
				"hostname": "truenas.local",
				"version":  "TrueNAS-SCALE-25.04",
				"physmem":  int64(32 * 1024 * 1024 * 1024),
				"cores":    12,
			}, nil
		}

		return nil, nil
	})

	m := New(c, Config{}, testLogger())

	// Should not panic even without OTEL initialized
	m.collectHostInfo(context.Background())
}

func TestCollectPoolInfo(t *testing.T) {
	c := client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "pool.query":
			return []map[string]any{
				{"id": 1, "name": "tank", "healthy": true, "status": "ONLINE", "size": 700 * 1024 * 1024 * 1024},
			}, nil
		case "pool.dataset.query":
			return map[string]any{
				"available": map[string]any{"parsed": 500 * 1024 * 1024 * 1024},
				"used":      map[string]any{"parsed": 200 * 1024 * 1024 * 1024},
			}, nil
		}

		return nil, nil
	})

	m := New(c, Config{}, testLogger())
	m.collectPoolInfo(context.Background())
}

func TestCollectVMInfo(t *testing.T) {
	c := client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return []client.VM{
				{ID: 1, Name: "vm1", Status: client.VMStatus{State: "RUNNING"}},
				{ID: 2, Name: "vm2", Status: client.VMStatus{State: "STOPPED"}},
				{ID: 3, Name: "vm3", Status: client.VMStatus{State: "RUNNING"}},
			}, nil
		}

		return nil, nil
	})

	m := New(c, Config{}, testLogger())
	m.collectVMInfo(context.Background())
}
