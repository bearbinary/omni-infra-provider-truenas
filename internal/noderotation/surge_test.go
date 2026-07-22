package noderotation

import (
	"context"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/omni/client/api/omni/specs"
	infraresources "github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// surgeOptedInAnnotations builds an opt-in for worker + surge. CP +
// surge is the same shape with role=controlplane (used by the CP
// happy-path test).
func surgeOptedInAnnotations() map[string]string {
	return map[string]string{
		AnnotationEnabled:  "true",
		AnnotationRole:     "worker",
		AnnotationStrategy: "surge",
	}
}

// setSurgePhase is a test helper to stamp a surge phase annotation on
// a MachineSet so Plan reaches the in-cycle branches.
func setSurgePhase(t *testing.T, st state.State, machineSetID string, phase SurgePhaseState) {
	t.Helper()

	_, err := safe.StateUpdateWithConflicts[*omni.MachineSet](
		context.Background(), st,
		omni.NewMachineSet(machineSetID).Metadata(),
		func(m *omni.MachineSet) error {
			m.Metadata().Annotations().Set(AnnotationSurgePhase, formatSurgePhaseAnnotation(phase))
			return nil
		},
	)
	require.NoError(t, err)
}

// setMachineCount overrides MachineCount on a MachineSet so tests can
// simulate "MRS controller just spawned a new request" or "MRS
// finished tearing down" without modeling MRS itself.
func setMachineCount(t *testing.T, st state.State, machineSetID string, count uint32) {
	t.Helper()

	_, err := safe.StateUpdateWithConflicts[*omni.MachineSet](
		context.Background(), st,
		omni.NewMachineSet(machineSetID).Metadata(),
		func(m *omni.MachineSet) error {
			m.TypedSpec().Value.MachineAllocation.MachineCount = count
			return nil
		},
	)
	require.NoError(t, err)
}

// TestSurge_HappyPath_Worker walks the full cycle for a worker set
// with one stale request:
//
//  1. idle  → SurgeUp     (count 2→3, phase=wait-up)
//  2. wait-up before fresh lands → WaitingForReady
//  3. wait-up after fresh lands  → SurgeDown (count 3→2, phase=wait-down)
//  4. wait-down before MRS drains → WaitingForTeardown
//  5. wait-down after stale gone  → SurgeCycleComplete (clears annotations)
//
// Pinning every step in one test verifies the state machine threads
// through correctly — a regression in any one transition would surface
// as a stalled cycle on a real cluster.
func TestSurge_HappyPath_Worker(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := surgeOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 2, false)
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	candidates, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", candidates[0].CurrentGeneration)

	clock := time.Unix(1000, 0)
	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return clock })

	// === Step 1: idle → SurgeUp ===
	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)
	plan := engine.Plan(&candidates[0])
	require.Equal(t, ActionSurgeUp, plan.Action)

	require.NoError(t, engine.Execute(context.Background(), plan))

	// MachineCount bumped, phase annotation stamped, lock taken.
	ms, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "ms-workers")
	require.NoError(t, err)
	assert.Equal(t, uint32(3), ms.TypedSpec().Value.MachineAllocation.MachineCount)
	phaseRaw, ok := ms.Metadata().Annotations().Get(AnnotationSurgePhase)
	require.True(t, ok)
	phase, ok := parseSurgePhaseAnnotation(phaseRaw)
	require.True(t, ok)
	assert.Equal(t, SurgePhaseWaitUp, phase.Phase)
	assert.Equal(t, 2, phase.OriginalCount)

	// === Step 2: wait-up but no replacement yet → WaitingForReady ===
	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)
	plan = engine.Plan(&candidates[0])
	assert.Equal(t, ActionWaitingForReady, plan.Action)

	// Execute the wait → lock refreshed but no other state change.
	require.ErrorIs(t, engine.Execute(context.Background(), plan), ErrCandidateNotActionable)

	// === Step 3: wait-up with replacement landed → SurgeDown ===
	// Simulate MRS having spawned a fresh replacement.
	seedRequest(t, st, "mr-fresh2", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)

	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)
	plan = engine.Plan(&candidates[0])
	require.Equal(t, ActionSurgeDown, plan.Action, "reason: %s", plan.Reason)

	require.NoError(t, engine.Execute(context.Background(), plan))

	ms, err = safe.StateGetByID[*omni.MachineSet](context.Background(), st, "ms-workers")
	require.NoError(t, err)
	assert.Equal(t, uint32(2), ms.TypedSpec().Value.MachineAllocation.MachineCount)
	phaseRaw, ok = ms.Metadata().Annotations().Get(AnnotationSurgePhase)
	require.True(t, ok)
	phase, ok = parseSurgePhaseAnnotation(phaseRaw)
	require.True(t, ok)
	assert.Equal(t, SurgePhaseWaitDown, phase.Phase)

	// === Step 4: wait-down but stale still present → WaitingForTeardown ===
	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)
	plan = engine.Plan(&candidates[0])
	assert.Equal(t, ActionWaitingForTeardown, plan.Action, "reason: %s", plan.Reason)

	// === Step 5: wait-down after MRS drained stale → SurgeCycleComplete ===
	// Simulate MRS having destroyed the oldest stale.
	mrStale, err := safe.StateGetByID[*infraresources.MachineRequest](context.Background(), st, "mr-stale")
	require.NoError(t, err)
	require.NoError(t, st.Destroy(context.Background(), mrStale.Metadata()))

	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)
	plan = engine.Plan(&candidates[0])
	require.Equal(t, ActionSurgeCycleComplete, plan.Action, "reason: %s", plan.Reason)
	assert.True(t, plan.clearSurge)

	require.NoError(t, engine.Execute(context.Background(), plan))

	// Annotations cleared.
	ms, err = safe.StateGetByID[*omni.MachineSet](context.Background(), st, "ms-workers")
	require.NoError(t, err)
	_, hasPhase := ms.Metadata().Annotations().Get(AnnotationSurgePhase)
	assert.False(t, hasPhase, "surge-phase annotation should be cleared after cycle completes")
	_, hasLock := ms.Metadata().Annotations().Get(AnnotationRotationState)
	assert.False(t, hasLock, "rotation-state lock should be cleared after cycle completes")
}

