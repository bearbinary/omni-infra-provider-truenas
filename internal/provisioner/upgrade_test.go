package provisioner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

func TestResetNVRAMIfNeeded_ErrorState(t *testing.T) {
	nvramReset := false
	vmStarted := false

	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return client.VM{ID: 42, Status: client.VMStatus{State: "ERROR"}}, nil
		}

		if method == "vm.update" {
			nvramReset = true

			return nil, nil
		}

		if method == "vm.start" {
			vmStarted = true

			return true, nil
		}

		return nil, nil
	})

	p.resetNVRAMIfNeeded(context.Background(), testLogger(), 42)

	assert.True(t, nvramReset, "should reset NVRAM for ERROR state VM")
	assert.True(t, vmStarted, "should restart VM after NVRAM reset")
}

func TestResetNVRAMIfNeeded_RunningState_Noop(t *testing.T) {
	nvramReset := false

	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return client.VM{ID: 42, Status: client.VMStatus{State: "RUNNING"}}, nil
		}

		if method == "vm.update" {
			nvramReset = true

			return nil, nil
		}

		return nil, nil
	})

	p.resetNVRAMIfNeeded(context.Background(), testLogger(), 42)

	assert.False(t, nvramReset, "should not reset NVRAM for running VM")
}

func TestHandleExistingVM_ErrorState_TriggersNVRAMReset(t *testing.T) {
	nvramReset := false

	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return client.VM{ID: 42, Status: client.VMStatus{State: "ERROR"}}, nil
		}

		if method == "vm.update" {
			nvramReset = true

			return nil, nil
		}

		if method == "vm.start" {
			return true, nil
		}

		return nil, nil
	})

	vm := &client.VM{ID: 42, Status: client.VMStatus{State: "ERROR"}}
	result := p.handleExistingVM(context.Background(), testLogger(), vm, "omni_test")

	require.NotNil(t, result)
	assert.Error(t, *result, "should return retry interval for ERROR state")
	assert.True(t, nvramReset, "should trigger NVRAM reset")
}

func TestSwapCDROM_Integration(t *testing.T) {
	// Test the full CDROM swap flow: find existing → update
	updated := false

	c := client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.device.query" {
			return []client.Device{
				{ID: 5, VM: 42, Attributes: map[string]any{"dtype": "CDROM", "path": "/mnt/old.iso"}},
				{ID: 6, VM: 42, Attributes: map[string]any{"dtype": "DISK"}},
			}, nil
		}

		if method == "vm.device.update" {
			updated = true

			return client.Device{ID: 5, VM: 42, Attributes: map[string]any{"dtype": "CDROM", "path": "/mnt/new.iso"}}, nil
		}

		return nil, nil
	})

	dev, err := c.SwapCDROM(context.Background(), 42, "/mnt/new.iso")
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Equal(t, 5, dev.ID)
}
