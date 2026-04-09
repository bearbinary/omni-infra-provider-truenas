package client

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// UserProperty is a key-value pair for ZFS user properties.
// TrueNAS 25.10+ expects user_properties as a list of objects, not a map.
type UserProperty struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// CreateDatasetRequest is the payload for creating a dataset or zvol.
type CreateDatasetRequest struct {
	Name              string             `json:"name"`                         // Full path, e.g. "tank/talos-iso" or "tank/omni-vms/vm-1"
	Type              string             `json:"type"`                         // "FILESYSTEM" or "VOLUME"
	Volsize           int64              `json:"volsize,omitempty"`            // Required for VOLUME type, in bytes
	Encryption         bool               `json:"encryption,omitempty"`          // Enable ZFS native encryption
	InheritEncryption  *bool              `json:"inherit_encryption,omitempty"`  // Must be false when encryption is explicitly enabled
	EncryptionOptions  *EncryptionOptions `json:"encryption_options,omitempty"`  // Encryption configuration
	UserProperties    []UserProperty     `json:"user_properties,omitempty"`    // Custom ZFS user properties
}

// OmniManagedProperties returns user properties that tag a dataset as Omni-managed.
func OmniManagedProperties(requestID string) []UserProperty {
	return []UserProperty{
		{Key: "org.omni:managed", Value: "true"},
		{Key: "org.omni:provider", Value: "truenas"},
		{Key: "org.omni:request-id", Value: requestID},
	}
}

// EncryptionOptions configures ZFS native encryption for a dataset or zvol.
type EncryptionOptions struct {
	Algorithm  string `json:"algorithm,omitempty"`  // e.g., "AES-256-GCM" (default)
	Passphrase string `json:"passphrase,omitempty"` // Encryption passphrase
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
// Optional user properties are set on the dataset (e.g., Omni managed tags).
func (c *Client) CreateZvol(ctx context.Context, name string, sizeGiB int, props ...[]UserProperty) (*Dataset, error) {
	req := CreateDatasetRequest{
		Name:    name,
		Type:    "VOLUME",
		Volsize: int64(sizeGiB) * 1024 * 1024 * 1024,
	}

	if len(props) > 0 {
		req.UserProperties = props[0]
	}

	return c.CreateDataset(ctx, req)
}

// CreateEncryptedZvol creates an encrypted zvol with the given name, size, and passphrase.
func (c *Client) CreateEncryptedZvol(ctx context.Context, name string, sizeGiB int, passphrase string, props ...[]UserProperty) (*Dataset, error) {
	req := CreateDatasetRequest{
		Name:       name,
		Type:       "VOLUME",
		Volsize:    int64(sizeGiB) * 1024 * 1024 * 1024,
		Encryption:        true,
		InheritEncryption: boolPtr(false),
		EncryptionOptions: &EncryptionOptions{
			Algorithm:  "AES-256-GCM",
			Passphrase: passphrase,
		},
	}

	if len(props) > 0 {
		req.UserProperties = props[0]
	}

	return c.CreateDataset(ctx, req)
}

// UnlockDataset unlocks an encrypted dataset or zvol with a passphrase.
// Must be called after TrueNAS reboot before VMs using encrypted zvols can start.
// JSON-RPC method: pool.dataset.unlock
func (c *Client) UnlockDataset(ctx context.Context, path, passphrase string) error {
	params := []any{
		path,
		map[string]any{
			"datasets": []map[string]any{
				{"name": path, "passphrase": passphrase},
			},
		},
	}

	if err := c.call(ctx, "pool.dataset.unlock", params, nil); err != nil {
		return fmt.Errorf("pool.dataset.unlock %q failed: %w", path, err)
	}

	return nil
}

// LockDataset locks an encrypted dataset or zvol.
// JSON-RPC method: pool.dataset.lock
func (c *Client) LockDataset(ctx context.Context, path string) error {
	if err := c.call(ctx, "pool.dataset.lock", []any{path}, nil); err != nil {
		return fmt.Errorf("pool.dataset.lock %q failed: %w", path, err)
	}

	return nil
}

// IsDatasetLocked checks if an encrypted dataset is locked.
// JSON-RPC method: pool.dataset.query with filter
func (c *Client) IsDatasetLocked(ctx context.Context, path string) (bool, error) {
	filter := []any{
		[]any{[]any{"id", "=", path}},
		map[string]any{"get": true},
	}

	var ds struct {
		Locked bool `json:"locked"`
	}

	if err := c.call(ctx, "pool.dataset.query", filter, &ds); err != nil {
		return false, fmt.Errorf("pool.dataset.query %q failed: %w", path, err)
	}

	return ds.Locked, nil
}

// GetDatasetUserProperty reads a single ZFS user property from a dataset.
// Returns empty string if the property is not set.
// JSON-RPC method: pool.dataset.query with filter
func (c *Client) GetDatasetUserProperty(ctx context.Context, path, property string) (string, error) {
	filter := []any{
		[]any{[]any{"id", "=", path}},
		map[string]any{"get": true},
	}

	var ds struct {
		UserProperties map[string]struct {
			Value string `json:"value"`
		} `json:"user_properties"`
	}

	if err := c.call(ctx, "pool.dataset.query", filter, &ds); err != nil {
		return "", fmt.Errorf("pool.dataset.query %q failed: %w", path, err)
	}

	if prop, ok := ds.UserProperties[property]; ok {
		return prop.Value, nil
	}

	return "", nil
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

// --- Zvol Resize ---

// GetZvolSize returns the current size of a zvol in bytes.
// JSON-RPC method: pool.dataset.query with filter [["id", "=", path]]
func (c *Client) GetZvolSize(ctx context.Context, path string) (int64, error) {
	filter := []any{
		[]any{[]any{"id", "=", path}},
		map[string]any{"get": true},
	}

	var ds struct {
		Volsize struct {
			Parsed int64 `json:"parsed"`
		} `json:"volsize"`
	}

	if err := c.call(ctx, "pool.dataset.query", filter, &ds); err != nil {
		return 0, fmt.Errorf("pool.dataset.query %q failed: %w", path, err)
	}

	return ds.Volsize.Parsed, nil
}

// ResizeZvol changes the size of an existing zvol. Only grow operations are supported.
// JSON-RPC method: pool.dataset.update
func (c *Client) ResizeZvol(ctx context.Context, path string, newSizeGiB int) error {
	newSizeBytes := int64(newSizeGiB) * 1024 * 1024 * 1024

	params := []any{
		path,
		map[string]any{"volsize": newSizeBytes},
	}

	if err := c.call(ctx, "pool.dataset.update", params, nil); err != nil {
		return fmt.Errorf("pool.dataset.update %q (resize to %d GiB) failed: %w", path, newSizeGiB, err)
	}

	return nil
}

// --- ZFS Snapshots ---

// Snapshot represents a ZFS snapshot.
type Snapshot struct {
	ID      string `json:"id"`      // Full path: pool/dataset@snapname
	Name    string `json:"name"`    // Full name including dataset
	Dataset string `json:"dataset"` // Parent dataset path
}

// CreateSnapshot creates a ZFS snapshot of a dataset or zvol.
// JSON-RPC method: zfs.snapshot.create
func (c *Client) CreateSnapshot(ctx context.Context, dataset, name string) error {
	params := map[string]any{
		"dataset": dataset,
		"name":    name,
	}

	if err := c.call(ctx, "zfs.snapshot.create", params, nil); err != nil {
		return fmt.Errorf("zfs.snapshot.create %q@%s failed: %w", dataset, name, err)
	}

	return nil
}

// ListSnapshots returns all snapshots for a dataset.
// JSON-RPC method: zfs.snapshot.query
func (c *Client) ListSnapshots(ctx context.Context, dataset string) ([]Snapshot, error) {
	var allSnaps []Snapshot

	if err := c.call(ctx, "zfs.snapshot.query", nil, &allSnaps); err != nil {
		return nil, fmt.Errorf("zfs.snapshot.query failed: %w", err)
	}

	// Filter client-side — TrueNAS snapshot query filtering varies between versions
	prefix := dataset + "@"
	var snaps []Snapshot

	for _, s := range allSnaps {
		if strings.HasPrefix(s.ID, prefix) {
			snaps = append(snaps, s)
		}
	}

	return snaps, nil
}

// DeleteSnapshot deletes a ZFS snapshot.
// JSON-RPC method: zfs.snapshot.delete
func (c *Client) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	if err := c.call(ctx, "zfs.snapshot.delete", []any{snapshotID}, nil); err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("zfs.snapshot.delete %q failed: %w", snapshotID, err)
	}

	return nil
}

