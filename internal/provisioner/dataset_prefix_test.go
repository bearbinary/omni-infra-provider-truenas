package provisioner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestData_BasePath_NoPrefix(t *testing.T) {
	t.Parallel()
	d := Data{Pool: "default"}
	assert.Equal(t, "default", d.BasePath())
}

func TestData_BasePath_SingleSegment(t *testing.T) {
	t.Parallel()
	d := Data{Pool: "default", DatasetPrefix: "myproject"}
	assert.Equal(t, "default/myproject", d.BasePath())
}

func TestData_BasePath_MultiSegment(t *testing.T) {
	t.Parallel()
	d := Data{Pool: "default", DatasetPrefix: "previewk8/k8"}
	assert.Equal(t, "default/previewk8/k8", d.BasePath())
}

func TestData_BasePath_DeepNesting(t *testing.T) {
	t.Parallel()
	d := Data{Pool: "tank", DatasetPrefix: "org/team/env/cluster"}
	assert.Equal(t, "tank/org/team/env/cluster", d.BasePath())
}

func TestData_BasePath_ZvolPath(t *testing.T) {
	t.Parallel()
	d := Data{Pool: "default", DatasetPrefix: "previewk8/k8"}
	zvolPath := d.BasePath() + "/omni-vms/test-request-123"
	assert.Equal(t, "default/previewk8/k8/omni-vms/test-request-123", zvolPath)
}

func TestData_BasePath_ISOPath(t *testing.T) {
	t.Parallel()
	d := Data{Pool: "default", DatasetPrefix: "previewk8/k8"}
	isoDataset := d.BasePath() + "/talos-iso"
	assert.Equal(t, "default/previewk8/k8/talos-iso", isoDataset)

	isoMountPath := "/mnt/" + isoDataset + "/abc123.iso"
	assert.Equal(t, "/mnt/default/previewk8/k8/talos-iso/abc123.iso", isoMountPath)
}

func TestData_Validate_DatasetPrefix_Valid(t *testing.T) {
	t.Parallel()
	valid := []struct {
		name   string
		prefix string
	}{
		{"single segment", "myproject"},
		{"two segments", "previewk8/k8"},
		{"deep nesting", "org/team/env"},
		{"with hyphens", "my-project/dev-env"},
		{"with underscores", "my_project/dev_env"},
		{"with dots", "v1.0/staging"},
	}

	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			d := Data{Pool: "default", NetworkInterface: "br100", DatasetPrefix: tc.prefix}
			assert.NoError(t, d.Validate())
		})
	}
}

func TestData_Validate_DatasetPrefix_Invalid(t *testing.T) {
	t.Parallel()
	invalid := []struct {
		name   string
		prefix string
		errMsg string
	}{
		{"empty segment", "a//b", "empty segment"},
		{"leading slash", "/a/b", "empty segment"},
		{"trailing slash", "a/b/", "empty segment"},
		{"path traversal", "../etc", "unsafe characters"},
		{"space in segment", "my project/env", "unsafe characters"},
		{"special chars", "pool$(whoami)/env", "unsafe characters"},
		{"leading hyphen segment", "a/-b", "unsafe characters"},
	}

	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			d := Data{Pool: "default", NetworkInterface: "br100", DatasetPrefix: tc.prefix}
			err := d.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errMsg)
		})
	}
}

func TestData_Validate_DatasetPrefix_Empty(t *testing.T) {
	t.Parallel()
	// Empty prefix is valid — it means no prefix
	d := Data{Pool: "default", NetworkInterface: "br100", DatasetPrefix: ""}
	assert.NoError(t, d.Validate())
}

func TestData_ApplyDefaults_PrefixPreserved(t *testing.T) {
	t.Parallel()
	cfg := ProviderConfig{DefaultPool: "tank", DefaultNetworkInterface: "br0"}
	d := Data{DatasetPrefix: "previewk8/k8"}
	d.ApplyDefaults(cfg)

	assert.Equal(t, "tank", d.Pool)
	assert.Equal(t, "previewk8/k8", d.DatasetPrefix)
	assert.Equal(t, "tank/previewk8/k8", d.BasePath())
}

func TestData_DifferentPrefixesSamePool(t *testing.T) {
	t.Parallel()
	// Two MachineClasses using the same pool but different prefixes
	staging := Data{Pool: "default", DatasetPrefix: "staging/k8s"}
	prod := Data{Pool: "default", DatasetPrefix: "prod/k8s"}

	assert.Equal(t, "default/staging/k8s", staging.BasePath())
	assert.Equal(t, "default/prod/k8s", prod.BasePath())
	assert.NotEqual(t, staging.BasePath(), prod.BasePath())

	// Zvol paths are fully isolated
	assert.Equal(t, "default/staging/k8s/omni-vms/req-1", staging.BasePath()+"/omni-vms/req-1")
	assert.Equal(t, "default/prod/k8s/omni-vms/req-1", prod.BasePath()+"/omni-vms/req-1")
}
