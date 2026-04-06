package client

import (
	"context"
	"fmt"
)

// VM represents a TrueNAS virtual machine.
type VM struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	VCPUs       int      `json:"vcpus"`
	Memory      int      `json:"memory"` // MiB
	Bootloader  string   `json:"bootloader"`
	Autostart   bool     `json:"autostart"`
	Status      VMStatus `json:"status"`
}

// VMStatus represents the runtime status of a VM.
type VMStatus struct {
	State string `json:"state"` // RUNNING, STOPPED, ERROR
	Pid   int    `json:"pid,omitempty"`
}

// CreateVMRequest is the payload for creating a VM.
type CreateVMRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	VCPUs       int    `json:"vcpus"`
	Memory      int    `json:"memory"`   // MiB
	Bootloader  string `json:"bootloader"`
	Autostart   bool   `json:"autostart"`
	CPUMode     string `json:"cpu_mode,omitempty"` // HOST-PASSTHROUGH, HOST-MODEL, or CUSTOM (default)
}

// CreateVM creates a new virtual machine.
// JSON-RPC method: vm.create
func (c *Client) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	var vm VM

	if err := c.call(ctx, "vm.create", req, &vm); err != nil {
		return nil, fmt.Errorf("vm.create failed: %w", err)
	}

	return &vm, nil
}

// GetVM retrieves a VM by ID.
// JSON-RPC method: vm.query with filter [["id", "=", id]]
func (c *Client) GetVM(ctx context.Context, id int) (*VM, error) {
	filter := []any{
		[]any{[]any{"id", "=", id}},
		map[string]any{"get": true},
	}

	var vm VM

	if err := c.call(ctx, "vm.query", filter, &vm); err != nil {
		return nil, fmt.Errorf("vm.query (id=%d) failed: %w", id, err)
	}

	return &vm, nil
}

// ListVMs returns all VMs.
// JSON-RPC method: vm.query
func (c *Client) ListVMs(ctx context.Context) ([]VM, error) {
	var vms []VM

	if err := c.call(ctx, "vm.query", nil, &vms); err != nil {
		return nil, fmt.Errorf("vm.query failed: %w", err)
	}

	return vms, nil
}

// FindVMByName searches for a VM by name.
// JSON-RPC method: vm.query with filter [["name", "=", name]]
func (c *Client) FindVMByName(ctx context.Context, name string) (*VM, error) {
	filter := []any{
		[]any{[]any{"name", "=", name}},
	}

	var vms []VM

	if err := c.call(ctx, "vm.query", filter, &vms); err != nil {
		return nil, fmt.Errorf("vm.query (name=%s) failed: %w", name, err)
	}

	if len(vms) == 0 {
		return nil, nil
	}

	return &vms[0], nil
}

// StartVM starts a VM by ID.
// JSON-RPC method: vm.start
func (c *Client) StartVM(ctx context.Context, id int) error {
	if err := c.call(ctx, "vm.start", []any{id}, nil); err != nil {
		return fmt.Errorf("vm.start (id=%d) failed: %w", id, err)
	}

	return nil
}

// StopVM stops a VM by ID.
// JSON-RPC method: vm.stop
func (c *Client) StopVM(ctx context.Context, id int, force bool) error {
	params := []any{id, map[string]any{"force": force}}

	if err := c.call(ctx, "vm.stop", params, nil); err != nil {
		return fmt.Errorf("vm.stop (id=%d) failed: %w", id, err)
	}

	return nil
}

// DeleteVM deletes a VM by ID.
// JSON-RPC method: vm.delete
func (c *Client) DeleteVM(ctx context.Context, id int) error {
	if err := c.call(ctx, "vm.delete", []any{id}, nil); err != nil {
		if IsNotFound(err) {
			return nil // already gone
		}

		return fmt.Errorf("vm.delete (id=%d) failed: %w", id, err)
	}

	return nil
}
