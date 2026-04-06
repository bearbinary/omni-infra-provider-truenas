package provisioner

import (
	"context"
	"fmt"

	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources"
)

// Deprovision tears down the VM and cleans up storage.
func (p *Provisioner) Deprovision(ctx context.Context, logger *zap.Logger, machine *resources.Machine, _ *infra.MachineRequest) error {
	state := machine.TypedSpec().Value

	vmID := int(state.VmId)
	zvolPath := state.ZvolPath

	// Stop and delete VM
	if vmID != 0 {
		logger.Info("stopping VM", zap.Int("vm_id", vmID))

		if err := p.client.StopVM(ctx, vmID, true); err != nil {
			if !isNotFound(err) {
				return fmt.Errorf("failed to stop VM %d: %w", vmID, err)
			}

			logger.Warn("VM already gone during stop", zap.Int("vm_id", vmID))
		}

		logger.Info("deleting VM", zap.Int("vm_id", vmID))

		if err := p.client.DeleteVM(ctx, vmID); err != nil {
			if !isNotFound(err) {
				return fmt.Errorf("failed to delete VM %d: %w", vmID, err)
			}

			logger.Warn("VM already gone during delete", zap.Int("vm_id", vmID))
		}
	}

	// Delete zvol
	if zvolPath != "" {
		logger.Info("deleting zvol", zap.String("path", zvolPath))

		if err := p.client.DeleteDataset(ctx, zvolPath); err != nil {
			if !isNotFound(err) {
				return fmt.Errorf("failed to delete zvol %q: %w", zvolPath, err)
			}

			logger.Warn("zvol already gone", zap.String("path", zvolPath))
		}
	}

	logger.Info("deprovision complete",
		zap.Int("vm_id", vmID),
		zap.String("zvol_path", zvolPath),
	)

	return nil
}
