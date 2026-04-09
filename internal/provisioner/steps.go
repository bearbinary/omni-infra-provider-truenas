package provisioner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/siderolabs/omni/client/pkg/constants"
	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/api/specs"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

var provTracer = otel.Tracer("truenas-provisioner")

const errUnmarshalProviderData = "failed to unmarshal provider data: %w"

// hashRequestID returns a truncated SHA-256 hash of the request ID for use in
// trace attributes. This avoids exposing raw request IDs (which map to VM names,
// zvol paths, and SideroLink tokens) in OTEL telemetry data.
// generateUUID returns a new random UUID v4 string (e.g. "550e8400-e29b-41d4-a716-446655440000").
func generateUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func hashRequestID(requestID string) string {
	h := sha256.Sum256([]byte(requestID))
	return hex.EncodeToString(h[:8]) // 16 hex chars — enough for correlation, not reversible
}

// passphraseProperty is the ZFS user property where auto-generated encryption passphrases are stored.
const passphraseProperty = "org.omni:passphrase"

// generatePassphrase creates a cryptographically random 32-byte passphrase encoded as hex.
func generatePassphrase() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random passphrase: %w", err)
	}

	return hex.EncodeToString(b), nil
}

// Default extensions included in every TrueNAS VM.
var defaultExtensions = []string{
	"siderolabs/qemu-guest-agent",
	"siderolabs/nfs-utils",
	"siderolabs/util-linux-tools",
}

// stepCreateSchematic generates a Talos image factory schematic ID.
func (p *Provisioner) stepCreateSchematic(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) (err error) {
	stepStart := time.Now()
	ctx, span := provTracer.Start(ctx, "provision.createSchematic",
		trace.WithAttributes(attribute.String("request_id_hash", hashRequestID(pctx.GetRequestID()))),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			recordProvisionError(ctx, err)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		recordStepDuration(ctx, "createSchematic", stepStart)
		span.End()
	}()
	// Connection params include SideroLink endpoint and join token with encoded request ID.
	// We use WithoutConnectionParams() to skip the SDK's built-in embedding (which conflicts
	// with WithEncodeRequestIDsIntoTokens), then pass them ourselves via WithExtraKernelArgs.
	extraArgs := append([]string{"console=ttyS0"}, pctx.ConnectionParams.KernelArgs...)

	// Merge default extensions with any extras from MachineClass config
	var data Data
	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return fmt.Errorf(errUnmarshalProviderData, err)
	}

	extensions := make([]string, 0, len(defaultExtensions)+len(data.Extensions))
	extensions = append(extensions, defaultExtensions...)
	extensions = append(extensions, data.Extensions...)

	schematic, err := pctx.GenerateSchematicID(ctx, logger,
		provision.WithExtraKernelArgs(extraArgs...),
		provision.WithExtraExtensions(extensions...),
		provision.WithoutConnectionParams(),
	)
	if err != nil {
		return fmt.Errorf("failed to generate schematic: %w", err)
	}

	state := pctx.State.TypedSpec().Value

	// Detect Talos version upgrade
	isUpgrade := state.ZvolPath != "" && state.TalosVersion != "" && state.TalosVersion != pctx.GetTalosVersion()
	if isUpgrade {
		logger.Info("Talos version upgrade detected",
			zap.String("from", state.TalosVersion),
			zap.String("to", pctx.GetTalosVersion()),
		)

		// Auto-snapshot the zvol before upgrade
		snapID := p.snapshotBeforeUpgrade(ctx, logger, state.ZvolPath, state.TalosVersion, pctx.GetTalosVersion())
		state.LastUpgradeSnapshot = snapID

		// Swap the CDROM to the new ISO (if still attached)
		if state.VmId != 0 && state.CdromDeviceId != 0 {
			p.swapCDROMForUpgrade(ctx, logger, state, pctx)
		}
	}

	state.Schematic = schematic

	logger.Debug("created schematic", zap.String("schematic_id", schematic))

	return nil
}

