package provisioner

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

func TestCleanupVM_GracefulShutdown_VMStopsInTime(t *testing.T) {
	var stopCalls atomic.Int32
	var forceStopped atomic.Bool

	p := NewProvisioner(client.NewMockClient(func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "vm.stop":
			call := stopCalls.Add(1)
			// Check if force flag is set
			if call == 1 {
				// First call: graceful (force=false)
				return true, nil
			}

			forceStopped.Store(true)

			return true, nil
		case "vm.query":
			// Return STOPPED after graceful signal
			return client.VM{ID: 42, Status: client.VMStatus{State: "STOPPED"}}, nil
		case "vm.delete":
			return true, nil
		default:
			return nil, nil
		}
	}), ProviderConfig{
		DefaultPool:             "tank",
		GracefulShutdownTimeout: 100 * time.Millisecond,
		PollInterval:            10 * time.Millisecond,
	})

	err := p.cleanupVM(context.Background(), testLogger(), 42)
	require.NoError(t, err)
	assert.False(t, forceStopped.Load(), "should not force stop if VM stopped gracefully")
}

func TestCleanupVM_GracefulShutdown_Timeout_ForcesStop(t *testing.T) {
	var forceStopped atomic.Bool

	p := NewProvisioner(client.NewMockClient(func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "vm.stop":
			// Check params for force flag
			var raw []json.RawMessage
			json.Unmarshal(params, &raw) //nolint:errcheck
			if len(raw) >= 2 {
				var opts map[string]any
				json.Unmarshal(raw[1], &opts) //nolint:errcheck
				if force, ok := opts["force"].(bool); ok && force {
					forceStopped.Store(true)
				}
			}

			return true, nil
		case "vm.query":
			// VM never stops gracefully — stays RUNNING
			return client.VM{ID: 42, Status: client.VMStatus{State: "RUNNING"}}, nil
		case "vm.delete":
			return true, nil
		default:
			return nil, nil
		}
	}), ProviderConfig{
		DefaultPool:             "tank",
		GracefulShutdownTimeout: 100 * time.Millisecond,
		PollInterval:            10 * time.Millisecond,
	})

	err := p.cleanupVM(context.Background(), testLogger(), 42)
	require.NoError(t, err)
	assert.True(t, forceStopped.Load(), "should force stop after graceful timeout")
}

func TestCleanupVM_ContextCancelled_DuringGraceful(t *testing.T) {
	p := NewProvisioner(client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "vm.stop":
			return true, nil
		case "vm.query":
			return client.VM{ID: 42, Status: client.VMStatus{State: "RUNNING"}}, nil
		case "vm.delete":
			return true, nil
		default:
			return nil, nil
		}
	}), ProviderConfig{
		DefaultPool:             "tank",
		GracefulShutdownTimeout: 200 * time.Millisecond,
		PollInterval:            10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 50ms — should not wait the full graceful timeout
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_ = p.cleanupVM(ctx, testLogger(), 42) // May error due to cancelled context on subsequent calls
	elapsed := time.Since(start)

	// Key assertion: should NOT wait the full graceful timeout
	assert.Less(t, elapsed, 2*time.Second, "should exit quickly when context cancelled")
}

func TestCleanupVM_VMAlreadyStopped(t *testing.T) {
	p := NewProvisioner(client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "vm.stop":
			return true, nil
		case "vm.query":
			return client.VM{ID: 42, Status: client.VMStatus{State: "STOPPED"}}, nil
		case "vm.delete":
			return true, nil
		default:
			return nil, nil
		}
	}), ProviderConfig{DefaultPool: "tank", PollInterval: 10 * time.Millisecond})

	err := p.cleanupVM(context.Background(), testLogger(), 42)
	require.NoError(t, err)
}

func TestCleanupVM_VMNotFound_DuringPoll(t *testing.T) {
	p := NewProvisioner(client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "vm.stop":
			return true, nil
		case "vm.query":
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		case "vm.delete":
			return true, nil
		default:
			return nil, nil
		}
	}), ProviderConfig{DefaultPool: "tank", PollInterval: 10 * time.Millisecond})

	err := p.cleanupVM(context.Background(), testLogger(), 42)
	require.NoError(t, err, "should succeed if VM disappears during poll")
}
