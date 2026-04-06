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

// AddNIC attaches a NIC device to a VM.
// nicAttach can be a bridge (e.g., "br100"), VLAN interface (e.g., "vlan666"),
// or physical interface (e.g., "enp5s0").
func (c *Client) AddNIC(ctx context.Context, vmID int, nicAttach string) (*Device, error) {
	return c.AddDevice(ctx, AddDeviceRequest{
		VM:    vmID,
		Order: 1003,
		Attributes: map[string]any{
			"dtype":      "NIC",
			"type":       "VIRTIO",
			"nic_attach": nicAttach,
		},
	})
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
