package provisioner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/api/specs"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

func testProvisioner(handler client.MockHandler) *Provisioner {
	return NewProvisioner(client.NewMockClient(handler), ProviderConfig{
		DefaultPool:       "tank",
		DefaultNICAttach:  "br0",
		DefaultBootMethod: "UEFI",
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
			return client.VM{ID: 42, Name: "omni_test", Status: client.VMStatus{State: "RUNNING"}}, nil
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
			return client.VM{ID: 42, Name: "omni_test", Status: client.VMStatus{State: "STOPPED"}}, nil
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
				return []client.VM{{ID: 99, Name: "omni_test", Status: client.VMStatus{State: "RUNNING"}}}, nil
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
	vm := &client.VM{ID: 42, Status: client.VMStatus{State: "RUNNING"}}

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

	vm := &client.VM{ID: 42, Status: client.VMStatus{State: "STOPPED"}}
	result := p.handleExistingVM(context.Background(), testLogger(), vm, "omni_test")

	require.NotNil(t, result)
	assert.Error(t, *result, "should return retry interval error")
}

func TestHandleExistingVM_Stopped_StartFails(t *testing.T) {
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.start" {
			return nil, &client.APIError{Code: 99, Message: "start failed"}
		}

		return nil, nil
	})

	vm := &client.VM{ID: 42, Status: client.VMStatus{State: "STOPPED"}}
	result := p.handleExistingVM(context.Background(), testLogger(), vm, "omni_test")

	require.NotNil(t, result)
	assert.Error(t, *result)
	assert.Contains(t, (*result).Error(), "failed to start existing VM")
}
