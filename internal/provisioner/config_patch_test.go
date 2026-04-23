package provisioner

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAdvertisedSubnetsPatch_SingleIPv4(t *testing.T) {
	t.Parallel()

	data, err := buildAdvertisedSubnetsPatch("192.168.100.0/24")
	require.NoError(t, err)
	require.NotNil(t, data)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	// Verify etcd
	etcd := patch["cluster"].(map[string]any)["etcd"].(map[string]any)
	subnets := etcd["advertisedSubnets"].([]any)
	assert.Equal(t, []any{"192.168.100.0/24"}, subnets)

	// Verify kubelet
	kubelet := patch["machine"].(map[string]any)["kubelet"].(map[string]any)
	nodeIP := kubelet["nodeIP"].(map[string]any)
	validSubnets := nodeIP["validSubnets"].([]any)
	assert.Equal(t, []any{"192.168.100.0/24"}, validSubnets)
}

func TestBuildAdvertisedSubnetsPatch_DualStack(t *testing.T) {
	t.Parallel()

	data, err := buildAdvertisedSubnetsPatch("192.168.100.0/24,fd00::/64")
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	etcd := patch["cluster"].(map[string]any)["etcd"].(map[string]any)
	subnets := etcd["advertisedSubnets"].([]any)
	assert.Len(t, subnets, 2)
	assert.Equal(t, "192.168.100.0/24", subnets[0])
	assert.Equal(t, "fd00::/64", subnets[1])
}

func TestBuildAdvertisedSubnetsPatch_Empty(t *testing.T) {
	t.Parallel()

	data, err := buildAdvertisedSubnetsPatch("")
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestBuildAdvertisedSubnetsPatch_WhitespaceOnly(t *testing.T) {
	t.Parallel()

	data, err := buildAdvertisedSubnetsPatch("  ,  , ")
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestBuildAdvertisedSubnetsPatch_InvalidCIDR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"no prefix length", "192.168.100.1"},
		{"invalid prefix length", "192.168.100.0/99"},
		{"not an IP", "notanip/24"},
		{"empty prefix", "/24"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := buildAdvertisedSubnetsPatch(tc.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not a valid CIDR")
		})
	}
}

func TestBuildAdvertisedSubnetsPatch_TrimsSpaces(t *testing.T) {
	t.Parallel()

	data, err := buildAdvertisedSubnetsPatch("  192.168.100.0/24 , 10.0.0.0/8  ")
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	etcd := patch["cluster"].(map[string]any)["etcd"].(map[string]any)
	subnets := etcd["advertisedSubnets"].([]any)
	assert.Equal(t, []any{"192.168.100.0/24", "10.0.0.0/8"}, subnets)
}

func TestBuildAdvertisedSubnetsPatch_JSONStructure(t *testing.T) {
	t.Parallel()

	data, err := buildAdvertisedSubnetsPatch("10.0.0.0/8")
	require.NoError(t, err)

	// Verify the JSON matches what Talos expects
	expected := `{"cluster":{"etcd":{"advertisedSubnets":["10.0.0.0/8"]}},"machine":{"kubelet":{"nodeIP":{"validSubnets":["10.0.0.0/8"]}}}}`

	var got, want any
	json.Unmarshal(data, &got)
	json.Unmarshal([]byte(expected), &want)
	assert.Equal(t, want, got)
}

// TestBuildKubeletSubnetsPatch_OmitsEtcd pins the worker-safe patch shape:
// ONLY `machine.kubelet.nodeIP.validSubnets`, no `cluster.etcd` anywhere.
// A regression that sneaks etcd back in (e.g., a refactor that merges
// builders) will trigger the exact Talos validation failure that bricked
// every worker in v0.15.0–v0.15.3:
//
//	configuration validation failed: 1 error occurred:
//	* v1alpha1.Config: 1 error occurred:
//	* etcd config is only allowed on control plane machines
func TestBuildKubeletSubnetsPatch_OmitsEtcd(t *testing.T) {
	t.Parallel()

	data, err := buildKubeletSubnetsPatch("10.0.0.0/8,192.168.1.0/24")
	require.NoError(t, err)
	require.NotNil(t, data)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	// No `cluster` key at all — if one appears, a future refactor has
	// sneaked etcd back into the worker path.
	_, hasCluster := patch["cluster"]
	assert.False(t, hasCluster, "worker patch must not contain a cluster.* section — Talos will reject it")

	// Machine section must carry the kubelet pinning.
	machine := patch["machine"].(map[string]any)
	kubelet := machine["kubelet"].(map[string]any)
	nodeIP := kubelet["nodeIP"].(map[string]any)
	subnets := nodeIP["validSubnets"].([]any)
	assert.Equal(t, []any{"10.0.0.0/8", "192.168.1.0/24"}, subnets)
}

