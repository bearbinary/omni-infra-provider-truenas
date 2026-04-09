package provisioner

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKnownFields_ContainsAllStructFields(t *testing.T) {
	t.Parallel()

	known := knownFields()

	expectedFields := []string{
		"pool", "network_interface", "boot_method", "architecture",
		"extensions", "encrypted", "cpus", "memory", "disk_size",
		"dataset_prefix", "additional_disks", "additional_nics", "advertised_subnets",
	}

	for _, f := range expectedFields {
		assert.True(t, known[f], "field %q should be in knownFields()", f)
	}
}

func TestUnknownFields_NoUnknown(t *testing.T) {
	t.Parallel()

	rawData := map[string]any{
		"pool":      "default",
		"cpus":      2,
		"memory":    4096,
		"disk_size": 40,
	}

	unknown := UnknownFields(rawData)
	assert.Empty(t, unknown)
}

func TestUnknownFields_DetectsUnknown(t *testing.T) {
	t.Parallel()

	rawData := map[string]any{
		"pool":       "default",
		"cpus":       2,
		"gpu_count":  1,       // Unknown
		"nic_attach": "br100", // Old field name — should be detected
	}

	unknown := UnknownFields(rawData)
	sort.Strings(unknown)
	assert.Equal(t, []string{"gpu_count", "nic_attach"}, unknown)
}

func TestUnknownFields_EmptyData(t *testing.T) {
	t.Parallel()

	unknown := UnknownFields(map[string]any{})
	assert.Empty(t, unknown)
}

func TestUnknownFields_AllKnown(t *testing.T) {
	t.Parallel()

	rawData := map[string]any{
		"pool":               "default",
		"network_interface":  "br100",
		"boot_method":        "UEFI",
		"architecture":       "amd64",
		"extensions":         []string{},
		"encrypted":          false,
		"cpus":               4,
		"memory":             8192,
		"disk_size":          100,
		"dataset_prefix":     "prod/k8s",
		"additional_disks":   []any{},
		"additional_nics":    []any{},
		"advertised_subnets": "192.168.1.0/24",
	}

	unknown := UnknownFields(rawData)
	assert.Empty(t, unknown)
}

func TestUnknownFields_DetectsOldFieldNames(t *testing.T) {
	t.Parallel()

	// Simulate a user who hasn't updated their MachineClass config
	// after the nic_attach → network_interface rename
	rawData := map[string]any{
		"pool":       "default",
		"nic_attach": "br100", // Old name
	}

	unknown := UnknownFields(rawData)
	assert.Contains(t, unknown, "nic_attach", "should detect old field name after rename")
}
