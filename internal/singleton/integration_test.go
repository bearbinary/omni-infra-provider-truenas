package singleton_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/joho/godotenv"
	"github.com/siderolabs/omni/client/pkg/client"
	omniclient "github.com/siderolabs/omni/client/pkg/client/omni"
	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/singleton"
)

func init() {
	// Load .env for local dev (same pattern as client integration tests).
	// Only load when OMNI_ENDPOINT is already set, so plain `make test`
	// never accidentally picks up live credentials.
	if os.Getenv("OMNI_ENDPOINT") != "" {
		_ = godotenv.Load("../../.env")
	}
}

// Integration tests exercise the singleton lease against a real Omni COSI state
// over gRPC. They validate what unit tests cannot: annotation CAS over the
// wire, heartbeat round-trips, and multi-instance contention with real latency.
//
// Prerequisites:
//   - OMNI_ENDPOINT — the Omni gRPC endpoint
//   - OMNI_SERVICE_ACCOUNT_KEY — a service account key with infra provider access
//   - The production provider must NOT be running (tests acquire the real lease)
//
// The Omni API enforces that ProviderStatus resource IDs match the provider ID
// embedded in the service account key, so all tests use the real provider ID.
// Tests run sequentially (no t.Parallel) and each test releases the lease on
// cleanup to leave the resource in a clean state for the next test.
//
// Run:
//
//	OMNI_ENDPOINT=https://... OMNI_SERVICE_ACCOUNT_KEY=... \
//	  go test -v -count=1 -timeout=120s -run TestIntegration_ ./internal/singleton/

// integrationProviderID is the provider ID that matches the service account key.
// The Omni API rejects ProviderStatus operations where the resource ID doesn't
// match the key's embedded provider ID.
const integrationProviderID = "truenas"

// omniStateForIntegration returns a live Omni COSI state.
// Skips the test if OMNI_ENDPOINT is not set.
func omniStateForIntegration(t *testing.T) state.State {
	t.Helper()

	endpoint := os.Getenv("OMNI_ENDPOINT")
	if endpoint == "" {
		t.Skip("OMNI_ENDPOINT not set — skipping singleton integration test")
	}

	saKey := os.Getenv("OMNI_SERVICE_ACCOUNT_KEY")

	opts := []client.Option{
		client.WithInsecureSkipTLSVerify(true),
		client.WithOmniClientOptions(omniclient.WithProviderID(integrationProviderID)),
	}

	if saKey != "" {
		opts = append(opts, client.WithServiceAccount(saKey))
	}

	c, err := client.New(endpoint, opts...)
	require.NoError(t, err, "failed to connect to Omni")

	t.Cleanup(func() { _ = c.Close() })

	return c.Omni().State()
}

// requireNoLiveProvider checks that no production provider is currently holding
// the lease with a fresh heartbeat. If one is, the test is skipped to prevent
// interfering with a running deployment.
func requireNoLiveProvider(t *testing.T, st state.State) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := st.Get(ctx, infra.NewProviderStatus(integrationProviderID).Metadata())
	if err != nil {
		if state.IsNotFoundError(err) {
			return // No resource → no live provider
		}

		// Unexpected error (e.g., network) — fail loud so it's investigated.
		t.Fatalf("failed to check for live provider: %v", err)
	}

	ps := res.(*infra.ProviderStatus) //nolint:forcetypeassert

	ownerID, hasOwner := ps.Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	heartbeatStr, hasHB := ps.Metadata().Annotations().Get(singleton.AnnotationHeartbeat)

	if !hasOwner || !hasHB {
		return // Annotations cleared (released) — safe to test
	}

	hb, err := time.Parse(time.RFC3339Nano, heartbeatStr)
	if err != nil {
		return // Malformed heartbeat — considered stale
	}

	age := time.Since(hb)
	if age < singleton.DefaultStaleAfter {
		t.Skipf("live provider %q holds a fresh lease (heartbeat %s ago) — "+
			"stop the provider before running integration tests", ownerID, age.Round(time.Second))
	}
}

// releaseOnCleanup registers a cleanup function that releases the lease,
// leaving the ProviderStatus in a clean state for the next test.
func releaseOnCleanup(t *testing.T, lease *singleton.Lease) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		lease.Release(ctx)
	})
}

