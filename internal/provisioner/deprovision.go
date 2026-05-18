package provisioner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources/meta"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

// errVMNotFound is the sentinel returned by fetchOwnedVM when the VM does
// not exist on TrueNAS. The cleanup path treats this as "already gone" and
// returns nil — but callers must distinguish it from real errors via
// errors.Is, not via (nil, nil).
var errVMNotFound = errors.New("vm not found on TrueNAS during cleanup")

// Deprovision tears down the VM and cleans up storage.
func (p *Provisioner) Deprovision(ctx context.Context, logger *zap.Logger, machine *resources.Machine, req *infra.MachineRequest) (err error) {
	ctx, span := provTracer.Start(ctx, "deprovision",
		trace.WithAttributes(attribute.Int("vm_id", int(machine.TypedSpec().Value.VmId))),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			if telemetry.VMsErrored != nil {
				telemetry.VMsErrored.Add(ctx, 1)
			}
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}()

	start := time.Now()
	state := machine.TypedSpec().Value

	// Ownership-traced request-id for zvol checks. May be empty on very old
	// MachineRequests; in that case we fall back to the managed=true tag only.
	var requestID string
	if req != nil {
		requestID = req.Metadata().ID()
	}

	if err := p.cleanupVM(ctx, logger, int(state.VmId)); err != nil {
		return err
	}

	// Cleanup must survive ctx cancellation but keep trace IDs from parent.
	cleanupCtx := context.WithoutCancel(ctx)

	// Clean up additional data disks first (order doesn't matter, but root disk last)
	for _, zvolPath := range state.AdditionalZvolPaths {
		if err := p.cleanupZvol(cleanupCtx, logger, zvolPath, requestID); err != nil {
			return err
		}
	}

	if err := p.cleanupZvol(cleanupCtx, logger, state.ZvolPath, requestID); err != nil {
		return err
	}

	if telemetry.VMsDeprovisioned != nil {
		telemetry.VMsDeprovisioned.Add(cleanupCtx, 1)
	}
	if telemetry.DeprovisionDuration != nil {
		telemetry.DeprovisionDuration.Record(cleanupCtx, time.Since(start).Seconds())
	}

	// Clear both circuit-breaker counters now that the VM is gone. Without
	// this, oomCounts[vmName] and errorCounts[vmID] accumulate forever for
	// every failed/permanent-failed/deprovisioned MachineRequest — a slow
	// memory leak in long-running providers AND a correctness bug if the
	// same MachineRequest is recreated (it would inherit a stale counter
	// and may immediately permanent-fail without retry budget).
	if requestID != "" {
		p.clearOOMAttempts(meta.BuildVMName(meta.ProviderID, requestID))
	}
	p.clearVMErrors(int(state.VmId))
	p.UntrackVMName(meta.BuildVMName(meta.ProviderID, requestID))

	logger.Info("deprovision complete",
		zap.Int("vm_id", int(state.VmId)),
		zap.String("zvol_path", state.ZvolPath),
	)

	return nil
}

func (p *Provisioner) cleanupVM(ctx context.Context, logger *zap.Logger, vmID int) error {
	stepStart := time.Now()
	defer func() {
		if telemetry.DeprovisionStepDuration != nil {
			telemetry.DeprovisionStepDuration.Record(ctx, time.Since(stepStart).Seconds(), telemetry.WithStep("cleanupVM"))
		}
	}()

	if vmID == 0 {
		return nil
	}

	if _, err := p.fetchOwnedVM(ctx, logger, vmID); err != nil {
		if errors.Is(err, errVMNotFound) {
			return nil
		}
		return err
	}

	stopped := p.gracefullyStopVM(ctx, logger, vmID)

	gracefulAttrs := metric.WithAttributes(attribute.String("provider_id", meta.ProviderID))
	if stopped {
		if telemetry.GracefulShutdownSuccess != nil {
			telemetry.GracefulShutdownSuccess.Add(ctx, 1, gracefulAttrs)
		}
	} else if telemetry.GracefulShutdownTimeout != nil {
		telemetry.GracefulShutdownTimeout.Add(ctx, 1, gracefulAttrs)
	}

	// Cleanup must survive ctx cancellation but keep trace IDs from parent.
	cleanupCtx := context.WithoutCancel(ctx)

	if !stopped {
		logger.Debug("graceful shutdown incomplete, force stopping", zap.Int("vm_id", vmID))

		if err := p.client.StopVM(cleanupCtx, vmID, true); err != nil && !isNotFound(err) {
			logger.Debug("force stop failed", zap.Int("vm_id", vmID), zap.Error(err))
		}
	}

	logger.Debug("deleting VM", zap.Int("vm_id", vmID))

	if err := p.client.DeleteVM(cleanupCtx, vmID); err != nil && !isNotFound(err) {
		return fmt.Errorf("failed to delete VM %d: %w", vmID, err)
	}

	return nil
}

