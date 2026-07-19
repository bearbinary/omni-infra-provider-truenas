package provisioner

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuildPatches_TalosStructuralValidation asserts, for every build*Patch
// output, that (a) the bytes decode as valid JSON and (b) the top-level keys
// match the surface Talos expects a strategic-merge patch to touch.
//
// A fuller round-trip through
// github.com/siderolabs/talos/pkg/machinery/config/configloader would require
// each patch to be a full machine config document; strategic-merge patches are
// intentionally partial so `NewFromBytes` rejects them with "missing version"
// or "missing kind". Instead we assert structural well-formedness plus the
// specific top-level layout each patch commits to. That is enough to catch
// gross schema breakage — a field rename like machine.network.interfaces →
// machine.network.iface would flip these to fail immediately, whereas the
// current unit tests only check the pre-rename layout matched itself.
//
// docs/testing.md self-documents this gap as the class of drift that caused
// the v0.15.0–v0.15.3 etcd-on-worker regression: a patch shape the provider
// happily emitted, Talos rejected at bootstrap, and no test in this repo
// caught before shipping.
func TestBuildPatches_TalosStructuralValidation(t *testing.T) {
	t.Parallel()

	// Each case pairs a patch-generator invocation with the top-level path
	// (dot-notation) that MUST exist in the emitted JSON. Missing means Talos
	// would receive an empty or non-mergeable document at bootstrap.
	tests := []struct {
		name     string
		gen      func(t *testing.T) []byte
		wantKeys [][]string // each inner slice is a required nested-key path
	}{
		{
			name: "MTUPatch/SingleNIC",
			gen: func(t *testing.T) []byte {
				t.Helper()

				b, err := buildMTUPatch([]nicMTUConfig{{mac: "aa:bb:cc:dd:ee:ff", mtu: 9000}})
				require.NoError(t, err)
				require.NotNil(t, b)

				return b
			},
			wantKeys: [][]string{
				{"machine", "network", "interfaces"},
			},
		},
		{
			name: "MTUPatch/MultipleNICs",
			gen: func(t *testing.T) []byte {
				t.Helper()

				b, err := buildMTUPatch([]nicMTUConfig{
					{mac: "aa:bb:cc:dd:ee:ff", mtu: 9000},
					{mac: "11:22:33:44:55:66", mtu: 1500},
				})
				require.NoError(t, err)
				require.NotNil(t, b)

				return b
			},
			wantKeys: [][]string{
				{"machine", "network", "interfaces"},
			},
		},
		{
			name: "AdvertisedSubnetsPatch/SingleIPv4",
			gen: func(t *testing.T) []byte {
				t.Helper()

				b, err := buildAdvertisedSubnetsPatch("192.168.100.0/24")
				require.NoError(t, err)
				require.NotNil(t, b)

				return b
			},
			wantKeys: [][]string{
				{"cluster", "etcd", "advertisedSubnets"},
				{"machine", "kubelet", "nodeIP", "validSubnets"},
			},
		},
		{
			name: "AdvertisedSubnetsPatch/DualStack",
			gen: func(t *testing.T) []byte {
				t.Helper()

				b, err := buildAdvertisedSubnetsPatch("192.168.100.0/24,fd00::/64")
				require.NoError(t, err)
				require.NotNil(t, b)

				return b
			},
			wantKeys: [][]string{
				{"cluster", "etcd", "advertisedSubnets"},
				{"machine", "kubelet", "nodeIP", "validSubnets"},
			},
		},
		{
			name: "KubeletSubnetsPatch",
			gen: func(t *testing.T) []byte {
				t.Helper()

				b, err := buildKubeletSubnetsPatch("10.0.0.0/24")
				require.NoError(t, err)
				require.NotNil(t, b)

				return b
			},
			wantKeys: [][]string{
				{"machine", "kubelet", "nodeIP", "validSubnets"},
			},
		},
		{
			name: "AdditionalNICInterfacesPatch/DHCP",
			gen: func(t *testing.T) []byte {
				t.Helper()

				b, err := buildAdditionalNICInterfacesPatch([]nicInterfaceConfig{
					{mac: "02:00:00:00:00:01", dhcp: true},
				})
				require.NoError(t, err)
				require.NotNil(t, b)

				return b
			},
			wantKeys: [][]string{
				{"machine", "network", "interfaces"},
			},
		},
		{
			name: "AdditionalNICInterfacesPatch/NoDHCP",
			gen: func(t *testing.T) []byte {
				t.Helper()

				b, err := buildAdditionalNICInterfacesPatch([]nicInterfaceConfig{
					{mac: "02:00:00:00:00:02", dhcp: false},
				})
				require.NoError(t, err)
				require.NotNil(t, b)

				return b
			},
			wantKeys: [][]string{
				{"machine", "network", "interfaces"},
			},
		},
		{
			name: "LonghornOperationalPatch",
			gen: func(t *testing.T) []byte {
				t.Helper()

				b, err := buildLonghornOperationalPatch()
				require.NoError(t, err)
				require.NotNil(t, b)

				return b
			},
			wantKeys: [][]string{
				{"machine", "kernel", "modules"},
				{"machine", "kubelet", "extraMounts"},
				{"machine", "sysctls"},
			},
		},
	}

	for _, tc := range tests {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := tc.gen(t)

			// (a) Must decode as valid JSON. This alone catches nil-map
			// serialization surprises and character-encoding regressions
			// that the raw-string assertions in the existing test file
			// would miss.
			var doc map[string]any

			require.NoError(t, json.Unmarshal(data, &doc), "patch is not valid JSON")

			// (b) Every expected top-level nested key path MUST exist.
			// Missing means Talos strategic-merge would receive an empty
			// section and silently ignore the intent of the patch.
			for _, path := range tc.wantKeys {
				requireNestedKey(t, doc, path)
			}
		})
	}
}

// requireNestedKey walks doc along path (dot-notation) and fails t if any
// intermediate key is missing or has the wrong nested type.
func requireNestedKey(t *testing.T, doc map[string]any, path []string) {
	t.Helper()

	current := any(doc)

	for i, key := range path {
		m, ok := current.(map[string]any)
		require.Truef(t, ok, "path %v: element %d is not a JSON object (got %T)", path, i, current)

		next, present := m[key]
		require.Truef(t, present, "path %v: key %q missing at depth %d", path, key, i)

		current = next
	}
}
