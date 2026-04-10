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

func TestNICConfig_ZeroValueDefaults(t *testing.T) {
	cfg := NICConfig{NetworkInterface: "br0"}

	assert.Equal(t, "br0", cfg.NetworkInterface)
	assert.Empty(t, cfg.Type, "type should default to empty (VIRTIO applied in AddNICWithConfig)")
	assert.Empty(t, cfg.MAC, "MAC should default to empty (TrueNAS auto-generates)")
	assert.False(t, cfg.TrustGuestRxFilters)
}

func TestAddNICWithConfig_ExplicitMAC(t *testing.T) {
	var receivedParams json.RawMessage

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		receivedParams = params

		return Device{ID: 5, VM: 42, Attributes: map[string]any{
			"dtype": "NIC",
			"mac":   "02:ab:cd:ef:01:23",
		}}, nil
	})

	cfg := NICConfig{
		NetworkInterface: "br100",
		MAC:              "02:ab:cd:ef:01:23",
	}
	dev, err := c.AddNICWithConfig(context.Background(), 42, cfg, 2001)
	require.NoError(t, err)
	assert.Equal(t, 5, dev.ID)

	assert.Contains(t, string(receivedParams), `"mac":"02:ab:cd:ef:01:23"`)
	assert.Contains(t, string(receivedParams), `"nic_attach":"br100"`)
}

func TestNICMACsOnSegment_FiltersbySegmentAndType(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.device.query", method)

		return []Device{
			{ID: 1, VM: 10, Attributes: map[string]any{"dtype": "NIC", "nic_attach": "br100", "mac": "02:aa:bb:cc:dd:01"}},
			{ID: 2, VM: 10, Attributes: map[string]any{"dtype": "DISK", "path": "/dev/zvol/tank/x"}},
			{ID: 3, VM: 20, Attributes: map[string]any{"dtype": "NIC", "nic_attach": "br100", "mac": "02:aa:bb:cc:dd:02"}},
			{ID: 4, VM: 20, Attributes: map[string]any{"dtype": "NIC", "nic_attach": "br200", "mac": "02:aa:bb:cc:dd:03"}}, // different segment
			{ID: 5, VM: 30, Attributes: map[string]any{"dtype": "NIC", "nic_attach": "br100", "mac": ""}},                  // empty MAC
		}, nil
	})

	macs, err := c.NICMACsOnSegment(context.Background(), "br100")
	require.NoError(t, err)

	assert.Len(t, macs, 2, "should only include NICs on br100 with non-empty MACs")
	assert.Equal(t, 10, macs["02:aa:bb:cc:dd:01"])
	assert.Equal(t, 20, macs["02:aa:bb:cc:dd:02"])
	_, hasBr200 := macs["02:aa:bb:cc:dd:03"]
	assert.False(t, hasBr200, "should not include MACs from br200")
}

func TestNICMACsOnSegment_NormalizesCase(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return []Device{
			{ID: 1, VM: 10, Attributes: map[string]any{"dtype": "NIC", "nic_attach": "br100", "mac": "02:AA:BB:CC:DD:EE"}},
		}, nil
	})

	macs, err := c.NICMACsOnSegment(context.Background(), "br100")
	require.NoError(t, err)

	_, ok := macs["02:aa:bb:cc:dd:ee"]
	assert.True(t, ok, "MACs should be lowercased for case-insensitive matching")
}

func TestNICMACsOnSegment_EmptyWhenNoMatch(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return []Device{
			{ID: 1, VM: 10, Attributes: map[string]any{"dtype": "NIC", "nic_attach": "br200", "mac": "02:aa:bb:cc:dd:01"}},
		}, nil
	})

	macs, err := c.NICMACsOnSegment(context.Background(), "br100")
	require.NoError(t, err)
	assert.Empty(t, macs, "should return empty map when no NICs on the requested segment")
}

func TestAddNICWithConfig_InvalidMAC(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		t.Fatal("should not reach TrueNAS with invalid MAC")
		return nil, nil
	})

	tests := []struct {
		name string
		mac  string
	}{
		{"uppercase", "02:AB:CD:EF:01:23"},
		{"missing colons", "02abcdef0123"},
		{"too short", "02:ab:cd"},
		{"too long", "02:ab:cd:ef:01:23:99"},
		{"garbage", "not-a-mac"},
		{"dash separated", "02-ab-cd-ef-01-23"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NICConfig{NetworkInterface: "br100", MAC: tt.mac}
			_, err := c.AddNICWithConfig(context.Background(), 42, cfg, 2001)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid MAC address")
		})
	}
}

func TestAddNICWithConfig_NoMAC(t *testing.T) {
	var receivedParams json.RawMessage

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		receivedParams = params

		return Device{ID: 6, VM: 42, Attributes: map[string]any{"dtype": "NIC"}}, nil
	})

	cfg := NICConfig{NetworkInterface: "br100"}
	_, err := c.AddNICWithConfig(context.Background(), 42, cfg, 2001)
	require.NoError(t, err)

	assert.NotContains(t, string(receivedParams), `"mac"`, "empty MAC should not be sent to TrueNAS")
}
