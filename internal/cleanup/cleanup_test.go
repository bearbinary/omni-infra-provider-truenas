package cleanup

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

func testCleaner(handler client.MockHandler, activeImages map[string]bool) *Cleaner {
	return &Cleaner{
		client: client.NewMockClient(handler),
		config: Config{Pool: "default", CleanupInterval: time.Hour},
		logger: zap.NewNop(),
		activeImageIDs: func() map[string]bool {
			return activeImages
		},
	}
}

// --- Run / lifecycle tests ---

func TestCleanerRun_CancelsOnContext(t *testing.T) {
	cl := &Cleaner{
		config:         Config{CleanupInterval: time.Hour},
		logger:         zap.NewNop(),
		activeImageIDs: func() map[string]bool { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		cl.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not exit after context cancellation")
	}
}

func TestNew_DefaultIntervals(t *testing.T) {
	c := client.NewMockClient(nil)
	cl := New(c, Config{Pool: "tank"}, zap.NewNop(),
		func() map[string]bool { return nil },
	)

	assert.Equal(t, time.Hour, cl.config.CleanupInterval)
	assert.Equal(t, 30*time.Minute, cl.config.OrphanGracePeriod)
}

func TestNew_CustomIntervals(t *testing.T) {
	c := client.NewMockClient(nil)
	cl := New(c, Config{Pool: "tank", CleanupInterval: 10 * time.Minute, OrphanGracePeriod: 5 * time.Minute}, zap.NewNop(),
		func() map[string]bool { return nil },
	)

	assert.Equal(t, 10*time.Minute, cl.config.CleanupInterval)
	assert.Equal(t, 5*time.Minute, cl.config.OrphanGracePeriod)
}

// --- cleanupISOs tests ---

func TestCleanupISOs_AllStale_RecreatesDataset(t *testing.T) {
	var recreated bool
	cl := testCleaner(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "filesystem.listdir":
			return []client.FileEntry{
				{Name: "abc123.iso", Type: "FILE"},
				{Name: "def456.iso", Type: "FILE"},
			}, nil
		case "pool.dataset.delete":
			return nil, nil
		case "pool.dataset.create":
			recreated = true
			return &client.Dataset{ID: "default/talos-iso"}, nil
		}
		return nil, nil
	}, map[string]bool{}) // No active images

	cl.cleanupISOs(context.Background())
	assert.True(t, recreated, "should recreate dataset when all ISOs are stale")
}

func TestCleanupISOs_SomeActive_SkipsCleanup(t *testing.T) {
	var recreated bool
	cl := testCleaner(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "filesystem.listdir":
			return []client.FileEntry{
				{Name: "abc123.iso", Type: "FILE"},
				{Name: "def456.iso", Type: "FILE"},
			}, nil
		case "pool.dataset.delete":
			recreated = true
			return nil, nil
		}
		return nil, nil
	}, map[string]bool{"abc123": true}) // abc123 is active

	cl.cleanupISOs(context.Background())
	assert.False(t, recreated, "should NOT recreate dataset when some ISOs are active")
}

func TestCleanupISOs_NoISOs_DoesNothing(t *testing.T) {
	var recreated bool
	cl := testCleaner(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "filesystem.listdir":
			return []client.FileEntry{}, nil
		case "pool.dataset.delete":
			recreated = true
			return nil, nil
		}
		return nil, nil
	}, map[string]bool{})

	cl.cleanupISOs(context.Background())
	assert.False(t, recreated, "should not recreate dataset when no ISOs exist")
}

func TestCleanupISOs_IgnoresNonISOFiles(t *testing.T) {
	var recreated bool
	cl := testCleaner(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "filesystem.listdir":
			return []client.FileEntry{
				{Name: "readme.txt", Type: "FILE"},
				{Name: "subdir", Type: "DIRECTORY"},
			}, nil
		case "pool.dataset.delete":
			recreated = true
			return nil, nil
		}
		return nil, nil
	}, map[string]bool{})

	cl.cleanupISOs(context.Background())
	assert.False(t, recreated, "should ignore non-ISO files")
}

func TestCleanupISOs_NilActiveIDs_Skips(t *testing.T) {
	cl := &Cleaner{
		client: client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
			if method == "filesystem.listdir" {
				return []client.FileEntry{{Name: "abc.iso", Type: "FILE"}}, nil
			}
			return nil, nil
		}),
		config:         Config{Pool: "default"},
		logger:         zap.NewNop(),
		activeImageIDs: func() map[string]bool { return nil },
	}

	cl.cleanupISOs(context.Background())
}

