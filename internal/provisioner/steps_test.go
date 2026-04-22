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

func testProvisioner(handler client.MockHandler) *Provisioner {
	return NewProvisioner(client.NewMockClient(handler), ProviderConfig{
		DefaultPool:             "tank",
		DefaultNetworkInterface: "br0",
		DefaultBootMethod:       "UEFI",
	})
}

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()

	return logger
}

// --- checkExistingVM tests ---

func TestCheckExistingVM_NoVmId_NoExisting(t *testing.T) {
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return []client.VM{}, nil
		}

		return nil, nil
	})

	state := &specs.MachineSpec{}
	result := p.checkExistingVM(context.Background(), testLogger(), state, "omni_test")

	assert.Nil(t, result, "should return nil when no existing VM found")
}

func TestCheckExistingVM_VmId_Running(t *testing.T) {
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return client.VM{ID: 42, Description: omniVMDescriptionPrefix + " (test)", Name: "omni_test", Status: client.VMStatus{State: "RUNNING"}}, nil
		}

		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42}
	result := p.checkExistingVM(context.Background(), testLogger(), state, "omni_test")

	require.NotNil(t, result, "should return a result for running VM")
	assert.NoError(t, *result, "should return nil error for running VM")
	assert.True(t, p.ActiveVMNames()["omni_test"], "should track VM name")
}

func TestCheckExistingVM_VmId_Stopped(t *testing.T) {
	started := false
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return client.VM{ID: 42, Description: omniVMDescriptionPrefix + " (test)", Name: "omni_test", Status: client.VMStatus{State: "STOPPED"}}, nil
		}

		if method == "vm.start" {
			started = true

			return true, nil
		}

		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42}
	result := p.checkExistingVM(context.Background(), testLogger(), state, "omni_test")

	require.NotNil(t, result, "should return a result for stopped VM")
	assert.Error(t, *result, "should return retry error")
	assert.True(t, started, "should have called StartVM")
}

func TestCheckExistingVM_VmId_DeletedExternally(t *testing.T) {
	callCount := 0
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			callCount++
			if callCount == 1 {
				// First call: GetVM by ID — not found
				return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
			}
			// Second call: FindVMByName — empty list
			return []client.VM{}, nil
		}

		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42}
	result := p.checkExistingVM(context.Background(), testLogger(), state, "omni_test")

	assert.Nil(t, result, "should return nil to proceed with creation after external deletion")
	assert.Equal(t, int32(0), state.VmId, "should reset VmId")
}

func TestCheckExistingVM_FoundByName_Running(t *testing.T) {
	p := testProvisioner(func(method string, params json.RawMessage) (any, error) {
		if method == "vm.query" {
			// Check if this is a name query (array of filters)
			var rawParams []json.RawMessage
			if err := json.Unmarshal(params, &rawParams); err == nil && len(rawParams) == 1 {
				// Name query — return a matching VM
				return []client.VM{{ID: 99, Name: "omni_test", Description: omniVMDescriptionPrefix + " (test)", Status: client.VMStatus{State: "RUNNING"}}}, nil
			}

			// ID query with get:true — not found
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		}

		return nil, nil
	})

	state := &specs.MachineSpec{} // No VmId set
	result := p.checkExistingVM(context.Background(), testLogger(), state, "omni_test")

	require.NotNil(t, result)
	assert.NoError(t, *result, "should return nil error for running VM found by name")
	assert.Equal(t, int32(99), state.VmId, "should set VmId from found VM")
}

// --- handleExistingVM tests ---

func TestHandleExistingVM_Running(t *testing.T) {
	p := testProvisioner(nil)
	vm := &client.VM{ID: 42, Description: omniVMDescriptionPrefix + " (test)", Status: client.VMStatus{State: "RUNNING"}}

	result := p.handleExistingVM(context.Background(), testLogger(), vm, "omni_test")

	require.NotNil(t, result)
	assert.NoError(t, *result)
	assert.True(t, p.ActiveVMNames()["omni_test"])
}

