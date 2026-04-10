package provisioner

import (
	"context"
	"fmt"
	"time"

	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources"
)

// stepConfigureStorage creates a per-cluster NFS share on TrueNAS and injects
// a Talos config patch that deploys nfs-subdir-external-provisioner with a
// default StorageClass. This gives clusters persistent storage out of the box.
//
// The NFS share is scoped per cluster (using MachineRequestSetID), not per VM.
// Concurrent provisions for the same cluster are deduplicated via singleflight.
func (p *Provisioner) stepConfigureStorage(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) (err error) {
	stepStart := time.Now()
	ctx, span := provTracer.Start(ctx, "provision.configureStorage",
		trace.WithAttributes(attribute.String("request_id_hash", hashRequestID(pctx.GetRequestID()))),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		recordStepDuration(ctx, "configureStorage", stepStart)
		span.End()
	}()

	// Check if auto storage is disabled globally
	if !p.config.AutoStorageEnabled {
		logger.Debug("auto storage disabled globally")

		return nil
	}

	// Check per-MachineClass toggle
	var data Data
	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return fmt.Errorf(errUnmarshalProviderData, err)
	}

	data.ApplyDefaults(p.config)

	if !data.IsAutoStorageEnabled(p.config.AutoStorageEnabled) {
		logger.Debug("auto storage disabled for this MachineClass")

		return nil
	}

	// Get MachineRequestSetID for cluster scoping
	mrSetID, ok := pctx.GetMachineRequestSetID()
	if !ok {
		logger.Debug("no MachineRequestSetID — skipping auto storage (standalone machine)")

		return nil
	}

	// Determine NFS server IP
	nfsHost := p.config.NFSHost
	if nfsHost == "" {
		// Auto-detect from the primary NIC's interface
		detected, detectErr := p.client.InterfaceSubnet(ctx, data.NetworkInterface)
		if detectErr != nil || detected == "" {
			logger.Warn("could not determine NFS server IP — set NFS_HOST env var to enable auto storage",
				zap.Error(detectErr),
			)

			return nil
		}

		// InterfaceSubnet returns CIDR (e.g., "192.168.100.0/24"), we need the host IP
		hostIP, ipErr := p.client.InterfaceIP(ctx, data.NetworkInterface)
		if ipErr != nil || hostIP == "" {
			logger.Warn("could not determine NFS server IP — set NFS_HOST env var",
				zap.Error(ipErr),
			)

			return nil
		}

		nfsHost = hostIP
	}

	// Create NFS dataset + share (idempotent, deduplicated per cluster)
	basePath := data.BasePath()
	nfsDatasetPath := basePath + "/omni-nfs/" + mrSetID
	nfsMountPath := "/mnt/" + nfsDatasetPath

	_, singleErr, _ := p.nfsGroup.Do(mrSetID, func() (any, error) {
		return nil, p.ensureClusterNFSShare(ctx, logger, nfsDatasetPath, nfsMountPath, mrSetID)
	})

	if singleErr != nil {
		return fmt.Errorf("failed to configure NFS storage: %w", singleErr)
	}

	// Build and apply the config patch with inline manifests
	patchData, patchErr := buildNFSStoragePatch(nfsHost, nfsMountPath)
	if patchErr != nil {
		return fmt.Errorf("failed to build NFS storage config patch: %w", patchErr)
	}

	if cpErr := pctx.CreateConfigPatch(ctx, "nfs-storage", patchData); cpErr != nil {
		return fmt.Errorf("failed to apply NFS storage config patch: %w", cpErr)
	}

	// Store the NFS dataset path in state for cleanup tracking
	pctx.State.TypedSpec().Value.NfsDatasetPath = nfsDatasetPath

	logger.Info("configured auto NFS storage for cluster",
		zap.String("nfs_server", nfsHost),
		zap.String("nfs_path", nfsMountPath),
		zap.String("cluster", mrSetID),
	)

	return nil
}

// ensureClusterNFSShare creates the NFS dataset, ensures the NFS service is running,
// and creates the NFS share. All operations are idempotent.
func (p *Provisioner) ensureClusterNFSShare(ctx context.Context, logger *zap.Logger, datasetPath, mountPath, mrSetID string) error {
	// Ensure parent dataset exists
	parentPath := datasetPath[:len(datasetPath)-len(mrSetID)-1] // strip "/<mrSetID>"
	if err := p.client.EnsureDataset(ctx, parentPath); err != nil {
		return fmt.Errorf("failed to create NFS parent dataset %q: %w", parentPath, err)
	}

	// Create cluster dataset
	_, err := p.client.CreateDataset(ctx, client.CreateDatasetRequest{
		Name: datasetPath,
		Type: "FILESYSTEM",
		UserProperties: []client.UserProperty{
			{Key: "org.omni:managed", Value: "true"},
			{Key: "org.omni:type", Value: "nfs-share"},
			{Key: "org.omni:machine-request-set", Value: mrSetID},
		},
	})
	if err != nil && !client.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create NFS dataset %q: %w", datasetPath, err)
	}

	// Ensure NFS service is running
	if err := p.client.EnsureNFSService(ctx); err != nil {
		return fmt.Errorf("failed to ensure NFS service: %w", err)
	}

	// Create NFS share (idempotent — check first)
	existing, err := p.client.GetNFSShareByPath(ctx, mountPath)
	if err != nil {
		return fmt.Errorf("failed to check for existing NFS share: %w", err)
	}

	if existing != nil {
		logger.Debug("NFS share already exists",
			zap.String("path", mountPath),
			zap.Int("share_id", existing.ID),
		)

		return nil
	}

	share, err := p.client.CreateNFSShare(ctx, client.CreateNFSShareRequest{
		Path:    mountPath,
		Comment: "Omni cluster: " + mrSetID,
	})
	if err != nil {
		return fmt.Errorf("failed to create NFS share: %w", err)
	}

	logger.Info("created NFS share for cluster",
		zap.String("path", mountPath),
		zap.Int("share_id", share.ID),
		zap.String("cluster", mrSetID),
	)

	return nil
}
