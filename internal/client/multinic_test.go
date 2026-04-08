package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddNICWithConfig_Default(t *testing.T) {
	var receivedParams json.RawMessage

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.device.create", method)
		receivedParams = params

		return Device{ID: 1, VM: 42, Attributes: map[string]any{"dtype": "NIC"}}, nil
	})

	cfg := NICConfig{NetworkInterface: "br200"}
	_, err := c.AddNICWithConfig(context.Background(), 42, cfg, 1004)
	require.NoError(t, err)

	assert.Contains(t, string(receivedParams), `"dtype":"NIC"`)
	assert.Contains(t, string(receivedParams), `"type":"VIRTIO"`)
	assert.Contains(t, string(receivedParams), `"nic_attach":"br200"`)
	assert.NotContains(t, string(receivedParams), "trust_guest_rx_filters")
}

func TestAddNICWithConfig_E1000(t *testing.T) {
	var receivedParams json.RawMessage

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		receivedParams = params

		return Device{ID: 2, VM: 42, Attributes: map[string]any{"dtype": "NIC"}}, nil
	})

	cfg := NICConfig{NetworkInterface: "enp6s0", Type: "E1000"}
	_, err := c.AddNICWithConfig(context.Background(), 42, cfg, 1005)
	require.NoError(t, err)

	assert.Contains(t, string(receivedParams), `"type":"E1000"`)
}

func TestAddNICWithConfig_TrustGuestRxFilters(t *testing.T) {
	var receivedParams json.RawMessage

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		receivedParams = params

		return Device{ID: 3, VM: 42, Attributes: map[string]any{"dtype": "NIC"}}, nil
	})

	cfg := NICConfig{
		NetworkInterface:    "enp5s0",
		TrustGuestRxFilters: true,
	}
	_, err := c.AddNICWithConfig(context.Background(), 42, cfg, 1004)
	require.NoError(t, err)

	assert.Contains(t, string(receivedParams), `"trust_guest_rx_filters":true`)
}

func TestAddNIC_BackwardCompatible(t *testing.T) {
	// The original AddNIC should still work exactly as before
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.device.create", method)
		assert.Contains(t, string(params), `"nic_attach":"br100"`)
		assert.Contains(t, string(params), `"type":"VIRTIO"`)

		return Device{ID: 1, VM: 42, Attributes: map[string]any{"dtype": "NIC", "nic_attach": "br100"}}, nil
	})

	dev, err := c.AddNIC(context.Background(), 42, "br100")
	require.NoError(t, err)
	assert.Equal(t, 1, dev.ID)
}

func TestAddNICWithConfig_VLANTag(t *testing.T) {
	var receivedParams json.RawMessage

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		receivedParams = params

		return Device{ID: 4, VM: 42, Attributes: map[string]any{"dtype": "NIC"}}, nil
	})

	cfg := NICConfig{
		NetworkInterface:    "enp5s0",
		VLANTag:             100,
		TrustGuestRxFilters: true,
	}
	_, err := c.AddNICWithConfig(context.Background(), 42, cfg, 1004)
	require.NoError(t, err)

	assert.Contains(t, string(receivedParams), `"vlan":100`, "VLAN tag should be sent to TrueNAS")
	assert.Contains(t, string(receivedParams), `"trust_guest_rx_filters":true`)
}

func TestAddNICWithConfig_NoVLANTag(t *testing.T) {
	var receivedParams json.RawMessage

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		receivedParams = params

		return Device{ID: 5, VM: 42, Attributes: map[string]any{"dtype": "NIC"}}, nil
	})

	cfg := NICConfig{NetworkInterface: "br200"}
	_, err := c.AddNICWithConfig(context.Background(), 42, cfg, 1004)
	require.NoError(t, err)

	assert.NotContains(t, string(receivedParams), `"vlan"`, "no VLAN tag when vlan_id=0")
}

func TestNICConfig_ZeroValueDefaults(t *testing.T) {
	cfg := NICConfig{NetworkInterface: "br0"}

	assert.Equal(t, "br0", cfg.NetworkInterface)
	assert.Empty(t, cfg.Type, "type should default to empty (VIRTIO applied in AddNICWithConfig)")
	assert.Equal(t, 0, cfg.VLANTag, "vlan_id should default to 0 (not set)")
	assert.False(t, cfg.TrustGuestRxFilters)
}
