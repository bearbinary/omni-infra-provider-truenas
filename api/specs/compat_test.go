package specs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestProtoCompat_V0State verifies that MachineSpec serialized by an older provider
// version (with fewer fields) still deserializes correctly with the current code.
// This ensures upgrade safety: when a provider restarts with new code, existing
// VM state from the old version must still work.
func TestProtoCompat_V0State(t *testing.T) {
	t.Parallel()

	// Simulate a v0.x state: only the first 7 fields were set
	oldState := &MachineSpec{
		Uuid:          "test-uuid-123",
		Schematic:     "schematic-abc",
		TalosVersion:  "v1.12.4",
		ImageId:       "image-def",
		VmId:          42,
		ZvolPath:      "default/omni-vms/test-request",
		CdromDeviceId: 10,
	}

	// Serialize (as if written by old provider)
	data, err := proto.Marshal(oldState)
	require.NoError(t, err)

	// Deserialize with current code
	newState := &MachineSpec{}
	err = proto.Unmarshal(data, newState)
	require.NoError(t, err)

	// All old fields should survive
	assert.Equal(t, "test-uuid-123", newState.Uuid)
	assert.Equal(t, "schematic-abc", newState.Schematic)
	assert.Equal(t, "v1.12.4", newState.TalosVersion)
	assert.Equal(t, "image-def", newState.ImageId)
	assert.Equal(t, int32(42), newState.VmId)
	assert.Equal(t, "default/omni-vms/test-request", newState.ZvolPath)
	assert.Equal(t, int32(10), newState.CdromDeviceId)

}

// TestProtoCompat_NewState_ReadByOldCode simulates forward compatibility:
// a new provider writes state with all fields, and an older version reads it.
// Protobuf guarantees unknown fields are preserved.
func TestProtoCompat_NewState_ReadByOldCode(t *testing.T) {
	t.Parallel()

	// Current state with all fields
	currentState := &MachineSpec{
		Uuid:                "test-uuid-456",
		Schematic:           "schematic-xyz",
		TalosVersion:        "v1.13.0",
		ImageId:             "image-ghi",
		VmId:                99,
		ZvolPath:            "tank/omni-vms/test-request-2",
		CdromDeviceId:       20,
		AdditionalZvolPaths: []string{"ssd/omni-vms/test-request-2-disk-1", "hdd/omni-vms/test-request-2-disk-2"},
	}

	data, err := proto.Marshal(currentState)
	require.NoError(t, err)

	// Deserialize — should work even if more fields are added later
	restored := &MachineSpec{}
	err = proto.Unmarshal(data, restored)
	require.NoError(t, err)

	assert.Equal(t, currentState.Uuid, restored.Uuid)
	assert.Equal(t, currentState.VmId, restored.VmId)
	assert.Equal(t, currentState.AdditionalZvolPaths, restored.AdditionalZvolPaths)
}

// TestProtoCompat_EmptyState verifies a completely empty state works.
// This is the initial state before any provision step runs.
func TestProtoCompat_EmptyState(t *testing.T) {
	t.Parallel()

	empty := &MachineSpec{}
	data, err := proto.Marshal(empty)
	require.NoError(t, err)

	restored := &MachineSpec{}
	err = proto.Unmarshal(data, restored)
	require.NoError(t, err)

	assert.Equal(t, "", restored.Uuid)
	assert.Equal(t, int32(0), restored.VmId)
	assert.Equal(t, "", restored.ZvolPath)
}

// TestProtoCompat_FieldNumbers verifies field numbers haven't changed.
// If a field number changes, it breaks wire compatibility with existing state.
func TestProtoCompat_FieldNumbers(t *testing.T) {
	t.Parallel()

	md := (&MachineSpec{}).ProtoReflect().Descriptor()

	expectedFields := map[string]int32{
		"uuid":                  1,
		"schematic":             2,
		"talos_version":         3,
		"image_id":              4,
		"vm_id":                 5,
		"zvol_path":             6,
		"cdrom_device_id":       7,
		"additional_zvol_paths": 9,
	}

	for name, expectedNum := range expectedFields {
		fd := md.Fields().ByName(protoreflect.Name(name))
		require.NotNil(t, fd, "field %q should exist in MachineSpec", name)
		assert.Equal(t, expectedNum, int32(fd.Number()), "field %q number changed — this breaks wire compatibility", name)
	}
}