// stepUploadISO downloads the Talos ISO and uploads it to TrueNAS.
func (p *Provisioner) stepUploadISO(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) (err error) {
	stepStart := time.Now()
	ctx, span := provTracer.Start(ctx, "provision.uploadISO",
		trace.WithAttributes(attribute.String("request_id_hash", hashRequestID(pctx.GetRequestID()))),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			recordProvisionError(ctx, err)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		recordStepDuration(ctx, "uploadISO", stepStart)
		span.End()
	}()
	pctx.State.TypedSpec().Value.TalosVersion = pctx.GetTalosVersion()

	var data Data
	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return fmt.Errorf(errUnmarshalProviderData, err)
	}

	data.ApplyDefaults(p.config)

	// Validate pool before any operations
	if err := p.validatePool(ctx, data.Pool); err != nil {
		return err
	}

	arch := data.Architecture

	imageURL, err := url.Parse(constants.ImageFactoryBaseURL)
	if err != nil {
		return fmt.Errorf("failed to parse image factory URL: %w", err)
	}

	imageURL = imageURL.JoinPath("image",
		pctx.State.TypedSpec().Value.Schematic,
		pctx.GetTalosVersion(),
		fmt.Sprintf("nocloud-%s.iso", arch),
	)

	// SHA-256 hash of URL for deduplication
	hash := sha256.Sum256([]byte(imageURL.String()))
	imageID := hex.EncodeToString(hash[:])
	isoFileName := imageID + ".iso"

	pctx.State.TypedSpec().Value.ImageId = imageID
	p.TrackImageID(imageID)

	// ISOs are cached under <basePath>/talos-iso/, downloaded automatically from Image Factory
	isoDataset := data.BasePath() + "/talos-iso"
	isoPath := "/mnt/" + isoDataset + "/" + isoFileName

	// Use singleflight to prevent concurrent downloads of the same ISO
	_, err, _ = p.isoGroup.Do(imageID, func() (any, error) {
		// Check if ISO already exists
		exists, err := p.client.FileExists(ctx, isoPath)
		if err != nil {
			return nil, fmt.Errorf("failed to check ISO existence: %w", err)
		}

		if exists {
			logger.Debug("ISO already exists, skipping download", zap.String("path", isoPath))
			if telemetry.ISOCacheHits != nil {
				telemetry.ISOCacheHits.Add(ctx, 1)
			}

			return nil, nil
		}

		// Ensure the dataset hierarchy exists
		if data.DatasetPrefix != "" {
			if err := p.client.EnsureDataset(ctx, data.BasePath()); err != nil {
				return nil, fmt.Errorf("failed to ensure dataset prefix: %w", err)
			}
		}

		if err := p.client.EnsureDataset(ctx, isoDataset); err != nil {
			return nil, fmt.Errorf("failed to ensure ISO dataset: %w", err)
		}

		if telemetry.ISOCacheMisses != nil {
			telemetry.ISOCacheMisses.Add(ctx, 1)
		}

		isoStart := time.Now()

		logger.Info("downloading Talos ISO",
			zap.String("url", imageURL.String()),
			zap.String("dest", isoPath),
		)

		// Download ISO from image factory
		isoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL.String(), nil) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("failed to create ISO download request: %w", err)
		}

		isoClient := &http.Client{Timeout: 10 * time.Minute}

		resp, err := isoClient.Do(isoReq)
		if err != nil {
			return nil, fmt.Errorf("failed to download ISO: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("ISO download returned status %d", resp.StatusCode)
		}

		// Upload to TrueNAS
		if err := p.client.UploadFile(ctx, isoPath, resp.Body, resp.ContentLength); err != nil {
			return nil, fmt.Errorf("failed to upload ISO to TrueNAS: %w", err)
		}

		if telemetry.ISODownloadDuration != nil {
			telemetry.ISODownloadDuration.Record(ctx, time.Since(isoStart).Seconds())
		}

		logger.Info("ISO uploaded successfully", zap.String("path", isoPath))

		return nil, nil
	})

	return err
}

