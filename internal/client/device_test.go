package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const methodDeviceCreate = "vm.device.create"

func TestAddCDROM(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, methodDeviceCreate, method)
		assert.Contains(t, string(params), `"dtype":"CDROM"`)
		assert.Contains(t, string(params), `"/mnt/default/talos-iso/abc.iso"`)

		return Device{ID: 1, VM: 42, Attributes: map[string]any{"dtype": "CDROM"}}, nil
	})

	dev, err := c.AddCDROM(context.Background(), 42, "/mnt/default/talos-iso/abc.iso")
	require.NoError(t, err)
	assert.Equal(t, "CDROM", dev.Attributes["dtype"])
}

// TestBootOrder_DiskBeforeCDROM pins the UEFI boot priority invariant: the root
// disk must have a lower `order` than the CDROM, so once Talos is installed the
// VM boots from disk and the halt_if_installed ISO is never re-entered.
// Regression for the bug where CDROM=1000 and DISK=1001 caused rebooted VMs to
// halt with "Talos is already installed to disk but booted from another media".
func TestBootOrder_DiskBeforeCDROM(t *testing.T) {
	var cdromOrder, diskOrder float64

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, methodDeviceCreate, method)

		var req map[string]any
		require.NoError(t, json.Unmarshal(params, &req))

		attrs, _ := req["attributes"].(map[string]any)
		dtype, _ := attrs["dtype"].(string)
		order, _ := req["order"].(float64)

		switch dtype {
		case "CDROM":
			cdromOrder = order
		case "DISK":
			diskOrder = order
		}

		return Device{ID: 1, VM: 42, Attributes: attrs}, nil
	})

	_, err := c.AddDisk(context.Background(), 42, "pool/omni-vms/root")
	require.NoError(t, err)
	_, err = c.AddCDROM(context.Background(), 42, "/mnt/pool/talos-iso/abc.iso")
	require.NoError(t, err)

	assert.Less(t, diskOrder, cdromOrder,
		"root disk order (%v) must be less than CDROM order (%v) so UEFI boots disk first — otherwise talos.halt_if_installed trips on reboot",
		diskOrder, cdromOrder)
}

func TestAddNIC_Bridge(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, methodDeviceCreate, method)
		assert.Contains(t, string(params), `"dtype":"NIC"`)
		assert.Contains(t, string(params), `"nic_attach":"br100"`)

		return Device{ID: 2, VM: 42, Attributes: map[string]any{"dtype": "NIC", "nic_attach": "br100"}}, nil
	})

	dev, err := c.AddNIC(context.Background(), 42, "br100")
	require.NoError(t, err)
	assert.Equal(t, "NIC", dev.Attributes["dtype"])
}

func TestAddNIC_VLAN(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, methodDeviceCreate, method)
		assert.Contains(t, string(params), `"nic_attach":"vlan666"`)

		return Device{ID: 3, VM: 42, Attributes: map[string]any{"dtype": "NIC", "nic_attach": "vlan666"}}, nil
	})

	dev, err := c.AddNIC(context.Background(), 42, "vlan666")
	require.NoError(t, err)
	assert.Equal(t, "vlan666", dev.Attributes["nic_attach"])
}

// TrueNAS 25.10's vm.device.create rejects `mtu` on NIC attributes with
// "Extra inputs are not permitted". MTU must never be forwarded to the
// hypervisor — it's applied guest-side via a Talos config patch matched on
// the NIC's MAC. These tests assert that NICConfig.MTU is silently ignored
// by the client regardless of whether the caller sets it.
func TestAddNICWithConfig_MTUNotSentToTrueNAS(t *testing.T) {
	var capturedParams string

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, methodDeviceCreate, method)
		capturedParams = string(params)
		assert.Contains(t, string(params), `"nic_attach":"br200"`)

		return Device{ID: 10, VM: 42, Attributes: map[string]any{"dtype": "NIC", "nic_attach": "br200"}}, nil
	})

	_, err := c.AddNICWithConfig(context.Background(), 42, NICConfig{
		NetworkInterface: "br200",
		MTU:              9000,
	}, 2002)
	require.NoError(t, err)
	assert.NotContains(t, capturedParams, "mtu", "MTU must not be sent to TrueNAS — it's applied guest-side via Talos config patch")
}

func TestAddNICWithConfig_NoMTU(t *testing.T) {
	var capturedParams string

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		capturedParams = string(params)

		return Device{ID: 11, VM: 42, Attributes: map[string]any{"dtype": "NIC"}}, nil
	})

	_, err := c.AddNICWithConfig(context.Background(), 42, NICConfig{
		NetworkInterface: "br200",
	}, 2002)
	require.NoError(t, err)
	assert.NotContains(t, capturedParams, "mtu", "MTU should not be in params when zero")
}

func TestAddDisk(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, methodDeviceCreate, method)
		assert.Contains(t, string(params), `"dtype":"DISK"`)
		assert.Contains(t, string(params), `/dev/zvol/default/omni-vms/test1`)

		return Device{ID: 4, VM: 42, Attributes: map[string]any{"dtype": "DISK"}}, nil
	})

	dev, err := c.AddDisk(context.Background(), 42, "default/omni-vms/test1")
	require.NoError(t, err)
	assert.Equal(t, "DISK", dev.Attributes["dtype"])
}

func TestAddDiskWithOrder(t *testing.T) {
	var capturedOrder float64

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, methodDeviceCreate, method)

		var req map[string]any
		json.Unmarshal(params, &req) //nolint:errcheck
		capturedOrder = req["order"].(float64)

		return Device{ID: 5, VM: 42, Attributes: map[string]any{"dtype": "DISK"}}, nil
	})

	dev, err := c.AddDiskWithOrder(context.Background(), 42, "ssd/omni-vms/test-disk-1", 1002)
	require.NoError(t, err)
	assert.Equal(t, 5, dev.ID)
	assert.Equal(t, float64(1002), capturedOrder)
}
