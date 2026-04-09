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