// stepCreateVM creates the VM on TrueNAS with disk, CDROM, and NIC devices.
func (p *Provisioner) stepCreateVM(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) (err error) {
	stepStart := time.Now()
	ctx, span := provTracer.Start(ctx, "provision.createVM",
		trace.WithAttributes(attribute.String("request_id_hash", hashRequestID(pctx.GetRequestID()))),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			if telemetry.VMsErrored != nil {
				telemetry.VMsErrored.Add(ctx, 1)
			}
			recordProvisionError(ctx, err)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		recordStepDuration(ctx, "createVM", stepStart)
		span.End()
	}()
	state := pctx.State.TypedSpec().Value
	vmName := "omni_" + strings.ReplaceAll(pctx.GetRequestID(), "-", "_")

	// Check if VM already exists (by ID or name) — handles restarts and idempotency
	if result := p.checkExistingVM(ctx, logger, state, vmName); result != nil {
		return *result
	}

	var data Data
	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return fmt.Errorf(errUnmarshalProviderData, err)
	}

	// Check for unrecognized fields in MachineClass config
	var rawData map[string]any
	if err := pctx.UnmarshalProviderData(&rawData); err == nil {
		if unknown := UnknownFields(rawData); len(unknown) > 0 {
			logger.Warn("MachineClass config contains unrecognized fields — these will be ignored",
				zap.Strings("unknown_fields", unknown),
				zap.String("hint", "check field names against the provider schema"),
			)
		}
	}

	data.ApplyDefaults(p.config)

	// Validate all user-provided names before using them in paths or API calls
	if err := data.Validate(); err != nil {
		return fmt.Errorf("invalid MachineClass config: %w", err)
	}

	// Pre-check: verify pool has enough free space for the zvol
	requiredBytes := int64(data.DiskSize) * 1024 * 1024 * 1024
	freeBytes, err := p.client.PoolFreeSpace(ctx, data.Pool)

	if err == nil {
		logger.Debug("pool space check",
			zap.String("pool", data.Pool),
			zap.Int64("free_gib", freeBytes/(1024*1024*1024)),
			zap.Int("required_gib", data.DiskSize),
		)
	}

	if err == nil && freeBytes < requiredBytes {
		return fmt.Errorf("pool %q has %d GiB free but VM needs %d GiB — free up space or use a different pool",
			data.Pool, freeBytes/(1024*1024*1024), data.DiskSize)
	}

	// Pre-check: verify VM memory doesn't exceed safe threshold of total host RAM.
	// This checks against total physmem (not free RAM) because TrueNAS dynamically
	// manages ZFS ARC. A single VM requesting >80% of total RAM would starve ZFS.
	hostMem, memErr := p.client.SystemMemoryAvailable(ctx)
	if memErr == nil {
		requestedMiB := int64(data.Memory)
		hostMiB := hostMem / (1024 * 1024)

		logger.Debug("memory check",
			zap.Int64("host_mib", hostMiB),
			zap.Int64("requested_mib", requestedMiB),
			zap.Int64("threshold_mib", hostMiB*80/100),
		)

		if requestedMiB > hostMiB*80/100 {
			return fmt.Errorf("host has %d MiB total memory but VM requests %d MiB — "+
				"a single VM should not exceed 80%% of host RAM (TrueNAS needs the rest for ZFS ARC). "+
				"Reduce VM memory or add more host RAM", hostMiB, requestedMiB)
		}
	}

	// Create zvol for the VM disk
	requestID := pctx.GetRequestID()
	basePath := data.BasePath()
	zvolPath := basePath + "/omni-vms/" + requestID

	// Tag all provider-managed zvols with Omni metadata
	omniProps := client.OmniManagedProperties(requestID)

	// Ensure parent dataset hierarchy exists
	if data.DatasetPrefix != "" {
		if err := p.client.EnsureDataset(ctx, basePath); err != nil {
			return fmt.Errorf("failed to ensure dataset prefix %q: %w", basePath, err)
		}
	}

	if err := p.client.EnsureDataset(ctx, basePath+"/omni-vms"); err != nil {
		return fmt.Errorf("failed to ensure omni-vms dataset: %w", err)
	}

	if data.Encrypted {
		// Auto-generate a unique passphrase per zvol. The passphrase is stored as a
		// ZFS user property (org.omni:passphrase) so the provider can retrieve it
		// for unlock after TrueNAS reboots. This protects data at rest against
		// physical disk theft while keeping key management invisible to the user.
		passphrase, genErr := generatePassphrase()
		if genErr != nil {
			return genErr
		}

		omniProps = append(omniProps, client.UserProperty{Key: passphraseProperty, Value: passphrase})

		if _, err := p.client.CreateEncryptedZvol(ctx, zvolPath, data.DiskSize, passphrase, omniProps); err != nil {
			if !isAlreadyExists(err) {
				return fmt.Errorf("failed to create encrypted zvol: %w", err)
			}

			// Encrypted zvol already exists — retrieve stored passphrase for unlock
			stored, propErr := p.client.GetDatasetUserProperty(ctx, zvolPath, passphraseProperty)
			if propErr != nil {
				return fmt.Errorf("failed to read stored passphrase from zvol %q: %w", zvolPath, propErr)
			}

			if stored == "" {
				return fmt.Errorf("encrypted zvol %q exists but has no stored passphrase — the zvol may have been created manually or by an older provider version", zvolPath)
			}

			passphrase = stored

			if locked, lockErr := p.client.IsDatasetLocked(ctx, zvolPath); lockErr == nil && locked {
				logger.Debug("unlocking encrypted zvol", zap.String("path", zvolPath))

				if unlockErr := p.client.UnlockDataset(ctx, zvolPath, passphrase); unlockErr != nil {
					return fmt.Errorf("failed to unlock encrypted zvol %q: %w", zvolPath, unlockErr)
				}
			}

			if resizeErr := p.maybeResizeZvol(ctx, logger, zvolPath, data.DiskSize); resizeErr != nil {
				return resizeErr
			}
		}

		logger.Info("created encrypted zvol", zap.String("path", zvolPath))
	} else {
		if _, err := p.client.CreateZvol(ctx, zvolPath, data.DiskSize, omniProps); err != nil {
			if !isAlreadyExists(err) {
				return fmt.Errorf("failed to create zvol: %w", err)
			}

			if resizeErr := p.maybeResizeZvol(ctx, logger, zvolPath, data.DiskSize); resizeErr != nil {
				return resizeErr
			}
		}
	}

	state.ZvolPath = zvolPath

	// Generate a stable UUID for the VM's SMBIOS identity.
	// This UUID is set on the bhyve VM so that when Talos boots, it reads
	// the same UUID via DMI and uses it to register with Omni — ensuring
	// the provisioned record and the joined machine are correlated.
	machineUUID, err := generateUUID()
	if err != nil {
		return fmt.Errorf("failed to generate machine UUID: %w", err)
	}

	// Create the VM
	vm, err := p.client.CreateVM(ctx, client.CreateVMRequest{
		Name:        vmName,
		Description: "Managed by Omni infra provider",
		UUID:        machineUUID,
		VCPUs:       data.CPUs,
		Memory:      data.Memory,
		CPUMode:     "HOST-PASSTHROUGH",
		Bootloader:  data.BootMethod,
		Autostart:   true,
	})
	if err != nil {
		return fmt.Errorf("failed to create VM: %w", err)
	}

	state.VmId = int32(vm.ID)
	state.Uuid = machineUUID

	// Set machine identifiers for Omni correlation
	vmIDStr := fmt.Sprintf("%d", vm.ID)
	pctx.SetMachineInfraID(vmIDStr)
	pctx.SetMachineUUID(machineUUID)

	logger.Info("created VM", zap.String("name", vmName), zap.Int("id", vm.ID))
	p.TrackVMName(vmName)

	// Attach CDROM with Talos ISO (cached under <basePath>/talos-iso/)
	isoPath := "/mnt/" + basePath + "/talos-iso/" + state.ImageId + ".iso"

	cdrom, err := p.client.AddCDROM(ctx, vm.ID, isoPath)
	if err != nil {
		return fmt.Errorf("failed to attach CDROM: %w", err)
	}

	state.CdromDeviceId = int32(cdrom.ID)

	// Attach disk
	if _, err := p.client.AddDisk(ctx, vm.ID, zvolPath); err != nil {
		return fmt.Errorf("failed to attach disk: %w", err)
	}

	// Attach primary NIC
	nicDev, err := p.client.AddNIC(ctx, vm.ID, data.NetworkInterface)
	if err != nil {
		return fmt.Errorf("failed to attach primary NIC: %w", err)
	}

	// Log MAC address so users can create DHCP reservations in their router
	if mac, ok := nicDev.Attributes["mac"].(string); ok && mac != "" {
		logger.Info("VM NIC MAC address — use this for DHCP reservation in your router",
			zap.String("mac", mac),
			zap.String("vm_name", vmName),
			zap.String("network_interface", data.NetworkInterface),
			zap.String("role", "primary"),
		)
	}

	// Attach additional NICs
	for i, nic := range data.AdditionalNICs {
		nicCfg := client.NICConfig{
			NetworkInterface:    nic.NetworkInterface,
			Type:                nic.Type,
			VLANTag:             nic.VLANTag,
			TrustGuestRxFilters: nic.VLANTag > 0, // Enable for VLAN tagging at VM level
		}

		dev, nicErr := p.client.AddNICWithConfig(ctx, vm.ID, nicCfg, 1004+i)
		if nicErr != nil {
			return fmt.Errorf("failed to attach additional NIC %d (%s): %w", i, nic.NetworkInterface, nicErr)
		}

		mac := ""
		if m, ok := dev.Attributes["mac"].(string); ok {
			mac = m
		}

		logger.Debug("attached additional NIC",
			zap.Int("index", i),
			zap.String("network_interface", nic.NetworkInterface),
			zap.Int("vlan_id", nic.VLANTag),
			zap.String("mac", mac),
			zap.String("vm_name", vmName),
		)
	}

	// Apply advertised_subnets config patch if set
	if data.AdvertisedSubnets != "" {
		patchData, patchErr := buildAdvertisedSubnetsPatch(data.AdvertisedSubnets)
		if patchErr != nil {
			return fmt.Errorf("failed to build advertised_subnets config patch: %w", patchErr)
		}

		if patchData != nil {
			if cpErr := pctx.CreateConfigPatch(ctx, "advertised-subnets", patchData); cpErr != nil {
				return fmt.Errorf("failed to apply advertised_subnets config patch: %w", cpErr)
			}

			logger.Info("applied advertised_subnets config patch",
				zap.String("subnets", data.AdvertisedSubnets),
				zap.String("vm_name", vmName),
			)
		}
	} else if len(data.AdditionalNICs) > 0 {
		// Warn if multiple NICs without advertised_subnets
		logger.Warn("multiple NICs configured without advertised_subnets — etcd/kubelet may pick the wrong interface. "+
			"Set advertised_subnets to pin cluster traffic to the primary NIC's subnet.",
			zap.String("vm_name", vmName),
			zap.Int("total_nics", 1+len(data.AdditionalNICs)),
		)
	}

	// Start the VM
	if err := p.client.StartVM(ctx, vm.ID); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	logger.Info("VM started, waiting for RUNNING state",
		zap.String("name", vmName),
		zap.Int("id", vm.ID),
	)

	return provision.NewRetryInterval(15 * time.Second)
}

