package client

import (
	"context"
	"fmt"
)

// JSON-RPC method constants.
const (
	methodVMCreate = "vm.create"
	methodVMQuery  = "vm.query"
	methodVMStart  = "vm.start"
	methodVMStop   = "vm.stop"
	methodVMDelete = "vm.delete"
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
	UUID        string `json:"uuid,omitempty"` // SMBIOS UUID — must match what we tell Omni so Talos identity correlates
	VCPUs       int    `json:"vcpus"`
	Memory      int    `json:"memory"` // MiB — hard ceiling, fully reserved at vm.start when MinMemory is zero
	// MinMemory, when non-zero, is sent as the TrueNAS `min_memory` field —
	// the soft floor for memory ballooning. The VM starts with MinMemory
	// reserved and balloons up to Memory as host RAM is available.
	// Omitted via `omitempty` when zero so TrueNAS gets `null` (its
	// "no balloon, fully reserve Memory" sentinel) rather than a numeric
	// zero, which the middleware would reject.
	MinMemory  int    `json:"min_memory,omitempty"` // MiB
	Bootloader string `json:"bootloader"`
	Autostart  bool   `json:"autostart"`
	CPUMode    string `json:"cpu_mode,omitempty"` // HOST-PASSTHROUGH, HOST-MODEL, or CUSTOM (default)
}

// CreateVM creates a new virtual machine.
// JSON-RPC method: vm.create
func (c *Client) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	var vm VM

	if err := c.call(ctx, methodVMCreate, req, &vm); err != nil {
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

	if err := c.call(ctx, methodVMQuery, filter, &vm); err != nil {
		return nil, fmt.Errorf("vm.query (id=%d) failed: %w", id, err)
	}

	return &vm, nil
}

// ListVMs returns all VMs.
// JSON-RPC method: vm.query
func (c *Client) ListVMs(ctx context.Context) ([]VM, error) {
	var vms []VM

	if err := c.call(ctx, methodVMQuery, nil, &vms); err != nil {
		return nil, fmt.Errorf("vm.query failed: %w", err)
	}

	return vms, nil
}

// RunningGuestsMemoryMiB returns the sum of the `memory` field for all VMs
// currently in the RUNNING state on the host. Used by the provision-time
// pre-flight to reject MachineClasses whose RAM, when added to what's
// already committed by other guests, would exceed host headroom — catching
// the runtime ENOMEM before vm.start can hit it.
//
// Counts only RUNNING VMs (not STOPPED) because TrueNAS only locks guest
// memory while a VM is live. A stopped VM with 32 GiB configured imposes
// no memory load until it boots.
//
// Provider-managed and externally-managed VMs are both included — the host
// memory ceiling doesn't care who created the guest.
func (c *Client) RunningGuestsMemoryMiB(ctx context.Context) (int64, error) {
	vms, err := c.ListVMs(ctx)
	if err != nil {
		return 0, err
	}

	var total int64
	for _, vm := range vms {
		if vm.Status.State == "RUNNING" {
			total += int64(vm.Memory)
		}
	}

	return total, nil
}

// FindVMByName searches for a VM by name.
// JSON-RPC method: vm.query with filter [["name", "=", name]]
func (c *Client) FindVMByName(ctx context.Context, name string) (*VM, error) {
	filter := []any{
		[]any{[]any{"name", "=", name}},
	}

	var vms []VM

	if err := c.call(ctx, methodVMQuery, filter, &vms); err != nil {
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
	if err := c.call(ctx, methodVMStart, []any{id}, nil); err != nil {
		return fmt.Errorf("vm.start (id=%d) failed: %w", id, err)
	}

	return nil
}

// StopVM stops a VM by ID.
// JSON-RPC method: vm.stop
func (c *Client) StopVM(ctx context.Context, id int, force bool) error {
	params := []any{id, map[string]any{"force": force}}

	if err := c.call(ctx, methodVMStop, params, nil); err != nil {
		return fmt.Errorf("vm.stop (id=%d) failed: %w", id, err)
	}

	return nil
}

// DeleteVM deletes a VM by ID.
// JSON-RPC method: vm.delete
//
// Passes {force: true} so the call succeeds for VMs in any lifecycle state.
// Without force, TrueNAS's `vm.delete` internally tries to stop the VM first
// and refuses with EFAULT "VM state is currently not 'RUNNING / SUSPENDED'"
// if the VM is in a transitional state (STOPPING, LOCKED, etc.) — which is
// exactly when orphan cleanup tries to remove it. Observed in production
// v0.15.0 logs as `failed to delete orphan VM`.
//
// NOTE: A previous attempt in v0.15.1 also passed `force_after_timeout: true`.
// TrueNAS 25.10 rejects that option with EINVAL ("Extra inputs are not
// permitted") — `vm.delete` only accepts `{force, zvols}`, not
// `force_after_timeout` (that's an `vm.stop` option). Do not re-add it.
func (c *Client) DeleteVM(ctx context.Context, id int) error {
	opts := map[string]any{
		"force": true,
	}

	if err := c.call(ctx, methodVMDelete, []any{id, opts}, nil); err != nil {
		if IsNotFound(err) {
			return nil // already gone
		}

		return fmt.Errorf("vm.delete (id=%d) failed: %w", id, err)
	}

	return nil
}