// fetchOwnedVM verifies the VM exists and is Omni-managed. Returns
// errVMNotFound when the VM is already gone (caller treats as success);
// returns an ownership-mismatch error for name collision / state corruption.
// Callers MUST distinguish via errors.Is(err, errVMNotFound) — refusing to
// touch a VM we didn't create is the only safe response to a collision.
func (p *Provisioner) fetchOwnedVM(ctx context.Context, logger *zap.Logger, vmID int) (*client.VM, error) {
	vm, err := p.client.GetVM(ctx, vmID)
	if err != nil {
		if isNotFound(err) {
			logger.Debug("VM not found during cleanup — nothing to do", zap.Int("vm_id", vmID))
			return nil, errVMNotFound
		}

		return nil, fmt.Errorf("failed to read VM %d for ownership check: %w", vmID, err)
	}

	if !isOmniManagedVM(vm) {
		return nil, fmt.Errorf("refusing to deprovision VM %d (%q): description %q does not match Omni management marker — a name collision or state corruption has mixed up ownership, investigate on TrueNAS before retrying",
			vmID, vm.Name, vm.Description)
	}

	return vm, nil
}

// gracefullyStopVM issues an ACPI stop and waits up to the configured timeout
// for the VM to enter STOPPED. Returns true on graceful exit, false on
// timeout / ctx cancel — the caller is responsible for force-stop on false.
func (p *Provisioner) gracefullyStopVM(ctx context.Context, logger *zap.Logger, vmID int) bool {
	logger.Debug("requesting graceful VM shutdown", zap.Int("vm_id", vmID))

	if err := p.client.StopVM(ctx, vmID, false); err != nil && !isNotFound(err) {
		// ACPI signal may fail if VM is already stopped or has no guest agent — that's fine
		logger.Debug("graceful shutdown signal failed, will force stop", zap.Int("vm_id", vmID), zap.Error(err))
	}

	// GracefulShutdownTimeout < 0 means force-stop immediately (skip graceful).
	gracefulTimeout := p.config.GracefulShutdownTimeout
	if gracefulTimeout < 0 {
		gracefulTimeout = 0
	}
	if gracefulTimeout == 0 {
		gracefulTimeout = 30 * time.Second
	}

	return p.waitForGracefulStop(ctx, logger, vmID, gracefulTimeout)
}

// waitForGracefulStop polls the VM state until it's STOPPED or the timeout/context expires.
// Returns true if the VM stopped gracefully, false if timeout or context cancelled.
func (p *Provisioner) waitForGracefulStop(ctx context.Context, logger *zap.Logger, vmID int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

loop:
	for time.Now().Before(deadline) {
		vm, getErr := p.client.GetVM(ctx, vmID)
		if getErr != nil {
			if isNotFound(getErr) {
				logger.Debug("VM already gone during graceful wait", zap.Int("vm_id", vmID))

				return true
			}

			break // Can't check state
		}

		if vm.Status.State == "STOPPED" {
			logger.Debug("VM stopped gracefully", zap.Int("vm_id", vmID))

			return true
		}

		pollInterval := p.config.PollInterval
		if pollInterval == 0 {
			pollInterval = 2 * time.Second
		}

		select {
		case <-time.After(pollInterval):
		case <-ctx.Done():
			logger.Debug("context cancelled during graceful shutdown", zap.Int("vm_id", vmID))

			break loop
		}
	}

	return false
}

func (p *Provisioner) cleanupZvol(ctx context.Context, logger *zap.Logger, zvolPath, requestID string) error {
	stepStart := time.Now()
	defer func() {
		if telemetry.DeprovisionStepDuration != nil {
			telemetry.DeprovisionStepDuration.Record(ctx, time.Since(stepStart).Seconds(), telemetry.WithStep("cleanupZvol"))
		}
	}()

	if zvolPath == "" {
		return nil
	}

	// Ownership check: the zvol's ZFS user properties must match our
	// management tags. Without this, a corrupted/colliding ZvolPath in the
	// Machine state could cause us to destroy unrelated data.
	if err := verifyZvolOwnership(ctx, p.client, zvolPath, requestID); err != nil {
		// Special-case: if the dataset doesn't exist anymore, treat as success
		// (already deleted). Only ownership-mismatch on an existing dataset is
		// a refusal.
		exists, existsErr := p.client.DatasetExists(ctx, zvolPath)
		if existsErr == nil && !exists {
			logger.Debug("zvol already gone during cleanup — nothing to do", zap.String("path", zvolPath))
			return nil
		}

		return fmt.Errorf("refusing to delete zvol %q: %w", zvolPath, err)
	}

	logger.Debug("deleting zvol", zap.String("path", zvolPath))

	if err := p.client.DeleteDataset(ctx, zvolPath); err != nil && !isNotFound(err) {
		return fmt.Errorf("failed to delete zvol %q: %w", zvolPath, err)
	}

	return nil
}
