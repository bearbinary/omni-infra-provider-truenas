// Package cleanup provides periodic maintenance for TrueNAS resources.
// Handles ISO cleanup (stale ISOs from old Talos versions) and orphan
// cleanup (VMs/zvols not tracked by any Omni MachineRequest).
package cleanup

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

var cleanupTracer = otel.Tracer("truenas-cleanup")

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
}

// New creates a new Cleaner.
// activeImageIDs returns the set of image IDs currently in use (for ISO cleanup).
func New(c *client.Client, cfg Config, logger *zap.Logger, activeImageIDs func() map[string]bool) *Cleaner {
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
	start := time.Now()
	cl.logger.Debug("cleanup cycle starting")

	cl.cleanupISOs(ctx)
	cl.cleanupOrphanVMs(ctx)
	cl.cleanupOrphanZvols(ctx)

	cl.logger.Debug("cleanup cycle complete", zap.Duration("elapsed", time.Since(start)))
}

// cleanupISOs removes stale ISOs from <pool>/talos-iso/.
// TrueNAS JSON-RPC doesn't expose a file delete method, so we check if ALL
// ISOs are stale. If so, we recreate the dataset (delete + create), which
// removes all files. If any ISO is still active, we skip cleanup entirely —
// active ISOs will be re-downloaded if needed after a full wipe.
func (cl *Cleaner) cleanupISOs(ctx context.Context) {
	ctx, span := cleanupTracer.Start(ctx, "cleanup.isos")
	defer span.End()

	isoDir := "/mnt/" + cl.config.Pool + "/talos-iso"
	isoDataset := cl.config.Pool + "/talos-iso"

	files, err := cl.client.ListFiles(ctx, isoDir)
	if err != nil {
		cl.logger.Warn("failed to list ISOs for cleanup", zap.Error(err))

		return
	}

	activeIDs := cl.activeImageIDs()
	if activeIDs == nil {
		return
	}

	var totalISOs, staleISOs int

	for _, f := range files {
		if f.Type != "FILE" || !strings.HasSuffix(f.Name, ".iso") {
			continue
		}

		totalISOs++

		imageID := strings.TrimSuffix(f.Name, ".iso")
		if !activeIDs[imageID] {
			staleISOs++
		}
	}

	if staleISOs == 0 {
		return
	}

	cl.logger.Debug("found stale ISOs",
		zap.Int("stale", staleISOs),
		zap.Int("total", totalISOs),
		zap.Int("active", totalISOs-staleISOs),
	)

	// Only wipe if ALL ISOs are stale (no active ISOs to preserve)
	if staleISOs < totalISOs {
		cl.logger.Debug("skipping ISO cleanup — some ISOs are still active",
			zap.Int("active", totalISOs-staleISOs),
		)

		return
	}

	span.SetAttributes(
		attribute.Int("stale_isos", staleISOs),
		attribute.Int("total_isos", totalISOs),
	)

	cl.logger.Info("all ISOs are stale — recreating dataset",
		zap.String("dataset", isoDataset),
		zap.Int("removing", staleISOs),
	)

	if err := cl.client.RecreateDataset(ctx, isoDataset); err != nil {
		cl.logger.Warn("failed to recreate ISO dataset", zap.Error(err))
		span.RecordError(err)
	} else if telemetry.CleanupISOsRemoved != nil {
		telemetry.CleanupISOsRemoved.Add(ctx, int64(staleISOs))
	}
}

