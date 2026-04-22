package provisioner

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// Data is the provider custom machine config from the MachineClass.
// Fields map to the schema.json that is reported to Omni.

// AdditionalNIC describes an extra NIC to attach to the VM beyond the primary.
// All NICs (primary and additional) receive a deterministic MAC derived from
// the machine request ID so DHCP reservations survive reprovisioning.
type AdditionalNIC struct {
	NetworkInterface string `yaml:"network_interface"` // Required: bridge, VLAN, or physical interface
	Type             string `yaml:"type,omitempty"`    // VIRTIO (default) or E1000
	MTU              int    `yaml:"mtu,omitempty"`     // MTU size (default: 0 = use host default, typically 1500). Set to 9000 for jumbo frames.
}

// AdditionalDisk describes an extra disk to attach to the VM beyond the root disk.
type AdditionalDisk struct {
	Size          int    `yaml:"size"`                     // Size in GiB (required)
	Pool          string `yaml:"pool,omitempty"`           // Pool override (defaults to primary pool)
	DatasetPrefix string `yaml:"dataset_prefix,omitempty"` // Dataset prefix override (defaults to MachineClass dataset_prefix)
	Encrypted     bool   `yaml:"encrypted,omitempty"`      // Per-disk encryption toggle

	// Name sets the Talos UserVolumeConfig name emitted for this disk. The
	// volume is mounted at /var/mnt/<name> inside the guest. Default: data-N
	// (1-indexed). Setting this to "longhorn" matches the default Longhorn
	// defaultDataPath = /var/mnt/longhorn.
	Name string `yaml:"name,omitempty"`

	// Filesystem for the emitted UserVolumeConfig. "xfs" (default) or "ext4".
	// Longhorn recommends xfs for best performance on modern kernels.
	Filesystem string `yaml:"filesystem,omitempty"`
}

type Data struct {
	Pool             string   `yaml:"pool,omitempty"`
	NetworkInterface string   `yaml:"network_interface,omitempty"` // Primary NIC: bridge, VLAN, or physical interface
	BootMethod       string   `yaml:"boot_method,omitempty"`
	Architecture     string   `yaml:"architecture,omitempty"`
	Extensions       []string `yaml:"extensions,omitempty"` // Additional Talos system extensions beyond the defaults
	Encrypted        bool     `yaml:"encrypted,omitempty"`  // Enable ZFS native encryption on the VM zvol
	CPUs             int      `yaml:"cpus,omitempty"`
	Memory           int      `yaml:"memory,omitempty"`
	DiskSize         int      `yaml:"disk_size,omitempty"`

	// DatasetPrefix is an optional ZFS dataset path under the pool.
	// When set, zvols are created at <pool>/<dataset_prefix>/omni-vms/<request-id>
	// and ISOs are cached at <pool>/<dataset_prefix>/talos-iso/.
	// Each segment must be a valid ZFS name (no slashes in individual segments).
	// Example: "previewk8/k8" places zvols at default/previewk8/k8/omni-vms/...
	DatasetPrefix string `yaml:"dataset_prefix,omitempty"`

	// Multi-disk: attach additional data disks beyond the root disk
	AdditionalDisks []AdditionalDisk `yaml:"additional_disks,omitempty"`

	// Multi-NIC: attach additional NICs for network segmentation
	AdditionalNICs []AdditionalNIC `yaml:"additional_nics,omitempty"`

	// Multihoming: pin etcd/kubelet to specific subnets when multiple NICs are present
	// Comma-separated CIDRs, e.g., "192.168.100.0/24" or "192.168.100.0/24,fd00::/64"
	AdvertisedSubnets string `yaml:"advertised_subnets,omitempty"`

	// StorageDiskSize adds a dedicated data disk (in GiB) for persistent storage (Longhorn).
	// Equivalent to additional_disks: [{size: N}].
	StorageDiskSize int `yaml:"storage_disk_size,omitempty"`
}

