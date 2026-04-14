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
	subnets := patchName("advertised-subnets", id)
	longhorn := patchName("longhorn-ops", id)

	names := []string{dataVol, mtu, subnets, longhorn}
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
