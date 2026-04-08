package client

import (
	"context"
	"fmt"
)

// HostInfo contains TrueNAS host system information.
type HostInfo struct {
	Hostname string    `json:"hostname"`
	Version  string    `json:"version"`
	Physmem  int64     `json:"physmem"` // Total physical memory in bytes
	Cores    int       `json:"cores"`   // Number of CPU cores
	Uptime   string    `json:"uptime"`
	LoadAvg  []float64 `json:"loadavg"` // 1, 5, 15 minute load averages
}

// SystemVersion returns the TrueNAS SCALE version string (e.g., "TrueNAS-SCALE-25.04.0").
// JSON-RPC method: system.version
func (c *Client) SystemVersion(ctx context.Context) (string, error) {
	var version string

	if err := c.call(ctx, "system.version", nil, &version); err != nil {
		return "", fmt.Errorf("system.version failed: %w", err)
	}

	return version, nil
}

// GetHostInfo returns detailed TrueNAS host information.
// JSON-RPC method: system.info
func (c *Client) GetHostInfo(ctx context.Context) (*HostInfo, error) {
	var info HostInfo

	if err := c.call(ctx, "system.info", nil, &info); err != nil {
		return nil, fmt.Errorf("system.info failed: %w", err)
	}

	return &info, nil
}

// PoolInfo contains ZFS pool health and space information.
type PoolInfo struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Status  string `json:"status"`    // ONLINE, DEGRADED, FAULTED
	Size    int64  `json:"size"`      // Total size in bytes
	Free    int64  `json:"free"`      // Free space in bytes
	Used    int64  `json:"allocated"` // Used space in bytes
}

// ListPools returns all ZFS pools with health and space info.
// JSON-RPC method: pool.query
func (c *Client) ListPools(ctx context.Context) ([]PoolInfo, error) {
	var pools []PoolInfo

	if err := c.call(ctx, "pool.query", nil, &pools); err != nil {
		return nil, fmt.Errorf("pool.query failed: %w", err)
	}

	return pools, nil
}

// DiskInfo contains disk health information.
type DiskInfo struct {
	Name   string `json:"name"` // e.g., "sda"
	Serial string `json:"serial"`
	Size   int64  `json:"size"` // Size in bytes
	Type   string `json:"type"` // HDD, SSD
	Pool   string `json:"pool"`
}

// ListDisks returns all disks with health info.
// JSON-RPC method: disk.query
func (c *Client) ListDisks(ctx context.Context) ([]DiskInfo, error) {
	var disks []DiskInfo

	if err := c.call(ctx, "disk.query", nil, &disks); err != nil {
		return nil, fmt.Errorf("disk.query failed: %w", err)
	}

	return disks, nil
}

// VMInstanceInfo contains VM runtime stats.
type VMInstanceInfo struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status struct {
		State string `json:"state"`
		Pid   int    `json:"pid"`
	} `json:"status"`
}

// GetVMInstance returns runtime info for a VM.
// JSON-RPC method: vm.get_instance
func (c *Client) GetVMInstance(ctx context.Context, id int) (*VMInstanceInfo, error) {
	var info VMInstanceInfo

	if err := c.call(ctx, "vm.get_instance", []any{id}, &info); err != nil {
		return nil, fmt.Errorf("vm.get_instance (id=%d) failed: %w", id, err)
	}

	return &info, nil
}
