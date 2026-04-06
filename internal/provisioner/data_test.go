package provisioner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestData_ApplyDefaults(t *testing.T) {
	cfg := ProviderConfig{
		DefaultPool:       "tank",
		DefaultNICAttach:  "br0",
		DefaultBootMethod: "UEFI",
	}

	t.Run("all empty", func(t *testing.T) {
		d := Data{}
		d.ApplyDefaults(cfg)

		assert.Equal(t, 2, d.CPUs)
		assert.Equal(t, 4096, d.Memory)
		assert.Equal(t, 40, d.DiskSize)
		assert.Equal(t, "tank", d.Pool)
		assert.Equal(t, "br0", d.NICAttach)
		assert.Equal(t, "UEFI", d.BootMethod)
		assert.Equal(t, "amd64", d.Architecture)
	})

	t.Run("custom values preserved", func(t *testing.T) {
		d := Data{
			CPUs:         8,
			Memory:       16384,
			DiskSize:     200,
			Pool:         "fast-nvme",
			NICAttach:    "vlan100",
			BootMethod:   "BIOS",
			Architecture: "arm64",
		}
		d.ApplyDefaults(cfg)

		assert.Equal(t, 8, d.CPUs)
		assert.Equal(t, 16384, d.Memory)
		assert.Equal(t, 200, d.DiskSize)
		assert.Equal(t, "fast-nvme", d.Pool)
		assert.Equal(t, "vlan100", d.NICAttach)
		assert.Equal(t, "BIOS", d.BootMethod)
		assert.Equal(t, "arm64", d.Architecture)
	})

	t.Run("boot method falls back to UEFI", func(t *testing.T) {
		emptyCfg := ProviderConfig{DefaultPool: "tank"}
		d := Data{}
		d.ApplyDefaults(emptyCfg)

		assert.Equal(t, "UEFI", d.BootMethod)
	})
}
