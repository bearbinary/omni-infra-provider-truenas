package provisioner

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildNFSStoragePatch_ValidPatch(t *testing.T) {
	data, err := buildNFSStoragePatch("192.168.100.1", "/mnt/default/omni-nfs/cluster-abc")
	require.NoError(t, err)
	require.NotNil(t, data)

	// Verify it's valid JSON
	var patch map[string]any
	err = json.Unmarshal(data, &patch)
	require.NoError(t, err)

	// Verify structure
	cluster, ok := patch["cluster"].(map[string]any)
	require.True(t, ok, "patch should have cluster key")

	manifests, ok := cluster["inlineManifests"].([]any)
	require.True(t, ok, "cluster should have inlineManifests")
	require.Len(t, manifests, 1)

	manifest := manifests[0].(map[string]any)
	assert.Equal(t, "nfs-storage", manifest["name"])

	contents, ok := manifest["contents"].(string)
	require.True(t, ok, "manifest should have string contents")

	// Verify NFS server and path are templated in
	assert.Contains(t, contents, "192.168.100.1")
	assert.Contains(t, contents, "/mnt/default/omni-nfs/cluster-abc")

	// Verify key resources are present
	assert.Contains(t, contents, "kind: Namespace")
	assert.Contains(t, contents, "kind: ServiceAccount")
	assert.Contains(t, contents, "kind: ClusterRole")
	assert.Contains(t, contents, "kind: Deployment")
	assert.Contains(t, contents, "kind: StorageClass")
	assert.Contains(t, contents, "is-default-class")
	assert.Contains(t, contents, "nfs-subdir-external-provisioner")
}

func TestBuildNFSStoragePatch_EmptyServer_Error(t *testing.T) {
	_, err := buildNFSStoragePatch("", "/mnt/pool/nfs")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server address")
}

func TestBuildNFSStoragePatch_EmptyPath_Error(t *testing.T) {
	_, err := buildNFSStoragePatch("192.168.1.1", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NFS path")
}

func TestBuildNFSManifests_ContainsAllResources(t *testing.T) {
	manifests := buildNFSManifests("10.0.0.1", "/mnt/tank/nfs")

	// Should contain all required K8s resources
	expectedResources := []string{
		"kind: Namespace",
		"kind: ServiceAccount",
		"kind: ClusterRole\n",
		"kind: ClusterRoleBinding",
		"kind: Role\n",
		"kind: RoleBinding",
		"kind: Deployment",
		"kind: StorageClass",
	}

	for _, resource := range expectedResources {
		assert.Contains(t, manifests, resource, "manifests should contain %s", resource)
	}
}

func TestBuildNFSManifests_NamespaceIsCorrect(t *testing.T) {
	manifests := buildNFSManifests("10.0.0.1", "/mnt/tank/nfs")
	assert.Contains(t, manifests, "namespace: nfs-provisioner")
}

func TestBuildNFSManifests_ProvisionerName(t *testing.T) {
	manifests := buildNFSManifests("10.0.0.1", "/mnt/tank/nfs")
	assert.Contains(t, manifests, "provisioner: truenas.io/nfs")
	assert.Contains(t, manifests, "value: truenas.io/nfs")
}