// cleanupOrphanVMs finds VMs with the omni_ prefix whose backing zvol no longer exists.
// A VM is considered an orphan only when its zvol has been deleted (by Deprovision) but
// the VM itself was not — indicating a partial cleanup. This avoids any dependency on
// in-memory state, which is lost on restart.
func (cl *Cleaner) cleanupOrphanVMs(ctx context.Context) {
	ctx, span := cleanupTracer.Start(ctx, "cleanup.orphanVMs")
	defer span.End()

	vms, err := cl.client.ListVMs(ctx)
	if err != nil {
		cl.logger.Warn("failed to list VMs for orphan cleanup", zap.Error(err))

		return
	}

	// Build a set of request IDs that have a managed zvol on TrueNAS.
	// A VM is only orphaned if its zvol has been deleted (by Deprovision).
	managedRequestIDs, err := cl.client.ListManagedRequestIDs(ctx)
	if err != nil {
		cl.logger.Warn("failed to list managed zvols — skipping orphan VM cleanup", zap.Error(err))

		return
	}

	for _, vm := range vms {
		if !strings.HasPrefix(vm.Name, "omni_") {
			continue
		}

		// Derive the request ID from the VM name.
		// VM name: "omni_talos_xxx_yyy" → request ID: "talos-xxx-yyy"
		requestID := strings.ReplaceAll(strings.TrimPrefix(vm.Name, "omni_"), "_", "-")

		// Check if a backing zvol with this request ID still exists.
		if managedRequestIDs[requestID] {
			continue
		}

		// Zvol is gone but VM still exists — orphaned from a partial deprovision
		cl.logger.Info("removing orphan VM (backing zvol not found)",
			zap.String("name", vm.Name),
			zap.Int("id", vm.ID),
			zap.String("request_id", requestID),
		)

		if err := cl.client.StopVM(ctx, vm.ID, true); err != nil && !client.IsNotFound(err) {
			cl.logger.Warn("failed to stop orphan VM", zap.Int("id", vm.ID), zap.Error(err))
		}

		if err := cl.client.DeleteVM(ctx, vm.ID); err != nil {
			cl.logger.Warn("failed to delete orphan VM", zap.Int("id", vm.ID), zap.Error(err))
			span.RecordError(err)
		} else if telemetry.CleanupOrphanVMs != nil {
			telemetry.CleanupOrphanVMs.Add(ctx, 1)
		}
	}
}

// cleanupOrphanZvols finds zvols under <pool>/*/omni-vms/ whose corresponding VM no longer exists.
// A zvol is considered an orphan only when its VM has been deleted (by Deprovision) but
// the zvol itself was not — indicating a partial cleanup.
func (cl *Cleaner) cleanupOrphanZvols(ctx context.Context) {
	ctx, span := cleanupTracer.Start(ctx, "cleanup.orphanZvols")
	defer span.End()

	parentPath := cl.config.Pool + "/omni-vms"

	datasets, err := cl.client.ListChildDatasets(ctx, parentPath)
	if err != nil {
		cl.logger.Warn("failed to list zvols for orphan cleanup", zap.Error(err))

		return
	}

	// Build a set of VM names for fast lookup
	vms, err := cl.client.ListVMs(ctx)
	if err != nil {
		cl.logger.Warn("failed to list VMs for orphan zvol cleanup", zap.Error(err))

		return
	}

	vmNames := make(map[string]bool, len(vms))
	for _, vm := range vms {
		vmNames[vm.Name] = true
	}

	for _, ds := range datasets {
		// Dataset ID is the full path (e.g., "default/omni-vms/talos-test-workers-abc")
		// Extract the request ID (last segment)
		parts := strings.Split(ds.ID, "/")
		requestID := parts[len(parts)-1]

		// Check if the corresponding VM still exists
		vmName := "omni_" + strings.ReplaceAll(requestID, "-", "_")
		if vmNames[vmName] {
			continue
		}

		// VM is gone but zvol still exists — orphaned from a partial deprovision
		cl.logger.Info("removing orphan zvol (VM deleted)",
			zap.String("path", ds.ID),
			zap.String("missing_vm", vmName),
		)

		if err := cl.client.DeleteDataset(ctx, ds.ID); err != nil {
			cl.logger.Warn("failed to delete orphan zvol", zap.String("path", ds.ID), zap.Error(err))
			span.RecordError(err)
		} else if telemetry.CleanupOrphanZvols != nil {
			telemetry.CleanupOrphanZvols.Add(ctx, 1)
		}
	}
}
