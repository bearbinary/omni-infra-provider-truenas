package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

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

func TestSnapshotBeforeUpgrade_FailureNonFatal(t *testing.T) {
	// Snapshot failure should NOT block the upgrade
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "zfs.snapshot.create" {
			return nil, errors.New("snapshot failed")
		}

		return nil, nil
	})

	logger, _ := zap.NewDevelopment()

	// Should not panic or return error
	p.snapshotBeforeUpgrade(context.Background(), logger, "tank/test", "v1.12.4", "v1.12.5")
}

func TestEnforceSnapshotRetention_ListFails_NonFatal(t *testing.T) {
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "zfs.snapshot.query" {
			return nil, errors.New("list failed")
		}

		return nil, nil
	})

	// Should not panic
	p.enforceSnapshotRetention(context.Background(), testLogger(), "tank/test", 3)
}

func TestEnforceSnapshotRetention_DeleteFails_NonFatal(t *testing.T) {
	// Delete failure for individual snapshots should not stop retention
	var deleteAttempts atomic.Int32
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "zfs.snapshot.query" {
			return []client.Snapshot{
				{ID: "tank/test@omni-snap-1", Name: "omni-snap-1"},
				{ID: "tank/test@omni-snap-2", Name: "omni-snap-2"},
				{ID: "tank/test@omni-snap-3", Name: "omni-snap-3"},
				{ID: "tank/test@omni-snap-4", Name: "omni-snap-4"},
			}, nil
		}

		if method == "zfs.snapshot.delete" {
			deleteAttempts.Add(1)

			return nil, errors.New("delete failed")
		}

		return nil, nil
	})

	p.enforceSnapshotRetention(context.Background(), testLogger(), "tank/test", 3)

	assert.Equal(t, int32(1), deleteAttempts.Load(), "should attempt to delete 1 snapshot (4 - 3 = 1)")
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
