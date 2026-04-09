package provisioner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestData_ApplyDefaults(t *testing.T) {
	t.Parallel()
	cfg := ProviderConfig{
		DefaultPool:             "tank",
		DefaultNetworkInterface: "br0",
		DefaultBootMethod:       "UEFI",
	}

	t.Run("all empty", func(t *testing.T) {
		d := Data{}
		d.ApplyDefaults(cfg)

		assert.Equal(t, 2, d.CPUs)
		assert.Equal(t, 4096, d.Memory)
		assert.Equal(t, 40, d.DiskSize)
		assert.Equal(t, "tank", d.Pool)
		assert.Equal(t, "br0", d.NetworkInterface)
		assert.Equal(t, "UEFI", d.BootMethod)
		assert.Equal(t, "amd64", d.Architecture)
	})

	t.Run("custom values preserved", func(t *testing.T) {
		d := Data{
			CPUs:             8,
			Memory:           16384,
			DiskSize:         200,
			Pool:             "fast-nvme",
			NetworkInterface: "vlan100",
			BootMethod:       "BIOS",
			Architecture:     "arm64",
		}
		d.ApplyDefaults(cfg)

		assert.Equal(t, 8, d.CPUs)
		assert.Equal(t, 16384, d.Memory)
		assert.Equal(t, 200, d.DiskSize)
		assert.Equal(t, "fast-nvme", d.Pool)
		assert.Equal(t, "vlan100", d.NetworkInterface)
		assert.Equal(t, "BIOS", d.BootMethod)
		assert.Equal(t, "arm64", d.Architecture)
	})

	t.Run("different pools per machine class", func(t *testing.T) {
		// Simulates two MachineClasses targeting different pools:
		// control plane on NVMe, workers on HDD
		controlPlane := Data{Pool: "fast-nvme", CPUs: 2, Memory: 2048, DiskSize: 10}
		worker := Data{Pool: "bulk-hdd", CPUs: 4, Memory: 8192, DiskSize: 100}
		controlPlane.ApplyDefaults(cfg)
		worker.ApplyDefaults(cfg)

		assert.Equal(t, "fast-nvme", controlPlane.Pool, "control plane should use fast-nvme pool")
		assert.Equal(t, "bulk-hdd", worker.Pool, "worker should use bulk-hdd pool")

		// Verify zvol paths would be constructed on the correct pools
		assert.Equal(t, "fast-nvme/omni-vms/cp-1", controlPlane.Pool+"/omni-vms/cp-1")
		assert.Equal(t, "bulk-hdd/omni-vms/worker-1", worker.Pool+"/omni-vms/worker-1")
	})

	t.Run("boot method falls back to UEFI", func(t *testing.T) {
		emptyCfg := ProviderConfig{DefaultPool: "tank"}
		d := Data{}
		d.ApplyDefaults(emptyCfg)

		assert.Equal(t, "UEFI", d.BootMethod)
	})

	t.Run("extensions preserved through defaults", func(t *testing.T) {
		d := Data{
			Extensions: []string{"siderolabs/iscsi-tools", "siderolabs/drbd"},
		}
		d.ApplyDefaults(cfg)

		assert.Equal(t, []string{"siderolabs/iscsi-tools", "siderolabs/drbd"}, d.Extensions)
	})

	t.Run("nil extensions stays nil", func(t *testing.T) {
		d := Data{}
		d.ApplyDefaults(cfg)

		assert.Nil(t, d.Extensions)
	})
}