// TestIntegration_AcquireAndRelease verifies the full acquire → read-back →
// release cycle over a real Omni gRPC connection. Validates that annotation
// CAS and heartbeat timestamp survive the round-trip.
func TestIntegration_AcquireAndRelease(t *testing.T) {
	st := omniStateForIntegration(t)
	requireNoLiveProvider(t, st)

	logger := zaptest.NewLogger(t)

	lease, err := singleton.New(st, singleton.Config{
		ProviderID: integrationProviderID,
		InstanceID: "inttest-acquire-A",
	}, logger)
	require.NoError(t, err)
	releaseOnCleanup(t, lease)

	ctx := context.Background()

	// Acquire
	require.NoError(t, lease.Acquire(ctx), "Acquire should succeed on fresh state")

	// Read back and verify annotations
	res, err := st.Get(ctx, infra.NewProviderStatus(integrationProviderID).Metadata())
	require.NoError(t, err, "ProviderStatus should exist after Acquire")

	ps := res.(*infra.ProviderStatus) //nolint:forcetypeassert

	owner, ok := ps.Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	require.True(t, ok, "instance-id annotation must be set")
	assert.Equal(t, "inttest-acquire-A", owner)

	heartbeat, ok := ps.Metadata().Annotations().Get(singleton.AnnotationHeartbeat)
	require.True(t, ok, "heartbeat annotation must be set")

	parsed, err := time.Parse(time.RFC3339Nano, heartbeat)
	require.NoError(t, err, "heartbeat should be valid RFC3339Nano")
	assert.WithinDuration(t, time.Now().UTC(), parsed, 30*time.Second,
		"heartbeat should be recent (within 30s of wallclock)")

	// Release
	lease.Release(ctx)

	res, err = st.Get(ctx, infra.NewProviderStatus(integrationProviderID).Metadata())
	require.NoError(t, err, "ProviderStatus should still exist after Release")

	ps = res.(*infra.ProviderStatus) //nolint:forcetypeassert

	_, hasOwner := ps.Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	_, hasHB := ps.Metadata().Annotations().Get(singleton.AnnotationHeartbeat)
	assert.False(t, hasOwner, "instance-id should be cleared after Release")
	assert.False(t, hasHB, "heartbeat should be cleared after Release")
}

// TestIntegration_TwoInstanceContention verifies that a second instance is
// correctly rejected with ErrLeaseHeld when the first holds a fresh lease.
// Both lease instances share the same COSI state — the same real-world
// scenario as two pods with the same PROVIDER_ID.
func TestIntegration_TwoInstanceContention(t *testing.T) {
	st := omniStateForIntegration(t)
	requireNoLiveProvider(t, st)

	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	// First instance acquires.
	first, err := singleton.New(st, singleton.Config{
		ProviderID: integrationProviderID,
		InstanceID: "inttest-first",
	}, logger)
	require.NoError(t, err)
	releaseOnCleanup(t, first)
	require.NoError(t, first.Acquire(ctx))

	// Second instance must fail.
	second, err := singleton.New(st, singleton.Config{
		ProviderID: integrationProviderID,
		InstanceID: "inttest-second",
	}, logger)
	require.NoError(t, err)

	err = second.Acquire(ctx)
	require.Error(t, err, "second Acquire must fail")
	require.ErrorIs(t, err, singleton.ErrLeaseHeld)

	var held *singleton.LeaseHeldError
	require.True(t, errors.As(err, &held))
	assert.Equal(t, "inttest-first", held.OtherInstanceID)
	assert.Equal(t, integrationProviderID, held.ProviderID)

	// Verify first still owns it.
	res, err := st.Get(ctx, infra.NewProviderStatus(integrationProviderID).Metadata())
	require.NoError(t, err)

	ps := res.(*infra.ProviderStatus) //nolint:forcetypeassert
	owner, _ := ps.Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	assert.Equal(t, "inttest-first", owner, "first instance must still own the lease")
}

// TestIntegration_ReleaseEnablesSuccessor verifies the fast-handoff path:
// Release clears annotations, allowing a successor to acquire immediately
// without waiting for staleAfter.
func TestIntegration_ReleaseEnablesSuccessor(t *testing.T) {
	st := omniStateForIntegration(t)
	requireNoLiveProvider(t, st)

	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	first, err := singleton.New(st, singleton.Config{
		ProviderID: integrationProviderID,
		InstanceID: "inttest-pred",
	}, logger)
	require.NoError(t, err)
	require.NoError(t, first.Acquire(ctx))

	first.Release(ctx)

	// Successor should acquire immediately.
	second, err := singleton.New(st, singleton.Config{
		ProviderID: integrationProviderID,
		InstanceID: "inttest-succ",
	}, logger)
	require.NoError(t, err)
	releaseOnCleanup(t, second)
	require.NoError(t, second.Acquire(ctx), "successor should acquire immediately after Release")

	res, err := st.Get(ctx, infra.NewProviderStatus(integrationProviderID).Metadata())
	require.NoError(t, err)

	ps := res.(*infra.ProviderStatus) //nolint:forcetypeassert
	owner, _ := ps.Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	assert.Equal(t, "inttest-succ", owner)
}