func TestHandleExistingVM_Stopped_StartSuccess(t *testing.T) {
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.start" {
			return true, nil
		}

		return nil, nil
	})

	vm := &client.VM{ID: 42, Description: omniVMDescriptionPrefix + " (test)", Status: client.VMStatus{State: "STOPPED"}}
	result := p.handleExistingVM(context.Background(), testLogger(), vm, "omni_test")

	require.NotNil(t, result)
	assert.Error(t, *result, "should return retry interval error")
}

// --- Zvol Resize Tests ---

func TestMaybeResizeZvol_GrowsWhenSmaller(t *testing.T) {
	resized := false
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.dataset.query" {
			return map[string]any{
				"volsize": map[string]any{"parsed": int64(40 * 1024 * 1024 * 1024)},
			}, nil
		}

		if method == "pool.dataset.update" {
			resized = true

			return nil, nil
		}

		return nil, nil
	})

	err := p.maybeResizeZvol(context.Background(), testLogger(), "tank/test", 80)
	require.NoError(t, err)
	assert.True(t, resized, "should have resized the zvol")
}

func TestMaybeResizeZvol_SkipsWhenSameSize(t *testing.T) {
	resized := false
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.dataset.query" {
			return map[string]any{
				"volsize": map[string]any{"parsed": int64(40 * 1024 * 1024 * 1024)},
			}, nil
		}

		if method == "pool.dataset.update" {
			resized = true

			return nil, nil
		}

		return nil, nil
	})

	err := p.maybeResizeZvol(context.Background(), testLogger(), "tank/test", 40)
	require.NoError(t, err)
	assert.False(t, resized, "should not resize when same size")
}

func TestMaybeResizeZvol_SkipsWhenShrinking(t *testing.T) {
	resized := false
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.dataset.query" {
			return map[string]any{
				"volsize": map[string]any{"parsed": int64(80 * 1024 * 1024 * 1024)},
			}, nil
		}

		if method == "pool.dataset.update" {
			resized = true

			return nil, nil
		}

		return nil, nil
	})

	err := p.maybeResizeZvol(context.Background(), testLogger(), "tank/test", 40)
	require.NoError(t, err)
	assert.False(t, resized, "should not shrink zvol")
}

// --- Additional Disk Tests ---

func TestAdditionalDisk_PoolDefaultsToPrimary(t *testing.T) {
	d := Data{
		Pool: "tank",
		AdditionalDisks: []AdditionalDisk{
			{Size: 100}, // No pool specified
		},
	}

	disk := d.AdditionalDisks[0]
	diskPool := disk.Pool
	if diskPool == "" {
		diskPool = d.Pool
	}

	assert.Equal(t, "tank", diskPool)
}

func TestAdditionalDisk_PoolOverride(t *testing.T) {
	d := Data{
		Pool: "tank",
		AdditionalDisks: []AdditionalDisk{
			{Size: 100, Pool: "ssd"},
		},
	}

	disk := d.AdditionalDisks[0]
	diskPool := disk.Pool
	if diskPool == "" {
		diskPool = d.Pool
	}

	assert.Equal(t, "ssd", diskPool)
}

func TestAdditionalDisk_ZvolPathWithPrefix(t *testing.T) {
	d := Data{
		Pool:          "tank",
		DatasetPrefix: "prod/k8s",
		AdditionalDisks: []AdditionalDisk{
			{Size: 100, Pool: "ssd"},
		},
	}

	disk := d.AdditionalDisks[0]
	diskPool := disk.Pool
	if diskPool == "" {
		diskPool = d.Pool
	}

	diskBasePath := diskPool
	if d.DatasetPrefix != "" {
		diskBasePath = diskPool + "/" + d.DatasetPrefix
	}

	requestID := "test-req-123"
	zvolPath := diskBasePath + "/omni-vms/" + requestID + "-disk-1"

	assert.Equal(t, "ssd/prod/k8s/omni-vms/test-req-123-disk-1", zvolPath)
}

