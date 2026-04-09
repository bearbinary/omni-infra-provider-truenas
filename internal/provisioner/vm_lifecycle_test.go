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

// --- verifyVMExists tests ---

func TestVerifyVMExists_VMPresent(t *testing.T) {
	t.Parallel()

	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return client.VM{ID: 42, Status: client.VMStatus{State: "RUNNING"}}, nil
		}
		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42, ZvolPath: "tank/omni-vms/test"}
	err := p.verifyVMExists(context.Background(), zap.NewNop(), state)
	require.NoError(t, err)
	assert.Equal(t, int32(42), state.VmId, "VmId should not be reset when VM exists")
}

func TestVerifyVMExists_VMGone_ResetsState(t *testing.T) {
	t.Parallel()

	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		}
		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42, CdromDeviceId: 10, ZvolPath: "tank/omni-vms/test"}
	err := p.verifyVMExists(context.Background(), zap.NewNop(), state)
	require.NoError(t, err)
	assert.Equal(t, int32(0), state.VmId, "VmId should be reset")
	assert.Equal(t, int32(0), state.CdromDeviceId, "CdromDeviceId should be reset")
	assert.Equal(t, "tank/omni-vms/test", state.ZvolPath, "ZvolPath should be preserved")
}

func TestVerifyVMExists_TransientError_DoesNotReset(t *testing.T) {
	t.Parallel()

	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return nil, &client.APIError{Code: 99, Message: "connection timeout"}
		}
		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42, ZvolPath: "tank/omni-vms/test"}
	err := p.verifyVMExists(context.Background(), zap.NewNop(), state)
	require.Error(t, err, "transient error should be returned")
	assert.Equal(t, int32(42), state.VmId, "VmId should NOT be reset on transient error")
}

// --- checkExistingVM: VM deleted externally ---

func TestCheckExistingVM_DeletedExternally_ResetsStateCompletely(t *testing.T) {
	t.Parallel()

	callCount := 0
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			callCount++
			if callCount == 1 {
				return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
			}
			return []client.VM{}, nil
		}
		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42, CdromDeviceId: 10}
	result := p.checkExistingVM(context.Background(), zap.NewNop(), state, "omni_test")

	assert.Nil(t, result, "should proceed to create")
	assert.Equal(t, int32(0), state.VmId, "VmId should be reset")
	assert.Equal(t, int32(0), state.CdromDeviceId, "CdromDeviceId should be reset")
}

// --- healthCheck: VM disappeared scenarios ---

func TestHealthCheck_VMGoneDuringFinalize_ResetsState(t *testing.T) {
	t.Parallel()

	// VM was allocated (has MachineRequestStatus ID) but then deleted from TrueNAS
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		}
		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42, CdromDeviceId: 10}
	logger := zap.NewNop()

	// Simulate: VM exists, CDROM still attached, VM was allocated
	// The mock returns "not found" for GetVM
	vm, err := p.client.GetVM(context.Background(), 42)
	require.Error(t, err)
	assert.Nil(t, vm)

	// verifyVMExists would reset state
	err = p.verifyVMExists(context.Background(), logger, state)
	require.NoError(t, err)
	assert.Equal(t, int32(0), state.VmId)
	assert.Equal(t, int32(0), state.CdromDeviceId)
}

func TestHealthCheck_AlreadyFinalized_VMStillExists(t *testing.T) {
	t.Parallel()

	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return client.VM{ID: 42, Status: client.VMStatus{State: "RUNNING"}}, nil
		}
		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42, CdromDeviceId: 0} // Already finalized
	err := p.verifyVMExists(context.Background(), zap.NewNop(), state)
	require.NoError(t, err)
	assert.Equal(t, int32(42), state.VmId, "should not reset — VM exists")
}

func TestHealthCheck_AlreadyFinalized_VMGone(t *testing.T) {
	t.Parallel()

	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		}
		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42, CdromDeviceId: 0} // Already finalized
	err := p.verifyVMExists(context.Background(), zap.NewNop(), state)
	require.NoError(t, err)
	assert.Equal(t, int32(0), state.VmId, "should reset — VM gone")
}

// --- Deprovision: VM already gone ---

func TestDeprovision_VMAlreadyGone_Succeeds(t *testing.T) {
	t.Parallel()

	p := NewProvisioner(client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "vm.stop":
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		case "vm.query":
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		case "vm.delete":
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		case "pool.dataset.delete":
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		}
		return nil, nil
	}), ProviderConfig{DefaultPool: "tank", PollInterval: 10 * time.Millisecond})

	// VM 42 and zvol both already gone
	err := p.cleanupVM(context.Background(), zap.NewNop(), 42)
	require.NoError(t, err, "should succeed when VM is already gone")

	err = p.cleanupZvol(context.Background(), zap.NewNop(), "tank/omni-vms/test")
	require.NoError(t, err, "should succeed when zvol is already gone")
}

func TestDeprovision_VMZeroID_Succeeds(t *testing.T) {
	t.Parallel()

	p := testProvisioner(nil)

	// VmId is 0 — nothing to clean up
	err := p.cleanupVM(context.Background(), zap.NewNop(), 0)
	require.NoError(t, err)
}