// TestSurge_AbortOnOperatorEdit catches the "operator manually edited
// MachineCount during our cycle" case. Plan detects the drift and
// emits SurgeAborted instead of continuing the cycle with stale
// assumptions.
func TestSurge_AbortOnOperatorEdit(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := surgeOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 3, false)
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	cands, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", cands[0].CurrentGeneration)

	// Pretend we're mid-cycle: wait-up with OriginalCount=2.
	setSurgePhase(t, st, "ms-workers", SurgePhaseState{
		Phase:          SurgePhaseWaitUp,
		OriginalCount:  2,
		CycleStartedAt: time.Unix(900, 0),
	})

	// But the operator just scaled MachineCount to 5 (way out of our
	// expected wait-up value of OriginalCount+1=3).
	setMachineCount(t, st, "ms-workers", 5)

	cands, err = d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&cands[0])

	assert.Equal(t, ActionSurgeAborted, plan.Action, "reason: %s", plan.Reason)

	// Execute the abort → annotations cleared.
	require.NoError(t, engine.Execute(context.Background(), plan))
	ms, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "ms-workers")
	require.NoError(t, err)
	_, hasPhase := ms.Metadata().Annotations().Get(AnnotationSurgePhase)
	assert.False(t, hasPhase)
}

// TestSurge_CPHappyPath verifies that surge works for a control-plane
// MachineClass — the surge flow with min-healthy=2 (default for CP)
// keeps etcd quorum throughout. This is the headline feature surge
// adds: CP rotation was impossible with in-place.
func TestSurge_CPHappyPath(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := map[string]string{
		AnnotationEnabled:  "true",
		AnnotationRole:     "controlplane",
		AnnotationStrategy: "surge",
		// default min-healthy for CP is 2; explicit for clarity.
		AnnotationMinHealthy: "2",
	}
	seedClass(t, st, "cp", `{"cpu":4,"mem":8}`, ann)
	seedSet(t, st, "ms-cp", "cp", 3, true)
	seedRequest(t, st, "cp-1", "ms-cp", `{"cpu":2,"mem":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "cp-2", "ms-cp", `{"cpu":2,"mem":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "cp-3", "ms-cp", `{"cpu":2,"mem":4}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	cands, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "cp", cands[0].CurrentGeneration)

	// Idle → SurgeUp.
	cands, err = d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, cands, 1)
	require.Equal(t, RoleControlPlane, cands[0].Config.Role)
	require.Equal(t, 2, cands[0].Config.MinHealthy)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&cands[0])
	require.Equal(t, ActionSurgeUp, plan.Action, "reason: %s", plan.Reason)
	assert.Equal(t, 3, plan.surgeNext.OriginalCount, "CP at count=3 surges from 3 to 4")

	require.NoError(t, engine.Execute(context.Background(), plan))

	ms, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "ms-cp")
	require.NoError(t, err)
	assert.Equal(t, uint32(4), ms.TypedSpec().Value.MachineAllocation.MachineCount,
		"CP surge bumps count to 4, etcd still has 3 healthy members during boot of the 4th")
}

// TestSurge_MinHealthyFloor refuses to start a cycle when current
// healthy capacity is already below MinHealthy. The cycle wouldn't
// drop healthy below that, but starting a rotation while degraded is
// a fragile state we'd rather not enter.
func TestSurge_MinHealthyFloor(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := surgeOptedInAnnotations()
	ann[AnnotationMinHealthy] = "3"

	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 2, false)
	// Only 2 stale requests provisioned — well below min-healthy=3.
	seedRequest(t, st, "mr-stale-1", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale-2", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	cands, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", cands[0].CurrentGeneration)

	cands, err = d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&cands[0])

	assert.Equal(t, ActionMinHealthyFloor, plan.Action, "reason: %s", plan.Reason)
}

// TestSurge_LockRefreshDuringWait confirms that a wait-style decision
// in the middle of a surge cycle refreshes the rotation-state lock so
// the autoscaler keeps pausing the node group.
func TestSurge_LockRefreshDuringWait(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := surgeOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 3, false)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	cands, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", cands[0].CurrentGeneration)

	// Mid-cycle, wait-up state, with a lock timestamp that's old (was
	// set when the cycle started ~10 minutes ago — past TTL).
	cycleStart := time.Unix(0, 0)
	setSurgePhase(t, st, "ms-workers", SurgePhaseState{
		Phase:          SurgePhaseWaitUp,
		OriginalCount:  2,
		CycleStartedAt: cycleStart,
	})
	// Wait-up expects MachineCount = OriginalCount+1 = 3 (which is the
	// seeded value), so no abort.

	// Stamp an old lock to verify Execute refreshes it.
	_, err = safe.StateUpdateWithConflicts[*omni.MachineSet](
		context.Background(), st,
		omni.NewMachineSet("ms-workers").Metadata(),
		func(m *omni.MachineSet) error {
			m.Metadata().Annotations().Set(AnnotationRotationState,
				formatRotationStateAnnotation("abc12345", cycleStart))
			return nil
		},
	)
	require.NoError(t, err)

	cands, err = d.Discover(context.Background())
	require.NoError(t, err)

	now := time.Unix(600, 0)
	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return now })

	plan := engine.Plan(&cands[0])
	require.Equal(t, ActionWaitingForReady, plan.Action, "reason: %s", plan.Reason)

	// Execute the wait — should refresh lock to `now`.
	require.ErrorIs(t, engine.Execute(context.Background(), plan), ErrCandidateNotActionable)

	ms, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "ms-workers")
	require.NoError(t, err)
	rawLock, _ := ms.Metadata().Annotations().Get(AnnotationRotationState)
	_, ts, ok := parseRotationStateAnnotation(rawLock)
	require.True(t, ok)
	assert.Equal(t, now, ts, "lock timestamp should be refreshed during a wait-style surge tick")
}

// TestSurgePhaseAnnotationRoundtrip pins the persisted format so a
// future format edit on one side without the other becomes a test
// failure rather than a silent stuck cycle in production.
func TestSurgePhaseAnnotationRoundtrip(t *testing.T) {
	t.Parallel()

	in := SurgePhaseState{
		Phase:          SurgePhaseWaitDown,
		OriginalCount:  5,
		CycleStartedAt: time.Unix(1700000000, 0),
	}

	got, ok := parseSurgePhaseAnnotation(formatSurgePhaseAnnotation(in))
	require.True(t, ok)
	assert.Equal(t, in, got)
}

func TestSurgePhaseAnnotationRejectsMalformed(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",
		"wait-up",
		"wait-up:3",
		"wait-up:abc:1000",
		"wait-up:-1:1000",
		"unknown:3:1000",
		"wait-up:3:not-a-number",
	}

	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			_, ok := parseSurgePhaseAnnotation(raw)
			assert.False(t, ok, "should reject %q", raw)
		})
	}
}
