package provisioner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/api/specs"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources"
)

func init() {
	if os.Getenv("RECORD_CASSETTES") != "" || os.Getenv("TRUENAS_TEST_HOST") != "" || os.Getenv("TRUENAS_TEST_SOCKET") != "" {
		_ = godotenv.Load("../../.env")
		_ = godotenv.Load("../../.env.test")
	}
}

// stepCassettePath returns the cassette file path for the current test.
func stepCassettePath(t *testing.T) string {
	t.Helper()

	name := strings.ReplaceAll(t.Name(), "/", "__")

	return filepath.Join("testdata", "cassettes", name+".json")
}

// stepTestClient returns a client in live, record, or replay mode.
func stepTestClient(t *testing.T) *client.Client {
	t.Helper()

	host := os.Getenv("TRUENAS_TEST_HOST")
	apiKey := os.Getenv("TRUENAS_TEST_API_KEY")

	if host != "" && apiKey != "" {
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
				path := stepCassettePath(t)
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
	path := stepCassettePath(t)
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

func stepTestPool(t *testing.T) string {
	t.Helper()

	pool := os.Getenv("TRUENAS_TEST_POOL")
	if pool == "" {
		pool = "tank"
	}

	return pool
}

// TestStepOrchestration_ValidatePool verifies validatePool is called early and fails
// cleanly for nonexistent pools. This tests the real client against real TrueNAS.
func TestStepOrchestration_ValidatePool(t *testing.T) {
	c := stepTestClient(t)

	p := NewProvisioner(c, ProviderConfig{
		DefaultPool:             "nonexistent-pool-xyz",
		DefaultNetworkInterface: "br0",
	})

	err := p.validatePool(context.Background(), "nonexistent-pool-xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestStepOrchestration_MaybeResizeZvol_Integration tests actual zvol resize against TrueNAS.
func TestStepOrchestration_MaybeResizeZvol_Integration(t *testing.T) {
	c := stepTestClient(t)
	pool := stepTestPool(t)

	p := NewProvisioner(c, ProviderConfig{DefaultPool: pool})
	logger, _ := zap.NewDevelopment()

	// Ensure parent dataset exists
	err := c.EnsureDataset(context.Background(), pool+"/omni-vms")
	require.NoError(t, err)

	// Create a small zvol
	zvolPath := pool + "/omni-vms/step-test-resize-" + time.Now().Format("20060102150405")
	_, err = c.CreateZvol(context.Background(), zvolPath, 10)
	require.NoError(t, err)

	defer func() {
		c.DeleteDataset(context.Background(), zvolPath) //nolint:errcheck
	}()

	// Resize to larger — should succeed
	err = p.maybeResizeZvol(context.Background(), logger, zvolPath, 20)
	require.NoError(t, err)

	// Verify new size
	newSize, err := c.GetZvolSize(context.Background(), zvolPath)
	require.NoError(t, err)
	assert.Equal(t, int64(20*1024*1024*1024), newSize)

	// Resize to same — should no-op
	err = p.maybeResizeZvol(context.Background(), logger, zvolPath, 20)
	require.NoError(t, err)

	// Resize to smaller — should no-op (not shrink)
	err = p.maybeResizeZvol(context.Background(), logger, zvolPath, 10)
	require.NoError(t, err)

	finalSize, err := c.GetZvolSize(context.Background(), zvolPath)
	require.NoError(t, err)
	assert.Equal(t, int64(20*1024*1024*1024), finalSize, "should not have shrunk")
}

// TestDeprovision_CleanupOrchestration verifies the full deprovision sequence
// using mock client to capture the call sequence.
func TestDeprovision_CleanupOrchestration(t *testing.T) {
	var callSequence []string

	p := NewProvisioner(client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		callSequence = append(callSequence, method)
		switch method {
		case "vm.stop":
			return true, nil
		case "vm.query":
			return client.VM{ID: 42, Description: omniVMDescriptionPrefix + " (test)", Status: client.VMStatus{State: "STOPPED"}}, nil
		case "vm.delete":
			return true, nil
		case "pool.dataset.query":
			// Ownership check: return managed tags so the deprovision path accepts the zvol.
			return map[string]any{
				"id": "tank/omni-vms/test-machine",
				"user_properties": map[string]any{
					"org.omni:managed": map[string]any{"value": "true"},
				},
			}, nil
		case "pool.dataset.delete":
			return nil, nil
		}
		return nil, nil
	}), ProviderConfig{
		DefaultPool:             "tank",
		GracefulShutdownTimeout: 100 * time.Millisecond,
		PollInterval:            10 * time.Millisecond,
	})

	machine := resources.NewMachine("default", "test-machine")
	machine.TypedSpec().Value = &specs.MachineSpec{
		VmId:     42,
		ZvolPath: "tank/omni-vms/test-machine",
	}

	// Can't call Deprovision directly (needs infra.MachineRequest),
	// but we can test cleanupVM + cleanupZvol in sequence
	logger, _ := zap.NewDevelopment()

	err := p.cleanupVM(context.Background(), logger, 42)
	require.NoError(t, err)

	err = p.cleanupZvol(context.Background(), logger, "tank/omni-vms/test-machine", "")
	require.NoError(t, err)

	// Verify call sequence: stop → poll → delete VM → delete zvol
	assert.Contains(t, callSequence, "vm.stop")
	assert.Contains(t, callSequence, "vm.query")
	assert.Contains(t, callSequence, "vm.delete")
	assert.Contains(t, callSequence, "pool.dataset.delete")

	// vm.stop should come before vm.delete
	stopIdx := indexOf(callSequence, "vm.stop")
	deleteIdx := indexOf(callSequence, "vm.delete")
	assert.Less(t, stopIdx, deleteIdx, "should stop VM before deleting it")
}

func indexOf(s []string, v string) int {
	for i, item := range s {
		if item == v {
			return i
		}
	}
	return -1
}

// TestStepOrchestration_ErrorCategorization verifies error categories are correctly
// assigned for various failure modes.
func TestStepOrchestration_ErrorCategorization(t *testing.T) {
	tests := []struct {
		name    string
		pool    string
		wantCat string
	}{
		{"nonexistent pool", "nonexistent-pool-xyz", "pool_not_found"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
				if method == "pool.query" {
					return []map[string]any{}, nil
				}
				return nil, nil
			})

			err := p.validatePool(context.Background(), tc.pool)
			require.Error(t, err)
			assert.Equal(t, tc.wantCat, categorizeError(err))
		})
	}
}

// Ensure NewMachine is available for tests
func init() {
	_ = provision.NewStep[*resources.Machine] // Verify the generic compiles
}
