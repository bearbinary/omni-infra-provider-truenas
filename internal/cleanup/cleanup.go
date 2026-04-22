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
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources/meta"
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

	// Query managed zvols once for both orphan VM and orphan zvol cleanup.
	// This avoids duplicate pool.dataset.query calls with retrieve_user_props.
	managedZvols, err := cl.client.ListManagedZvols(ctx)
	if err != nil {
		cl.logger.Warn("failed to list managed zvols — skipping orphan cleanup", zap.Error(err))
	} else {
		cl.cleanupOrphanVMs(ctx, managedZvols)
		cl.cleanupOrphanZvols(ctx, managedZvols)
	}

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
func (cl *Cleaner) cleanupOrphanVMs(ctx context.Context, managedZvols []client.ManagedZvol) {
	ctx, span := cleanupTracer.Start(ctx, "cleanup.orphanVMs")
	defer span.End()

	vms, err := cl.client.ListVMs(ctx)
	if err != nil {
		cl.logger.Warn("failed to list VMs for orphan cleanup", zap.Error(err))

		return
	}

	// Build a set of request IDs from managed zvols for fast lookup
	managedRequestIDs := make(map[string]bool, len(managedZvols))
	for _, z := range managedZvols {
		managedRequestIDs[z.RequestID] = true
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

// cleanupOrphanZvols finds Omni-managed zvols whose corresponding VM no longer exists.
// Uses org.omni:managed and org.omni:request-id user properties to find managed zvols
// across all dataset paths (handles dataset_prefix variations).
// A zvol is considered an orphan only when its VM has been deleted (by Deprovision) but
// the zvol itself was not — indicating a partial cleanup.
func (cl *Cleaner) cleanupOrphanZvols(ctx context.Context, managedZvols []client.ManagedZvol) {
	ctx, span := cleanupTracer.Start(ctx, "cleanup.orphanZvols")
	defer span.End()

	if len(managedZvols) == 0 {
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

	for _, zvol := range managedZvols {
		// Check if the corresponding VM still exists. Try both the v0.15+
		// namespaced name and the legacy v0.14 shape so orphan detection keeps
		// working across an upgrade. The zvol ownership tag is the
		// authoritative answer; matching either name just tells us "VM still
		// exists under either naming convention".
		newVMName := meta.BuildVMName(meta.ProviderID, zvol.RequestID)
		legacyVMName := "omni_" + strings.ReplaceAll(zvol.RequestID, "-", "_")

		if vmNames[newVMName] || vmNames[legacyVMName] {
			continue
		}

		vmName := newVMName

		// VM is gone but zvol still exists — orphaned from a partial deprovision
		cl.logger.Info("removing orphan zvol (VM deleted)",
			zap.String("path", zvol.Path),
			zap.String("request_id", zvol.RequestID),
			zap.String("missing_vm", vmName),
		)

		if err := cl.client.DeleteDataset(ctx, zvol.Path); err != nil {
			cl.logger.Warn("failed to delete orphan zvol", zap.String("path", zvol.Path), zap.Error(err))
			span.RecordError(err)
		} else if telemetry.CleanupOrphanZvols != nil {
			telemetry.CleanupOrphanZvols.Add(ctx, 1)
		}
	}
}