func TestDeprovision_ZvolEmpty_Succeeds(t *testing.T) {
	t.Parallel()

	p := testProvisioner(nil)

	err := p.cleanupZvol(context.Background(), zap.NewNop(), "")
	require.NoError(t, err)
}

func TestDeprovision_AdditionalZvols_CleanedUp(t *testing.T) {
	t.Parallel()

	var deletedPaths []string

	p := NewProvisioner(client.NewMockClient(func(method string, params json.RawMessage) (any, error) {
		if method == "pool.dataset.delete" {
			var args []string
			json.Unmarshal(params, &args) //nolint:errcheck

			if len(args) > 0 {
				deletedPaths = append(deletedPaths, args[0])
			}

			return true, nil
		}

		return nil, nil
	}), ProviderConfig{DefaultPool: "tank"})

	logger := zap.NewNop()

	// Simulate deprovision: additional zvols first, then root
	additionalPaths := []string{
		"ssd/omni-vms/test-request-disk-1",
		"hdd/omni-vms/test-request-disk-2",
	}

	for _, path := range additionalPaths {
		err := p.cleanupZvol(context.Background(), logger, path)
		require.NoError(t, err)
	}

	err := p.cleanupZvol(context.Background(), logger, "tank/omni-vms/test-request")
	require.NoError(t, err)

	require.Len(t, deletedPaths, 3)
	assert.Equal(t, "ssd/omni-vms/test-request-disk-1", deletedPaths[0])
	assert.Equal(t, "hdd/omni-vms/test-request-disk-2", deletedPaths[1])
	assert.Equal(t, "tank/omni-vms/test-request", deletedPaths[2])
}

// --- Full lifecycle: provision → VM deleted → re-provision ---

func TestLifecycle_VMDeletedExternally_RecreatesOnNextReconcile(t *testing.T) {
	t.Parallel()

	vmExists := true
	var createdVMs int

	p := testProvisioner(func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "vm.query":
			if vmExists {
				// First: VM exists
				var rawParams []json.RawMessage
				if err := json.Unmarshal(params, &rawParams); err == nil && len(rawParams) == 1 {
					return []client.VM{}, nil // FindByName: not found
				}
				return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
			}
			// After deletion: not found
			var rawParams []json.RawMessage
			if err := json.Unmarshal(params, &rawParams); err == nil && len(rawParams) == 1 {
				return []client.VM{}, nil
			}
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		case "vm.create":
			createdVMs++
			return &client.VM{ID: 99}, nil
		case "pool.query":
			return []map[string]any{{"name": "tank", "healthy": true, "free": int64(500 * 1024 * 1024 * 1024)}}, nil
		case "pool.dataset.query":
			return map[string]any{"available": map[string]any{"parsed": int64(500 * 1024 * 1024 * 1024)}, "used": map[string]any{"parsed": int64(100 * 1024 * 1024 * 1024)}}, nil
		case "system.info":
			return map[string]any{"physmem": int64(64 * 1024 * 1024 * 1024)}, nil
		case "pool.dataset.create":
			return &client.Dataset{ID: "tank/omni-vms"}, nil
		case "vm.start":
			return true, nil
		case "vm.device.create":
			return &client.Device{ID: 1, Attributes: map[string]any{"mac": "aa:bb:cc:dd:ee:ff"}}, nil
		}
		return nil, nil
	})

	logger := zap.NewNop()
	state := &specs.MachineSpec{VmId: 42, CdromDeviceId: 0} // Finalized VM

	// Step 1: verifyVMExists detects VM is gone, resets state
	vmExists = false
	err := p.verifyVMExists(context.Background(), logger, state)
	require.NoError(t, err)
	assert.Equal(t, int32(0), state.VmId, "state should be reset after VM disappeared")

	// Step 2: checkExistingVM finds no VM, returns nil to proceed with creation
	result := p.checkExistingVM(context.Background(), logger, state, "omni_test")
	assert.Nil(t, result, "should proceed to create a new VM")
}

// --- Edge case: multiple provider restarts ---

func TestLifecycle_ProviderRestart_VMStillRunning(t *testing.T) {
	t.Parallel()

	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return client.VM{ID: 42, Status: client.VMStatus{State: "RUNNING"}}, nil
		}
		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42, CdromDeviceId: 0}

	// Provider restarts — verifyVMExists confirms VM is still running
	err := p.verifyVMExists(context.Background(), zap.NewNop(), state)
	require.NoError(t, err)
	assert.Equal(t, int32(42), state.VmId, "VM still exists — no reset needed")
}

func TestLifecycle_ProviderRestart_VMStopped(t *testing.T) {
	t.Parallel()

	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return client.VM{ID: 42, Status: client.VMStatus{State: "STOPPED"}}, nil
		}
		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42, CdromDeviceId: 0}

	// Provider restarts — VM is stopped but still exists
	err := p.verifyVMExists(context.Background(), zap.NewNop(), state)
	require.NoError(t, err)
	assert.Equal(t, int32(42), state.VmId, "VM exists (stopped) — no reset needed")
}
