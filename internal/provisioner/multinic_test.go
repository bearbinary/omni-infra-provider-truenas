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

// --- Static addressing / gateway / DHCP opt-out ---

func TestValidate_StaticAddress_Valid(t *testing.T) {
	t.Parallel()
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", Addresses: []string{"10.20.0.5/24"}},
			{NetworkInterface: "br201", Addresses: []string{"10.30.0.5/24", "fd00::5/64"}},
		},
	}

	err := d.Validate()
	require.NoError(t, err)
}

func TestValidate_StaticAddress_NotCIDR(t *testing.T) {
	t.Parallel()
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", Addresses: []string{"10.20.0.5"}},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid CIDR")
}

func TestValidate_StaticAddress_Junk(t *testing.T) {
	t.Parallel()
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", Addresses: []string{"not-an-ip/24"}},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
}

func TestValidate_StaticAddress_Unspecified(t *testing.T) {
	t.Parallel()
	// 0.0.0.0/0 / ::/0 — default-route CIDR — is rejected. Security F1 fix:
	// a malicious operator could steer every worker's default traffic out
	// of a secondary NIC without touching the gateway field.
	for _, bad := range []string{"0.0.0.0/0", "::/0", "0.0.0.0/32", "::/128"} {
		d := Data{AdditionalNICs: []AdditionalNIC{{NetworkInterface: "br200", Addresses: []string{bad}}}}
		err := d.Validate()
		assert.Errorf(t, err, "address %q must be rejected", bad)
	}
}

func TestValidate_StaticAddress_Multicast(t *testing.T) {
	t.Parallel()
	d := Data{AdditionalNICs: []AdditionalNIC{{NetworkInterface: "br200", Addresses: []string{"224.0.0.1/24"}}}}
	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multicast")
}

func TestValidate_StaticAddress_Loopback(t *testing.T) {
	t.Parallel()
	d := Data{AdditionalNICs: []AdditionalNIC{{NetworkInterface: "br200", Addresses: []string{"127.0.0.1/8"}}}}
	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "loopback")
}

func TestValidate_Gateway_Valid(t *testing.T) {
	t.Parallel()
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", Addresses: []string{"10.20.0.5/24"}, Gateway: "10.20.0.1"},
		},
	}

	err := d.Validate()
	require.NoError(t, err)
}

func TestValidate_Gateway_InvalidIP(t *testing.T) {
	t.Parallel()
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", Addresses: []string{"10.20.0.5/24"}, Gateway: "not-an-ip"},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid IP")
}

func TestValidate_Gateway_NonUnicast(t *testing.T) {
	t.Parallel()
	// Unspecified, multicast, loopback, broadcast — all rejected. Security F2.
	cases := []struct{ gw, want string }{
		{"0.0.0.0", "unicast"},
		{"224.0.0.1", "unicast"},
		{"127.0.0.1", "unicast"},
		{"255.255.255.255", "unicast"},
		{"ff02::1", "unicast"},
	}

	for _, tc := range cases {
		d := Data{AdditionalNICs: []AdditionalNIC{{NetworkInterface: "br200", Addresses: []string{"10.20.0.5/24"}, Gateway: tc.gw}}}
		err := d.Validate()
		assert.Errorf(t, err, "gateway %q must be rejected", tc.gw)
		if err != nil {
			assert.Contains(t, err.Error(), tc.want)
		}
	}
}

func TestValidate_Gateway_FamilyMismatch(t *testing.T) {
	t.Parallel()
	// IPv4 addresses + IPv6 gateway — Security F2 / QA F4 fix. Without this,
	// the builder would emit {"network":"0.0.0.0/0","gateway":"fd00::1"}.
	d := Data{AdditionalNICs: []AdditionalNIC{{NetworkInterface: "br200", Addresses: []string{"10.20.0.5/24"}, Gateway: "fd00::1"}}}
	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IPv6")
	assert.Contains(t, err.Error(), "family")
}

func TestValidate_Gateway_NotOnLink(t *testing.T) {
	t.Parallel()
	// Gateway must be in at least one of the Addresses' CIDRs. Otherwise
	// Talos/Linux refuses to install the route.
	d := Data{AdditionalNICs: []AdditionalNIC{{NetworkInterface: "br200", Addresses: []string{"10.20.0.5/24"}, Gateway: "10.99.0.1"}}}
	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "on-link")
}

func TestValidate_Gateway_WithoutAddresses_Rejected(t *testing.T) {
	t.Parallel()
	// A gateway without a static address is meaningless — DHCP supplies
	// its own gateway, and a static-only route without a link address
	// can't be installed. Fail fast rather than ship a broken patch.
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", Gateway: "10.20.0.1"},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "gateway")
	assert.Contains(t, err.Error(), "addresses")
}

func TestValidate_MultipleGateways_Rejected(t *testing.T) {
	t.Parallel()
	// Only one NIC may declare a gateway. Two default routes without
	// distinct metrics cause non-deterministic kernel routing — Security F5.
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", Addresses: []string{"10.20.0.5/24"}, Gateway: "10.20.0.1"},
			{NetworkInterface: "br201", Addresses: []string{"10.30.0.5/24"}, Gateway: "10.30.0.1"},
		},
	}

	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at most one NIC may declare a gateway")
}

func TestValidate_DHCPFalse_WithNoAddresses_Allowed(t *testing.T) {
	t.Parallel()
	// Explicit opt-out: user wants the link attached but no autoconfig.
	// Valid — they may plan to configure it via a separate config patch,
	// or they intend to make it a bond slave.
	b := false
	d := Data{
		AdditionalNICs: []AdditionalNIC{
			{NetworkInterface: "br200", DHCP: &b},
		},
	}

	err := d.Validate()
	require.NoError(t, err)
}

func TestValidate_AdditionalNICs_ExceedMax(t *testing.T) {
	t.Parallel()
	// Operator-input DoS cap — Security F3.
	nics := make([]AdditionalNIC, MaxAdditionalNICs+1)
	for i := range nics {
		nics[i] = AdditionalNIC{NetworkInterface: fmt.Sprintf("br%d", 200+i)}
	}

	d := Data{AdditionalNICs: nics}
	err := d.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at most")
}

func TestValidate_AddressesPerNIC_ExceedMax(t *testing.T) {
	t.Parallel()
	// Per-NIC address cap — Security F3.
	addrs := make([]string, MaxAddressesPerNIC+1)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("10.20.%d.5/24", i)
	}

	d := Data{AdditionalNICs: []AdditionalNIC{{NetworkInterface: "br200", Addresses: addrs}}}
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
