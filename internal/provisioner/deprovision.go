package provisioner

import (
	"context"
	"fmt"
	"time"

	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

// Deprovision tears down the VM and cleans up storage.
func (p *Provisioner) Deprovision(ctx context.Context, logger *zap.Logger, machine *resources.Machine, _ *infra.MachineRequest) (err error) {
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
		}
		span.End()
	}()

	start := time.Now()
	state := machine.TypedSpec().Value

	if err := p.cleanupVM(ctx, logger, int(state.VmId)); err != nil {
		return err
	}

	if err := p.cleanupZvol(ctx, logger, state.ZvolPath); err != nil {
		return err
	}

	if telemetry.VMsDeprovisioned != nil {
		telemetry.VMsDeprovisioned.Add(ctx, 1)
	}
	if telemetry.DeprovisionDuration != nil {
		telemetry.DeprovisionDuration.Record(ctx, time.Since(start).Seconds())
	}

	logger.Info("deprovision complete",
		zap.Int("vm_id", int(state.VmId)),
		zap.String("zvol_path", state.ZvolPath),
	)

	return nil
}

func (p *Provisioner) cleanupVM(ctx context.Context, logger *zap.Logger, vmID int) error {
	if vmID == 0 {
		return nil
	}

	logger.Info("stopping VM", zap.Int("vm_id", vmID))

	if err := p.client.StopVM(ctx, vmID, true); err != nil && !isNotFound(err) {
		return fmt.Errorf("failed to stop VM %d: %w", vmID, err)
	}

	logger.Info("deleting VM", zap.Int("vm_id", vmID))

	if err := p.client.DeleteVM(ctx, vmID); err != nil && !isNotFound(err) {
		return fmt.Errorf("failed to delete VM %d: %w", vmID, err)
	}

	return nil
}

func (p *Provisioner) cleanupZvol(ctx context.Context, logger *zap.Logger, zvolPath string) error {
	if zvolPath == "" {
		return nil
	}

	logger.Info("deleting zvol", zap.String("path", zvolPath))

	if err := p.client.DeleteDataset(ctx, zvolPath); err != nil && !isNotFound(err) {
		return fmt.Errorf("failed to delete zvol %q: %w", zvolPath, err)
	}

	return nil
}