func TestBuildKubeletSubnetsPatch_Empty(t *testing.T) {
	t.Parallel()

	data, err := buildKubeletSubnetsPatch("")
	assert.NoError(t, err)
	assert.Nil(t, data)
}

func TestBuildKubeletSubnetsPatch_InvalidCIDR(t *testing.T) {
	t.Parallel()

	_, err := buildKubeletSubnetsPatch("not-a-cidr")
	assert.Error(t, err, "invalid CIDRs must surface as errors, not be silently dropped — matches buildAdvertisedSubnetsPatch semantics")
}

// --- MTU Config Patch Tests ---

func TestBuildMTUPatch_SingleNIC(t *testing.T) {
	t.Parallel()

	data, err := buildMTUPatch([]nicMTUConfig{
		{mac: "00:a0:98:12:34:56", mtu: 9000},
	})
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	machine := patch["machine"].(map[string]any)
	network := machine["network"].(map[string]any)
	interfaces := network["interfaces"].([]any)

	require.Len(t, interfaces, 1)

	iface := interfaces[0].(map[string]any)
	selector := iface["deviceSelector"].(map[string]any)
	assert.Equal(t, "00:a0:98:12:34:56", selector["hardwareAddr"])
	assert.Equal(t, float64(9000), iface["mtu"])
}

func TestBuildMTUPatch_MultipleNICs(t *testing.T) {
	t.Parallel()

	data, err := buildMTUPatch([]nicMTUConfig{
		{mac: "00:a0:98:11:11:11", mtu: 9000},
		{mac: "00:a0:98:22:22:22", mtu: 1500},
	})
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	interfaces := patch["machine"].(map[string]any)["network"].(map[string]any)["interfaces"].([]any)
	assert.Len(t, interfaces, 2)
}

func TestBuildMTUPatch_JSONStructure(t *testing.T) {
	t.Parallel()

	data, err := buildMTUPatch([]nicMTUConfig{
		{mac: "aa:bb:cc:dd:ee:ff", mtu: 9000},
	})
	require.NoError(t, err)

	expected := `{"machine":{"network":{"interfaces":[{"deviceSelector":{"hardwareAddr":"aa:bb:cc:dd:ee:ff"},"mtu":9000}]}}}`

	var got, want any
	json.Unmarshal(data, &got)
	json.Unmarshal([]byte(expected), &want)
	assert.Equal(t, want, got)
}

// Regression for v0.14.3–v0.14.5: every CreateConfigPatch call site used a
// static name like "data-volumes", "nic-mtu", "advertised-subnets". The
// SDK's CreateConfigPatch uses the literal name as the resource ID and
// upserts on every reconcile, so 6 MachineRequests writing the same name
// produced 1 surviving ConfigPatchRequest — labeled for whichever request
// reconciled last. The other 5 machines silently went without their patch.
// patchName() now produces unique-per-request resource names. These tests
// pin the contract so any future call site can't accidentally regress.
func TestPatchName_IncludesRequestID(t *testing.T) {
	t.Parallel()

	got := patchName("data-volumes", "talos-preview-workers-crpngp")
	assert.Equal(t, "data-volumes-talos-preview-workers-crpngp", got)
	assert.Contains(t, got, "talos-preview-workers-crpngp",
		"patch name must include the request ID — without it, names collide across MachineRequests and only the last writer's patch survives")
}

func TestPatchName_DistinctAcrossRequests(t *testing.T) {
	t.Parallel()

	a := patchName("data-volumes", "request-a")
	b := patchName("data-volumes", "request-b")
	assert.NotEqual(t, a, b,
		"two MachineRequests using the same patch kind must produce different ConfigPatchRequest names — otherwise their patches collide and only one survives in Omni state")
}

func TestPatchName_DistinctAcrossKinds(t *testing.T) {
	t.Parallel()

	id := "talos-preview-workers-crpngp"
	dataVol := patchName("data-volumes", id)
	mtu := patchName("nic-mtu", id)
	ifaces := patchName("nic-interfaces", id)
	subnets := patchName("advertised-subnets", id)
	longhorn := patchName("longhorn-ops", id)

	names := []string{dataVol, mtu, ifaces, subnets, longhorn}
	seen := make(map[string]bool)
	for _, n := range names {
		assert.False(t, seen[n], "duplicate patch name across kinds: %q — each (kind, request) pair must map to a unique resource name", n)
		seen[n] = true
	}
}

