package noderotation

import (
	"context"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/omni/client/api/omni/specs"
	infraresources "github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestSurge_AbortOnOperatorEdit_WaitDown covers the operator-edit
// drift abort during the wait-down phase. wait-down typically lasts
// minutes (MRS teardown is the slowest step in the cycle), so the
// window for operator interference is largest here; a regression in
// the wait-down abort branch would surface as the engine silently
// continuing with a wrong assumption about MachineCount.
func TestSurge_AbortOnOperatorEdit_WaitDown(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := surgeOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 2, false)
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	cands, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", cands[0].CurrentGeneration)

	// Pretend we're mid-cycle at wait-down with OriginalCount=2.
	setSurgePhase(t, st, "ms-workers", SurgePhaseState{
		Phase:          SurgePhaseWaitDown,
		OriginalCount:  2,
		CycleStartedAt: time.Unix(900, 0),
	})

	// Operator edited MachineCount up to 7 — far from the expected
	// OriginalCount=2 at wait-down.
	setMachineCount(t, st, "ms-workers", 7)

	cands, err = d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&cands[0])

	require.Equal(t, ActionSurgeAborted, plan.Action, "reason: %s", plan.Reason)
	assert.Equal(t, AbortKindCountDriftWaitDown, plan.AbortKind)
	assert.Equal(t, 7, plan.LiveCount)
	assert.Equal(t, 2, plan.ExpectedCount)

	// Executing the abort clears annotations.
	require.NoError(t, engine.Execute(context.Background(), plan))
	ms, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "ms-workers")
	require.NoError(t, err)
	_, hasPhase := ms.Metadata().Annotations().Get(AnnotationSurgePhase)
	assert.False(t, hasPhase, "surge-phase annotation should be cleared after abort")
}

// TestSurge_MultipleStale_ChainsCycles verifies the multi-cycle
// recovery loop: a class with 3 stale members rotates them one cycle
// at a time, with cycle N+1 starting on the tick after cycle N
// completes. A regression that fails to detect remaining stale on the
// post-complete tick would leave a MachineSet half-rotated forever.
func TestSurge_MultipleStale_ChainsCycles(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := surgeOptedInAnnotations()
	// MinHealthy=1 so we can rotate with stale-only on a small set.
	ann[AnnotationMinHealthy] = "1"
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 3, false)
	seedRequest(t, st, "mr-stale-1", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale-2", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale-3", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	cands, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", cands[0].CurrentGeneration)

	clock := time.Unix(1000, 0)
	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return clock })

	// Helper that runs one full cycle: SurgeUp → fresh lands → SurgeDown
	// → stale drained → SurgeCycleComplete. Returns the live MachineSet
	// for assertion.
	runOneCycle := func(t *testing.T, freshID, staleIDToDrain string) {
		t.Helper()

		// SurgeUp.
		cands, err = d.Discover(context.Background())
		require.NoError(t, err)
		require.Len(t, cands, 1)
		plan := engine.Plan(&cands[0])
		require.Equal(t, ActionSurgeUp, plan.Action, "reason: %s", plan.Reason)
		require.NoError(t, engine.Execute(context.Background(), plan))

		// Fresh replacement lands.
		seedRequest(t, st, freshID, "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)

		// SurgeDown.
		cands, err = d.Discover(context.Background())
		require.NoError(t, err)
		plan = engine.Plan(&cands[0])
		require.Equal(t, ActionSurgeDown, plan.Action, "reason: %s", plan.Reason)
		require.NoError(t, engine.Execute(context.Background(), plan))

		// MRS drains the picked stale member.
		stale, getErr := safe.StateGetByID[*infraresources.MachineRequest](context.Background(), st, staleIDToDrain)
		require.NoError(t, getErr)
		require.NoError(t, st.Destroy(context.Background(), stale.Metadata()))

		// Cycle complete.
		cands, err = d.Discover(context.Background())
		require.NoError(t, err)
		plan = engine.Plan(&cands[0])
		require.Equal(t, ActionSurgeCycleComplete, plan.Action, "reason: %s", plan.Reason)
		require.NoError(t, engine.Execute(context.Background(), plan))
	}

	runOneCycle(t, "mr-fresh-1", "mr-stale-1")
	runOneCycle(t, "mr-fresh-2", "mr-stale-2")
	runOneCycle(t, "mr-fresh-3", "mr-stale-3")

	// After three cycles, the set should be all-fresh and at idle.
	cands, err = d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, cands, 1)
	assert.Empty(t, cands[0].StaleRequests(), "all members should be at current generation after three chained cycles")

	plan := engine.Plan(&cands[0])
	assert.Equal(t, ActionNone, plan.Action, "no work left after rotation completes; reason: %s", plan.Reason)
}

// TestExecute_InPlace_WaitingForReady_DoesNotRefreshLock pins the
// contract for the in-place strategy: wait-style decisions do NOT
// refresh the rotation-state lock because there is no persistent
// surge-phase annotation marking the cycle as in-flight. The
// reconciler's lock TTL is the safety net — if a teardown takes
// longer than the TTL, the autoscaler resumes scaling. Whichever
// behavior is correct must stay pinned so a refactor doesn't
// silently invert it.
func TestExecute_InPlace_WaitingForReady_DoesNotRefreshLock(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := rotationOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 3, false)
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-prov", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONING)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	cands, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", cands[0].CurrentGeneration)

	// Pre-stamp a lock from 4 minutes ago. If Execute were to refresh
	// it, the timestamp would jump to `now`.
	priorLockTime := time.Unix(0, 0)
	_, err = safe.StateUpdateWithConflicts[*omni.MachineSet](
		context.Background(), st,
		omni.NewMachineSet("ms-workers").Metadata(),
		func(m *omni.MachineSet) error {
			m.Metadata().Annotations().Set(AnnotationRotationState,
				formatRotationStateAnnotation("dummy-gen", priorLockTime))
			return nil
		},
	)
	require.NoError(t, err)

	cands, err = d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, cands, 1)

	now := time.Unix(240, 0) // 4 min after priorLockTime
	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return now })

	// In-place + PROVISIONING request in flight → ActionLockedByPriorStep
	// because the lock from the foreign reconciler is fresh (within TTL).
	// We're verifying that Execute on a wait-style action does NOT
	// refresh that foreign lock — only surge cycles refresh.
	plan := engine.Plan(&cands[0])
	require.Equal(t, ActionLockedByPriorStep, plan.Action, "reason: %s", plan.Reason)

	require.ErrorIs(t, engine.Execute(context.Background(), plan), ErrCandidateNotActionable)

	// Lock timestamp should still be the prior value, NOT `now`.
	ms, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "ms-workers")
	require.NoError(t, err)
	rawLock, ok := ms.Metadata().Annotations().Get(AnnotationRotationState)
	require.True(t, ok)
	_, ts, ok := parseRotationStateAnnotation(rawLock)
	require.True(t, ok)
	assert.Equal(t, priorLockTime, ts, "in-place wait must not refresh a foreign reconciler's lock")
}
