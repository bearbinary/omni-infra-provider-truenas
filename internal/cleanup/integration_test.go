package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

func init() {
	if os.Getenv("RECORD_CASSETTES") != "" || os.Getenv("TRUENAS_TEST_HOST") != "" || os.Getenv("TRUENAS_TEST_SOCKET") != "" {
		_ = godotenv.Load("../../.env")
		_ = godotenv.Load("../../.env.test")
	}
}

// cleanupCassettePath returns the cassette file path for the current test.
func cleanupCassettePath(t *testing.T) string {
	t.Helper()

	name := strings.ReplaceAll(t.Name(), "/", "__")

	return filepath.Join("testdata", "cassettes", name+".json")
}

func testClient(t *testing.T) *client.Client {
	t.Helper()

	host := os.Getenv("TRUENAS_TEST_HOST")
	apiKey := os.Getenv("TRUENAS_TEST_API_KEY")

	// Live or Record mode
	if host != "" {
		c, err := client.New(client.Config{
			Host:               host,
			APIKey:             apiKey,
			InsecureSkipVerify: true,
		})
		require.NoError(t, err)

		if os.Getenv("RECORD_CASSETTES") != "" {
			rec := client.NewRecordingTransport(client.TransportOf(c))
			client.ReplaceTransport(c, rec)

			t.Cleanup(func() {
				path := cleanupCassettePath(t)
				if err := rec.Save(path); err != nil {
					t.Errorf("failed to save cassette: %v", err)
				} else {
					t.Logf("Cassette saved: %s", path)
				}
			})
		}

		t.Cleanup(func() { c.Close() })

		return c
	}

	// Replay mode
	path := cleanupCassettePath(t)
	if _, err := os.Stat(path); err == nil {
		replay := client.NewReplayTransport(t, path)
		c := client.NewReplayClient(replay)

		t.Cleanup(func() { replay.AssertAllConsumed(t) })

		return c
	}

	if os.Getenv("CI_REQUIRE_CASSETTES") != "" {
		t.Fatalf("cassette missing and CI_REQUIRE_CASSETTES is set: %s — re-record with `make test-record` or delete the test", path)
	}

	t.Skip("no TrueNAS connection and no cassette at " + path)

	return nil
}

func testPool(t *testing.T) string {
	t.Helper()

	pool := os.Getenv("TRUENAS_TEST_POOL")
	if pool == "" {
		pool = "default"
	}

	return pool
}

func testLogger(t *testing.T) *zap.Logger {
	t.Helper()

	logger, _ := zap.NewDevelopment()

	return logger
}

// --- ISO Cleanup ---

func TestIntegration_ISOCleanup_AllStale(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	// Create a temporary ISO dataset for this test (don't use the real one)
	isoDataset := pool + "/talos-iso-test-" + time.Now().Format("150405")
	_, err := c.CreateDataset(ctx, client.CreateDatasetRequest{Name: isoDataset, Type: "FILESYSTEM"})
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), isoDataset) //nolint:errcheck
	})

	// Upload two fake stale ISOs
	iso1 := "/mnt/" + isoDataset + "/stale1.iso"
	iso2 := "/mnt/" + isoDataset + "/stale2.iso"
	require.NoError(t, c.UploadFile(ctx, iso1, strings.NewReader("fake"), 4))
	require.NoError(t, c.UploadFile(ctx, iso2, strings.NewReader("fake"), 4))

	// Verify they exist
	exists, err := c.FileExists(ctx, iso1)
	require.NoError(t, err)
	assert.True(t, exists)

	// Test RecreateDataset directly — this is the core mechanism used by ISO cleanup
	// when all ISOs are stale.
	err = c.RecreateDataset(ctx, isoDataset)
	require.NoError(t, err, "RecreateDataset should succeed")

	// ISOs should be gone
	exists, err = c.FileExists(ctx, iso1)
	require.NoError(t, err)
	assert.False(t, exists, "ISO should be gone after dataset recreate")

	exists, err = c.FileExists(ctx, iso2)
	require.NoError(t, err)
	assert.False(t, exists, "ISO should be gone after dataset recreate")

	// Dataset should still exist (recreated empty)
	dsExists, err := c.FileExists(ctx, "/mnt/"+isoDataset)
	require.NoError(t, err)
	assert.True(t, dsExists, "dataset should exist after recreate")
}

func TestIntegration_ISOCleanup_SkipsWhenActive(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)
	logger := testLogger(t)

	isoDir := "/mnt/" + pool + "/talos-iso"

	// Ensure ISO dataset exists
	err := c.EnsureDataset(ctx, pool+"/talos-iso")
	require.NoError(t, err)

	// Upload a test ISO
	testISO := isoDir + "/active_test_keep.iso"
	require.NoError(t, c.UploadFile(ctx, testISO, strings.NewReader("keep me"), 7))

	t.Cleanup(func() {
		// Can't delete individual files via API, but the test ISO is harmless
	})

	// Run cleanup with "active_test_keep" as active — should skip cleanup
	cleaner := New(c, Config{Pool: pool}, logger,
		func() map[string]bool {
			return map[string]bool{"active_test_keep": true}
		},
	)

	cleaner.cleanupISOs(ctx)

	// ISO should still exist
	exists, err := c.FileExists(ctx, testISO)
	require.NoError(t, err)
	assert.True(t, exists, "active ISO should not be deleted")
}