func TestAdditionalDisk_ZvolPathWithoutPrefix(t *testing.T) {
	d := Data{
		Pool: "tank",
		AdditionalDisks: []AdditionalDisk{
			{Size: 100, Pool: "ssd"},
		},
	}

	disk := d.AdditionalDisks[0]
	diskPool := disk.Pool
	if diskPool == "" {
		diskPool = d.Pool
	}

	diskBasePath := diskPool
	if d.DatasetPrefix != "" {
		diskBasePath = diskPool + "/" + d.DatasetPrefix
	}

	requestID := "test-req-456"
	zvolPath := diskBasePath + "/omni-vms/" + requestID + "-disk-1"

	assert.Equal(t, "ssd/omni-vms/test-req-456-disk-1", zvolPath)
}

func TestPoolSpaceCheck_AggregatesMultipleDisksOnSamePool(t *testing.T) {
	d := Data{
		Pool:     "tank",
		DiskSize: 40,
		AdditionalDisks: []AdditionalDisk{
			{Size: 100},             // Same pool as root
			{Size: 200},             // Same pool as root
			{Size: 50, Pool: "ssd"}, // Different pool
		},
	}

	poolRequired := map[string]int{d.Pool: d.DiskSize}
	for _, disk := range d.AdditionalDisks {
		diskPool := disk.Pool
		if diskPool == "" {
			diskPool = d.Pool
		}

		poolRequired[diskPool] += disk.Size
	}

	assert.Equal(t, 340, poolRequired["tank"], "root (40) + two additional (100+200) on same pool")
	assert.Equal(t, 50, poolRequired["ssd"], "one additional disk on ssd")
}

// --- Additional Disk Resize Tests ---

func TestAdditionalDisk_ResizeOnReProvision(t *testing.T) {
	var resizedPath string
	var resizedSize int64

	p := testProvisioner(func(method string, params json.RawMessage) (any, error) {
		if method == "pool.dataset.query" {
			// Current size is 50 GiB
			return map[string]any{
				"volsize": map[string]any{"parsed": int64(50 * 1024 * 1024 * 1024)},
			}, nil
		}

		if method == "pool.dataset.update" {
			var args []json.RawMessage
			json.Unmarshal(params, &args) //nolint:errcheck

			json.Unmarshal(args[0], &resizedPath) //nolint:errcheck

			var opts map[string]any
			json.Unmarshal(args[1], &opts) //nolint:errcheck
			resizedSize = int64(opts["volsize"].(float64))

			return nil, nil
		}

		return nil, nil
	})

	// Simulate re-provision: disk exists at 50 GiB, config says 100 GiB
	err := p.maybeResizeZvol(context.Background(), testLogger(), "ssd/omni-vms/test-disk-1", 100)
	require.NoError(t, err)
	assert.Equal(t, int64(100*1024*1024*1024), resizedSize)
}

func TestAdditionalDisk_NoShrinkOnReProvision(t *testing.T) {
	resized := false

	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.dataset.query" {
			// Current size is 100 GiB
			return map[string]any{
				"volsize": map[string]any{"parsed": int64(100 * 1024 * 1024 * 1024)},
			}, nil
		}

		if method == "pool.dataset.update" {
			resized = true

			return nil, nil
		}

		return nil, nil
	})

	// Config says 50 GiB but disk is already 100 GiB — should not shrink
	err := p.maybeResizeZvol(context.Background(), testLogger(), "ssd/omni-vms/test-disk-1", 50)
	require.NoError(t, err)
	assert.False(t, resized, "should not shrink additional disk")
}

// --- Circuit Breaker Tests ---