// stepHealthCheck runs on every reconcile after the VM is created.
// Verifies the VM still exists on TrueNAS — if it was deleted externally
// (manual deletion, TrueNAS restart, etc.), resets state so Omni can re-provision.
// The CDROM is intentionally left attached — Talos boots from disk by default
// once installed, and removing the CDROM requires stopping the VM which kills
// Talos before it finishes installing. The CDROM is cleaned up on deprovision.
func (p *Provisioner) stepHealthCheck(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) (err error) {
	stepStart := time.Now()
	ctx, span := provTracer.Start(ctx, "provision.healthCheck",
		trace.WithAttributes(attribute.String("request_id_hash", hashRequestID(pctx.GetRequestID()))),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			recordProvisionError(ctx, err)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		recordStepDuration(ctx, "healthCheck", stepStart)
		span.End()
	}()

	state := pctx.State.TypedSpec().Value

	// Verify the VM still exists on TrueNAS. If it was deleted externally
	// (manual deletion, TrueNAS restart, etc.), reset state so Omni can re-provision.
	if state.VmId != 0 {
		if err := p.verifyVMExists(ctx, logger, state); err != nil {
			return err
		}

		// verifyVMExists may have reset VmId if VM is gone
		if state.VmId == 0 {
			return provision.NewRetryInterval(5 * time.Second)
		}
	}

	// The CDROM is intentionally left attached. Talos boots from disk by default
	// once installed (disk has higher boot priority). Removing the CDROM requires
	// stopping the VM, which kills Talos before it has finished installing to disk.
	// The CDROM stays attached but unused — it will be cleaned up on deprovision.
	//
	// If the CDROM was already removed (by an older provider version), that's fine.
	logger.Debug("VM provisioned and healthy",
		zap.Int32("vm_id", state.VmId),
	)

	return nil
}

