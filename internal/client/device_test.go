package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddCDROM(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.device.create", method)
		assert.Contains(t, string(params), `"dtype":"CDROM"`)
		assert.Contains(t, string(params), `"/mnt/default/talos-iso/abc.iso"`)

		return Device{ID: 1, VM: 42, Attributes: map[string]any{"dtype": "CDROM"}}, nil
	})

	dev, err := c.AddCDROM(context.Background(), 42, "/mnt/default/talos-iso/abc.iso")
	require.NoError(t, err)
	assert.Equal(t, "CDROM", dev.Attributes["dtype"])
}

func TestAddNIC_Bridge(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.device.create", method)
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
		assert.Equal(t, "vm.device.create", method)
		assert.Contains(t, string(params), `"nic_attach":"vlan666"`)

		return Device{ID: 3, VM: 42, Attributes: map[string]any{"dtype": "NIC", "nic_attach": "vlan666"}}, nil
	})

	dev, err := c.AddNIC(context.Background(), 42, "vlan666")
	require.NoError(t, err)
	assert.Equal(t, "vlan666", dev.Attributes["nic_attach"])
}

func TestAddDisk(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.device.create", method)
		assert.Contains(t, string(params), `"dtype":"DISK"`)
		assert.Contains(t, string(params), `/dev/zvol/default/omni-vms/test1`)

		return Device{ID: 4, VM: 42, Attributes: map[string]any{"dtype": "DISK"}}, nil
	})

	dev, err := c.AddDisk(context.Background(), 42, "default/omni-vms/test1")
	require.NoError(t, err)
	assert.Equal(t, "DISK", dev.Attributes["dtype"])
}
