package provisioner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/google/uuid"
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
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources/meta"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

var provTracer = otel.Tracer("truenas-provisioner")

// isoHTTPClient is reused across ISO downloads to benefit from connection pooling
// (TLS session resumption, keep-alive) when hitting Image Factory repeatedly.
var isoHTTPClient = &http.Client{Timeout: 10 * time.Minute}

const errUnmarshalProviderData = "failed to unmarshal provider data: %w"

// hashRequestID returns a truncated SHA-256 hash of the request ID for use in
// trace attributes. This avoids exposing raw request IDs (which map to VM names,
// zvol paths, and SideroLink tokens) in OTEL telemetry data.
// generateUUID returns a new UUID v7 string.
func generateUUID() string {
	return uuid.Must(uuid.NewV7()).String()
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
//
// iscsi-tools is required for Longhorn (the default storage) — Longhorn uses
// iSCSI internally to attach replicas to pods. It's also needed for democratic-csi
// iSCSI mode. Adding it by default avoids a "PVC stuck Pending" failure mode that
// only surfaces after the user tries to use persistent storage.
//
// nfs-utils was previously included, but was removed in v0.14.0 alongside the
// provider-managed NFS auto-storage. Users who want democratic-csi in NFS mode or
// manual NFS mounts can add it to their MachineClass `extensions` field.
var defaultExtensions = []string{
	"siderolabs/qemu-guest-agent",
	"siderolabs/util-linux-tools",
	"siderolabs/iscsi-tools",
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
			recordProvisionError(ctx, logger, err)
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
			recordProvisionError(ctx, logger, err)
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

	isoHashProperty := "org.omni:iso-sha256-" + imageID

	// Use singleflight to prevent concurrent downloads of the same ISO
	_, err, _ = p.isoGroup.Do(imageID, func() (any, error) {
		// Check if ISO already exists
		exists, err := p.client.FileExists(ctx, isoPath)
		if err != nil {
			return nil, fmt.Errorf("failed to check ISO existence: %w", err)
		}

		if exists {
			// Cache hit — ensure the stored hash hasn't been poisoned by a
			// previous mismatch. This protects against the scenario where a
			// prior provision detected a factory compromise and marked the
			// on-disk ISO as tainted; we refuse to reuse it until the
			// operator has cleaned up (no filesystem.unlink in the client
			// today — see docs/hardening.md for the manual cleanup recipe).
			storedHash, _ := p.client.GetDatasetUserProperty(ctx, isoDataset, isoHashProperty)
			if cachedISOPoisoned(storedHash) {
				return nil, fmt.Errorf("ISO %s is marked POISONED from a prior factory-compromise detection — delete %s on TrueNAS and retry (see docs/hardening.md)", imageID, isoPath)
			}

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

		// TOFU (trust-on-first-use) supply-chain hash: on the first download
		// for this imageID we just record the SHA-256. On subsequent
		// downloads (cache loss + re-provision), we compare against the
		// recorded hash. Mismatch means someone changed the bytes under the
		// same factory URL — the ISO is treated as compromised.
		expectedHash, _ := p.client.GetDatasetUserProperty(ctx, isoDataset, isoHashProperty)

		if cachedISOPoisoned(expectedHash) {
			return nil, fmt.Errorf("ISO %s is marked POISONED from a prior factory-compromise detection — delete %s on TrueNAS and retry", imageID, isoPath)
		}

		isoStart := time.Now()

		logger.Info("downloading Talos ISO",
			zap.String("url", imageURL.String()),
			zap.String("dest", isoPath),
			zap.Bool("tofu_pinned", expectedHash != ""),
		)

		// Download ISO from image factory
		isoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL.String(), nil) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("failed to create ISO download request: %w", err)
		}

		resp, err := isoHTTPClient.Do(isoReq)
		if err != nil {
			return nil, fmt.Errorf("failed to download ISO: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("ISO download returned status %d", resp.StatusCode)
		}

		// Compute SHA-256 while streaming to TrueNAS so we don't buffer the
		// entire ISO in memory. Hash is only usable after upload completes.
		hasher := sha256.New()
		teed := io.TeeReader(resp.Body, hasher)

		// Upload to TrueNAS
		if err := p.client.UploadFile(ctx, isoPath, teed, resp.ContentLength); err != nil {
			return nil, fmt.Errorf("failed to upload ISO to TrueNAS: %w", err)
		}

		downloadedHash := hex.EncodeToString(hasher.Sum(nil))

		if classifyTOFU(expectedHash, downloadedHash) == tofuMismatch {
			// Mismatch: mark the stored hash with POISONED- prefix so a
			// future provision refuses to use this ISO (cache hit branch
			// checks the poison marker). Fail the current provision loudly.
			_ = p.client.SetDatasetUserProperty(ctx, isoDataset, isoHashProperty, poisonMarker(downloadedHash))

			if telemetry.ISOHashMismatches != nil {
				telemetry.ISOHashMismatches.Add(ctx, 1)
			}

			logger.Error("ISO hash mismatch — possible supply-chain compromise at factory.talos.dev",
				zap.String("image_id", imageID),
				zap.String("expected_sha256", expectedHash),
				zap.String("got_sha256", downloadedHash),
				zap.String("iso_path", isoPath),
			)

			return nil, fmt.Errorf("ISO hash mismatch for %s: expected %s, got %s — delete %s and rotate factory trust before retrying", imageID, expectedHash, downloadedHash, isoPath)
		}

		// Record the hash so future downloads of this imageID are verified.
		// Non-fatal: if the set fails we still return success because the
		// upload succeeded and the hash comparison is a best-effort defense.
		if err := p.client.SetDatasetUserProperty(ctx, isoDataset, isoHashProperty, downloadedHash); err != nil {
			logger.Warn("failed to record ISO hash for TOFU verification — future downloads will re-establish trust-on-first-use",
				zap.String("image_id", imageID),
				zap.Error(err),
			)
		}

		if telemetry.ISODownloadDuration != nil {
			telemetry.ISODownloadDuration.Record(ctx, time.Since(isoStart).Seconds())
		}

		logger.Info("ISO uploaded successfully",
			zap.String("path", isoPath),
			zap.String("sha256", downloadedHash),
		)

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
			recordProvisionError(ctx, logger, err)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		recordStepDuration(ctx, "createVM", stepStart)
		span.End()
	}()
	state := pctx.State.TypedSpec().Value
	vmName := meta.BuildVMName(meta.ProviderID, pctx.GetRequestID())

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

	// Pre-check: verify pools have enough free space for all zvols
	// Aggregate disk requirements per pool
	poolRequiredGiB := map[string]int{data.Pool: data.DiskSize}
	for _, disk := range data.AdditionalDisks {
		diskPool := disk.Pool
		if diskPool == "" {
			diskPool = data.Pool
		}

		poolRequiredGiB[diskPool] += disk.Size
	}

	for pool, requiredGiB := range poolRequiredGiB {
		requiredBytes := int64(requiredGiB) * 1024 * 1024 * 1024
		freeBytes, poolErr := p.client.PoolFreeSpace(ctx, pool)

		if poolErr == nil {
			logger.Debug("pool space check",
				zap.String("pool", pool),
				zap.Int64("free_gib", freeBytes/(1024*1024*1024)),
				zap.Int("required_gib", requiredGiB),
			)
		}

		if poolErr == nil && freeBytes < requiredBytes {
			return fmt.Errorf("pool %q has %d GiB free but needs %d GiB — free up space or use a different pool",
				pool, freeBytes/(1024*1024*1024), requiredGiB)
		}
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

	if err := p.ensureZvol(ctx, logger, zvolPath, data.DiskSize, data.Encrypted, omniProps); err != nil {
		return err
	}

	state.ZvolPath = zvolPath

	// Generate a stable UUID for the VM's SMBIOS identity.
	// This UUID is set on the bhyve VM so that when Talos boots, it reads
	// the same UUID via DMI and uses it to register with Omni — ensuring
	// the provisioned record and the joined machine are correlated.
	machineUUID := generateUUID()

	// Create the VM. Description encodes ownership so Deprovision (and the
	// adoption path in handleExistingVM) can refuse to touch VMs this provider
	// didn't create — preventing accidental deletion of look-alike VMs created
	// by another operator or a second provider instance.
	vm, err := p.client.CreateVM(ctx, client.CreateVMRequest{
		Name:        vmName,
		Description: omniVMDescription(requestID),
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

	// Attach root disk
	if _, err := p.client.AddDisk(ctx, vm.ID, zvolPath); err != nil {
		return fmt.Errorf("failed to attach root disk: %w", err)
	}

	// Create and attach additional data disks
	state.AdditionalZvolPaths = nil // Reset to avoid duplicates on retry

	for i, disk := range data.AdditionalDisks {
		diskPool := disk.Pool
		if diskPool == "" {
			diskPool = data.Pool
		}

		// Per-disk dataset_prefix overrides the MachineClass-level one
		diskPrefix := disk.DatasetPrefix
		if diskPrefix == "" {
			diskPrefix = data.DatasetPrefix
		}

		diskBasePath := diskPool
		if diskPrefix != "" {
			diskBasePath = diskPool + "/" + diskPrefix
		}

		additionalZvolPath := fmt.Sprintf("%s/omni-vms/%s-disk-%d", diskBasePath, requestID, i+1)

		// Ensure parent dataset hierarchy exists on the target pool
		if diskPrefix != "" {
			if err := p.client.EnsureDataset(ctx, diskBasePath); err != nil {
				return fmt.Errorf("failed to ensure dataset prefix on pool %q for additional disk %d: %w", diskPool, i, err)
			}
		}

		if err := p.client.EnsureDataset(ctx, diskBasePath+"/omni-vms"); err != nil {
			return fmt.Errorf("failed to ensure omni-vms dataset on pool %q for additional disk %d: %w", diskPool, i, err)
		}

		if err := p.ensureZvol(ctx, logger, additionalZvolPath, disk.Size, disk.Encrypted, client.OmniManagedProperties(requestID)); err != nil {
			return fmt.Errorf("additional disk %d: %w", i, err)
		}

		if _, err := p.client.AddDiskWithOrder(ctx, vm.ID, additionalZvolPath, 1001+i); err != nil {
			return fmt.Errorf("failed to attach additional disk %d: %w", i, err)
		}

		state.AdditionalZvolPaths = append(state.AdditionalZvolPaths, additionalZvolPath)

		logger.Info("attached additional disk",
			zap.Int("index", i),
			zap.String("pool", diskPool),
			zap.Int("size_gib", disk.Size),
			zap.Bool("encrypted", disk.Encrypted),
			zap.String("path", additionalZvolPath),
		)
	}

	if telemetry.AdditionalDisksTotal != nil && len(data.AdditionalDisks) > 0 {
		telemetry.AdditionalDisksTotal.Record(ctx, int64(len(data.AdditionalDisks)))
	}

	// Emit Talos UserVolumeConfig for each additional disk so Talos formats
	// and mounts them at /var/mnt/<name>. Without this patch the disks are
	// attached to the VM but show up as raw unformatted block devices inside
	// the guest, invisible to Kubernetes workloads (Longhorn, local-path, etc.).
	if len(data.AdditionalDisks) > 0 {
		patchData, patchErr := buildUserVolumePatch(data.AdditionalDisks)
		if patchErr != nil {
			return fmt.Errorf("failed to build UserVolumeConfig patch: %w", patchErr)
		}

		if patchData != nil {
			if cpErr := applyConfigPatch(ctx, pctx, "data-volumes", requestID, patchData); cpErr != nil {
				return fmt.Errorf("failed to apply UserVolumeConfig patch: %w", cpErr)
			}

			volumeNames := make([]string, len(data.AdditionalDisks))
			for i, d := range data.AdditionalDisks {
				volumeNames[i] = d.Name
			}

			logger.Info("applied UserVolumeConfig patch for additional disks",
				zap.Strings("volumes", volumeNames),
				zap.String("vm_name", vmName),
			)
		}

		// Emit the Longhorn operational patch if any disk is named "longhorn".
		// This makes the node Longhorn-ready (iscsi_tcp module, bind mount,
		// sysctl) at provision time, so the only remaining step for the user
		// is `helm install longhorn`. See buildLonghornOperationalPatch for
		// the three silent failure modes this prevents.
		if hasLonghornDisk(data.AdditionalDisks) {
			longhornPatch, longhornErr := buildLonghornOperationalPatch()
			if longhornErr != nil {
				return fmt.Errorf("failed to build Longhorn operational patch: %w", longhornErr)
			}

			if cpErr := applyConfigPatch(ctx, pctx, "longhorn-ops", requestID, longhornPatch); cpErr != nil {
				return fmt.Errorf("failed to apply Longhorn operational patch: %w", cpErr)
			}

			logger.Info("applied Longhorn operational patch",
				zap.String("vm_name", vmName),
			)
		}
	}

	// Attach primary NIC with a deterministic MAC derived from the request ID.
	// This ensures the MAC survives reprovisioning, so DHCP reservations stay valid.
	// Collision detection is scoped to the same network segment (bridge/VLAN) because
	// MAC addresses only need to be unique within a single L2 broadcast domain.
	primaryMAC := DeterministicMAC(requestID, 0)

	segmentMACs, macErr := p.client.NICMACsOnSegment(ctx, data.NetworkInterface)
	if macErr != nil {
		logger.Warn("could not query segment MACs for collision detection — proceeding without",
			zap.String("network_interface", data.NetworkInterface),
			zap.Error(macErr),
		)
	} else {
		resolved, collided := ResolveDeterministicMAC(requestID, 0, segmentMACs)
		if collided {
			logger.Warn("deterministic MAC collision on segment — resolved with alternate hash",
				zap.String("original_mac", primaryMAC),
				zap.String("resolved_mac", resolved),
				zap.String("network_interface", data.NetworkInterface),
				zap.String("vm_name", vmName),
			)
		}

		primaryMAC = resolved
	}

	nicDev, err := p.client.AddNICWithConfig(ctx, vm.ID, client.NICConfig{
		NetworkInterface: data.NetworkInterface,
		MAC:              primaryMAC,
	}, 2001)
	if err != nil {
		return fmt.Errorf("failed to attach primary NIC: %w", err)
	}

	// Log MAC address so users can create DHCP reservations in their router
	if mac, ok := nicDev.Attributes["mac"].(string); ok && mac != "" {
		logger.Info("VM NIC MAC address (deterministic) — stable across reprovision for DHCP reservations",
			zap.String("mac", mac),
			zap.String("vm_name", vmName),
			zap.String("network_interface", data.NetworkInterface),
			zap.String("role", "primary"),
		)
	}

	// Attach additional NICs
	var (
		mtuPatches   []nicMTUConfig
		attachedMACs = make([]string, len(data.AdditionalNICs))
	)

	for i, nic := range data.AdditionalNICs {
		nicCfg := client.NICConfig{
			NetworkInterface: nic.NetworkInterface,
			Type:             nic.Type,
			MTU:              nic.MTU,
		}

		// Always assign a deterministic MAC — matches primary NIC behavior so
		// DHCP reservations survive reprovisioning on every interface.
		nicMAC := DeterministicMAC(requestID, i+1)

		nicSegmentMACs, segErr := p.client.NICMACsOnSegment(ctx, nic.NetworkInterface)
		if segErr != nil {
			logger.Warn("could not query segment MACs for collision detection — proceeding without",
				zap.String("network_interface", nic.NetworkInterface),
				zap.Error(segErr),
			)
		} else {
			resolved, nicCollided := ResolveDeterministicMAC(requestID, i+1, nicSegmentMACs)
			if nicCollided {
				logger.Warn("deterministic MAC collision on segment — resolved with alternate hash",
					zap.Int("index", i),
					zap.String("original_mac", nicMAC),
					zap.String("resolved_mac", resolved),
					zap.String("network_interface", nic.NetworkInterface),
					zap.String("vm_name", vmName),
				)
			}

			nicMAC = resolved
		}

		nicCfg.MAC = nicMAC

		dev, nicErr := p.client.AddNICWithConfig(ctx, vm.ID, nicCfg, 2002+i)
		if nicErr != nil {
			return fmt.Errorf("failed to attach additional NIC %d (%s): %w", i, nic.NetworkInterface, nicErr)
		}

		mac := ""
		if m, ok := dev.Attributes["mac"].(string); ok {
			mac = m
		}

		attachedMACs[i] = mac

		if mac == "" {
			// TrueNAS returned success but no MAC attribute on the attached
			// device. Without a MAC, the Talos config patch's deviceSelector
			// has nothing to match on, so this NIC is silently skipped from
			// interface config — VM will boot single-homed on this link. Loud
			// Warn so SRE can correlate when a multi-homed VM comes up with
			// fewer IPs than expected.
			logger.Warn("additional NIC attached but TrueNAS returned no MAC — skipping interface config patch for this NIC; VM will boot single-homed on this link",
				zap.Int("index", i),
				zap.String("network_interface", nic.NetworkInterface),
				zap.String("vm_name", vmName),
			)
		}

		if nic.MTU > 0 && mac != "" {
			mtuPatches = append(mtuPatches, nicMTUConfig{mac: mac, mtu: nic.MTU})
		}

		dhcp := resolveNICDHCP(nic)

		logger.Debug("attached additional NIC",
			zap.Int("index", i),
			zap.String("network_interface", nic.NetworkInterface),
			zap.String("mac", mac),
			zap.Int("mtu", nic.MTU),
			zap.Bool("dhcp", dhcp),
			zap.String("vm_name", vmName),
		)
	}

	nicInterfaces, nicAggregate := collectNICInterfaceConfigs(data.AdditionalNICs, attachedMACs)

	// Apply MTU config patches for NICs with custom MTU
	if len(mtuPatches) > 0 {
		patchData, patchErr := buildMTUPatch(mtuPatches)
		if patchErr != nil {
			return fmt.Errorf("failed to build MTU config patch: %w", patchErr)
		}

		if cpErr := applyConfigPatch(ctx, pctx, "nic-mtu", requestID, patchData); cpErr != nil {
			return fmt.Errorf("failed to apply MTU config patch: %w", cpErr)
		}

		logger.Info("applied MTU config patch",
			zap.Int("nic_count", len(mtuPatches)),
			zap.String("vm_name", vmName),
		)
	}

	// Apply interface config patch for every additional NIC. Talos only
	// configures the primary link by default — without this patch,
	// additional NICs come up with link-local IPv6 only and never acquire
	// an IPv4 lease. Patch emits deviceSelector (by MAC) + dhcp per NIC.
	// Static addresses / gateways are intentionally unsupported: the
	// MachineClass is shared across every worker in a MachineSet, so any
	// per-NIC IP typed into the class would be claimed by N workers and
	// collide. DHCP + deterministic MACs + upstream DHCP reservations is
	// the only way to pin specific per-worker IPs from a shared class.
	if len(nicInterfaces) > 0 {
		patchData, patchErr := buildAdditionalNICInterfacesPatch(nicInterfaces)
		if patchErr != nil {
			return fmt.Errorf("failed to build additional-NIC interfaces config patch: %w", patchErr)
		}

		if patchData != nil {
			if cpErr := applyConfigPatch(ctx, pctx, "nic-interfaces", requestID, patchData); cpErr != nil {
				return fmt.Errorf("failed to apply additional-NIC interfaces config patch: %w", cpErr)
			}

			logger.Info("applied additional-NIC interfaces config patch",
				zap.Int("nic_count", len(nicInterfaces)),
				zap.Int("dhcp_nics", nicAggregate.DHCPNICs),
				zap.Int("noconfig_nics", nicAggregate.NoConfigNICs),
				zap.String("vm_name", vmName),
			)
		}
	}

	// Apply advertised_subnets config patch if set. The patch content depends
	// on machine role — `cluster.etcd.advertisedSubnets` is rejected by Talos
	// validation on workers ("etcd config is only allowed on control plane
	// machines"), so workers get only the kubelet portion.
	isCP := isControlPlaneRequest(pctx)

	buildRoleAwareSubnetsPatch := func(subnets string) ([]byte, error) {
		if isCP {
			return buildAdvertisedSubnetsPatch(subnets)
		}

		return buildKubeletSubnetsPatch(subnets)
	}

	if data.AdvertisedSubnets != "" {
		patchData, patchErr := buildRoleAwareSubnetsPatch(data.AdvertisedSubnets)
		if patchErr != nil {
			return fmt.Errorf("failed to build advertised_subnets config patch: %w", patchErr)
		}

		if patchData != nil {
			if cpErr := applyConfigPatch(ctx, pctx, "advertised-subnets", requestID, patchData); cpErr != nil {
				return fmt.Errorf("failed to apply advertised_subnets config patch: %w", cpErr)
			}

			logger.Info("applied advertised_subnets config patch",
				zap.String("subnets", data.AdvertisedSubnets),
				zap.Bool("is_control_plane", isCP),
				zap.String("vm_name", vmName),
			)
		}
	} else if len(data.AdditionalNICs) > 0 {
		// Auto-detect the primary NIC's subnet and pin kubelet (+ etcd on CPs) to it
		subnet, subnetErr := p.client.InterfaceSubnet(ctx, data.NetworkInterface)

		switch {
		case subnetErr != nil:
			logger.Warn("could not auto-detect primary NIC subnet — set advertised_subnets manually",
				zap.String("network_interface", data.NetworkInterface),
				zap.Error(subnetErr),
			)
		case subnet != "":
			patchData, patchErr := buildRoleAwareSubnetsPatch(subnet)
			if patchErr == nil && patchData != nil {
				if cpErr := applyConfigPatch(ctx, pctx, "advertised-subnets", requestID, patchData); cpErr != nil {
					return fmt.Errorf("failed to apply auto-detected advertised_subnets config patch: %w", cpErr)
				}

				logger.Info("auto-detected primary NIC subnet, applied advertised_subnets config patch",
					zap.String("subnet", subnet),
					zap.String("network_interface", data.NetworkInterface),
					zap.Bool("is_control_plane", isCP),
					zap.String("vm_name", vmName),
				)
			}
		default:
			logger.Warn("primary NIC has no IPv4 address — set advertised_subnets manually to pin etcd/kubelet",
				zap.String("network_interface", data.NetworkInterface),
				zap.String("vm_name", vmName),
			)
		}
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
// The CDROM is intentionally left attached — the root disk has the lowest
// boot order (1000) so UEFI boots it once Talos is installed, and the CDROM
// at order 1500 is only reached on a fresh VM where the disk is empty.
// Removing the CDROM would require stopping the VM, which kills Talos before
// it finishes installing. The CDROM is cleaned up on deprovision.
func (p *Provisioner) stepHealthCheck(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) (err error) {
	stepStart := time.Now()
	ctx, span := provTracer.Start(ctx, "provision.healthCheck",
		trace.WithAttributes(attribute.String("request_id_hash", hashRequestID(pctx.GetRequestID()))),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			recordProvisionError(ctx, logger, err)
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

	// The CDROM is intentionally left attached. The root disk has boot order 1000
	// (lowest = UEFI tries first), the CDROM is at 1500, so once Talos is installed
	// on disk UEFI never reaches the CDROM. Removing it would require stopping the
	// VM, which kills Talos mid-install. The CDROM stays attached but unused and is
	// cleaned up on deprovision.
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

// recordProvisionError categorizes, logs, and records a provision error.
//
// Unwraps cosi-runtime's *controller.RequeueError: its outer Error() string is
// just "requeue in <duration>" — a benign retry signal, not a failure — while
// any real underlying error lives in Err(). Prior versions treated the whole
// RequeueError as fatal, spamming Error-level logs and the
// truenas_provision_errors_total counter on every normal step-wait.
//
//   - RequeueError with nil Err(): pure backoff, skip log and counter entirely
//   - RequeueError wrapping context.Canceled: shutdown, skip
//   - RequeueError wrapping a real error: log and count the inner error
//   - context.Canceled at the top level: shutdown, skip
//   - anything else: log and count as before
func recordProvisionError(ctx context.Context, logger *zap.Logger, err error) {
	if err == nil {
		return
	}

	var requeueErr *controller.RequeueError
	if errors.As(err, &requeueErr) {
		inner := requeueErr.Err()
		if inner == nil {
			// Benign retry signal with no underlying failure — nothing to report.
			return
		}

		err = inner
	}

	// Context cancellation is the shutdown-handshake signal, not a provision
	// failure. Counting it as an error conflates "we asked the step to stop"
	// with "the step failed on its own" — masks real regressions during
	// operator-initiated restarts.
	if errors.Is(err, context.Canceled) {
		return
	}

	category := categorizeError(err)
	if logger != nil {
		logger.Error("provision error",
			zap.String("error_category", category),
			zap.Error(err),
		)
	}

	if telemetry.ProvisionErrors != nil {
		telemetry.ProvisionErrors.Add(ctx, 1, telemetry.WithErrorCategory(category))
	}
}

// applyConfigPatch wraps pctx.CreateConfigPatch with timing telemetry keyed
// by patch_kind. Every patch kind the provider emits (data-volumes,
// longhorn-ops, nic-mtu, nic-interfaces, advertised-subnets) goes through
// this helper so operators can dashboard/alert on "which patch kind is
// failing or slow" without grepping step-duration logs.
//
// The helper records the duration for BOTH success and failure so p99
// spikes on a failing Omni backend show up in the histogram — otherwise
// a slow-then-failing RPC leaves no latency trace.
func applyConfigPatch(ctx context.Context, pctx provision.Context[*resources.Machine], kind, requestID string, data []byte) error {
	start := time.Now()
	err := pctx.CreateConfigPatch(ctx, patchName(kind, requestID), data)

	if telemetry.ConfigPatchDuration != nil {
		telemetry.ConfigPatchDuration.Record(ctx, time.Since(start).Seconds(), telemetry.WithPatchKind(kind))
	}

	return err
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
	// `config_invalid` precedes `nic_invalid` so MachineClass validation
	// errors (wrapped via "invalid MachineClass config: %w" in stepCreateVM)
	// route to their own bucket even when the inner message mentions
	// `additional_nics`. Without this, an operator typo on a CIDR pages the
	// same alert path as a real TrueNAS NIC-attach failure.
	case strings.Contains(errMsg, "invalid MachineClass config"):
		return "config_invalid"
	// `config_patch` covers every CreateConfigPatch failure path (data-volumes,
	// longhorn-ops, nic-mtu, nic-interfaces, advertised-subnets). Without this,
	// a patch-emission regression shows up as `unknown` on the dashboard and
	// on-call has to grep logs to attribute which patch failed.
	case strings.Contains(errMsg, "config patch"):
		return "config_patch"
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

// ensureZvol creates a zvol (encrypted or plain), handling the "already exists" case
// with passphrase retrieval, unlock, and resize. Used for both root and additional disks.
func (p *Provisioner) ensureZvol(ctx context.Context, logger *zap.Logger, zvolPath string, sizeGiB int, encrypted bool, props []client.UserProperty) error {
	if encrypted {
		passphrase, genErr := generatePassphrase()
		if genErr != nil {
			return genErr
		}

		encProps := make([]client.UserProperty, len(props))
		copy(encProps, props)
		encProps = append(encProps, client.UserProperty{Key: passphraseProperty, Value: passphrase})

		if _, err := p.client.CreateEncryptedZvol(ctx, zvolPath, sizeGiB, passphrase, encProps); err != nil {
			if !isAlreadyExists(err) {
				return fmt.Errorf("failed to create encrypted zvol %q: %w", zvolPath, err)
			}

			stored, propErr := p.client.GetDatasetUserProperty(ctx, zvolPath, passphraseProperty)
			if propErr != nil {
				return fmt.Errorf("failed to read stored passphrase from %q: %w", zvolPath, propErr)
			}

			if stored == "" {
				return fmt.Errorf("encrypted zvol %q exists but has no stored passphrase — it may have been created manually or by an older provider version", zvolPath)
			}

			if locked, lockErr := p.client.IsDatasetLocked(ctx, zvolPath); lockErr == nil && locked {
				logger.Debug("unlocking encrypted zvol", zap.String("path", zvolPath))

				if unlockErr := p.client.UnlockDataset(ctx, zvolPath, stored); unlockErr != nil {
					return fmt.Errorf("failed to unlock encrypted zvol %q: %w", zvolPath, unlockErr)
				}
			}

			if resizeErr := p.maybeResizeZvol(ctx, logger, zvolPath, sizeGiB); resizeErr != nil {
				return resizeErr
			}
		}

		return nil
	}

	if _, err := p.client.CreateZvol(ctx, zvolPath, sizeGiB, props); err != nil {
		if !isAlreadyExists(err) {
			return fmt.Errorf("failed to create zvol %q: %w", zvolPath, err)
		}

		if resizeErr := p.maybeResizeZvol(ctx, logger, zvolPath, sizeGiB); resizeErr != nil {
			return resizeErr
		}
	}

	return nil
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
	// Refuse to adopt a VM that isn't ours. A same-name VM created out-of-band
	// (operator manually, second provider instance) would otherwise get taken
	// over — and destroyed on deprovision. Fail loudly so the operator can
	// investigate rather than silently inheriting unknown state.
	if !isOmniManagedVM(vm) {
		err := fmt.Errorf("refusing to adopt VM %d (%q): description %q does not carry the Omni management marker — a non-provider VM is using the requested name, rename it on TrueNAS or pick a different MachineClass name",
			vm.ID, vmName, vm.Description)
		return &err
	}

	if vm.Status.State == "RUNNING" {
		logger.Debug("VM is already running", zap.Int("vm_id", vm.ID))
		p.TrackVMName(vmName)
		p.clearVMErrors(vm.ID)

		if telemetry.VMsProvisioned != nil {
			telemetry.VMsProvisioned.Add(ctx, 1)
		}

		var nilErr error
		return &nilErr
	}

	if vm.Status.State == "ERROR" {
		count := p.recordVMError(vm.ID)

		if p.config.MaxErrorRecoveries > 0 && count > p.config.MaxErrorRecoveries {
			logger.Error("VM exceeded maximum error recoveries — deprovisioning for replacement",
				zap.Int("vm_id", vm.ID),
				zap.Int("error_count", count),
				zap.Int("max_recoveries", p.config.MaxErrorRecoveries),
				zap.String("vm_name", vmName),
			)

			p.clearVMErrors(vm.ID)

			if telemetry.VMsAutoReplaced != nil {
				telemetry.VMsAutoReplaced.Add(ctx, 1)
			}

			if err := p.cleanupVM(ctx, logger, vm.ID); err != nil {
				logger.Warn("failed to deprovision broken VM", zap.Int("vm_id", vm.ID), zap.Error(err))
			}

			// Reset state so the provisioner recreates the VM from scratch
			err := provision.NewRetryInterval(5 * time.Second)
			return &err
		}

		logger.Warn("VM in ERROR state — attempting recovery",
			zap.Int("vm_id", vm.ID),
			zap.Int("error_count", count),
			zap.Int("max_recoveries", p.config.MaxErrorRecoveries),
		)

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

// isControlPlaneRequest reports whether the MachineRequest being provisioned
// belongs to a control-plane MachineSet. Detected from the
// LabelMachineRequestSet value's suffix (`-control-planes` is Omni's
// convention for CP MachineSets, matching the audit trail shape:
// e.g., `talos-home-control-planes` vs `talos-home-workers`).
//
// Conservative by design: on any ambiguity (missing label, unknown suffix)
// return false so the caller falls through to the worker-safe patch. The
// cost of a false worker classification on a CP (no etcd advertise-subnet
// pinning) is a latent multi-NIC etcd instability; the cost of false CP on
// a worker (shipping `cluster.etcd.advertisedSubnets` to a worker) is a
// hard Talos validation failure that bricks the machine, so we skew toward
// the safer error.
func isControlPlaneRequest(pctx provision.Context[*resources.Machine]) bool {
	setID, ok := pctx.GetMachineRequestSetID()
	if !ok {
		return false
	}

	return strings.HasSuffix(setID, "-control-planes")
}

func isAlreadyExists(err error) bool {
	return client.IsAlreadyExists(err)
}