// verifyVMExists checks that a provisioned VM still exists on TrueNAS.
// If the VM was deleted externally (manual deletion, TrueNAS restart, cleanup),
// resets the machine state so the SDK can re-provision or teardown cleanly.
// This prevents the "stuck in limbo" state where Omni thinks the VM exists
// but TrueNAS has already deleted it.
func (p *Provisioner) verifyVMExists(ctx context.Context, logger *zap.Logger, state *specs.MachineSpec) error {
	_, err := p.client.GetVM(ctx, int(state.VmId))
	if err == nil {
		return nil // VM exists, all good
	}

	if !isNotFound(err) {
		// Transient error — don't reset state, just retry
		return fmt.Errorf("failed to verify VM %d exists: %w", state.VmId, err)
	}

	// VM is gone from TrueNAS — reset state so provisioning restarts from scratch
	logger.Warn("VM no longer exists on TrueNAS — resetting state for re-provision",
		zap.Int32("vm_id", state.VmId),
		zap.String("zvol_path", state.ZvolPath),
	)

	state.VmId = 0
	state.CdromDeviceId = 0
	// Keep ZvolPath — the zvol may still exist even if the VM was deleted.
	// stepCreateVM will handle the "already exists" case on the zvol.

	return nil
}

// recordStepDuration records the duration of a provision step.
func recordStepDuration(ctx context.Context, step string, start time.Time) {
	if telemetry.StepDuration != nil {
		telemetry.StepDuration.Record(ctx, time.Since(start).Seconds(), telemetry.WithStep(step))
	}
}

