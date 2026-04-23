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
	d.ApplyDefaults(ProviderConfig{DefaultPool: "tank", DefaultNetworkInterface: "br0"})

	err := d.Validate()
	require.NoError(t, err, "no additional NICs should be valid")
}

func TestValidate_ValidAdditionalNIC(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200"},
		},
	}

	err := d.Validate()
	require.NoError(t, err)
}

func TestValidate_MultipleAdditionalNICs(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", Type: "VIRTIO"},
			{NetworkInterface: "vlan300"},
			{NetworkInterface: "enp6s0", Type: "E1000"},
		},
	}

	err := d.Validate()
	require.NoError(t, err)
}

func TestValidate_MissingNetworkInterface(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: ""},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "network_interface is required")
	assert.Contains(t, err.Error(), "[0]")
}

func TestValidate_MissingNetworkInterface_SecondNIC(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200"},
			{NetworkInterface: ""},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "[1]")
}

func TestValidate_InvalidNICType(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", Type: "INVALID"},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be VIRTIO or E1000")
}

func TestValidate_ValidNICTypes(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", Type: "VIRTIO"},
			{NetworkInterface: "br201", Type: "E1000"},
			{NetworkInterface: "br202"}, // empty = VIRTIO default
		},
	}

	err := d.Validate()
	require.NoError(t, err)
}

// --- MTU Validation ---

func TestValidate_MTU_Valid(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", MTU: 9000},
			{NetworkInterface: "br201", MTU: 1500},
			{NetworkInterface: "br202", MTU: 576},  // Minimum
			{NetworkInterface: "br203", MTU: 9216}, // Maximum
			{NetworkInterface: "br204"},            // MTU 0 = default
		},
	}

	err := d.Validate()
	require.NoError(t, err)
}

func TestValidate_MTU_TooLow(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", MTU: 100},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mtu must be between 576 and 9216")
}

func TestValidate_MTU_TooHigh(t *testing.T) {
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", MTU: 10000},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mtu must be between 576 and 9216")
}

// --- AdditionalNIC preserved through defaults ---

func TestData_AdditionalNICsPreserved(t *testing.T) {
	cfg := ProviderConfig{DefaultPool: "tank", DefaultNetworkInterface: "br0"}

	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200"},
			{NetworkInterface: "enp6s0"},
		},
		AdvertisedSubnets: "192.168.100.0/24",
	}
	d.ApplyDefaults(cfg)

	assert.Len(t, d.AdditionalNICs, 2)
	assert.Equal(t, "br200", d.AdditionalNICs[0].NetworkInterface)
	assert.Equal(t, "enp6s0", d.AdditionalNICs[1].NetworkInterface)
	assert.Equal(t, "192.168.100.0/24", d.AdvertisedSubnets)
}

func TestData_EmptyAdditionalNICs(t *testing.T) {
	cfg := ProviderConfig{DefaultPool: "tank"}

	d := Data{}
	d.ApplyDefaults(cfg)

	assert.Nil(t, d.AdditionalNICs)
	assert.Empty(t, d.AdvertisedSubnets)
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

func TestValidate_DuplicateNetworkInterface_SameAsPrimary(t *testing.T) {
	d := Data{
		NetworkInterface: "br100",
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br100"}, // Same as primary
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate network_interface")
	assert.Contains(t, err.Error(), "br100")
}

func TestValidate_DuplicateNetworkInterface_WithinAdditional(t *testing.T) {
	d := Data{
		NetworkInterface: "br100",
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200"},
			{NetworkInterface: "br200"}, // Duplicate within additional
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate network_interface")
	assert.Contains(t, err.Error(), "[1]") // Second one flagged
}

func TestValidate_NoDuplicate_DifferentInterfaces(t *testing.T) {
	d := Data{
		NetworkInterface: "br100",
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200"},
			{NetworkInterface: "vlan300"},
			{NetworkInterface: "enp6s0"},
		},
	}

	err := d.Validate()
	require.NoError(t, err, "all different interfaces should be valid")
}

// --- DHCP tri-state + NIC cap ---

func TestValidate_DHCPTrue_Allowed(t *testing.T) {
	t.Parallel()

	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", DHCP: boolPtr(true)},
		},
	}

	require.NoError(t, d.Validate())
}

func TestValidate_DHCPFalse_Allowed(t *testing.T) {
	t.Parallel()

	// Explicit opt-out: advanced user wants the link attached but no
	// autoconfig — bond slave, VLAN parent, or they'll apply a separate
	// per-node config patch.
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", DHCP: boolPtr(false)},
		},
	}

	require.NoError(t, d.Validate())
}

func TestValidate_AdditionalNICs_ExceedMax(t *testing.T) {
	t.Parallel()

	// Operator-input DoS cap — 16 NICs is the TrueNAS practical ceiling.
	nics := make([]AdditionalNIC, MaxAdditionalNICs+1)
	for i := range nics {
		nics[i] = AdditionalNIC{NetworkInterface: fmt.Sprintf("br%d", 200+i)}
	}

	d := Data{AdditionalNICs: nics}
	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at most")
}

// --- Edge cases ---

func TestValidate_MaxNICs(t *testing.T) {
	// TrueNAS supports many NICs — verify we don't break with several
	nics := make([]AdditionalNIC, 10)
	for i := range nics {
		nics[i] = AdditionalNIC{NetworkInterface: fmt.Sprintf("br%d", 200+i)}
	}

	d := Data{
		NetworkInterface: "br100",
		AdditionalNICs:   nics,
	}

	err := d.Validate()
	require.NoError(t, err)
}
