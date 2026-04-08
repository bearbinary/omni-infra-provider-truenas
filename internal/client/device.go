package client

import (
	"context"
	"fmt"
)

// AddDeviceRequest is the payload for adding a device to a VM.
// In TrueNAS 25.10+, dtype is inside attributes, not at the top level.
type AddDeviceRequest struct {
	VM         int            `json:"vm"`
	Order      int            `json:"order,omitempty"`
	Attributes map[string]any `json:"attributes"`
}

// Device represents a VM device.
type Device struct {
	ID         int            `json:"id"`
	VM         int            `json:"vm"`
	Order      int            `json:"order"`
	Attributes map[string]any `json:"attributes"`
}

// AddDevice adds a device to a VM.
// JSON-RPC method: vm.device.create
func (c *Client) AddDevice(ctx context.Context, req AddDeviceRequest) (*Device, error) {
	var dev Device

	dtype, _ := req.Attributes["dtype"].(string)

	if err := c.call(ctx, "vm.device.create", req, &dev); err != nil {
		return nil, fmt.Errorf("vm.device.create (%s for vm %d) failed: %w", dtype, req.VM, err)
	}

	return &dev, nil
}

// AddCDROM attaches a CDROM device to a VM pointing to an ISO path.
func (c *Client) AddCDROM(ctx context.Context, vmID int, isoPath string) (*Device, error) {
	return c.AddDevice(ctx, AddDeviceRequest{
		VM:    vmID,
		Order: 1000,
		Attributes: map[string]any{
			"dtype": "CDROM",
			"path":  isoPath,
		},
	})
}

// NICConfig describes a NIC to attach to a VM.
type NICConfig struct {
	NICAttach           string `json:"nic_attach" yaml:"nic_attach"`                                             // Bridge, VLAN, or physical interface
	Type                string `json:"type,omitempty" yaml:"type,omitempty"`                                     // VIRTIO (default) or E1000
	VLANTag             int    `json:"vlan_id,omitempty" yaml:"vlan_id,omitempty"`                               // Optional: tag traffic with this VLAN ID
	TrustGuestRxFilters bool   `json:"trust_guest_rx_filters,omitempty" yaml:"trust_guest_rx_filters,omitempty"` // Required for VLAN tagging
}

// AddNIC attaches a NIC device to a VM.
// nicAttach can be a bridge (e.g., "br100"), VLAN interface (e.g., "vlan666"),
// or physical interface (e.g., "enp5s0").
func (c *Client) AddNIC(ctx context.Context, vmID int, nicAttach string) (*Device, error) {
	return c.AddNICWithConfig(ctx, vmID, NICConfig{NICAttach: nicAttach}, 1003)
}

// AddNICWithConfig attaches a NIC device to a VM with full configuration options.
func (c *Client) AddNICWithConfig(ctx context.Context, vmID int, cfg NICConfig, order int) (*Device, error) {
	nicType := cfg.Type
	if nicType == "" {
		nicType = "VIRTIO"
	}

	attrs := map[string]any{
		"dtype":      "NIC",
		"type":       nicType,
		"nic_attach": cfg.NICAttach,
	}

	if cfg.VLANTag > 0 {
		attrs["vlan"] = cfg.VLANTag
	}

	if cfg.TrustGuestRxFilters {
		attrs["trust_guest_rx_filters"] = true
	}

	return c.AddDevice(ctx, AddDeviceRequest{
		VM:         vmID,
		Order:      order,
		Attributes: attrs,
	})
}

// DeleteDevice removes a device from a VM by device ID.
// JSON-RPC method: vm.device.delete
func (c *Client) DeleteDevice(ctx context.Context, id int) error {
	if err := c.call(ctx, "vm.device.delete", []any{id}, nil); err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("vm.device.delete (id=%d) failed: %w", id, err)
	}

	return nil
}

// UpdateDevice updates a device's attributes.
// JSON-RPC method: vm.device.update
func (c *Client) UpdateDevice(ctx context.Context, id int, attrs map[string]any) (*Device, error) {
	var dev Device

	params := []any{id, map[string]any{"attributes": attrs}}

	if err := c.call(ctx, "vm.device.update", params, &dev); err != nil {
		return nil, fmt.Errorf("vm.device.update (id=%d) failed: %w", id, err)
	}

	return &dev, nil
}

// ListDevices returns all devices attached to a VM.
// JSON-RPC method: vm.device.query with filter [["vm", "=", vmID]]
func (c *Client) ListDevices(ctx context.Context, vmID int) ([]Device, error) {
	filter := []any{
		[]any{[]any{"vm", "=", vmID}},
	}

	var devices []Device

	if err := c.call(ctx, "vm.device.query", filter, &devices); err != nil {
		return nil, fmt.Errorf("vm.device.query (vm=%d) failed: %w", vmID, err)
	}

	return devices, nil
}

// FindCDROM finds the CDROM device on a VM, if any.
func (c *Client) FindCDROM(ctx context.Context, vmID int) (*Device, error) {
	devices, err := c.ListDevices(ctx, vmID)
	if err != nil {
		return nil, err
	}

	for i, d := range devices {
		if dtype, _ := d.Attributes["dtype"].(string); dtype == "CDROM" {
			return &devices[i], nil
		}
	}

	return nil, nil
}

// SwapCDROM updates the CDROM device to point to a new ISO path.
// If no CDROM exists, attaches a new one.
func (c *Client) SwapCDROM(ctx context.Context, vmID int, isoPath string) (*Device, error) {
	existing, err := c.FindCDROM(ctx, vmID)
	if err != nil {
		return nil, fmt.Errorf("failed to find CDROM: %w", err)
	}

	if existing != nil {
		return c.UpdateDevice(ctx, existing.ID, map[string]any{
			"dtype": "CDROM",
			"path":  isoPath,
		})
	}

	return c.AddCDROM(ctx, vmID, isoPath)
}

// ResetVMNVRAM deletes the VM's NVRAM file to force firmware re-initialization.
// This is needed when TrueNAS updates OVMF firmware.
// JSON-RPC method: vm.update with remove_nvram=true (TrueNAS 25.04+)
func (c *Client) ResetVMNVRAM(ctx context.Context, vmID int) error {
	params := []any{vmID, map[string]any{"remove_nvram": true}}

	if err := c.call(ctx, "vm.update", params, nil); err != nil {
		return fmt.Errorf("vm.update (reset NVRAM, id=%d) failed: %w", vmID, err)
	}

	return nil
}

// AddDisk attaches a DISK device to a VM pointing to a zvol path.
func (c *Client) AddDisk(ctx context.Context, vmID int, zvolPath string) (*Device, error) {
	return c.AddDevice(ctx, AddDeviceRequest{
		VM:    vmID,
		Order: 1001,
		Attributes: map[string]any{
			"dtype": "DISK",
			"type":  "VIRTIO",
			"path":  "/dev/zvol/" + zvolPath,
		},
	})
}
