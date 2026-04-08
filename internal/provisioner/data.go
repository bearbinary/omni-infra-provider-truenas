package provisioner

import (
	"fmt"
	"regexp"
)

// Data is the provider custom machine config from the MachineClass.
// Fields map to the schema.json that is reported to Omni.
// AdditionalNIC describes an extra NIC to attach to the VM beyond the primary.
type AdditionalNIC struct {
	NICAttach string `yaml:"nic_attach"`        // Required: bridge, VLAN, or physical interface
	Type      string `yaml:"type,omitempty"`    // VIRTIO (default) or E1000
	VLANTag   int    `yaml:"vlan_id,omitempty"` // Optional: tag traffic with this VLAN ID at the VM level
}

type Data struct {
	Pool         string   `yaml:"pool,omitempty"`
	NICAttach    string   `yaml:"nic_attach,omitempty"` // Primary NIC: bridge, VLAN, or physical interface
	BootMethod   string   `yaml:"boot_method,omitempty"`
	Architecture string   `yaml:"architecture,omitempty"`
	Extensions   []string `yaml:"extensions,omitempty"` // Additional Talos system extensions beyond the defaults
	Encrypted    bool     `yaml:"encrypted,omitempty"`  // Enable ZFS native encryption on the VM zvol
	CPUs         int      `yaml:"cpus,omitempty"`
	Memory       int      `yaml:"memory,omitempty"`
	DiskSize     int      `yaml:"disk_size,omitempty"`

	// Multi-NIC: attach additional NICs for network segmentation
	AdditionalNICs []AdditionalNIC `yaml:"additional_nics,omitempty"`

	// Multihoming: pin etcd/kubelet to specific subnets when multiple NICs are present
	// Comma-separated CIDRs, e.g., "192.168.100.0/24" or "192.168.100.0/24,fd00::/64"
	AdvertisedSubnets string `yaml:"advertised_subnets,omitempty"`
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

	if d.NICAttach == "" {
		d.NICAttach = cfg.DefaultNICAttach
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
	// Validate names used in filesystem paths to prevent path traversal
	if err := validateSafeName("pool", d.Pool); err != nil {
		return err
	}

	if err := validateSafeName("nic_attach", d.NICAttach); err != nil {
		return err
	}

	seen := make(map[string]bool)

	// Primary NIC is always in the "seen" set
	if d.NICAttach != "" {
		seen[d.NICAttach] = true
	}

	for i, nic := range d.AdditionalNICs {
		if nic.NICAttach == "" {
			return fmt.Errorf("additional_nics[%d]: nic_attach is required", i)
		}

		if err := validateSafeName(fmt.Sprintf("additional_nics[%d].nic_attach", i), nic.NICAttach); err != nil {
			return err
		}

		if seen[nic.NICAttach] {
			return fmt.Errorf("additional_nics[%d]: duplicate nic_attach %q — each NIC must use a different interface", i, nic.NICAttach)
		}

		seen[nic.NICAttach] = true

		if nic.VLANTag < 0 || nic.VLANTag > 4094 {
			return fmt.Errorf("additional_nics[%d]: vlan_id must be between 1 and 4094, got %d", i, nic.VLANTag)
		}

		if nic.Type != "" && nic.Type != "VIRTIO" && nic.Type != "E1000" {
			return fmt.Errorf("additional_nics[%d]: type must be VIRTIO or E1000, got %q", i, nic.Type)
		}
	}

	return nil
}
