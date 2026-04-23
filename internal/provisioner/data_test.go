package provisioner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		assert.Contains(t, err.Error(), "size must be >= 5")
	})

	t.Run("negative size rejected", func(t *testing.T) {
		d := Data{
			Pool:             "tank",
			NetworkInterface: "br0",
			AdditionalDisks:  []AdditionalDisk{{Size: -10}},
		}
		assert.Error(t, d.Validate())
	})

	t.Run("below minimum size rejected", func(t *testing.T) {
		d := Data{
			Pool:             "tank",
			NetworkInterface: "br0",
			AdditionalDisks:  []AdditionalDisk{{Size: 4}},
		}
		err := d.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "size must be >= 5")
	})

	t.Run("above maximum size rejected", func(t *testing.T) {
		d := Data{
			Pool:             "tank",
			NetworkInterface: "br0",
			AdditionalDisks:  []AdditionalDisk{{Size: MaxDiskSizeGiB + 1}},
		}
		err := d.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "size must be <=")
	})
}

// TestValidate_NumericBounds is the single source of truth for the knob
// bounds the v0.15 hardening pass added. Each sub-case exercises one field
// and both edges (negative + over-max) so regressions that remove a bound
// surface immediately.
func TestValidate_NumericBounds(t *testing.T) {
	t.Parallel()

	base := func() Data {
		return Data{
			Pool:             "tank",
			NetworkInterface: "br0",
		}
	}

	t.Run("cpus negative rejected", func(t *testing.T) {
		d := base()
		d.CPUs = -1
		require.Error(t, d.Validate())
	})

	t.Run("cpus above max rejected", func(t *testing.T) {
		d := base()
		d.CPUs = MaxCPUs + 1
		require.Error(t, d.Validate())
	})

	t.Run("memory negative rejected", func(t *testing.T) {
		d := base()
		d.Memory = -10
		require.Error(t, d.Validate())
	})

	t.Run("memory below minimum (but non-zero) rejected", func(t *testing.T) {
		d := base()
		d.Memory = MinMemoryMiB - 1
		require.Error(t, d.Validate())
	})

	t.Run("memory above max rejected", func(t *testing.T) {
		d := base()
		d.Memory = MaxMemoryMiB + 1
		require.Error(t, d.Validate())
	})

	t.Run("disk_size negative rejected", func(t *testing.T) {
		d := base()
		d.DiskSize = -1
		require.Error(t, d.Validate())
	})

	t.Run("disk_size above max rejected", func(t *testing.T) {
		d := base()
		d.DiskSize = MaxDiskSizeGiB + 1
		require.Error(t, d.Validate())
	})

	// disk_size floor pin (v0.16+ bumped from 5 → MinRootDiskSizeGiB). The old
	// 5 GiB floor let control-planes ship with too little space for the Talos
	// image + kube-apiserver/etcd/scheduler/controller-manager/CNI/CoreDNS
	// image pulls during bootstrap. An undersized root disk triggers kubelet
	// image GC mid-install and etcd never comes up — a silent failure mode
	// because the VM boots but the cluster never stabilizes.
	t.Run("disk_size below floor rejected", func(t *testing.T) {
		d := base()
		d.DiskSize = MinRootDiskSizeGiB - 1
		err := d.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "disk_size must be >=")
	})

	t.Run("disk_size at floor accepted", func(t *testing.T) {
		d := base()
		d.DiskSize = MinRootDiskSizeGiB
		require.NoError(t, d.Validate())
	})

	t.Run("disk_size 5 rejected (v0.16 floor bump from 5→20)", func(t *testing.T) {
		d := base()
		d.DiskSize = 5
		err := d.Validate()
		require.Error(t, err, "pre-v0.16 MachineClasses with disk_size=5 must now fail fast — the 5 GiB floor was insufficient for Talos + kube control-plane image pulls")
		require.Contains(t, err.Error(), ">=")
	})

	t.Run("storage_disk_size above max rejected", func(t *testing.T) {
		d := base()
		d.StorageDiskSize = MaxDiskSizeGiB + 1
		require.Error(t, d.Validate())
	})

	t.Run("zero values are accepted (rely on ApplyDefaults)", func(t *testing.T) {
		d := base()
		d.CPUs = 0
		d.Memory = 0
		d.DiskSize = 0
		d.StorageDiskSize = 0
		require.NoError(t, d.Validate())
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

func TestStorageDiskSize_ExpandsToAdditionalDisk(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{DefaultPool: "tank", DefaultNetworkInterface: "br0"}

	d := Data{StorageDiskSize: 100}
	d.ApplyDefaults(cfg)

	assert.Len(t, d.AdditionalDisks, 1)
	assert.Equal(t, 100, d.AdditionalDisks[0].Size)
	assert.Equal(t, 0, d.StorageDiskSize, "should be zeroed after expansion")
}

func TestStorageDiskSize_PrependsToExistingDisks(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{DefaultPool: "tank", DefaultNetworkInterface: "br0"}

	d := Data{
		StorageDiskSize: 50,
		AdditionalDisks: []AdditionalDisk{{Size: 200, Pool: "hdd"}},
	}
	d.ApplyDefaults(cfg)

	assert.Len(t, d.AdditionalDisks, 2)
	assert.Equal(t, 50, d.AdditionalDisks[0].Size, "storage disk should be first")
	assert.Equal(t, 200, d.AdditionalDisks[1].Size, "existing disk should be second")
	assert.Equal(t, "hdd", d.AdditionalDisks[1].Pool, "existing disk pool preserved")
}

func TestStorageDiskSize_ZeroDoesNotExpand(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{DefaultPool: "tank", DefaultNetworkInterface: "br0"}

	d := Data{StorageDiskSize: 0}
	d.ApplyDefaults(cfg)

	assert.Nil(t, d.AdditionalDisks)
}

// TestStorageDiskSize_ZeroValidates pins the contract that zero is a valid
// storage_disk_size meaning "no storage disk" — control planes and other
// node types that don't want Longhorn-style local storage should pass
// validation without supplying any value (zero is the schema default).
func TestStorageDiskSize_ZeroValidates(t *testing.T) {
	t.Parallel()

	d := Data{
		Pool:             "tank",
		NetworkInterface: "br0",
		StorageDiskSize:  0,
	}

	assert.NoError(t, d.Validate())
}

// TestStorageDiskSize_BelowMinimumRejected pins the lower bound: any non-zero
// value must meet MinDiskSizeGiB. Zero is the only sub-minimum value that
// passes because it signals "no disk".
func TestStorageDiskSize_BelowMinimumRejected(t *testing.T) {
	t.Parallel()

	d := Data{
		Pool:             "tank",
		NetworkInterface: "br0",
		StorageDiskSize:  4,
	}

	err := d.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage_disk_size must be >= 5")
}

// TestStorageDiskSize_ExpandsWithLonghornVolumeName pins the convention that
// storage_disk_size expands into an AdditionalDisk named "longhorn". The name
// becomes the mount path (/var/mnt/longhorn) which matches Longhorn's
// defaultDataPath — changing this breaks every existing Longhorn deployment
// that was provisioned through storage_disk_size.
func TestStorageDiskSize_ExpandsWithLonghornVolumeName(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{DefaultPool: "tank", DefaultNetworkInterface: "br0"}

	d := Data{StorageDiskSize: 150}
	d.ApplyDefaults(cfg)

	require.Len(t, d.AdditionalDisks, 1)
	assert.Equal(t, "longhorn", d.AdditionalDisks[0].Name,
		"storage_disk_size must expand with Name=longhorn so UserVolumeConfig mounts at /var/mnt/longhorn")
	assert.Equal(t, "xfs", d.AdditionalDisks[0].Filesystem)
}

func TestAdditionalDisks_DefaultsFillNameAndFilesystem(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{DefaultPool: "tank", DefaultNetworkInterface: "br0"}

	d := Data{
		AdditionalDisks: []AdditionalDisk{
			{Size: 100},                                   // no name, no fs
			{Size: 200, Name: "cache"},                    // named, no fs
			{Size: 300, Filesystem: "ext4"},               // fs, no name
			{Size: 400, Name: "logs", Filesystem: "ext4"}, // both
		},
	}
	d.ApplyDefaults(cfg)

	assert.Equal(t, "data-1", d.AdditionalDisks[0].Name, "index 0 defaults to data-1 (1-based)")
	assert.Equal(t, "xfs", d.AdditionalDisks[0].Filesystem)

	assert.Equal(t, "cache", d.AdditionalDisks[1].Name, "explicit name preserved")
	assert.Equal(t, "xfs", d.AdditionalDisks[1].Filesystem)

	assert.Equal(t, "data-3", d.AdditionalDisks[2].Name, "1-based index stays positional even when earlier disks are named")
	assert.Equal(t, "ext4", d.AdditionalDisks[2].Filesystem)

	assert.Equal(t, "logs", d.AdditionalDisks[3].Name)
	assert.Equal(t, "ext4", d.AdditionalDisks[3].Filesystem)
}

func TestValidate_AdditionalDisks_DuplicateNamesRejected(t *testing.T) {
	t.Parallel()

	d := Data{
		Pool:             "tank",
		NetworkInterface: "br0",
		AdditionalDisks: []AdditionalDisk{
			{Size: 100, Name: "longhorn"},
			{Size: 200, Name: "longhorn"},
		},
	}

	err := d.Validate()
	require.Error(t, err, "two disks with the same name would both try to mount at /var/mnt/longhorn")
	assert.Contains(t, err.Error(), "collides")
	assert.Contains(t, err.Error(), "longhorn")
}

func TestValidate_AdditionalDisks_UnsafeNameRejected(t *testing.T) {
	t.Parallel()

	d := Data{
		Pool:             "tank",
		NetworkInterface: "br0",
		AdditionalDisks: []AdditionalDisk{
			{Size: 100, Name: "../escape"},
		},
	}

	err := d.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsafe characters")
}

func TestValidate_AdditionalDisks_BadFilesystemRejected(t *testing.T) {
	t.Parallel()

	d := Data{
		Pool:             "tank",
		NetworkInterface: "br0",
		AdditionalDisks: []AdditionalDisk{
			{Size: 100, Name: "data-1", Filesystem: "btrfs"},
		},
	}

	err := d.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xfs")
	assert.Contains(t, err.Error(), "ext4")
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
			"siderolabs/util-linux-tools",
			"siderolabs/iscsi-tools",
		}, extensions)
	})

	t.Run("defaults plus custom", func(t *testing.T) {
		data := Data{
			Extensions: []string{"siderolabs/nfs-utils", "siderolabs/drbd"},
		}
		extensions := make([]string, 0, len(defaultExtensions)+len(data.Extensions))
		extensions = append(extensions, defaultExtensions...)
		extensions = append(extensions, data.Extensions...)

		assert.Equal(t, []string{
			"siderolabs/qemu-guest-agent",
			"siderolabs/util-linux-tools",
			"siderolabs/iscsi-tools",
			"siderolabs/nfs-utils",
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

// TestDefaultExtensions_RequiredEntries pins every Talos extension that the
// provider depends on. Each entry below corresponds to a runtime feature that
// silently breaks if the extension is removed:
//
//   - qemu-guest-agent: TrueNAS sends ACPI shutdown signals via the agent.
//     Without it, Deprovision can't gracefully stop VMs and falls back to
//     force-stop after the grace timeout — risks Talos data corruption.
//   - util-linux-tools: Talos disk-format and partition operations require
//     util-linux binaries (e.g. mkfs.xfs for UserVolumeConfig). Without
//     this, the data-volumes patch can't format the additional disk.
//   - iscsi-tools: Longhorn attaches replicas to pods over iSCSI. Without
//     the iSCSI initiator from this extension, PVCs stay Pending forever
//     and Longhorn manager logs show "iscsi: failed to start session".
//     Added to defaults in v0.14.0 specifically to fix this.
//
// If you intentionally remove one of these, also update the runtime
// behavior that relies on it AND the matching failure mode docs.
func TestDefaultExtensions_RequiredEntries(t *testing.T) {
	t.Parallel()

	required := map[string]string{
		"siderolabs/qemu-guest-agent": "ACPI shutdown signal — graceful VM stop on Deprovision",
		"siderolabs/util-linux-tools": "Talos disk-format operations (mkfs.xfs for UserVolumeConfig)",
		"siderolabs/iscsi-tools":      "Longhorn iSCSI replica attachment — without it, PVCs stay Pending",
	}

	for ext, why := range required {
		assert.Contains(t, defaultExtensions, ext,
			"required extension %q missing from defaultExtensions — needed for: %s", ext, why)
	}
}
