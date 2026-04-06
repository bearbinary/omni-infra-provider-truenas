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
	"go.uber.org/zap"

	"github.com/zclifton/omni-infra-provider-truenas/internal/client"
	"github.com/zclifton/omni-infra-provider-truenas/internal/resources"
)

// Default extensions included in every TrueNAS VM.
var defaultExtensions = []string{
	"siderolabs/qemu-guest-agent",
	"siderolabs/nfs-utils",
	"siderolabs/util-linux-tools",
}

// stepCreateSchematic generates a Talos image factory schematic ID.
func (p *Provisioner) stepCreateSchematic(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
	// Connection params include SideroLink endpoint and join token with encoded request ID.
	// We use WithoutConnectionParams() to skip the SDK's built-in embedding (which conflicts
	// with WithEncodeRequestIDsIntoTokens), then pass them ourselves via WithExtraKernelArgs.
	extraArgs := append([]string{"console=ttyS0"}, pctx.ConnectionParams.KernelArgs...)

	// Merge default extensions with any extras from MachineClass config
	var data Data
	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return fmt.Errorf("failed to unmarshal provider data: %w", err)
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
func (p *Provisioner) stepUploadISO(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
	pctx.State.TypedSpec().Value.TalosVersion = pctx.GetTalosVersion()

	var data Data
	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return fmt.Errorf("failed to unmarshal provider data: %w", err)
	}

	arch := data.Architecture
	if arch == "" {
		arch = "amd64"
	}

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

	pool := data.Pool
	if pool == "" {
		pool = p.config.DefaultPool
	}

	// ISOs are cached under <pool>/talos-iso/, downloaded automatically from Image Factory
	isoDataset := pool + "/talos-iso"
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
func (p *Provisioner) stepCreateVM(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
	state := pctx.State.TypedSpec().Value

	// If we already have a VM ID, check its status
	if state.VmId != 0 {
		vm, err := p.client.GetVM(ctx, int(state.VmId))
		if err != nil {
			if !isNotFound(err) {
				return fmt.Errorf("failed to get VM: %w", err)
			}
			// VM was deleted externally, reset and recreate
			state.VmId = 0
		} else if vm.Status.State == "RUNNING" {
			logger.Info("VM is already running", zap.Int("vm_id", vm.ID))

			return nil
		} else if vm.Status.State == "STOPPED" {
			// VM exists but stopped, start it
			if err := p.client.StartVM(ctx, vm.ID); err != nil {
				return fmt.Errorf("failed to start existing VM: %w", err)
			}

			return provision.NewRetryInterval(10 * time.Second)
		}
	}

	var data Data
	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return fmt.Errorf("failed to unmarshal provider data: %w", err)
	}

	// Apply defaults
	cpus := data.CPUs
	if cpus == 0 {
		cpus = 2
	}

	memory := data.Memory
	if memory == 0 {
		memory = 4096
	}

	diskSize := data.DiskSize
	if diskSize == 0 {
		diskSize = 40
	}

	pool := data.Pool
	if pool == "" {
		pool = p.config.DefaultPool
	}

	nicAttach := data.NICAttach
	if nicAttach == "" {
		nicAttach = p.config.DefaultNICAttach
	}

	bootMethod := data.BootMethod
	if bootMethod == "" {
		bootMethod = p.config.DefaultBootMethod
	}

	if bootMethod == "" {
		bootMethod = "UEFI"
	}

	requestID := pctx.GetRequestID()
	// TrueNAS VM names only allow alphanumeric characters and underscores
	vmName := "omni_" + strings.ReplaceAll(requestID, "-", "_")

	// Check if VM already exists by name (idempotency)
	existingVM, err := p.client.FindVMByName(ctx, vmName)
	if err != nil {
		return fmt.Errorf("failed to check for existing VM: %w", err)
	}

	if existingVM != nil {
		state.VmId = int32(existingVM.ID)

		if existingVM.Status.State == "RUNNING" {
			logger.Info("VM already exists and is running", zap.String("name", vmName))

			return nil
		}

		if err := p.client.StartVM(ctx, existingVM.ID); err != nil {
			return fmt.Errorf("failed to start existing VM: %w", err)
		}

		return provision.NewRetryInterval(10 * time.Second)
	}

	// Create zvol for the VM disk
	zvolPath := pool + "/omni-vms/" + requestID

	// Ensure parent dataset exists
	if err := p.client.EnsureDataset(ctx, pool+"/omni-vms"); err != nil {
		return fmt.Errorf("failed to ensure omni-vms dataset: %w", err)
	}

	if _, err := p.client.CreateZvol(ctx, zvolPath, diskSize); err != nil {
		if !isAlreadyExists(err) {
			return fmt.Errorf("failed to create zvol: %w", err)
		}
	}

	state.ZvolPath = zvolPath

	// Create the VM
	vm, err := p.client.CreateVM(ctx, client.CreateVMRequest{
		Name:        vmName,
		Description: "Managed by Omni infra provider",
		VCPUs:       cpus,
		Memory:      memory,
		CPUMode:     "HOST-PASSTHROUGH",
		Bootloader:  bootMethod,
		Autostart:   true,
	})
	if err != nil {
		return fmt.Errorf("failed to create VM: %w", err)
	}

	state.VmId = int32(vm.ID)

	logger.Info("created VM", zap.String("name", vmName), zap.Int("id", vm.ID))

	// Attach CDROM with Talos ISO (cached under <pool>/talos-iso/)
	isoPath := "/mnt/" + pool + "/talos-iso/" + state.ImageId + ".iso"

	if _, err := p.client.AddCDROM(ctx, vm.ID, isoPath); err != nil {
		return fmt.Errorf("failed to attach CDROM: %w", err)
	}

	// Attach disk
	if _, err := p.client.AddDisk(ctx, vm.ID, zvolPath); err != nil {
		return fmt.Errorf("failed to attach disk: %w", err)
	}

	// Attach NIC
	if _, err := p.client.AddNIC(ctx, vm.ID, nicAttach); err != nil {
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

func isNotFound(err error) bool {
	return client.IsNotFound(err)
}

func isAlreadyExists(err error) bool {
	return client.IsAlreadyExists(err)
}