// ApplyDefaults fills in zero values from the provider config.
func (d *Data) ApplyDefaults(cfg ProviderConfig) {
	if d.CPUs == 0 {
		d.CPUs = 2
	}

	if d.Memory == 0 {
		d.Memory = 4096
	}

	if d.DiskSize == 0 {
		d.DiskSize = 40
	}

	if d.Pool == "" {
		d.Pool = cfg.DefaultPool
	}

	if d.NetworkInterface == "" {
		d.NetworkInterface = cfg.DefaultNetworkInterface
	}

	if d.BootMethod == "" {
		d.BootMethod = cfg.DefaultBootMethod
	}

	if d.BootMethod == "" {
		d.BootMethod = "UEFI"
	}

	if d.Architecture == "" {
		d.Architecture = "amd64"
	}

	// Expand storage_disk_size into additional_disks[0].
	// This is a convenience shorthand for adding a dedicated data disk for Longhorn:
	// the emitted UserVolumeConfig is named "longhorn" so the volume mounts at
	// /var/mnt/longhorn, which matches Longhorn's defaultDataPath. The
	// provisioner also emits a Longhorn operational patch (iscsi_tcp kernel
	// module + /var/lib/longhorn bind mount + vm.overcommit_memory sysctl)
	// when it sees a disk named "longhorn" — see buildLonghornOperationalPatch.
	if d.StorageDiskSize > 0 {
		storageDisk := AdditionalDisk{Size: d.StorageDiskSize, Name: LonghornVolumeName}
		d.AdditionalDisks = append([]AdditionalDisk{storageDisk}, d.AdditionalDisks...)
		d.StorageDiskSize = 0 // consumed — prevent double-expansion
	}

	// Fill defaults for each additional disk. Index assignment happens after
	// any prepended storage_disk_size expansion so names stay 1-based and stable.
	for i := range d.AdditionalDisks {
		if d.AdditionalDisks[i].Name == "" {
			d.AdditionalDisks[i].Name = fmt.Sprintf("data-%d", i+1)
		}

		if d.AdditionalDisks[i].Filesystem == "" {
			d.AdditionalDisks[i].Filesystem = "xfs"
		}
	}
}

// BasePath returns the ZFS dataset root for this machine config.
// If DatasetPrefix is set, returns "<pool>/<prefix>", otherwise just "<pool>".
func (d *Data) BasePath() string {
	if d.DatasetPrefix != "" {
		return d.Pool + "/" + d.DatasetPrefix
	}

	return d.Pool
}

// cachedKnownFields is computed once at init time since Data struct tags never change.
var cachedKnownFields = func() map[string]bool {
	fields := make(map[string]bool)
	t := reflect.TypeOf(Data{})

	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}

		name := strings.Split(tag, ",")[0]
		fields[name] = true
	}

	return fields
}()

// knownFields returns the set of known YAML field names from the Data struct tags.
func knownFields() map[string]bool {
	return cachedKnownFields
}

// UnknownFields returns field names present in rawData that are not recognized by the Data struct.
func UnknownFields(rawData map[string]any) []string {
	known := knownFields()
	var unknown []string

	for key := range rawData {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}

	return unknown
}

// safeNameRe matches ZFS-safe identifiers: alphanumeric, hyphens, underscores, dots.
// No slashes, spaces, or special characters that could enable path traversal.
var safeNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// validateSafeName checks that a user-provided name is safe for use in filesystem paths and API calls.
func validateSafeName(field, value string) error {
	if value == "" {
		return nil
	}

	if !safeNameRe.MatchString(value) {
		return fmt.Errorf("%s contains unsafe characters: %q — only alphanumeric, hyphens, underscores, and dots are allowed", field, value)
	}

	return nil
}

// MinDiskSizeGiB is the minimum allowed size for any VM-attached disk (root or additional).
const MinDiskSizeGiB = 5

// MaxDiskSizeGiB mirrors the JSON schema ceiling (1 PiB). Schema is the UX-level
// gate; this Go-side check is defense-in-depth for callers that bypass schema
// validation (direct COSI edits, legacy MachineClass payloads).
const MaxDiskSizeGiB = 1048576

// MaxCPUs and MaxMemoryMiB mirror the schema ceilings and protect int arithmetic
// downstream (byte conversion, pool capacity planning) from silent overflow.
const (
	MaxCPUs       = 512
	MaxMemoryMiB  = 16777216
	MinMemoryMiB  = 1024
)

