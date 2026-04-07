package provisioner

// Data is the provider custom machine config from the MachineClass.
// Fields map to the schema.json that is reported to Omni.
type Data struct {
	Pool         string   `yaml:"pool,omitempty"`
	NICAttach    string   `yaml:"nic_attach,omitempty"` // Bridge, VLAN, or physical interface (e.g., "br100", "vlan666", "enp5s0")
	BootMethod   string   `yaml:"boot_method,omitempty"`
	Architecture string   `yaml:"architecture,omitempty"`
	Extensions   []string `yaml:"extensions,omitempty"` // Additional Talos system extensions beyond the defaults
	Encrypted    bool     `yaml:"encrypted,omitempty"`  // Enable ZFS native encryption on the VM zvol
	CPUs         int      `yaml:"cpus,omitempty"`
	Memory       int      `yaml:"memory,omitempty"`
	DiskSize     int      `yaml:"disk_size,omitempty"`
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