// --- cleanupOrphanVMs tests ---
// Orphan VMs are detected by checking if their backing zvol (tagged with org.omni:managed)
// still exists. No in-memory tracking is used.

// managedDatasetResponse returns a mock pool.dataset.query response with user properties.
func managedDatasetResponse(requestIDs ...string) []map[string]any {
	var datasets []map[string]any
	for _, id := range requestIDs {
		datasets = append(datasets, map[string]any{
			"id": "default/omni-vms/" + id,
			"user_properties": map[string]any{
				"org.omni:managed":    map[string]any{"value": "true"},
				"org.omni:request-id": map[string]any{"value": id},
			},
		})
	}
	return datasets
}

func TestCleanupOrphanVMs_DeletesOrphans(t *testing.T) {
	var deleted []int
	cl := testCleaner(func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "vm.query":
			return []client.VM{
				{ID: 1, Name: "omni_active_vm"},
				{ID: 2, Name: "omni_orphan_vm"},
				{ID: 3, Name: "not_omni_vm"},
			}, nil
		case "pool.dataset.query":
			// Only "active-vm" has a managed zvol
			return managedDatasetResponse("active-vm"), nil
		case "vm.stop":
			return nil, nil
		case "vm.delete":
			var args []json.RawMessage
			_ = json.Unmarshal(params, &args)
			var id int
			_ = json.Unmarshal(args[0], &id)
			deleted = append(deleted, id)
			return nil, nil
		}
		return nil, nil
	}, map[string]bool{})

	cl.cleanupOrphanVMs(context.Background())
	assert.Equal(t, []int{2}, deleted, "should only delete orphan omni_ VMs whose zvol is gone")
}

func TestCleanupOrphanVMs_SkipsNonOmniVMs(t *testing.T) {
	var deleteCalled bool
	cl := testCleaner(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "vm.query":
			return []client.VM{
				{ID: 1, Name: "manual_vm"},
				{ID: 2, Name: "plex"},
			}, nil
		case "pool.dataset.query":
			return managedDatasetResponse(), nil
		case "vm.delete":
			deleteCalled = true
			return nil, nil
		}
		return nil, nil
	}, map[string]bool{})

	cl.cleanupOrphanVMs(context.Background())
	assert.False(t, deleteCalled, "should not delete VMs without omni_ prefix")
}

func TestCleanupOrphanVMs_AllHaveZvols_NoneDeleted(t *testing.T) {
	var deleteCalled bool
	cl := testCleaner(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "vm.query":
			return []client.VM{
				{ID: 1, Name: "omni_vm_1"},
				{ID: 2, Name: "omni_vm_2"},
			}, nil
		case "pool.dataset.query":
			return managedDatasetResponse("vm-1", "vm-2"), nil
		case "vm.delete":
			deleteCalled = true
			return nil, nil
		}
		return nil, nil
	}, map[string]bool{})

	cl.cleanupOrphanVMs(context.Background())
	assert.False(t, deleteCalled, "should not delete VMs when all have backing zvols")
}

func TestCleanupOrphanVMs_StopFails_StillDeletes(t *testing.T) {
	var deleted bool
	cl := testCleaner(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "vm.query":
			return []client.VM{{ID: 1, Name: "omni_orphan"}}, nil
		case "pool.dataset.query":
			return managedDatasetResponse(), nil // No managed zvols → orphan
		case "vm.stop":
			return nil, &client.APIError{Code: 99, Message: "stop failed"}
		case "vm.delete":
			deleted = true
			return nil, nil
		}
		return nil, nil
	}, map[string]bool{})

	cl.cleanupOrphanVMs(context.Background())
	assert.True(t, deleted, "should still delete VM even if stop fails")
}

func TestCleanupOrphanVMs_ProviderRestart_DoesNotDeleteActiveVMs(t *testing.T) {
	// Simulates provider restart: VMs exist on TrueNAS, zvols exist with managed tags.
	// Even though no in-memory tracking exists, VMs should NOT be deleted.
	var deleteCalled bool
	cl := testCleaner(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "vm.query":
			return []client.VM{
				{ID: 1, Name: "omni_talos_cp_1"},
				{ID: 2, Name: "omni_talos_worker_1"},
			}, nil
		case "pool.dataset.query":
			return managedDatasetResponse("talos-cp-1", "talos-worker-1"), nil
		case "vm.delete":
			deleteCalled = true
			return nil, nil
		}
		return nil, nil
	}, map[string]bool{})

	cl.cleanupOrphanVMs(context.Background())
	assert.False(t, deleteCalled, "must NOT delete VMs after restart when zvols exist")
}