// Regression for the silent Longhorn-on-ephemeral-disk bug that lived from
// v0.13.0 through v0.14.2: the install-longhorn.sh script had `source: /var/lib/longhorn`
// (same as destination) — a no-op self-bind that left Longhorn writing to
// Talos's ephemeral root partition instead of the dedicated data zvol. From
// v0.14.6 the provider emits the operational patch itself whenever a disk
// is named "longhorn" (set implicitly by storage_disk_size, explicitly by
// the user). These tests pin every property that makes the patch work.
func TestLonghornOperationalPatch_BindMountSourceIsDataDisk(t *testing.T) {
	t.Parallel()

	data, err := buildLonghornOperationalPatch()
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	machine := patch["machine"].(map[string]any)
	kubelet := machine["kubelet"].(map[string]any)
	mounts := kubelet["extraMounts"].([]any)
	require.Len(t, mounts, 1, "exactly one extraMount for /var/lib/longhorn")

	mount := mounts[0].(map[string]any)
	assert.Equal(t, "/var/lib/longhorn", mount["destination"],
		"destination must be /var/lib/longhorn — Longhorn's containers expect this exact path")
	assert.Equal(t, "/var/mnt/longhorn", mount["source"],
		"source MUST be /var/mnt/longhorn (the data-zvol mount), NOT /var/lib/longhorn — "+
			"a source==destination self-bind was the v0.13.0–v0.14.2 bug where Longhorn silently "+
			"ran on Talos's ephemeral root disk. Do NOT let this regress.")
	assert.NotEqual(t, mount["source"], mount["destination"],
		"source and destination must differ — a self-bind is a no-op that lets Longhorn write to ephemeral root")
	assert.Equal(t, "bind", mount["type"])
}

func TestLonghornOperationalPatch_BindMountOptions(t *testing.T) {
	t.Parallel()

	data, err := buildLonghornOperationalPatch()
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	mount := patch["machine"].(map[string]any)["kubelet"].(map[string]any)["extraMounts"].([]any)[0].(map[string]any)
	opts := mount["options"].([]any)

	optSet := make(map[string]bool)
	for _, o := range opts {
		optSet[o.(string)] = true
	}

	assert.True(t, optSet["bind"], "bind option required — this is a bind mount")
	assert.True(t, optSet["rshared"],
		"rshared required — Longhorn CSI driver mounts volumes into pods via this path; without rshared, mount propagation breaks and pods can't attach volumes")
	assert.True(t, optSet["rw"], "rw required — Longhorn needs to write replica data")
}

func TestLonghornOperationalPatch_IncludesIscsiTcpKernelModule(t *testing.T) {
	t.Parallel()

	data, err := buildLonghornOperationalPatch()
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	machine := patch["machine"].(map[string]any)
	kernel, ok := machine["kernel"].(map[string]any)
	require.True(t, ok, "patch must include machine.kernel section")

	modules, ok := kernel["modules"].([]any)
	require.True(t, ok, "kernel.modules must be a list")
	require.NotEmpty(t, modules, "at least one kernel module must be loaded")

	var found bool
	for _, m := range modules {
		if m.(map[string]any)["name"] == "iscsi_tcp" {
			found = true

			break
		}
	}

	assert.True(t, found,
		"iscsi_tcp kernel module MUST be loaded — Longhorn uses iSCSI to attach replicas to pods. "+
			"Without this module, PVCs stay Pending forever and Longhorn's manager logs show iSCSI session failures.")
}

func TestLonghornOperationalPatch_SetsVMOvercommitSysctl(t *testing.T) {
	t.Parallel()

	data, err := buildLonghornOperationalPatch()
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	sysctls, ok := patch["machine"].(map[string]any)["sysctls"].(map[string]any)
	require.True(t, ok, "patch must include machine.sysctls")

	assert.Equal(t, "1", sysctls["vm.overcommit_memory"],
		"vm.overcommit_memory=1 is a Longhorn requirement for replica process stability under memory pressure")
}