// recordProvisionError categorizes and records a provision error.
func recordProvisionError(ctx context.Context, err error) {
	if telemetry.ProvisionErrors == nil || err == nil {
		return
	}

	telemetry.ProvisionErrors.Add(ctx, 1, telemetry.WithErrorCategory(categorizeError(err)))
}

// categorizeError returns a category string for a provision error.
func categorizeError(err error) string {
	if err == nil {
		return "unknown"
	}

	errMsg := err.Error()
	switch {
	case strings.Contains(errMsg, "pool") && strings.Contains(errMsg, "not found"):
		return "pool_not_found"
	case strings.Contains(errMsg, "ENOSPC") || strings.Contains(errMsg, "pool is full"):
		return "pool_full"
	case strings.Contains(errMsg, "network_interface") || strings.Contains(errMsg, "nic_attach") || strings.Contains(errMsg, "NIC"):
		return "nic_invalid"
	case strings.Contains(errMsg, "reconnect") || strings.Contains(errMsg, "unreachable"):
		return "connection"
	case strings.Contains(errMsg, "permission") || strings.Contains(errMsg, "EACCES"):
		return "auth"
	case strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline"):
		return "timeout"
	case strings.Contains(errMsg, "memory") || strings.Contains(errMsg, "RAM"):
		return "memory"
	case strings.Contains(errMsg, "schematic") || strings.Contains(errMsg, "ISO"):
		return "image"
	default:
		return "unknown"
	}
}

// validatePool checks that the configured pool exists on TrueNAS.
// Provides clear error messages for common mistakes (e.g., using a dataset path instead of a pool name).
func (p *Provisioner) validatePool(ctx context.Context, pool string) error {
	exists, err := p.client.PoolExists(ctx, pool)
	if err != nil {
		return fmt.Errorf("failed to verify pool %q: %w", pool, err)
	}

	if !exists {
		// Check if it looks like a dataset path (contains "/")
		if strings.Contains(pool, "/") {
			return fmt.Errorf("pool %q not found — this looks like a dataset path, not a pool name. "+
				"Set pool to just the top-level pool (e.g., 'default') and use dataset_prefix for the rest "+
				"(e.g., pool='default', dataset_prefix='%s')", pool, pool[strings.Index(pool, "/")+1:])
		}

		return fmt.Errorf("pool %q not found on TrueNAS — this must be a top-level ZFS pool name (e.g., 'default', 'tank'), "+
			"not a dataset. If your VMs should live under a dataset like '%s/mydata', set pool='%s' and dataset_prefix='mydata'. "+
			"Run 'zpool list' on TrueNAS to see available pools", pool, pool, pool)
	}

	return nil
}

// maybeResizeZvol grows a zvol if the requested size is larger than the current size.
// Shrinking is not supported (destructive).
func (p *Provisioner) maybeResizeZvol(ctx context.Context, logger *zap.Logger, zvolPath string, requestedGiB int) error {
	currentBytes, err := p.client.GetZvolSize(ctx, zvolPath)
	if err != nil {
		logger.Warn("could not check zvol size for resize", zap.String("path", zvolPath), zap.Error(err))

		return nil // Non-fatal — skip resize check
	}

	requestedBytes := int64(requestedGiB) * 1024 * 1024 * 1024

	if requestedBytes <= currentBytes {
		return nil // Same size or smaller — no action (shrinking not supported)
	}

	logger.Info("resizing zvol",
		zap.String("path", zvolPath),
		zap.Int64("from_bytes", currentBytes),
		zap.Int64("to_bytes", requestedBytes),
	)

	if err := p.client.ResizeZvol(ctx, zvolPath, requestedGiB); err != nil {
		return fmt.Errorf("failed to resize zvol %q to %d GiB: %w", zvolPath, requestedGiB, err)
	}

	if telemetry.ZvolsResized != nil {
		telemetry.ZvolsResized.Add(ctx, 1)
	}

	logger.Info("zvol resized successfully", zap.String("path", zvolPath), zap.Int("new_size_gib", requestedGiB))

	return nil
}

