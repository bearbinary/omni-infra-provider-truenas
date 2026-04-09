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
	Status  string `json:"status"` // ONLINE, DEGRADED, FAULTED
	Size    int64  `json:"size"`   // Raw pool size in bytes
	Free    int64  // Usable free space (from root dataset)
	Used    int64  // Used space (from root dataset)
}

// rawPoolInfo is the raw JSON-RPC response from pool.query.
type rawPoolInfo struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Status  string `json:"status"`
	Size    int64  `json:"size"`
}

// rootDatasetInfo is the response from pool.dataset.query for the root dataset.
type rootDatasetInfo struct {
	Available struct {
		Parsed int64 `json:"parsed"`
	} `json:"available"`
	Used struct {
		Parsed int64 `json:"parsed"`
	} `json:"used"`
}

// ListPools returns all ZFS pools with health and usable space info.
// Space values come from the root dataset (matches TrueNAS UI), not raw pool stats.
func (c *Client) ListPools(ctx context.Context) ([]PoolInfo, error) {
	var rawPools []rawPoolInfo

	if err := c.call(ctx, "pool.query", nil, &rawPools); err != nil {
		return nil, fmt.Errorf("pool.query failed: %w", err)
	}

	pools := make([]PoolInfo, len(rawPools))
	for i, rp := range rawPools {
		pools[i] = PoolInfo{
			ID:      rp.ID,
			Name:    rp.Name,
			Healthy: rp.Healthy,
			Status:  rp.Status,
			Size:    rp.Size,
			Free:    rp.Size, // Fallback to raw size
		}

		// Get usable space from root dataset (accounts for ZFS overhead, parity, metadata)
		ds, err := c.getRootDatasetSpace(ctx, rp.Name)
		if err == nil {
			pools[i].Free = ds.Available.Parsed
			pools[i].Used = ds.Used.Parsed
		}
	}

	return pools, nil
}

// getRootDatasetSpace queries the root dataset for usable space info.
func (c *Client) getRootDatasetSpace(ctx context.Context, poolName string) (*rootDatasetInfo, error) {
	filter := []any{
		[]any{[]any{"id", "=", poolName}},
		map[string]any{"get": true},
	}

	var ds rootDatasetInfo

	if err := c.call(ctx, "pool.dataset.query", filter, &ds); err != nil {
		return nil, err
	}

	return &ds, nil
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