// TestIntegration_RefreshLoopUpdatesHeartbeat verifies that Run() actually
// refreshes the heartbeat annotation over time via real gRPC calls.
func TestIntegration_RefreshLoopUpdatesHeartbeat(t *testing.T) {
	st := omniStateForIntegration(t)
	requireNoLiveProvider(t, st)

	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	lease, err := singleton.New(st, singleton.Config{
		ProviderID:      integrationProviderID,
		InstanceID:      "inttest-refresh",
		RefreshInterval: 1 * time.Second,
		StaleAfter:      5 * time.Second,
	}, logger)
	require.NoError(t, err)
	releaseOnCleanup(t, lease)
	require.NoError(t, lease.Acquire(ctx))

	// Read initial heartbeat.
	res, err := st.Get(ctx, infra.NewProviderStatus(integrationProviderID).Metadata())
	require.NoError(t, err)

	ps := res.(*infra.ProviderStatus) //nolint:forcetypeassert
	firstHB, _ := ps.Metadata().Annotations().Get(singleton.AnnotationHeartbeat)

	// Start the refresh loop.
	runCtx, runCancel := context.WithCancel(ctx)
	runDone := make(chan error, 1)

	go func() { runDone <- lease.Run(runCtx) }()

	// Wait for at least one refresh tick to update the heartbeat.
	require.Eventually(t, func() bool {
		r, getErr := st.Get(ctx, infra.NewProviderStatus(integrationProviderID).Metadata())
		if getErr != nil {
			return false
		}

		p := r.(*infra.ProviderStatus) //nolint:forcetypeassert
		hb, ok := p.Metadata().Annotations().Get(singleton.AnnotationHeartbeat)

		return ok && hb != firstHB
	}, 10*time.Second, 500*time.Millisecond,
		"heartbeat should advance after at least one refresh tick")

	// Clean shutdown.
	runCancel()

	select {
	case err := <-runDone:
		require.NoError(t, err, "Run should return nil on clean ctx cancel")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}

	// Lost must NOT be closed on clean shutdown.
	select {
	case <-lease.Lost():
		t.Fatal("Lost channel should not close on clean shutdown")
	default:
	}
}

// TestIntegration_StaleHeartbeatTakeover verifies that a second instance can
// take over a lease whose heartbeat has gone stale using real wallclock time.
// Uses aggressive timing (2s stale) to keep the test fast.
func TestIntegration_StaleHeartbeatTakeover(t *testing.T) {
	st := omniStateForIntegration(t)
	requireNoLiveProvider(t, st)

	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	// First instance acquires but does NOT run the refresh loop.
	first, err := singleton.New(st, singleton.Config{
		ProviderID:      integrationProviderID,
		InstanceID:      "inttest-stale-old",
		RefreshInterval: 1 * time.Second,
		StaleAfter:      2 * time.Second,
	}, logger)
	require.NoError(t, err)
	require.NoError(t, first.Acquire(ctx))

	// Wait for the heartbeat to go stale.
	time.Sleep(3 * time.Second)

	// Second instance should take over.
	second, err := singleton.New(st, singleton.Config{
		ProviderID:      integrationProviderID,
		InstanceID:      "inttest-stale-new",
		RefreshInterval: 1 * time.Second,
		StaleAfter:      2 * time.Second,
	}, logger)
	require.NoError(t, err)
	releaseOnCleanup(t, second)
	require.NoError(t, second.Acquire(ctx), "should take over stale lease")

	res, err := st.Get(ctx, infra.NewProviderStatus(integrationProviderID).Metadata())
	require.NoError(t, err)

	ps := res.(*infra.ProviderStatus) //nolint:forcetypeassert
	owner, _ := ps.Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	assert.Equal(t, "inttest-stale-new", owner, "new instance should own the lease")
}

// TestIntegration_ReentrantAcquireRefreshesHeartbeat verifies that calling
// Acquire a second time from the same instance bumps the heartbeat rather
// than failing, over real gRPC.
func TestIntegration_ReentrantAcquireRefreshesHeartbeat(t *testing.T) {
	st := omniStateForIntegration(t)
	requireNoLiveProvider(t, st)

	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	lease, err := singleton.New(st, singleton.Config{
		ProviderID: integrationProviderID,
		InstanceID: "inttest-reentrant",
	}, logger)
	require.NoError(t, err)
	releaseOnCleanup(t, lease)

	require.NoError(t, lease.Acquire(ctx))

	res, err := st.Get(ctx, infra.NewProviderStatus(integrationProviderID).Metadata())
	require.NoError(t, err)

	ps := res.(*infra.ProviderStatus) //nolint:forcetypeassert
	firstHB, _ := ps.Metadata().Annotations().Get(singleton.AnnotationHeartbeat)

	// Small delay so the heartbeat actually changes.
	time.Sleep(10 * time.Millisecond)

	require.NoError(t, lease.Acquire(ctx), "re-entrant Acquire should succeed")

	res, err = st.Get(ctx, infra.NewProviderStatus(integrationProviderID).Metadata())
	require.NoError(t, err)

	ps = res.(*infra.ProviderStatus) //nolint:forcetypeassert
	secondHB, _ := ps.Metadata().Annotations().Get(singleton.AnnotationHeartbeat)

	assert.NotEqual(t, firstHB, secondHB, "heartbeat should advance on re-entrant Acquire")
}
