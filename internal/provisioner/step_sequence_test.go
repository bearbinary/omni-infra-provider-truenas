package provisioner

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/api/specs"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

// callRecorder tracks the sequence and params of client calls.
type callRecorder struct {
	calls []recordedCall
}

type recordedCall struct {
	method string
	params json.RawMessage
}

func (r *callRecorder) handler(method string, params json.RawMessage) (any, error) {
	r.calls = append(r.calls, recordedCall{method: method, params: params})

	switch method {
	case "pool.query":
		return []map[string]any{{"name": "default", "healthy": true, "free": int64(500 * 1024 * 1024 * 1024)}}, nil
	case "system.info":
		return map[string]any{"physmem": int64(64 * 1024 * 1024 * 1024)}, nil
	case "pool.dataset.create":
		return &client.Dataset{ID: "default/omni-vms"}, nil
	case "pool.dataset.query":
		return map[string]any{"volsize": map[string]any{"parsed": int64(40 * 1024 * 1024 * 1024)}}, nil
	case "vm.create":
		return &client.VM{ID: 99, Name: "omni_test"}, nil
	case "vm.start":
		return true, nil
	case "vm.query":
		var rawParams []json.RawMessage
		if err := json.Unmarshal(params, &rawParams); err == nil && len(rawParams) == 1 {
			return []client.VM{}, nil // FindVMByName: not found
		}
		return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
	case "vm.device.create":
		return &client.Device{ID: 1, VM: 99, Attributes: map[string]any{"mac": "00:11:22:33:44:55"}}, nil
	case "vm.device.query":
		return []client.Device{}, nil
	case "vm.device.delete":
		return true, nil
	case "vm.stop":
		return true, nil
	case "vm.delete":
		return true, nil
	case "pool.dataset.delete":
		return nil, nil
	case "filesystem.stat":
		return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
	}

	return nil, nil
}

func (r *callRecorder) methodCalls() []string {
	methods := make([]string, len(r.calls))
	for i, c := range r.calls {
		methods[i] = c.method
	}
	return methods
}

func (r *callRecorder) hasCall(method string) bool {
	for _, c := range r.calls {
		if c.method == method {
			return true
		}
	}
	return false
}

// --- Deprovision full sequence ---

func TestDeprovisionSequence_FullOrchestration(t *testing.T) {
	t.Parallel()

	rec := &callRecorder{}
	p := NewProvisioner(client.NewMockClient(rec.handler), ProviderConfig{
		DefaultPool:             "default",
		GracefulShutdownTimeout: 50 * time.Millisecond,
		PollInterval:            5 * time.Millisecond,
	})
	logger := zap.NewNop()

	// Simulate full deprovision
	err := p.cleanupVM(context.Background(), logger, 42)
	require.NoError(t, err)

	err = p.cleanupZvol(context.Background(), logger, "default/omni-vms/test-req")
	require.NoError(t, err)

	methods := rec.methodCalls()

	// Should stop → poll → delete VM → delete zvol
	assert.True(t, rec.hasCall("vm.stop"), "should call StopVM")
	assert.True(t, rec.hasCall("vm.delete"), "should call DeleteVM")
	assert.True(t, rec.hasCall("pool.dataset.delete"), "should call DeleteDataset")

	// vm.stop must precede vm.delete
	for i, m := range methods {
		if m == "vm.stop" {
			for j := i + 1; j < len(methods); j++ {
				if methods[j] == "vm.delete" {
					return // Correct order found
				}
			}
			t.Fatal("vm.delete should come after vm.stop")
		}
	}
}

// --- CheckExistingVM sequence ---

func TestCheckExistingVM_Sequence_VMNotFound_ThenByName(t *testing.T) {
	t.Parallel()

	rec := &callRecorder{}
	p := NewProvisioner(client.NewMockClient(rec.handler), ProviderConfig{DefaultPool: "default"})

	state := &specs.MachineSpec{VmId: 42}
	result := p.checkExistingVM(context.Background(), zap.NewNop(), state, "omni_test")

	// VM not found by ID, then not found by name → proceed with creation
	assert.Nil(t, result)
	assert.Equal(t, int32(0), state.VmId, "VmId should be reset after external deletion")

	// Should have called vm.query twice: once for ID lookup, once for name lookup
	vmQueryCount := 0
	for _, c := range rec.calls {
		if c.method == "vm.query" {
			vmQueryCount++
		}
	}
	assert.Equal(t, 2, vmQueryCount, "should query by ID then by name")
}

// --- Pool validation sequence ---

func TestValidatePool_Sequence(t *testing.T) {
	t.Parallel()

	rec := &callRecorder{}
	p := NewProvisioner(client.NewMockClient(rec.handler), ProviderConfig{DefaultPool: "default"})

	err := p.validatePool(context.Background(), "default")
	require.NoError(t, err)

	assert.True(t, rec.hasCall("pool.query"))
}