// Validate checks the Data config for logical errors.
func (d *Data) Validate() error {
	if d.CPUs < 0 {
		return fmt.Errorf("cpus must be >= 0, got %d", d.CPUs)
	}

	if d.CPUs > MaxCPUs {
		return fmt.Errorf("cpus must be <= %d, got %d", MaxCPUs, d.CPUs)
	}

	if d.Memory < 0 {
		return fmt.Errorf("memory must be >= 0, got %d", d.Memory)
	}

	if d.Memory != 0 && d.Memory < MinMemoryMiB {
		return fmt.Errorf("memory must be >= %d MiB when set, got %d", MinMemoryMiB, d.Memory)
	}

	if d.Memory > MaxMemoryMiB {
		return fmt.Errorf("memory must be <= %d MiB, got %d", MaxMemoryMiB, d.Memory)
	}

	if d.DiskSize < 0 {
		return fmt.Errorf("disk_size must be >= 0, got %d", d.DiskSize)
	}

	if d.DiskSize != 0 && d.DiskSize < MinDiskSizeGiB {
		return fmt.Errorf("disk_size must be >= %d GiB, got %d", MinDiskSizeGiB, d.DiskSize)
	}

	if d.DiskSize > MaxDiskSizeGiB {
		return fmt.Errorf("disk_size must be <= %d GiB, got %d", MaxDiskSizeGiB, d.DiskSize)
	}

	if d.StorageDiskSize < 0 {
		return fmt.Errorf("storage_disk_size must be >= 0, got %d", d.StorageDiskSize)
	}

	if d.StorageDiskSize > 0 && d.StorageDiskSize < MinDiskSizeGiB {
		return fmt.Errorf("storage_disk_size must be >= %d GiB when set, got %d", MinDiskSizeGiB, d.StorageDiskSize)
	}

	if d.StorageDiskSize > MaxDiskSizeGiB {
		return fmt.Errorf("storage_disk_size must be <= %d GiB, got %d", MaxDiskSizeGiB, d.StorageDiskSize)
	}

	if err := validateExtensions(d.Extensions); err != nil {
		return err
	}

	// Validate names used in filesystem paths to prevent path traversal
	if err := validateSafeName("pool", d.Pool); err != nil {
		return err
	}

	if err := validateSafeName("network_interface", d.NetworkInterface); err != nil {
		return err
	}

	// Validate each segment of dataset_prefix individually (slashes are path separators, not part of names)
	if d.DatasetPrefix != "" {
		segments := strings.Split(d.DatasetPrefix, "/")
		for i, seg := range segments {
			if seg == "" {
				return fmt.Errorf("dataset_prefix has empty segment at position %d — use 'a/b' not 'a//b' or '/a/b'", i)
			}

			if err := validateSafeName(fmt.Sprintf("dataset_prefix segment %d (%q)", i, seg), seg); err != nil {
				return err
			}
		}
	}

	if len(d.AdditionalDisks) > 16 {
		return fmt.Errorf("additional_disks: maximum 16 additional disks allowed, got %d", len(d.AdditionalDisks))
	}

	seenVolumeNames := make(map[string]int)

	for i, disk := range d.AdditionalDisks {
		if disk.Size < MinDiskSizeGiB {
			return fmt.Errorf("additional_disks[%d]: size must be >= %d GiB, got %d", i, MinDiskSizeGiB, disk.Size)
		}

		if disk.Size > MaxDiskSizeGiB {
			return fmt.Errorf("additional_disks[%d]: size must be <= %d GiB, got %d", i, MaxDiskSizeGiB, disk.Size)
		}

		if disk.Pool != "" {
			if err := validateSafeName(fmt.Sprintf("additional_disks[%d].pool", i), disk.Pool); err != nil {
				return err
			}
		}

		if disk.DatasetPrefix != "" {
			for j, seg := range strings.Split(disk.DatasetPrefix, "/") {
				if seg == "" {
					continue
				}

				if err := validateSafeName(fmt.Sprintf("additional_disks[%d].dataset_prefix segment %d (%q)", i, j, seg), seg); err != nil {
					return err
				}
			}
		}

		if disk.Name != "" {
			if err := validateSafeName(fmt.Sprintf("additional_disks[%d].name", i), disk.Name); err != nil {
				return err
			}

			if prev, dup := seenVolumeNames[disk.Name]; dup {
				return fmt.Errorf("additional_disks[%d].name %q collides with additional_disks[%d].name — each volume name must be unique because it becomes the mount path at /var/mnt/<name>", i, disk.Name, prev)
			}

			seenVolumeNames[disk.Name] = i
		}

		if disk.Filesystem != "" && disk.Filesystem != "xfs" && disk.Filesystem != "ext4" {
			return fmt.Errorf("additional_disks[%d].filesystem must be \"xfs\" or \"ext4\", got %q", i, disk.Filesystem)
		}
	}

	seen := make(map[string]bool)

	// Primary NIC is always in the "seen" set
	if d.NetworkInterface != "" {
		seen[d.NetworkInterface] = true
	}

	for i, nic := range d.AdditionalNICs {
		if nic.NetworkInterface == "" {
			return fmt.Errorf("additional_nics[%d]: network_interface is required", i)
		}

		if err := validateSafeName(fmt.Sprintf("additional_nics[%d].network_interface", i), nic.NetworkInterface); err != nil {
			return err
		}

		if seen[nic.NetworkInterface] {
			return fmt.Errorf("additional_nics[%d]: duplicate network_interface %q — each NIC must use a different interface", i, nic.NetworkInterface)
		}

		seen[nic.NetworkInterface] = true

		if nic.Type != "" && nic.Type != "VIRTIO" && nic.Type != "E1000" {
			return fmt.Errorf("additional_nics[%d]: type must be VIRTIO or E1000, got %q", i, nic.Type)
		}

		if nic.MTU != 0 && (nic.MTU < 576 || nic.MTU > 9216) {
			return fmt.Errorf("additional_nics[%d]: mtu must be between 576 and 9216, got %d", i, nic.MTU)
		}
	}

	return nil
}