// snapshotBeforeUpgrade creates a ZFS snapshot before a Talos version upgrade.
// Returns the full snapshot ID (dataset@snapname) or empty string on failure.
func (p *Provisioner) snapshotBeforeUpgrade(ctx context.Context, logger *zap.Logger, zvolPath, oldVersion, newVersion string) string {
	ctx, span := provTracer.Start(ctx, "provision.snapshotBeforeUpgrade",
		trace.WithAttributes(
			attribute.String("zvol", zvolPath),
			attribute.String("from_version", oldVersion),
			attribute.String("to_version", newVersion),
		),
	)
	defer span.End()

	snapName := fmt.Sprintf("omni-pre-upgrade-%s-%d", newVersion, time.Now().Unix())
	snapID := zvolPath + "@" + snapName

	logger.Debug("creating pre-upgrade snapshot",
		zap.String("zvol", zvolPath),
		zap.String("from_version", oldVersion),
		zap.String("to_version", newVersion),
		zap.String("snapshot", snapName),
	)

	if err := p.client.CreateSnapshot(ctx, zvolPath, snapName); err != nil {
		logger.Warn("failed to create pre-upgrade snapshot — continuing without snapshot",
			zap.String("zvol", zvolPath),
			zap.Error(err),
		)

		return ""
	}

	if telemetry.SnapshotsCreated != nil {
		telemetry.SnapshotsCreated.Add(ctx, 1)
	}

	// Enforce retention: keep only the last 3 snapshots
	p.enforceSnapshotRetention(ctx, logger, zvolPath, 3)

	return snapID
}

// swapCDROMForUpgrade updates the CDROM device to point to the new ISO.
// This ensures that if the VM reboots from CDROM (before CDROM removal), it gets the correct Talos version.
func (p *Provisioner) swapCDROMForUpgrade(ctx context.Context, logger *zap.Logger, state *specs.MachineSpec, pctx provision.Context[*resources.Machine]) {
	var data Data
	if err := pctx.UnmarshalProviderData(&data); err != nil {
		logger.Warn("could not unmarshal provider data for CDROM swap", zap.Error(err))

		return
	}

	data.ApplyDefaults(p.config)

	isoPath := "/mnt/" + data.BasePath() + "/talos-iso/" + state.ImageId + ".iso"

	logger.Info("swapping CDROM to new ISO for upgrade",
		zap.Int32("vm_id", state.VmId),
		zap.String("iso_path", isoPath),
	)

	dev, err := p.client.SwapCDROM(ctx, int(state.VmId), isoPath)
	if err != nil {
		logger.Warn("failed to swap CDROM — non-fatal, Omni handles upgrades via config",
			zap.Error(err),
		)

		return
	}

	state.CdromDeviceId = int32(dev.ID)

	logger.Debug("CDROM swapped to new ISO", zap.Int("device_id", dev.ID))
}

// enforceSnapshotRetention keeps only the last N snapshots for a dataset.
func (p *Provisioner) enforceSnapshotRetention(ctx context.Context, logger *zap.Logger, dataset string, keep int) {
	ctx, span := provTracer.Start(ctx, "provision.enforceSnapshotRetention",
		trace.WithAttributes(
			attribute.String("dataset", dataset),
			attribute.Int("keep", keep),
		),
	)
	defer span.End()

	snaps, err := p.client.ListSnapshots(ctx, dataset)
	if err != nil {
		logger.Warn("failed to list snapshots for retention", zap.String("dataset", dataset), zap.Error(err))

		return
	}

	// Only manage omni- prefixed snapshots
	var omniSnaps []client.Snapshot
	for _, s := range snaps {
		// Extract snap name from ID (format: dataset@snapname)
		snapName := s.ID
		if idx := strings.LastIndex(s.ID, "@"); idx >= 0 {
			snapName = s.ID[idx+1:]
		}

		if strings.HasPrefix(snapName, "omni-") {
			omniSnaps = append(omniSnaps, s)
		}
	}

	if len(omniSnaps) <= keep {
		return
	}

	// Delete oldest snapshots (list is typically in creation order)
	toDelete := omniSnaps[:len(omniSnaps)-keep]
	for _, s := range toDelete {
		logger.Debug("deleting old snapshot", zap.String("snapshot", s.ID))

		if err := p.client.DeleteSnapshot(ctx, s.ID); err != nil {
			logger.Warn("failed to delete old snapshot", zap.String("snapshot", s.ID), zap.Error(err))
		}
	}
}

