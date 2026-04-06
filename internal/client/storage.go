package client

import (
	"context"
	"fmt"
	"io"
)

// CreateDatasetRequest is the payload for creating a dataset or zvol.
type CreateDatasetRequest struct {
	Name    string `json:"name"`              // Full path, e.g. "tank/talos-iso" or "tank/omni-vms/vm-1"
	Type    string `json:"type"`              // "FILESYSTEM" or "VOLUME"
	Volsize int64  `json:"volsize,omitempty"` // Required for VOLUME type, in bytes
}

// Dataset represents a ZFS dataset or zvol.
type Dataset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Pool string `json:"pool"`
}

// CreateDataset creates a ZFS dataset or zvol.
// JSON-RPC method: pool.dataset.create
func (c *Client) CreateDataset(ctx context.Context, req CreateDatasetRequest) (*Dataset, error) {
	var ds Dataset

	if err := c.call(ctx, "pool.dataset.create", req, &ds); err != nil {
		return nil, fmt.Errorf("pool.dataset.create %q failed: %w", req.Name, err)
	}

	return &ds, nil
}

// CreateZvol creates a zvol with the given name and size in GiB.
func (c *Client) CreateZvol(ctx context.Context, name string, sizeGiB int) (*Dataset, error) {
	return c.CreateDataset(ctx, CreateDatasetRequest{
		Name:    name,
		Type:    "VOLUME",
		Volsize: int64(sizeGiB) * 1024 * 1024 * 1024,
	})
}

// EnsureDataset creates a FILESYSTEM dataset if it doesn't exist.
func (c *Client) EnsureDataset(ctx context.Context, name string) error {
	_, err := c.CreateDataset(ctx, CreateDatasetRequest{
		Name: name,
		Type: "FILESYSTEM",
	})
	if err != nil && IsAlreadyExists(err) {
		return nil // already exists
	}

	return err
}

// DeleteDataset deletes a dataset or zvol by path.
// JSON-RPC method: pool.dataset.delete
func (c *Client) DeleteDataset(ctx context.Context, path string) error {
	if err := c.call(ctx, "pool.dataset.delete", []any{path}, nil); err != nil {
		if IsNotFound(err) {
			return nil // already gone
		}

		return fmt.Errorf("pool.dataset.delete %q failed: %w", path, err)
	}

	return nil
}

// FileExists checks if a file exists at the given path on TrueNAS.
// JSON-RPC method: filesystem.stat
func (c *Client) FileExists(ctx context.Context, path string) (bool, error) {
	var stat map[string]any

	if err := c.call(ctx, "filesystem.stat", []any{path}, &stat); err != nil {
		if IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("filesystem.stat %q failed: %w", path, err)
	}

	return true, nil
}

// UploadFile uploads a file to the given path on TrueNAS.
//
// filesystem.put requires a pipe-based upload which isn't available over the
// standard WebSocket call interface. We use the REST upload endpoint instead,
// which is available on all TrueNAS SCALE versions alongside the WebSocket API.
func (c *Client) UploadFile(ctx context.Context, destPath string, data io.Reader, size int64) error {
	return c.transport.UploadFile(ctx, destPath, data, size)
}

// ListFiles lists files in a directory on TrueNAS.
// JSON-RPC method: filesystem.listdir
func (c *Client) ListFiles(ctx context.Context, path string) ([]FileEntry, error) {
	var entries []FileEntry

	if err := c.call(ctx, "filesystem.listdir", []any{path}, &entries); err != nil {
		if IsNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("filesystem.listdir %q failed: %w", path, err)
	}

	return entries, nil
}

// FileEntry represents a file or directory from filesystem.listdir.
type FileEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // FILE, DIRECTORY, etc.
}

// RecreateDataset deletes a dataset and recreates it empty.
// Used for cleaning up files on a dataset when the TrueNAS API doesn't
// expose a direct file delete method.
func (c *Client) RecreateDataset(ctx context.Context, name string) error {
	if err := c.DeleteDataset(ctx, name); err != nil {
		return fmt.Errorf("failed to delete dataset %q: %w", name, err)
	}

	_, err := c.CreateDataset(ctx, CreateDatasetRequest{
		Name: name,
		Type: "FILESYSTEM",
	})
	if err != nil {
		return fmt.Errorf("failed to recreate dataset %q: %w", name, err)
	}

	return nil
}

// ListChildDatasets returns child datasets/zvols under a parent path.
// JSON-RPC method: pool.dataset.query with filter [["id", "^", parentPath + "/"]]
func (c *Client) ListChildDatasets(ctx context.Context, parentPath string) ([]Dataset, error) {
	filter := []any{
		[]any{[]any{"id", "^", parentPath + "/"}},
	}

	var datasets []Dataset

	if err := c.call(ctx, "pool.dataset.query", filter, &datasets); err != nil {
		return nil, fmt.Errorf("pool.dataset.query (parent=%s) failed: %w", parentPath, err)
	}

	return datasets, nil
}

// PoolExists checks if a ZFS pool exists.
// JSON-RPC method: pool.query with filter [["name", "=", pool]]
func (c *Client) PoolExists(ctx context.Context, pool string) (bool, error) {
	filter := []any{
		[]any{[]any{"name", "=", pool}},
	}

	var pools []map[string]any

	if err := c.call(ctx, "pool.query", filter, &pools); err != nil {
		return false, fmt.Errorf("pool.query failed: %w", err)
	}

	return len(pools) > 0, nil
}

// NICAttachChoices returns the valid NIC attach targets (bridges, VLANs, physical interfaces).
// JSON-RPC method: vm.device.nic_attach_choices
func (c *Client) NICAttachChoices(ctx context.Context) (map[string]string, error) {
	var choices map[string]string

	if err := c.call(ctx, "vm.device.nic_attach_choices", nil, &choices); err != nil {
		return nil, fmt.Errorf("vm.device.nic_attach_choices failed: %w", err)
	}

	return choices, nil
}

// NICAttachValid checks if a NIC attach target exists on TrueNAS.
// Valid targets include bridges, VLANs, and physical interfaces.
func (c *Client) NICAttachValid(ctx context.Context, nicAttach string) (bool, error) {
	choices, err := c.NICAttachChoices(ctx)
	if err != nil {
		return false, err
	}

	_, exists := choices[nicAttach]

	return exists, nil
}
