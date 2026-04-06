// Package cleanup provides periodic maintenance for TrueNAS resources.
// Handles ISO cleanup (stale ISOs from old Talos versions) and orphan
// cleanup (VMs/zvols not tracked by any Omni MachineRequest).
package cleanup

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

// Config holds cleanup configuration.
type Config struct {
	Pool              string
	CleanupInterval   time.Duration // How often to run cleanup (default: 1h)
	OrphanGracePeriod time.Duration // How long to wait before cleaning orphans (default: 30m)
}

// Cleaner performs periodic cleanup of stale TrueNAS resources.
type Cleaner struct {
	client *client.Client
	config Config
	logger *zap.Logger
	// activeImageIDs is called to get the set of image IDs currently in use.
	activeImageIDs func() map[string]bool
	// activeVMNames is called to get the set of VM names currently tracked by Omni.
	activeVMNames func() map[string]bool
}

// New creates a new Cleaner.
// activeImageIDs and activeVMNames are callbacks that return the currently active resources.
func New(c *client.Client, cfg Config, logger *zap.Logger, activeImageIDs, activeVMNames func() map[string]bool) *Cleaner {
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = time.Hour
	}

	if cfg.OrphanGracePeriod == 0 {
		cfg.OrphanGracePeriod = 30 * time.Minute
	}

	return &Cleaner{
		client:         c,
		config:         cfg,
		logger:         logger.Named("cleanup"),
		activeImageIDs: activeImageIDs,
		activeVMNames:  activeVMNames,
	}
}

// Run starts the periodic cleanup loop. Blocks until ctx is cancelled.
func (cl *Cleaner) Run(ctx context.Context) {
	ticker := time.NewTicker(cl.config.CleanupInterval)
	defer ticker.Stop()

	// Run once on startup after a short delay
	select {
	case <-time.After(5 * time.Minute):
		cl.runOnce(ctx)
	case <-ctx.Done():
		return
	}

	for {
		select {
		case <-ticker.C:
			cl.runOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (cl *Cleaner) runOnce(ctx context.Context) {
	cl.cleanupISOs(ctx)
	cl.cleanupOrphanVMs(ctx)
	cl.cleanupOrphanZvols(ctx)
}

// cleanupISOs removes ISOs from <pool>/talos-iso/ that are not referenced by any active VM.
func (cl *Cleaner) cleanupISOs(ctx context.Context) {
	isoDir := "/mnt/" + cl.config.Pool + "/talos-iso"

	files, err := cl.client.ListFiles(ctx, isoDir)
	if err != nil {
		cl.logger.Warn("failed to list ISOs for cleanup", zap.Error(err))

		return
	}

	activeIDs := cl.activeImageIDs()
	if activeIDs == nil {
		return
	}

	for _, f := range files {
		if f.Type != "FILE" || !strings.HasSuffix(f.Name, ".iso") {
			continue
		}

		// ISO filename is <imageID>.iso
		imageID := strings.TrimSuffix(f.Name, ".iso")
		if activeIDs[imageID] {
			continue
		}

		cl.logger.Info("removing stale ISO",
			zap.String("file", f.Name),
			zap.String("path", f.Path),
		)

		if err := cl.client.DeleteFile(ctx, f.Path); err != nil {
			cl.logger.Warn("failed to delete stale ISO", zap.String("path", f.Path), zap.Error(err))
		}
	}
}

// cleanupOrphanVMs finds VMs with the omni_ prefix that are not tracked by Omni.
func (cl *Cleaner) cleanupOrphanVMs(ctx context.Context) {
	vms, err := cl.client.ListVMs(ctx)
	if err != nil {
		cl.logger.Warn("failed to list VMs for orphan cleanup", zap.Error(err))

		return
	}

	activeNames := cl.activeVMNames()
	if activeNames == nil {
		return
	}

	for _, vm := range vms {
		if !strings.HasPrefix(vm.Name, "omni_") {
			continue
		}

		if activeNames[vm.Name] {
			continue
		}

		cl.logger.Info("removing orphan VM",
			zap.String("name", vm.Name),
			zap.Int("id", vm.ID),
		)

		if err := cl.client.StopVM(ctx, vm.ID, true); err != nil && !client.IsNotFound(err) {
			cl.logger.Warn("failed to stop orphan VM", zap.Int("id", vm.ID), zap.Error(err))
		}

		if err := cl.client.DeleteVM(ctx, vm.ID); err != nil {
			cl.logger.Warn("failed to delete orphan VM", zap.Int("id", vm.ID), zap.Error(err))
		}
	}
}

// cleanupOrphanZvols finds zvols under <pool>/omni-vms/ that are not tracked by Omni.
func (cl *Cleaner) cleanupOrphanZvols(ctx context.Context) {
	zvolDir := "/mnt/" + cl.config.Pool + "/omni-vms"

	files, err := cl.client.ListFiles(ctx, zvolDir)
	if err != nil {
		cl.logger.Warn("failed to list zvols for orphan cleanup", zap.Error(err))

		return
	}

	activeNames := cl.activeVMNames()
	if activeNames == nil {
		return
	}

	for _, f := range files {
		if f.Type != "DIRECTORY" {
			continue
		}

		// zvol name under omni-vms/ maps to the request ID
		// VM name is "omni_" + requestID with hyphens replaced by underscores
		vmName := "omni_" + strings.ReplaceAll(f.Name, "-", "_")
		if activeNames[vmName] {
			continue
		}

		zvolPath := cl.config.Pool + "/omni-vms/" + f.Name

		cl.logger.Info("removing orphan zvol",
			zap.String("path", zvolPath),
		)

		if err := cl.client.DeleteDataset(ctx, zvolPath); err != nil {
			cl.logger.Warn("failed to delete orphan zvol", zap.String("path", zvolPath), zap.Error(err))
		}
	}
}
