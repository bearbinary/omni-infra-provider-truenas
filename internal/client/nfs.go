package client

import (
	"context"
	"fmt"
)

// NFSShare represents a TrueNAS NFS share.
type NFSShare struct {
	ID      int    `json:"id"`
	Path    string `json:"path"`
	Comment string `json:"comment,omitempty"`
	Enabled bool   `json:"enabled"`
}

// CreateNFSShareRequest is the payload for creating an NFS share.
type CreateNFSShareRequest struct {
	Path    string `json:"path"`
	Comment string `json:"comment,omitempty"`
}

// CreateNFSShare creates an NFS share on TrueNAS.
// JSON-RPC method: sharing.nfs.create
func (c *Client) CreateNFSShare(ctx context.Context, req CreateNFSShareRequest) (*NFSShare, error) {
	var share NFSShare

	if err := c.call(ctx, "sharing.nfs.create", req, &share); err != nil {
		return nil, fmt.Errorf("sharing.nfs.create failed: %w", err)
	}

	return &share, nil
}

// GetNFSShareByPath finds an NFS share by its filesystem path.
// Returns nil if no share exists at the given path.
// JSON-RPC method: sharing.nfs.query
func (c *Client) GetNFSShareByPath(ctx context.Context, path string) (*NFSShare, error) {
	filter := []any{
		[]any{[]any{"path", "=", path}},
	}

	var shares []NFSShare

	if err := c.call(ctx, "sharing.nfs.query", filter, &shares); err != nil {
		return nil, fmt.Errorf("sharing.nfs.query (path=%s) failed: %w", path, err)
	}

	if len(shares) == 0 {
		return nil, nil
	}

	return &shares[0], nil
}

// DeleteNFSShare deletes an NFS share by ID.
// JSON-RPC method: sharing.nfs.delete
func (c *Client) DeleteNFSShare(ctx context.Context, id int) error {
	if err := c.call(ctx, "sharing.nfs.delete", []any{id}, nil); err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("sharing.nfs.delete (id=%d) failed: %w", id, err)
	}

	return nil
}

// ListNFSShares returns all NFS shares.
// JSON-RPC method: sharing.nfs.query
func (c *Client) ListNFSShares(ctx context.Context) ([]NFSShare, error) {
	var shares []NFSShare

	if err := c.call(ctx, "sharing.nfs.query", nil, &shares); err != nil {
		return nil, fmt.Errorf("sharing.nfs.query failed: %w", err)
	}

	return shares, nil
}

// EnsureNFSService starts the NFS service if it is not already running.
func (c *Client) EnsureNFSService(ctx context.Context) error {
	running, err := c.isServiceRunning(ctx, "nfs")
	if err != nil {
		return fmt.Errorf("failed to check NFS service state: %w", err)
	}

	if running {
		return nil
	}

	if err := c.startService(ctx, "nfs"); err != nil {
		return fmt.Errorf("failed to start NFS service: %w", err)
	}

	return nil
}

// isServiceRunning checks if a TrueNAS service is currently running.
// JSON-RPC method: service.query
func (c *Client) isServiceRunning(ctx context.Context, service string) (bool, error) {
	filter := []any{
		[]any{[]any{"service", "=", service}},
		map[string]any{"get": true},
	}

	var svc struct {
		State string `json:"state"`
	}

	if err := c.call(ctx, "service.query", filter, &svc); err != nil {
		return false, err
	}

	return svc.State == "RUNNING", nil
}

// startService starts a TrueNAS service.
// JSON-RPC method: service.start
func (c *Client) startService(ctx context.Context, service string) error {
	if err := c.call(ctx, "service.start", []any{service}, nil); err != nil {
		return err
	}

	return nil
}