// RollbackSnapshot rolls back a dataset to a snapshot.
// JSON-RPC method: zfs.snapshot.rollback
func (c *Client) RollbackSnapshot(ctx context.Context, snapshotID string) error {
	params := []any{snapshotID, map[string]any{"force": true}}

	if err := c.call(ctx, "zfs.snapshot.rollback", params, nil); err != nil {
		return fmt.Errorf("zfs.snapshot.rollback %q failed: %w", snapshotID, err)
	}

	return nil
}

// PoolFreeSpace returns the usable available space in bytes for a ZFS pool.
// Queries the root dataset for accurate values that match the TrueNAS UI
// (accounts for ZFS overhead, parity, and metadata).
func (c *Client) PoolFreeSpace(ctx context.Context, pool string) (int64, error) {
	ds, err := c.getRootDatasetSpace(ctx, pool)
	if err != nil {
		return 0, fmt.Errorf("failed to query pool %q space: %w", pool, err)
	}

	return ds.Available.Parsed, nil
}

// SystemMemoryAvailable returns the available system memory in bytes.
// JSON-RPC method: system.info
func (c *Client) SystemMemoryAvailable(ctx context.Context) (int64, error) {
	var info struct {
		Physmem int64 `json:"physmem"`
	}

	if err := c.call(ctx, "system.info", nil, &info); err != nil {
		return 0, fmt.Errorf("system.info failed: %w", err)
	}

	return info.Physmem, nil
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

// NetworkInterfaceChoices returns the valid NIC attach targets (bridges, VLANs, physical interfaces).
// JSON-RPC method: vm.device.nic_attach_choices
func (c *Client) NetworkInterfaceChoices(ctx context.Context) (map[string]string, error) {
	var choices map[string]string

	if err := c.call(ctx, "vm.device.nic_attach_choices", nil, &choices); err != nil {
		return nil, fmt.Errorf("vm.device.nic_attach_choices failed: %w", err)
	}

	return choices, nil
}

// NetworkInterfaceValid checks if a NIC attach target exists on TrueNAS.
// Valid targets include bridges, VLANs, and physical interfaces.
func (c *Client) NetworkInterfaceValid(ctx context.Context, networkInterface string) (bool, error) {
	choices, err := c.NetworkInterfaceChoices(ctx)
	if err != nil {
		return false, err
	}

	_, exists := choices[networkInterface]

	return exists, nil
}

func boolPtr(b bool) *bool {
	return &b
}