// --- cleanupOrphanZvols tests ---
// Orphan zvols are detected by checking if their corresponding VM exists.
// ListManagedZvols queries all datasets with org.omni:managed=true user property.

func TestCleanupOrphanZvols_DeletesOrphans(t *testing.T) {
	var deleted []string
	cl := testCleaner(func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "pool.dataset.query":
			// Return managed zvols (with user properties)
			return managedDatasetResponse("active-request-123", "orphan-request-456"), nil
		case "vm.query":
			// Only active-request-123 has a VM
			return []client.VM{{ID: 1, Name: "omni_active_request_123"}}, nil
		case "pool.dataset.delete":
			var args []json.RawMessage
			_ = json.Unmarshal(params, &args)
			var path string
			_ = json.Unmarshal(args[0], &path)
			deleted = append(deleted, path)
			return nil, nil
		}
		return nil, nil
	}, map[string]bool{})

	cl.cleanupOrphanZvols(context.Background())

	require.Len(t, deleted, 1)
	assert.Equal(t, "default/omni-vms/orphan-request-456", deleted[0])
}

func TestCleanupOrphanZvols_AllHaveVMs_NoneDeleted(t *testing.T) {
	var deleteCalled bool
	cl := testCleaner(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "pool.dataset.query":
			return managedDatasetResponse("req-1"), nil
		case "vm.query":
			return []client.VM{{ID: 1, Name: "omni_req_1"}}, nil
		case "pool.dataset.delete":
			deleteCalled = true
			return nil, nil
		}
		return nil, nil
	}, map[string]bool{})

	cl.cleanupOrphanZvols(context.Background())
	assert.False(t, deleteCalled, "should not delete zvols that have corresponding VMs")
}

func TestCleanupOrphanZvols_DatasetPrefix_FindsZvols(t *testing.T) {
	// Zvols under a dataset prefix (e.g., default/previewk8/omni-vms/) should also be found
	var deleted []string
	cl := testCleaner(func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "pool.dataset.query":
			return []map[string]any{
				{
					"id": "default/previewk8/omni-vms/deep-orphan",
					"user_properties": map[string]any{
						"org.omni:managed":    map[string]any{"value": "true"},
						"org.omni:request-id": map[string]any{"value": "deep-orphan"},
					},
				},
			}, nil
		case "vm.query":
			return []client.VM{}, nil // No VMs at all
		case "pool.dataset.delete":
			var args []json.RawMessage
			_ = json.Unmarshal(params, &args)
			var path string
			_ = json.Unmarshal(args[0], &path)
			deleted = append(deleted, path)
			return nil, nil
		}
		return nil, nil
	}, map[string]bool{})

	cl.cleanupOrphanZvols(context.Background())

	require.Len(t, deleted, 1)
	assert.Equal(t, "default/previewk8/omni-vms/deep-orphan", deleted[0])
}

func TestCleanupOrphanZvols_HyphenToUnderscoreMapping(t *testing.T) {
	tests := []struct {
		zvolID string
		vmName string
	}{
		{"default/omni-vms/talos-test-workers-abc", "omni_talos_test_workers_abc"},
		{"default/omni-vms/simple", "omni_simple"},
		{"tank/omni-vms/a-b-c-d", "omni_a_b_c_d"},
	}

	for _, tt := range tests {
		t.Run(tt.zvolID, func(t *testing.T) {
			parts := strings.Split(tt.zvolID, "/")
			requestID := parts[len(parts)-1]
			vmName := "omni_" + strings.ReplaceAll(requestID, "-", "_")
			assert.Equal(t, tt.vmName, vmName)
		})
	}
}

// --- runOnce tests ---

func TestRunOnce_CallsAllCleanupFunctions(t *testing.T) {
	var listFilesCalled, vmQueryCalled atomic.Bool

	cl := testCleaner(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "filesystem.listdir":
			listFilesCalled.Store(true)
			return []client.FileEntry{}, nil
		case "vm.query":
			vmQueryCalled.Store(true)
			return []client.VM{}, nil
		case "pool.dataset.query":
			return []client.Dataset{}, nil
		}
		return nil, nil
	}, map[string]bool{})

	cl.runOnce(context.Background())

	assert.True(t, listFilesCalled.Load(), "should call ListFiles for ISO cleanup")
	assert.True(t, vmQueryCalled.Load(), "should call vm.query for orphan cleanup")
}