func TestHandleExistingVM_ErrorState_CircuitBreaker(t *testing.T) {
	vmDeleted := false

	p := NewProvisioner(client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return client.VM{ID: 42, Description: omniVMDescriptionPrefix + " (test)", Status: client.VMStatus{State: "ERROR"}}, nil
		}

		if method == "vm.update" {
			return nil, nil // NVRAM reset
		}

		if method == "vm.start" {
			return true, nil
		}

		if method == "vm.stop" || method == "vm.delete" {
			vmDeleted = true

			return true, nil
		}

		return nil, nil
	}), ProviderConfig{
		DefaultPool:        "tank",
		MaxErrorRecoveries: 3,
		PollInterval:       10 * time.Millisecond,
	})

	vm := &client.VM{ID: 42, Description: omniVMDescriptionPrefix + " (test)", Status: client.VMStatus{State: "ERROR"}}

	// First 3 errors should retry
	for i := 0; i < 3; i++ {
		result := p.handleExistingVM(context.Background(), testLogger(), vm, "omni_test")
		require.NotNil(t, result)
		assert.Error(t, *result) // RetryInterval is non-nil error
	}

	assert.False(t, vmDeleted, "should not delete VM within max recoveries")

	// 4th error (count > max) should trigger deprovision
	result := p.handleExistingVM(context.Background(), testLogger(), vm, "omni_test")
	require.NotNil(t, result)
	assert.True(t, vmDeleted, "should delete VM after exceeding max recoveries")
}

func TestHandleExistingVM_Running_ResetsErrorCount(t *testing.T) {
	p := NewProvisioner(client.NewMockClient(func(_ string, _ json.RawMessage) (any, error) {
		return nil, nil
	}), ProviderConfig{
		DefaultPool:        "tank",
		MaxErrorRecoveries: 3,
	})

	// Simulate 2 errors
	p.recordVMError(42)
	p.recordVMError(42)

	// VM reaches RUNNING — should clear errors
	vm := &client.VM{ID: 42, Description: omniVMDescriptionPrefix + " (test)", Status: client.VMStatus{State: "RUNNING"}}
	result := p.handleExistingVM(context.Background(), testLogger(), vm, "omni_test")

	require.NotNil(t, result)
	assert.NoError(t, *result, "RUNNING VM should return nil error")

	// Error count should be cleared
	p.errorMu.Lock()
	assert.Zero(t, p.errorCounts[42], "error count should be reset after RUNNING")
	p.errorMu.Unlock()
}

func TestCircuitBreaker_Disabled(t *testing.T) {
	p := NewProvisioner(client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return client.VM{ID: 42, Description: omniVMDescriptionPrefix + " (test)", Status: client.VMStatus{State: "ERROR"}}, nil
		}

		return nil, nil
	}), ProviderConfig{
		DefaultPool:        "tank",
		MaxErrorRecoveries: -1, // Disabled
	})

	vm := &client.VM{ID: 42, Description: omniVMDescriptionPrefix + " (test)", Status: client.VMStatus{State: "ERROR"}}

	// Should retry indefinitely without deprovisioning
	for i := 0; i < 100; i++ {
		result := p.handleExistingVM(context.Background(), testLogger(), vm, "omni_test")
		require.NotNil(t, result)
		assert.Error(t, *result) // RetryInterval
	}
}

func TestRecordAndClearVMErrors(t *testing.T) {
	p := NewProvisioner(client.NewMockClient(nil), ProviderConfig{DefaultPool: "tank"})

	assert.Equal(t, 1, p.recordVMError(42))
	assert.Equal(t, 2, p.recordVMError(42))
	assert.Equal(t, 3, p.recordVMError(42))

	p.clearVMErrors(42)

	assert.Equal(t, 1, p.recordVMError(42), "should restart from 1 after clear")
}

func TestHandleExistingVM_Stopped_StartFails(t *testing.T) {
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.start" {
			return nil, &client.APIError{Code: 99, Message: "start failed"}
		}

		return nil, nil
	})

	vm := &client.VM{ID: 42, Description: omniVMDescriptionPrefix + " (test)", Status: client.VMStatus{State: "STOPPED"}}
	result := p.handleExistingVM(context.Background(), testLogger(), vm, "omni_test")

	require.NotNil(t, result)
	assert.Error(t, *result)
	assert.Contains(t, (*result).Error(), "failed to start existing VM")
}