// resetNVRAMIfNeeded checks if a VM's NVRAM needs resetting (e.g., after OVMF firmware update).
// TrueNAS VMs may fail to boot after firmware updates if the NVRAM is stale.
// This is a best-effort operation — failure is non-fatal.
func (p *Provisioner) resetNVRAMIfNeeded(ctx context.Context, logger *zap.Logger, vmID int) {
	vm, err := p.client.GetVM(ctx, vmID)
	if err != nil {
		return
	}

	// If the VM is in ERROR state, it may be a firmware mismatch — try NVRAM reset
	if vm.Status.State == "ERROR" {
		logger.Info("VM in ERROR state — attempting NVRAM reset",
			zap.Int("vm_id", vmID),
		)

		if err := p.client.ResetVMNVRAM(ctx, vmID); err != nil {
			logger.Error("NVRAM reset failed — manual intervention required",
				zap.Int("vm_id", vmID),
				zap.Error(err),
			)

			return
		}

		logger.Info("NVRAM reset successful — restarting VM", zap.Int("vm_id", vmID))

		// Try to start the VM after NVRAM reset
		if err := p.client.StartVM(ctx, vmID); err != nil {
			logger.Error("failed to start VM after NVRAM reset", zap.Int("vm_id", vmID), zap.Error(err))
		}
	}
}

// checkExistingVM checks if a VM already exists by ID or name.
// Returns a pointer to the error to return (nil means "continue creating"), or nil if no VM found.
func (p *Provisioner) checkExistingVM(ctx context.Context, logger *zap.Logger, state *specs.MachineSpec, vmName string) *error {
	if state.VmId != 0 {
		vm, err := p.client.GetVM(ctx, int(state.VmId))
		if err != nil && !isNotFound(err) {
			err = fmt.Errorf("failed to get VM: %w", err)
			return &err
		}

		if err == nil {
			return p.handleExistingVM(ctx, logger, vm, vmName)
		}

		// VM was deleted externally — reset state to trigger re-creation
		logger.Warn("VM was deleted externally from TrueNAS — will recreate",
			zap.Int32("old_vm_id", state.VmId),
		)

		state.VmId = 0
		state.CdromDeviceId = 0
	}

	// Check by name (idempotency)
	existingVM, err := p.client.FindVMByName(ctx, vmName)
	if err != nil {
		err = fmt.Errorf("failed to check for existing VM: %w", err)
		return &err
	}

	if existingVM != nil {
		state.VmId = int32(existingVM.ID)
		return p.handleExistingVM(ctx, logger, existingVM, vmName)
	}

	return nil // No existing VM — proceed with creation
}

func (p *Provisioner) handleExistingVM(ctx context.Context, logger *zap.Logger, vm *client.VM, vmName string) *error {
	if vm.Status.State == "RUNNING" {
		logger.Debug("VM is already running", zap.Int("vm_id", vm.ID))
		p.TrackVMName(vmName)

		if telemetry.VMsProvisioned != nil {
			telemetry.VMsProvisioned.Add(ctx, 1)
		}

		var nilErr error
		return &nilErr
	}

	if vm.Status.State == "ERROR" {
		// VM in error state — attempt NVRAM reset (firmware mismatch recovery)
		p.resetNVRAMIfNeeded(ctx, logger, vm.ID)

		retryErr := provision.NewRetryInterval(30 * time.Second)
		return &retryErr
	}

	if err := p.client.StartVM(ctx, vm.ID); err != nil {
		err = fmt.Errorf("failed to start existing VM: %w", err)
		return &err
	}

	retryErr := provision.NewRetryInterval(10 * time.Second)
	return &retryErr
}

func isNotFound(err error) bool {
	return client.IsNotFound(err)
}

func isAlreadyExists(err error) bool {
	return client.IsAlreadyExists(err)
}
