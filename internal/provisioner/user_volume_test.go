package provisioner

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// splitYAMLDocs returns each `---`-separated document as its own byte slice.
// Includes the leading document even though YAML doesn't require a leading
// separator — the encoder in buildUserVolumePatch emits one anyway.
func splitYAMLDocs(t *testing.T, data []byte) [][]byte {
	t.Helper()

	parts := [][]byte{}

	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			break
		}

		out, err := yaml.Marshal(&node)
		require.NoError(t, err)
		parts = append(parts, out)
	}

	return parts
}

func TestBuildUserVolumePatch_NoDisks(t *testing.T) {
	patch, err := buildUserVolumePatch(nil)
	require.NoError(t, err)
	assert.Nil(t, patch, "empty disk list must produce no patch so the caller can skip CreateConfigPatch")
}

func TestBuildUserVolumePatch_SingleDisk_Longhorn(t *testing.T) {
	patch, err := buildUserVolumePatch([]AdditionalDisk{
		{Size: 150, Name: "longhorn", Filesystem: "xfs"},
	})
	require.NoError(t, err)
	require.NotNil(t, patch)

	s := string(patch)
	assert.Contains(t, s, "apiVersion: v1alpha1")
	assert.Contains(t, s, "kind: UserVolumeConfig")
	assert.Contains(t, s, "name: longhorn")
	assert.Contains(t, s, "type: xfs")
	assert.Contains(t, s, "grow: true")
	assert.Contains(t, s, "!system_disk")

	// 150 GiB = 161061273600 bytes. ±1 MiB window.
	assert.Contains(t, s, "disk.size >= 161060225024u", "lower bound must be 150 GiB minus 1 MiB")
	assert.Contains(t, s, "disk.size <= 161062322176u", "upper bound must be 150 GiB plus 1 MiB")

	// Regression for v0.14.3–v0.14.5: emitting `maxSize: 0` made Talos
	// reject the document with "min size is greater than max size" because
	// it parses 0 as a literal byte count, not "unbounded". The correct
	// way to express "fill the disk" in Talos UserVolumeConfig is to omit
	// maxSize and rely on grow:true. Fail loudly if the key reappears.
	assert.NotContains(t, s, "maxSize",
		"maxSize must be omitted — emitting maxSize:0 fails Talos validation with 'min size is greater than max size'")
}

func TestBuildUserVolumePatch_MultipleDisks_DistinctSelectors(t *testing.T) {
	disks := []AdditionalDisk{
		{Size: 100, Name: "data-1", Filesystem: "xfs"},
		{Size: 200, Name: "data-2", Filesystem: "ext4"},
		{Size: 500, Name: "longhorn", Filesystem: "xfs"},
	}

	patch, err := buildUserVolumePatch(disks)
	require.NoError(t, err)

	docs := splitYAMLDocs(t, patch)
	require.Len(t, docs, 3, "one UserVolumeConfig document per additional disk")

	for i, doc := range docs {
		var parsed map[string]any
		require.NoError(t, yaml.Unmarshal(doc, &parsed), "doc %d must be valid YAML", i)

		assert.Equal(t, "v1alpha1", parsed["apiVersion"])
		assert.Equal(t, "UserVolumeConfig", parsed["kind"])
		assert.Equal(t, disks[i].Name, parsed["name"])

		fs := parsed["filesystem"].(map[string]any)
		assert.Equal(t, disks[i].Filesystem, fs["type"])

		prov := parsed["provisioning"].(map[string]any)
		selector := prov["diskSelector"].(map[string]any)["match"].(string)
		assert.Contains(t, selector, "!system_disk")

		// Each selector must embed its disk's exact byte range — not a shared
		// "size > 50" match — so Talos can uniquely assign disks to volumes
		// even when multiple data disks are attached.
		expectedBytes := int64(disks[i].Size) * gibiByte
		assert.Contains(t, selector, "disk.size >=")
		assert.Contains(t, selector, "disk.size <=")
		// Sanity: the expected size must fall inside the range the selector
		// describes. Parsing CEL here would be overkill; substring the exact
		// numeric bound instead.
		lowStr := lowBoundOf(expectedBytes)
		highStr := highBoundOf(expectedBytes)
		assert.Contains(t, selector, lowStr)
		assert.Contains(t, selector, highStr)
	}
}

func TestBuildUserVolumePatch_EmptyName_IsError(t *testing.T) {
	// ApplyDefaults is expected to fill Name before we get here. An empty
	// name indicates the provisioner skipped ApplyDefaults, which would
	// silently produce an invalid UserVolumeConfig — fail loudly instead.
	_, err := buildUserVolumePatch([]AdditionalDisk{{Size: 50}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is empty")
	assert.Contains(t, err.Error(), "ApplyDefaults")
}

func TestBuildUserVolumePatch_ZeroSize_IsError(t *testing.T) {
	_, err := buildUserVolumePatch([]AdditionalDisk{{Size: 0, Name: "longhorn"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size must be > 0")
}

func TestBuildUserVolumePatch_DefaultsFilesystemToXFS(t *testing.T) {
	patch, err := buildUserVolumePatch([]AdditionalDisk{
		{Size: 50, Name: "longhorn"}, // no Filesystem set
	})
	require.NoError(t, err)
	assert.Contains(t, string(patch), "type: xfs", "unspecified filesystem defaults to xfs")
}

// Helpers — keep test readable by name. Keep them in the test file to avoid
// exposing internal byte-math helpers in production code.
func lowBoundOf(bytes int64) string  { return toCELUint(bytes - diskSizeTolerance) }
func highBoundOf(bytes int64) string { return toCELUint(bytes + diskSizeTolerance) }
func toCELUint(n int64) string {
	// Matches the format used in buildUserVolumePatch: "%du".
	return formatUint(n) + "u"
}
func formatUint(n int64) string {
	if n < 0 {
		return "0"
	}

	// Avoid importing strconv just for this — stdlib sprintf is fine but we
	// already have enough in the test file.
	var buf [20]byte
	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	if i == len(buf) {
		i--
		buf[i] = '0'
	}

	return string(buf[i:])
}
