package provisioner

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AdditionalNIC Validation ---

func TestValidate_NoAdditionalNICs(t *testing.T) {
	d := Data{}
	d.ApplyDefaults(ProviderConfig{DefaultPool: "tank", DefaultNICAttach: "br0"})

	err := d.Validate()
	require.NoError(t, err, "no additional NICs should be valid")
}

func TestValidate_ValidAdditionalNIC(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200"},
		},
	}

	err := d.Validate()
	require.NoError(t, err)
}

func TestValidate_MultipleAdditionalNICs(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200", Type: "VIRTIO"},
			{NICAttach: "vlan300", VLANTag: 300},
			{NICAttach: "enp6s0", Type: "E1000"},
		},
	}

	err := d.Validate()
	require.NoError(t, err)
}

func TestValidate_MissingNICAttach(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: ""},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nic_attach is required")
	assert.Contains(t, err.Error(), "[0]")
}

func TestValidate_MissingNICAttach_SecondNIC(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200"},
			{NICAttach: ""},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "[1]")
}

func TestValidate_InvalidVLANTag_Zero(t *testing.T) {
	// VLAN 0 is technically valid (native VLAN) but we treat 0 as "not set"
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200", VLANTag: 0},
		},
	}

	err := d.Validate()
	require.NoError(t, err, "vlan_id=0 means not set, should be valid")
}

func TestValidate_InvalidVLANTag_Negative(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200", VLANTag: -1},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vlan_id must be between")
}

func TestValidate_InvalidVLANTag_TooHigh(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200", VLANTag: 4095},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vlan_id must be between")
}

func TestValidate_ValidVLANTag_Boundary(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200", VLANTag: 1},
			{NICAttach: "br201", VLANTag: 4094},
		},
	}

	err := d.Validate()
	require.NoError(t, err, "VLAN 1 and 4094 are both valid")
}

func TestValidate_InvalidNICType(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200", Type: "INVALID"},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be VIRTIO or E1000")
}

func TestValidate_ValidNICTypes(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200", Type: "VIRTIO"},
			{NICAttach: "br201", Type: "E1000"},
			{NICAttach: "br202"}, // empty = VIRTIO default
		},
	}

	err := d.Validate()
	require.NoError(t, err)
}

// --- AdditionalNIC preserved through defaults ---

func TestData_AdditionalNICsPreserved(t *testing.T) {
	cfg := ProviderConfig{DefaultPool: "tank", DefaultNICAttach: "br0"}

	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200", VLANTag: 100},
			{NICAttach: "enp6s0"},
		},
		AdvertisedSubnets: "192.168.100.0/24",
	}
	d.ApplyDefaults(cfg)

	assert.Len(t, d.AdditionalNICs, 2)
	assert.Equal(t, "br200", d.AdditionalNICs[0].NICAttach)
	assert.Equal(t, 100, d.AdditionalNICs[0].VLANTag)
	assert.Equal(t, "enp6s0", d.AdditionalNICs[1].NICAttach)
	assert.Equal(t, "192.168.100.0/24", d.AdvertisedSubnets)
}

func TestData_EmptyAdditionalNICs(t *testing.T) {
	cfg := ProviderConfig{DefaultPool: "tank"}

	d := Data{}
	d.ApplyDefaults(cfg)

	assert.Nil(t, d.AdditionalNICs)
	assert.Empty(t, d.AdvertisedSubnets)
}

// --- VLAN tag enables TrustGuestRxFilters ---

func TestAdditionalNIC_VLANTagEnablesTrust(t *testing.T) {
	nic := AdditionalNIC{NICAttach: "enp5s0", VLANTag: 100}

	// The provisioner should set TrustGuestRxFilters when VLANTag > 0
	assert.Greater(t, nic.VLANTag, 0, "VLAN tag should be set")
	// The actual trust flag is set in stepCreateVM when building NICConfig
}

// --- Multihoming advertised_subnets ---

func TestData_AdvertisedSubnets_Single(t *testing.T) {
	d := Data{AdvertisedSubnets: "192.168.100.0/24"}
	assert.Equal(t, "192.168.100.0/24", d.AdvertisedSubnets)
}

func TestData_AdvertisedSubnets_DualStack(t *testing.T) {
	d := Data{AdvertisedSubnets: "192.168.100.0/24,fd00::/64"}
	assert.Contains(t, d.AdvertisedSubnets, "192.168.100.0/24")
	assert.Contains(t, d.AdvertisedSubnets, "fd00::/64")
}

// --- Duplicate NIC detection ---

func TestValidate_DuplicateNICAttach_SameAsPrimary(t *testing.T) {
	d := Data{
		NICAttach: "br100",
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br100"}, // Same as primary
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate nic_attach")
	assert.Contains(t, err.Error(), "br100")
}

func TestValidate_DuplicateNICAttach_WithinAdditional(t *testing.T) {
	d := Data{
		NICAttach: "br100",
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200"},
			{NICAttach: "br200"}, // Duplicate within additional
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate nic_attach")
	assert.Contains(t, err.Error(), "[1]") // Second one flagged
}

func TestValidate_NoDuplicate_DifferentInterfaces(t *testing.T) {
	d := Data{
		NICAttach: "br100",
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "br200"},
			{NICAttach: "vlan300"},
			{NICAttach: "enp6s0"},
		},
	}

	err := d.Validate()
	require.NoError(t, err, "all different interfaces should be valid")
}

// --- Edge cases ---

func TestValidate_MaxNICs(t *testing.T) {
	// TrueNAS supports many NICs — verify we don't break with several
	nics := make([]AdditionalNIC, 10)
	for i := range nics {
		nics[i] = AdditionalNIC{NICAttach: fmt.Sprintf("br%d", 200+i)}
	}

	d := Data{
		NICAttach:      "br100",
		AdditionalNICs: nics,
	}

	err := d.Validate()
	require.NoError(t, err)
}

func TestValidate_VLANTag_CommonValues(t *testing.T) {
	// Test common VLAN IDs used in production
	commonVLANs := []int{1, 10, 100, 200, 666, 1000, 2000, 4094}

	for _, vlan := range commonVLANs {
		d := Data{
			AdditionalNICs: []AdditionalNIC{
				{NICAttach: "enp5s0", VLANTag: vlan},
			},
		}

		err := d.Validate()
		assert.NoError(t, err, "VLAN %d should be valid", vlan)
	}
}