func TestHasLonghornDisk_DetectsByName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		disks []AdditionalDisk
		want  bool
	}{
		{"empty list", nil, false},
		{"single non-longhorn disk", []AdditionalDisk{{Size: 100, Name: "data-1"}}, false},
		{"single longhorn disk", []AdditionalDisk{{Size: 100, Name: "longhorn"}}, true},
		{"mixed — longhorn first", []AdditionalDisk{{Size: 100, Name: "longhorn"}, {Size: 50, Name: "cache"}}, true},
		{"mixed — longhorn last", []AdditionalDisk{{Size: 50, Name: "cache"}, {Size: 100, Name: "longhorn"}}, true},
		{"case-sensitive Longhorn is NOT a match", []AdditionalDisk{{Size: 100, Name: "Longhorn"}}, false},
		{"empty name is not longhorn", []AdditionalDisk{{Size: 100, Name: ""}}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, hasLonghornDisk(tc.disks))
		})
	}
}

// Regression for v0.14.3 storage_disk_size→longhorn expansion: if someone
// renames the reserved volume name later, the shorthand stops auto-emitting
// the operational patch. This test pins the constant to the exact string
// Longhorn's defaultDataPath / Helm chart expects in the mount path.
func TestLonghornVolumeName_MatchesMountPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "longhorn", LonghornVolumeName,
		"LonghornVolumeName must equal \"longhorn\" — changing it breaks the implicit contract that /var/mnt/<name> = /var/mnt/longhorn, which is what the Longhorn operational patch's bind mount source points to. If you change this, also update buildLonghornOperationalPatch.")
}

// --- Additional-NIC Interfaces Config Patch Tests ---
//
// Regression for v0.15.5: the provider attached additional NICs to the VM
// at the hypervisor but emitted no machine-config patch to configure them.
// Talos only DHCPs the primary link by default, so eth1 came up with
// link-local IPv6 only — VMs were effectively single-homed despite being
// provisioned multi-NIC. Observed on talos-home workers where
// `talosctl get addresses` showed eth1 with only fe80::/64. Fixed by
// emitting a per-NIC deviceSelector patch that carries dhcp, addresses,
// and an optional default-route gateway.

func TestResolveNICDHCP_NilDefaultsToTrue(t *testing.T) {
	t.Parallel()

	assert.True(t, resolveNICDHCP(AdditionalNIC{NetworkInterface: "br200"}),
		"nil DHCP defaults to true (golden-path) — DHCP on or the link stays IPv4-less (the v0.15.5 bug)")
}

func TestResolveNICDHCP_ExplicitTrueWins(t *testing.T) {
	t.Parallel()

	nic := AdditionalNIC{NetworkInterface: "br200", DHCP: boolPtr(true)}
	assert.True(t, resolveNICDHCP(nic), "explicit DHCP: true matches default — documents intent")
}

func TestResolveNICDHCP_ExplicitFalseWins(t *testing.T) {
	t.Parallel()

	nic := AdditionalNIC{NetworkInterface: "br200", DHCP: boolPtr(false)}
	assert.False(t, resolveNICDHCP(nic),
		"explicit DHCP: false keeps DHCP off — advanced users opt the NIC out of autoconfig (bond slave, VLAN parent, manual patch)")
}

func TestBuildAdditionalNICInterfacesPatch_SingleDHCPNIC(t *testing.T) {
	t.Parallel()

	data, err := buildAdditionalNICInterfacesPatch([]nicInterfaceConfig{
		{mac: "02:cb:66:73:43:7b", dhcp: true},
	})
	require.NoError(t, err)
	require.NotNil(t, data)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	interfaces := patch["machine"].(map[string]any)["network"].(map[string]any)["interfaces"].([]any)
	require.Len(t, interfaces, 1)

	iface := interfaces[0].(map[string]any)
	selector := iface["deviceSelector"].(map[string]any)
	assert.Equal(t, "02:cb:66:73:43:7b", selector["hardwareAddr"])
	assert.Equal(t, true, iface["dhcp"])
	_, hasAddresses := iface["addresses"]
	assert.False(t, hasAddresses, "patch must never carry addresses[] — MachineClass is shared, static IPs would collide across workers")
	_, hasRoutes := iface["routes"]
	assert.False(t, hasRoutes, "patch must never carry routes[] — static gateways would collide across workers")
}

func TestBuildAdditionalNICInterfacesPatch_DHCPFalse(t *testing.T) {
	t.Parallel()

	data, err := buildAdditionalNICInterfacesPatch([]nicInterfaceConfig{
		{mac: "02:aa:aa:aa:aa:aa", dhcp: false},
	})
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	iface := patch["machine"].(map[string]any)["network"].(map[string]any)["interfaces"].([]any)[0].(map[string]any)
	assert.Equal(t, false, iface["dhcp"],
		"dhcp: false must serialize as a literal false — the advanced-user opt-out path (bond slave / VLAN parent / manual patch)")
}