// --- Orphan VM Cleanup ---

func TestIntegration_OrphanVMCleanup(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	logger := testLogger(t)
	pool := testPool(t)

	// Create an "orphan" VM with omni_ prefix (not tracked by the provider)
	orphanName := "omni_cleanup_test_orphan"

	vm, err := c.CreateVM(ctx, client.CreateVMRequest{
		Name:       orphanName,
		VCPUs:      1,
		Memory:     512,
		Bootloader: "UEFI",
		Autostart:  false,
		CPUMode:    "HOST-PASSTHROUGH",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		c.StopVM(context.Background(), vm.ID, true) //nolint:errcheck
		c.DeleteVM(context.Background(), vm.ID)     //nolint:errcheck
	})

	// Create a "tracked" VM with omni_ prefix
	trackedName := "omni_cleanup_test_tracked"

	trackedVM, err := c.CreateVM(ctx, client.CreateVMRequest{
		Name:       trackedName,
		VCPUs:      1,
		Memory:     512,
		Bootloader: "UEFI",
		Autostart:  false,
		CPUMode:    "HOST-PASSTHROUGH",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		c.StopVM(context.Background(), trackedVM.ID, true) //nolint:errcheck
		c.DeleteVM(context.Background(), trackedVM.ID)     //nolint:errcheck
	})

	// Run cleanup — the tracked VM has a backing zvol, the orphan does not
	cleaner := New(c, Config{Pool: pool}, logger,
		func() map[string]bool { return map[string]bool{} },
	)

	// Build managed zvols list: only the tracked VM has a backing zvol.
	// The orphan VM has no backing zvol, so cleanup should remove it.
	trackedRequestID := strings.TrimPrefix(trackedName, "omni_")
	trackedRequestID = strings.ReplaceAll(trackedRequestID, "_", "-")
	managedZvols := []client.ManagedZvol{
		{Path: pool + "/omni-vms/" + trackedRequestID, RequestID: trackedRequestID},
	}

	cleaner.cleanupOrphanVMs(ctx, managedZvols)

	// Orphan VM should be gone
	found, err := c.FindVMByName(ctx, orphanName)
	require.NoError(t, err)
	assert.Nil(t, found, "orphan VM should be deleted by cleanup")

	// Tracked VM should remain
	found, err = c.FindVMByName(ctx, trackedName)
	require.NoError(t, err)
	assert.NotNil(t, found, "tracked VM should not be deleted")
}

// --- Orphan Zvol Cleanup ---

func TestIntegration_OrphanZvolCleanup(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	logger := testLogger(t)
	pool := testPool(t)

	// Ensure parent dataset
	parentDS := pool + "/omni-vms"
	err := c.EnsureDataset(ctx, parentDS)
	require.NoError(t, err)

	// Fixed suffix: deterministic for replay, unique enough for serial test runs
	suffix := "fixed"

	// Create an orphan zvol (request ID with hyphens)
	orphanRequestID := "cleanup-test-orphan-" + suffix
	orphanZvol := parentDS + "/" + orphanRequestID

	_, err = c.CreateZvol(ctx, orphanZvol, 1)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), orphanZvol) //nolint:errcheck
	})

	// Create a tracked zvol with a corresponding VM (so it won't be cleaned up)
	trackedRequestID := "cleanup-test-tracked-" + suffix
	trackedZvol := parentDS + "/" + trackedRequestID
	trackedVMName := "omni_" + strings.ReplaceAll(trackedRequestID, "-", "_")

	_, err = c.CreateZvol(ctx, trackedZvol, 1)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), trackedZvol) //nolint:errcheck
	})

	// Create a VM matching the tracked zvol so cleanup won't treat it as orphan
	trackedVM, err := c.CreateVM(ctx, client.CreateVMRequest{
		Name:       trackedVMName,
		VCPUs:      1,
		Memory:     512,
		Bootloader: "UEFI",
		Autostart:  false,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteVM(context.Background(), trackedVM.ID) //nolint:errcheck
	})

	// Run cleanup — the tracked zvol has a corresponding VM, the orphan does not
	cleaner := New(c, Config{Pool: pool}, logger,
		func() map[string]bool { return map[string]bool{} },
	)

	// Build managed zvols list: both zvols appear as managed.
	// cleanupOrphanZvols checks if the corresponding VM exists — only orphan's VM is missing.
	managedZvols := []client.ManagedZvol{
		{Path: orphanZvol, RequestID: orphanRequestID},
		{Path: trackedZvol, RequestID: trackedRequestID},
	}

	cleaner.cleanupOrphanZvols(ctx, managedZvols)

	// Orphan zvol should be gone — listing child datasets should not include it
	datasets, err := c.ListChildDatasets(ctx, parentDS)
	require.NoError(t, err)

	var foundOrphan, foundTracked bool
	for _, ds := range datasets {
		if strings.HasSuffix(ds.ID, orphanRequestID) {
			foundOrphan = true
		}
		if strings.HasSuffix(ds.ID, trackedRequestID) {
			foundTracked = true
		}
	}

	assert.False(t, foundOrphan, "orphan zvol should be deleted by cleanup")
	assert.True(t, foundTracked, "tracked zvol should not be deleted")
}
