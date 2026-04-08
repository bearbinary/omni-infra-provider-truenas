package provisioner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bearbinary/omni-infra-provider-truenas/api/specs"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

func TestSwapCDROMForUpgrade_UpdatesExistingCDROM(t *testing.T) {
	var updatedPath string
	p := testProvisioner(func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "vm.device.query":
			return []client.Device{
				{ID: 10, VM: 42, Attributes: map[string]any{"dtype": "CDROM", "path": "/mnt/tank/talos-iso/old.iso"}},
			}, nil
		case "vm.device.update":
			var args []json.RawMessage
			_ = json.Unmarshal(params, &args)
			var attrs map[string]any
			_ = json.Unmarshal(args[1], &attrs)
			if a, ok := attrs["attributes"].(map[string]any); ok {
				updatedPath, _ = a["path"].(string)
			}
			return &client.Device{ID: 10, VM: 42}, nil
		}
		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42, CdromDeviceId: 10, ImageId: "newimage123"}

	// We can't call swapCDROMForUpgrade directly since it needs provision.Context,
	// but we can test the underlying SwapCDROM client method that it calls
	dev, err := p.client.SwapCDROM(context.Background(), 42, "/mnt/tank/talos-iso/newimage123.iso")
	require.NoError(t, err)
	assert.Equal(t, 10, dev.ID)
	assert.Contains(t, updatedPath, "newimage123")
	_ = state // Verify state fields are the right types
}

func TestSwapCDROM_NoCDROMExists_CreatesNew(t *testing.T) {
	var createdPath string
	p := testProvisioner(func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "vm.device.query":
			return []client.Device{}, nil // No CDROM exists
		case "vm.device.create":
			var req client.AddDeviceRequest
			_ = json.Unmarshal(params, &req)
			createdPath, _ = req.Attributes["path"].(string)
			return &client.Device{ID: 99, VM: 42}, nil
		}
		return nil, nil
	})

	dev, err := p.client.SwapCDROM(context.Background(), 42, "/mnt/tank/talos-iso/upgrade.iso")
	require.NoError(t, err)
	assert.Equal(t, 99, dev.ID)
	assert.Equal(t, "/mnt/tank/talos-iso/upgrade.iso", createdPath)
}