func TestBuildAdditionalNICInterfacesPatch_MultipleNICsMixed(t *testing.T) {
	t.Parallel()

	data, err := buildAdditionalNICInterfacesPatch([]nicInterfaceConfig{
		{mac: "02:11:11:11:11:11", dhcp: true},
		{mac: "02:22:22:22:22:22", dhcp: false},
		{mac: "02:33:33:33:33:33", dhcp: true},
	})
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	interfaces := patch["machine"].(map[string]any)["network"].(map[string]any)["interfaces"].([]any)
	assert.Len(t, interfaces, 3)
}

func TestBuildAdditionalNICInterfacesPatch_Empty(t *testing.T) {
	t.Parallel()

	data, err := buildAdditionalNICInterfacesPatch(nil)
	require.NoError(t, err)
	assert.Nil(t, data, "empty NIC list must return nil so the caller skips CreateConfigPatch — otherwise we create an empty patch resource every reconcile")
}

func TestBuildAdditionalNICInterfacesPatch_SkipsEmptyMAC(t *testing.T) {
	t.Parallel()

	// If one NIC attach failed to return a MAC, don't poison the whole
	// patch — skip the empty entry and emit the patch for the remaining
	// NICs. An empty hardwareAddr selector would match every interface
	// and apply this config to the primary NIC too.
	data, err := buildAdditionalNICInterfacesPatch([]nicInterfaceConfig{
		{mac: "02:aa:aa:aa:aa:aa", dhcp: true},
		{mac: "", dhcp: true},
		{mac: "02:bb:bb:bb:bb:bb", dhcp: true},
	})
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	interfaces := patch["machine"].(map[string]any)["network"].(map[string]any)["interfaces"].([]any)
	assert.Len(t, interfaces, 2, "empty MAC must be dropped, not emitted as an open-match selector")
}

func TestBuildAdditionalNICInterfacesPatch_AllEmptyReturnsNil(t *testing.T) {
	t.Parallel()

	data, err := buildAdditionalNICInterfacesPatch([]nicInterfaceConfig{
		{mac: "", dhcp: true},
	})
	require.NoError(t, err)
	assert.Nil(t, data, "if every MAC is empty there's nothing to patch — return nil so the caller skips the empty create")
}

func TestBuildAdditionalNICInterfacesPatch_JSONStructure(t *testing.T) {
	t.Parallel()

	data, err := buildAdditionalNICInterfacesPatch([]nicInterfaceConfig{
		{mac: "aa:bb:cc:dd:ee:ff", dhcp: true},
	})
	require.NoError(t, err)

	expected := `{"machine":{"network":{"interfaces":[{"deviceSelector":{"hardwareAddr":"aa:bb:cc:dd:ee:ff"},"dhcp":true}]}}}`

	var got, want any
	json.Unmarshal(data, &got)
	json.Unmarshal([]byte(expected), &want)
	assert.Equal(t, want, got)
}

// --- collectNICInterfaceConfigs (caller-seam unit tests) ---
//
// Regression guard for the wiring in stepCreateVM. The per-NIC attach loop
// builds a parallel attachedMACs slice (one MAC per NIC index, "" on skip),
// then collectNICInterfaceConfigs turns the (NICs, MACs) pair into the
// patch-builder input plus the aggregate count used for the Info log. Unit
// tests here pin the seam behavior without needing a live provision.Context.

func TestCollectNICInterfaceConfigs_EmptyInputs(t *testing.T) {
	t.Parallel()

	configs, agg := collectNICInterfaceConfigs(nil, nil)
	assert.Nil(t, configs)
	assert.Equal(t, nicInterfaceAggregate{}, agg)
}

func TestCollectNICInterfaceConfigs_AllDHCP(t *testing.T) {
	t.Parallel()

	nics := []AdditionalNIC{
		{NetworkInterface: "br200"},
		{NetworkInterface: "br201"},
	}
	macs := []string{"02:aa:aa:aa:aa:aa", "02:bb:bb:bb:bb:bb"}

	configs, agg := collectNICInterfaceConfigs(nics, macs)

	require.Len(t, configs, 2)
	assert.Equal(t, "02:aa:aa:aa:aa:aa", configs[0].mac)
	assert.True(t, configs[0].dhcp)
	assert.Equal(t, nicInterfaceAggregate{DHCPNICs: 2}, agg)
}

