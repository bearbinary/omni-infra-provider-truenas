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
	// This is a convenience shorthand for adding a dedicated data disk (e.g., for Longhorn).
	if d.StorageDiskSize > 0 {
		storageDisk := AdditionalDisk{Size: d.StorageDiskSize}
		d.AdditionalDisks = append([]AdditionalDisk{storageDisk}, d.AdditionalDisks...)
		d.StorageDiskSize = 0 // consumed — prevent double-expansion
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

// Validate checks the Data config for logical errors.
func (d *Data) Validate() error {
	if d.StorageDiskSize < 0 {
		return fmt.Errorf("storage_disk_size must be >= 0, got %d", d.StorageDiskSize)
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

	for i, disk := range d.AdditionalDisks {
		if disk.Size <= 0 {
			return fmt.Errorf("additional_disks[%d]: size must be > 0", i)
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