func TestValidate_AdditionalDisks(t *testing.T) {
	t.Parallel()

	t.Run("valid additional disks", func(t *testing.T) {
		d := Data{
			Pool:             "tank",
			NetworkInterface: "br0",
			AdditionalDisks: []AdditionalDisk{
				{Size: 100, Pool: "ssd"},
				{Size: 200},
				{Size: 50, Encrypted: true},
			},
		}
		assert.NoError(t, d.Validate())
	})

	t.Run("zero size rejected", func(t *testing.T) {
		d := Data{
			Pool:             "tank",
			NetworkInterface: "br0",
			AdditionalDisks:  []AdditionalDisk{{Size: 0}},
		}
		err := d.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "size must be > 0")
	})

	t.Run("negative size rejected", func(t *testing.T) {
		d := Data{
			Pool:             "tank",
			NetworkInterface: "br0",
			AdditionalDisks:  []AdditionalDisk{{Size: -10}},
		}
		assert.Error(t, d.Validate())
	})

	t.Run("unsafe pool name rejected", func(t *testing.T) {
		d := Data{
			Pool:             "tank",
			NetworkInterface: "br0",
			AdditionalDisks:  []AdditionalDisk{{Size: 100, Pool: "../escape"}},
		}
		err := d.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsafe characters")
	})

	t.Run("too many disks rejected", func(t *testing.T) {
		disks := make([]AdditionalDisk, 17)
		for i := range disks {
			disks[i] = AdditionalDisk{Size: 10}
		}

		d := Data{
			Pool:             "tank",
			NetworkInterface: "br0",
			AdditionalDisks:  disks,
		}
		err := d.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "maximum 16")
	})

	t.Run("empty additional disks valid", func(t *testing.T) {
		d := Data{Pool: "tank", NetworkInterface: "br0"}
		assert.NoError(t, d.Validate())
	})
}

func TestData_AdditionalDisksPreserved(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{DefaultPool: "tank", DefaultNetworkInterface: "br0"}

	d := Data{
		AdditionalDisks: []AdditionalDisk{
			{Size: 100, Pool: "ssd"},
			{Size: 500, Encrypted: true},
		},
	}
	d.ApplyDefaults(cfg)

	assert.Len(t, d.AdditionalDisks, 2)
	assert.Equal(t, 100, d.AdditionalDisks[0].Size)
	assert.Equal(t, "ssd", d.AdditionalDisks[0].Pool)
	assert.Equal(t, 500, d.AdditionalDisks[1].Size)
	assert.True(t, d.AdditionalDisks[1].Encrypted)
}

func TestData_AdditionalDisks_PoolDefaultsToEmpty(t *testing.T) {
	t.Parallel()

	// When pool is omitted, it should remain empty in the struct.
	// The provisioner is responsible for defaulting to the primary pool at runtime.
	d := Data{
		Pool:            "tank",
		AdditionalDisks: []AdditionalDisk{{Size: 50}},
	}

	assert.Empty(t, d.AdditionalDisks[0].Pool)
}

func TestData_EmptyAdditionalDisks(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{DefaultPool: "tank"}
	d := Data{}
	d.ApplyDefaults(cfg)

	assert.Nil(t, d.AdditionalDisks)
}

func TestExtensionMerge(t *testing.T) {
	t.Parallel()
	t.Run("defaults only", func(t *testing.T) {
		data := Data{}
		extensions := make([]string, 0, len(defaultExtensions)+len(data.Extensions))
		extensions = append(extensions, defaultExtensions...)
		extensions = append(extensions, data.Extensions...)

		assert.Equal(t, []string{
			"siderolabs/qemu-guest-agent",
			"siderolabs/nfs-utils",
			"siderolabs/util-linux-tools",
		}, extensions)
	})

	t.Run("defaults plus custom", func(t *testing.T) {
		data := Data{
			Extensions: []string{"siderolabs/iscsi-tools", "siderolabs/drbd"},
		}
		extensions := make([]string, 0, len(defaultExtensions)+len(data.Extensions))
		extensions = append(extensions, defaultExtensions...)
		extensions = append(extensions, data.Extensions...)

		assert.Equal(t, []string{
			"siderolabs/qemu-guest-agent",
			"siderolabs/nfs-utils",
			"siderolabs/util-linux-tools",
			"siderolabs/iscsi-tools",
			"siderolabs/drbd",
		}, extensions)
		assert.Len(t, extensions, 5)
	})

	t.Run("no duplicates from user matching defaults", func(t *testing.T) {
		data := Data{
			Extensions: []string{"siderolabs/qemu-guest-agent"}, // user adds one that's already default
		}
		extensions := make([]string, 0, len(defaultExtensions)+len(data.Extensions))
		extensions = append(extensions, defaultExtensions...)
		extensions = append(extensions, data.Extensions...)

		// Currently allows duplicates — the Image Factory deduplicates
		assert.Len(t, extensions, 4)
		assert.Contains(t, extensions, "siderolabs/qemu-guest-agent")
	})
}