func TestCollectNICInterfaceConfigs_MixedDHCPAndOptOut(t *testing.T) {
	t.Parallel()

	nics := []AdditionalNIC{
		{NetworkInterface: "br200"},                       // DHCP default
		{NetworkInterface: "br201", DHCP: boolPtr(false)}, // advanced opt-out
		{NetworkInterface: "br202", DHCP: boolPtr(true)},  // explicit on (same as default)
	}
	macs := []string{"02:aa:aa:aa:aa:aa", "02:bb:bb:bb:bb:bb", "02:cc:cc:cc:cc:cc"}

	configs, agg := collectNICInterfaceConfigs(nics, macs)

	require.Len(t, configs, 3)
	assert.Equal(t, 2, agg.DHCPNICs, "NIC 0 default + NIC 2 explicit true = 2 DHCP")
	assert.Equal(t, 1, agg.NoConfigNICs, "NIC 1 explicit false = opt-out counted under NoConfigNICs")
}

func TestCollectNICInterfaceConfigs_EmptyMACDroppedFromConfigsAndAggregate(t *testing.T) {
	t.Parallel()

	// Attach returning no MAC means this NIC is silently dropped from the
	// patch (the deviceSelector would otherwise open-match the primary NIC).
	// The aggregate must NOT count the dropped NIC.
	nics := []AdditionalNIC{
		{NetworkInterface: "br200"},
		{NetworkInterface: "br201", DHCP: boolPtr(false)}, // would be opt-out, but attach returned no MAC
		{NetworkInterface: "br202"},
	}
	macs := []string{"02:aa:aa:aa:aa:aa", "", "02:cc:cc:cc:cc:cc"}

	configs, agg := collectNICInterfaceConfigs(nics, macs)

	require.Len(t, configs, 2, "empty MAC entry must be skipped")
	assert.Equal(t, "02:aa:aa:aa:aa:aa", configs[0].mac)
	assert.Equal(t, "02:cc:cc:cc:cc:cc", configs[1].mac)
	assert.Equal(t, 2, agg.DHCPNICs)
	assert.Equal(t, 0, agg.NoConfigNICs,
		"the dropped NIC (br201) was an opt-out but never made it to the patch — aggregate must reflect what was actually applied")
}

func TestCollectNICInterfaceConfigs_UsesResolvedMACNotDeclaredInput(t *testing.T) {
	t.Parallel()

	// The MAC that lands in the patch MUST be the MAC returned by
	// client.AddNICWithConfig (post-collision-resolution), NOT anything
	// derived from the MachineClass. The caller gets this right by passing
	// the resolved MAC in attachedMACs[i]. Pin that contract here.
	nics := []AdditionalNIC{
		{NetworkInterface: "br200"},
	}
	macs := []string{"02:99:99:99:99:99"} // resolved/attached MAC

	configs, _ := collectNICInterfaceConfigs(nics, macs)
	require.Len(t, configs, 1)
	assert.Equal(t, "02:99:99:99:99:99", configs[0].mac,
		"patch MAC must come from attachedMACs (the resolved/post-collision MAC) — not from the declared NIC")
}

func TestCollectNICInterfaceConfigs_PanicsOnLengthMismatch(t *testing.T) {
	t.Parallel()

	assert.Panics(t, func() {
		collectNICInterfaceConfigs(
			[]AdditionalNIC{{NetworkInterface: "br200"}, {NetworkInterface: "br201"}},
			[]string{"02:aa:aa:aa:aa:aa"}, // only one MAC for two NICs
		)
	}, "length mismatch between nics and attachedMACs is a caller bug — panic rather than silently mis-pair")
}

// Pins the worker-safe shape: no cluster.* section. The v0.15.0–v0.15.3
// etcd-on-worker regression taught us that anything under cluster.* fails
// Talos validation on workers. This patch is cluster-free by design —
// applied to every multi-NIC machine regardless of role.
func TestBuildAdditionalNICInterfacesPatch_NoClusterSection(t *testing.T) {
	t.Parallel()

	data, err := buildAdditionalNICInterfacesPatch([]nicInterfaceConfig{
		{mac: "02:cb:66:73:43:7b", dhcp: true},
	})
	require.NoError(t, err)

	var patch map[string]any
	require.NoError(t, json.Unmarshal(data, &patch))

	_, hasCluster := patch["cluster"]
	assert.False(t, hasCluster, "interfaces patch must not contain a cluster.* section — Talos rejects cluster config on workers")
}
