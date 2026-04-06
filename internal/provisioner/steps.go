package provisioner

import (
	"context"
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

// userError returns a user-friendly error message for Omni UI display.
func userError(err error) string {
	return client.UserFriendlyError(err)
}

var provTracer = otel.Tracer("truenas-provisioner")

const errUnmarshalProviderData = "failed to unmarshal provider data: %w"

// Default extensions included in every TrueNAS VM.
var defaultExtensions = []string{
	"siderolabs/qemu-guest-agent",
	"siderolabs/nfs-utils",
	"siderolabs/util-linux-tools",
}

// stepCreateSchematic generates a Talos image factory schematic ID.
func (p *Provisioner) stepCreateSchematic(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) (err error) {
	ctx, span := provTracer.Start(ctx, "provision.createSchematic",
		trace.WithAttributes(attribute.String("request_id", pctx.GetRequestID())),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
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

	extensions := append(defaultExtensions, data.Extensions...)

	schematic, err := pctx.GenerateSchematicID(ctx, logger,
		provision.WithExtraKernelArgs(extraArgs...),
		provision.WithExtraExtensions(extensions...),
		provision.WithoutConnectionParams(),
	)
	if err != nil {
		return fmt.Errorf("failed to generate schematic: %w", err)
	}

	pctx.State.TypedSpec().Value.Schematic = schematic

	logger.Info("created schematic", zap.String("schematic_id", schematic))

	return nil
}

// stepUploadISO downloads the Talos ISO and uploads it to TrueNAS.
func (p *Provisioner) stepUploadISO(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) (err error) {
	ctx, span := provTracer.Start(ctx, "provision.uploadISO",
		trace.WithAttributes(attribute.String("request_id", pctx.GetRequestID())),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	pctx.State.TypedSpec().Value.TalosVersion = pctx.GetTalosVersion()

	var data Data
	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return fmt.Errorf(errUnmarshalProviderData, err)
	}

	data.ApplyDefaults(p.config)
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

	// ISOs are cached under <pool>/talos-iso/, downloaded automatically from Image Factory
	isoDataset := data.Pool + "/talos-iso"
	isoPath := "/mnt/" + isoDataset + "/" + isoFileName

	// Use singleflight to prevent concurrent downloads of the same ISO
	_, err, _ = p.isoGroup.Do(imageID, func() (any, error) {
		// Check if ISO already exists
		exists, err := p.client.FileExists(ctx, isoPath)
		if err != nil {
			return nil, fmt.Errorf("failed to check ISO existence: %w", err)
		}

		if exists {
			logger.Info("ISO already exists, skipping download", zap.String("path", isoPath))

			return nil, nil
		}

		// Ensure the dataset exists
		if err := p.client.EnsureDataset(ctx, isoDataset); err != nil {
			return nil, fmt.Errorf("failed to ensure ISO dataset: %w", err)
		}

		logger.Info("downloading Talos ISO",
			zap.String("url", imageURL.String()),
			zap.String("dest", isoPath),
		)

		// Download ISO from image factory
		resp, err := http.Get(imageURL.String()) //nolint:gosec,noctx
		if err != nil {
			return nil, fmt.Errorf("failed to download ISO: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("ISO download returned status %d", resp.StatusCode)
		}

		// Upload to TrueNAS
		if err := p.client.UploadFile(ctx, isoPath, resp.Body, resp.ContentLength); err != nil {
			return nil, fmt.Errorf("failed to upload ISO to TrueNAS: %w", err)
		}

		logger.Info("ISO uploaded successfully", zap.String("path", isoPath))

		return nil, nil
	})

	return err
}

// stepCreateVM creates the VM on TrueNAS with disk, CDROM, and NIC devices.
func (p *Provisioner) stepCreateVM(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) (err error) {
	ctx, span := provTracer.Start(ctx, "provision.createVM",
		trace.WithAttributes(attribute.String("request_id", pctx.GetRequestID())),
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

	data.ApplyDefaults(p.config)

	// Pre-check: verify pool has enough free space for the zvol
	requiredBytes := int64(data.DiskSize) * 1024 * 1024 * 1024
	freeBytes, err := p.client.PoolFreeSpace(ctx, data.Pool)

	if err == nil && freeBytes < requiredBytes {
		return fmt.Errorf("pool %q has %d GiB free but VM needs %d GiB — free up space or use a different pool",
			data.Pool, freeBytes/(1024*1024*1024), data.DiskSize)
	}

	// Create zvol for the VM disk
	zvolPath := data.Pool + "/omni-vms/" + pctx.GetRequestID()

	// Ensure parent dataset exists
	if err := p.client.EnsureDataset(ctx, data.Pool+"/omni-vms"); err != nil {
		return fmt.Errorf("failed to ensure omni-vms dataset: %w", err)
	}

	if _, err := p.client.CreateZvol(ctx, zvolPath, data.DiskSize); err != nil {
		if !isAlreadyExists(err) {
			return fmt.Errorf("failed to create zvol: %w", err)
		}
	}

	state.ZvolPath = zvolPath

	// Create the VM
	vm, err := p.client.CreateVM(ctx, client.CreateVMRequest{
		Name:        vmName,
		Description: "Managed by Omni infra provider",
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

	logger.Info("created VM", zap.String("name", vmName), zap.Int("id", vm.ID))
	p.TrackVMName(vmName)

	// Attach CDROM with Talos ISO (cached under <pool>/talos-iso/)
	isoPath := "/mnt/" + data.Pool + "/talos-iso/" + state.ImageId + ".iso"

	cdrom, err := p.client.AddCDROM(ctx, vm.ID, isoPath)
	if err != nil {
		return fmt.Errorf("failed to attach CDROM: %w", err)
	}

	state.CdromDeviceId = int32(cdrom.ID)

	// Attach disk
	if _, err := p.client.AddDisk(ctx, vm.ID, zvolPath); err != nil {
		return fmt.Errorf("failed to attach disk: %w", err)
	}

	// Attach NIC
	if _, err := p.client.AddNIC(ctx, vm.ID, data.NICAttach); err != nil {
		return fmt.Errorf("failed to attach NIC: %w", err)
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

// stepRemoveCDROM detaches the ISO CDROM once Talos has installed to disk.
// Waits for the machine to be allocated in Omni (meaning Talos installed, rebooted, and rejoined).
func (p *Provisioner) stepRemoveCDROM(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) (err error) {
	ctx, span := provTracer.Start(ctx, "provision.removeCDROM",
		trace.WithAttributes(attribute.String("request_id", pctx.GetRequestID())),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	state := pctx.State.TypedSpec().Value

	// Already removed
	if state.CdromDeviceId == 0 {
		return nil
	}

	// Wait until Omni has allocated this machine (ID is set when the machine
	// connects via SideroLink, gets config, installs to disk, reboots, and rejoins).
	if pctx.MachineRequestStatus.TypedSpec().Value.Id == "" {
		logger.Info("waiting for machine to be allocated before removing CDROM")

		return provision.NewRetryInterval(30 * time.Second)
	}

	logger.Info("removing CDROM device",
		zap.Int32("device_id", state.CdromDeviceId),
		zap.Int32("vm_id", state.VmId),
	)

	if err := p.client.DeleteDevice(ctx, int(state.CdromDeviceId)); err != nil {
		return fmt.Errorf("failed to remove CDROM: %w", err)
	}

	state.CdromDeviceId = 0

	logger.Info("CDROM removed — VM will boot directly from disk on next restart")

	return nil
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

		// VM was deleted externally, reset
		state.VmId = 0
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
		logger.Info("VM is already running", zap.Int("vm_id", vm.ID))
		p.TrackVMName(vmName)

		if telemetry.VMsProvisioned != nil {
			telemetry.VMsProvisioned.Add(ctx, 1)
		}

		var nilErr error
		return &nilErr
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
