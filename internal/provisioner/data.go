package provisioner

// Data is the provider custom machine config from the MachineClass.
// Fields map to the schema.json that is reported to Omni.
type Data struct {
	Pool         string `yaml:"pool,omitempty"`
	NICAttach    string `yaml:"nic_attach,omitempty"` // Bridge, VLAN, or physical interface (e.g., "br100", "vlan666", "enp5s0")
	BootMethod   string `yaml:"boot_method,omitempty"`
	Architecture string `yaml:"architecture,omitempty"`
	CPUs         int    `yaml:"cpus,omitempty"`
	Memory       int    `yaml:"memory,omitempty"`
	DiskSize     int    `yaml:"disk_size,omitempty"`
}
