package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bearbinary/omni-infra-provider-truenas/api/specs"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

// --- Chaos: mid-operation failures ---

func TestCheckExistingVM_GetVM_TransientError(t *testing.T) {
	// Simulate a transient error from TrueNAS (not "not found", not an API error)
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.query" {
			return nil, errors.New("connection reset by peer")
		}

		return nil, nil
	})

	state := &specs.MachineSpec{VmId: 42}
	result := p.checkExistingVM(context.Background(), testLogger(), state, "omni_test")

	require.NotNil(t, result, "transient error should return an error")
	assert.Error(t, *result)
	assert.Contains(t, (*result).Error(), "failed to get VM")
}

func TestMaybeResizeZvol_GetSizeFails_NonFatal(t *testing.T) {
	// If we can't check the size, resize should be skipped (non-fatal)
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.dataset.query" {
			return nil, errors.New("timeout")
		}

		return nil, nil
	})

	err := p.maybeResizeZvol(context.Background(), testLogger(), "tank/test", 80)
	assert.NoError(t, err, "size check failure should be non-fatal")
}

func TestMaybeResizeZvol_ResizeFails_Fatal(t *testing.T) {
	// If the actual resize fails, that IS fatal
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.dataset.query" {
			return map[string]any{
				"volsize": map[string]any{"parsed": int64(40 * 1024 * 1024 * 1024)},
			}, nil
		}

		if method == "pool.dataset.update" {
			return nil, &client.APIError{Code: 28, Message: "[ENOSPC] pool is full"}
		}

		return nil, nil
	})

	err := p.maybeResizeZvol(context.Background(), testLogger(), "tank/test", 80)
	assert.Error(t, err, "resize failure should be fatal")
	assert.Contains(t, err.Error(), "failed to resize zvol")
}

func TestCleanupAdditionalZvol_NotFound_Idempotent(t *testing.T) {
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.dataset.delete" {
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		}

		return nil, nil
	})

	// Additional zvol already gone — should not error
	err := p.cleanupZvol(context.Background(), testLogger(), "ssd/omni-vms/test-disk-1")
	assert.NoError(t, err, "deleting nonexistent additional zvol should be idempotent")
}

func TestCleanupAdditionalZvol_DeleteFails_Fatal(t *testing.T) {
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.dataset.delete" {
			return nil, errors.New("disk I/O error")
		}

		return nil, nil
	})

	err := p.cleanupZvol(context.Background(), testLogger(), "ssd/omni-vms/test-disk-1")
	assert.Error(t, err, "real delete failure should be fatal")
}

func TestCleanupVM_StopFails_ContinuesDelete(t *testing.T) {
	deleted := false
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "vm.stop" {
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		}

		if method == "vm.delete" {
			deleted = true

			return true, nil
		}

		return nil, nil
	})

	err := p.cleanupVM(context.Background(), testLogger(), 42)
	require.NoError(t, err)
	assert.True(t, deleted, "should still delete VM even if stop returns not found")
}

func TestCleanupZvol_NotFound_Idempotent(t *testing.T) {
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.dataset.delete" {
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		}

		return nil, nil
	})

	err := p.cleanupZvol(context.Background(), testLogger(), "tank/nonexistent")
	assert.NoError(t, err, "deleting nonexistent zvol should be idempotent")
}
