package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- DeleteDevice ---

func TestDeleteDevice_Success(t *testing.T) {
	t.Parallel()
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.device.delete", method)
		return true, nil
	})

	err := c.DeleteDevice(context.Background(), 42)
	require.NoError(t, err)
}

func TestDeleteDevice_NotFound_Idempotent(t *testing.T) {
	t.Parallel()
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, notFoundErr()
	})

	err := c.DeleteDevice(context.Background(), 42)
	require.NoError(t, err, "deleting nonexistent device should be idempotent")
}

func TestDeleteDevice_OtherError(t *testing.T) {
	t.Parallel()
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, &jsonRPCError{Code: ErrCodeDenied, Message: "permission denied"}
	})

	err := c.DeleteDevice(context.Background(), 42)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vm.device.delete")
}

// --- ListFiles ---

func TestListFiles_Success(t *testing.T) {
	t.Parallel()
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "filesystem.listdir", method)
		return []FileEntry{
			{Name: "abc.iso", Path: "/mnt/tank/talos-iso/abc.iso", Type: "FILE"},
			{Name: "subdir", Path: "/mnt/tank/talos-iso/subdir", Type: "DIRECTORY"},
		}, nil
	})

	files, err := c.ListFiles(context.Background(), "/mnt/tank/talos-iso")
	require.NoError(t, err)
	assert.Len(t, files, 2)
	assert.Equal(t, "abc.iso", files[0].Name)
}

func TestListFiles_NotFound_ReturnsNil(t *testing.T) {
	t.Parallel()
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, notFoundErr()
	})

	files, err := c.ListFiles(context.Background(), "/mnt/tank/nonexistent")
	require.NoError(t, err)
	assert.Nil(t, files)
}

// --- ListChildDatasets ---

func TestListChildDatasets_Success(t *testing.T) {
	t.Parallel()
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "pool.dataset.query", method)
		// Verify the filter includes the parent path prefix
		assert.Contains(t, string(params), "default/omni-vms/")
		return []Dataset{
			{ID: "default/omni-vms/req-1", Name: "req-1", Type: "VOLUME"},
			{ID: "default/omni-vms/req-2", Name: "req-2", Type: "VOLUME"},
		}, nil
	})

	datasets, err := c.ListChildDatasets(context.Background(), "default/omni-vms")
	require.NoError(t, err)
	assert.Len(t, datasets, 2)
}

// --- RecreateDataset ---

func TestRecreateDataset_Success(t *testing.T) {
	t.Parallel()
	var deleteCalled, createCalled bool
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		switch method {
		case "pool.dataset.delete":
			deleteCalled = true
			return nil, nil
		case "pool.dataset.create":
			createCalled = true
			return &Dataset{ID: "tank/talos-iso"}, nil
		}
		return nil, nil
	})

	err := c.RecreateDataset(context.Background(), "tank/talos-iso")
	require.NoError(t, err)
	assert.True(t, deleteCalled, "should delete dataset first")
	assert.True(t, createCalled, "should recreate dataset after delete")
}

func TestRecreateDataset_DeleteFails(t *testing.T) {
	t.Parallel()
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		if method == "pool.dataset.delete" {
			return nil, &jsonRPCError{Code: ErrCodeDenied, Message: "denied"}
		}
		return nil, nil
	})

	err := c.RecreateDataset(context.Background(), "tank/talos-iso")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete dataset")
}

// --- GetVMInstance ---

func TestGetVMInstance_Success(t *testing.T) {
	t.Parallel()
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.get_instance", method)
		return &VMInstanceInfo{
			ID:   42,
			Name: "omni_test",
			Status: struct {
				State string `json:"state"`
				Pid   int    `json:"pid"`
			}{State: "RUNNING", Pid: 12345},
		}, nil
	})

	info, err := c.GetVMInstance(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, 42, info.ID)
	assert.Equal(t, "RUNNING", info.Status.State)
}
