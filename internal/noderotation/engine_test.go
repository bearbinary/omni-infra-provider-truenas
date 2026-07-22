package noderotation

import (
	"context"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/omni/client/api/omni/specs"
	infraresources "github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	testProviderID = "truenas"
	testCluster    = "talos-home"
)

func newInMemState(t *testing.T) state.State {
	t.Helper()
	return state.WrapCore(namespaced.NewState(inmem.Build))
}

func seedClass(t *testing.T, st state.State, id string, providerData string, ann map[string]string) *omni.MachineClass {
	t.Helper()

	mc := omni.NewMachineClass(id)

	mc.TypedSpec().Value.AutoProvision = &specs.MachineClassSpec_Provision{
		ProviderId:   testProviderID,
		ProviderData: providerData,
	}

	for k, v := range ann {
		mc.Metadata().Annotations().Set(k, v)
	}

	require.NoError(t, st.Create(context.Background(), mc))

	return mc
}

func seedSet(t *testing.T, st state.State, id, classID string, count uint32, isCP bool) *omni.MachineSet {
	t.Helper()

	ms := omni.NewMachineSet(id)
	ms.Metadata().Labels().Set(omni.LabelCluster, testCluster)

	if isCP {
		ms.Metadata().Labels().Set(omni.LabelControlPlaneRole, "")
	} else {
		ms.Metadata().Labels().Set(omni.LabelWorkerRole, "")
	}

	ms.TypedSpec().Value.MachineAllocation = &specs.MachineSetSpec_MachineAllocation{
		Name:           classID,
		MachineCount:   count,
		AllocationType: specs.MachineSetSpec_MachineAllocation_Static,
	}

	require.NoError(t, st.Create(context.Background(), ms))

	return ms
}

func seedRequest(t *testing.T, st state.State, id, machineSetID, providerData string, stage specs.MachineRequestStatusSpec_Stage) *infraresources.MachineRequest {
	t.Helper()

	mr := infraresources.NewMachineRequest(id)
	mr.Metadata().Labels().Set(omni.LabelMachineRequestSet, machineSetID)
	mr.Metadata().Labels().Set(omni.LabelInfraProviderID, testProviderID)

	mr.TypedSpec().Value.ProviderData = providerData

	require.NoError(t, st.Create(context.Background(), mr))

	// Seed a matching MachineRequestStatus so the discoverer can
	// classify provisioning state. Same ID as the MachineRequest;
	// also carries LabelInfraProviderID so Discoverer's per-tick
	// list-by-provider-ID query finds it (production Omni stamps the
	// same label on the status resource).
	mrs := infraresources.NewMachineRequestStatus(id)
	mrs.Metadata().Labels().Set(omni.LabelInfraProviderID, testProviderID)
	mrs.TypedSpec().Value.Stage = stage

	require.NoError(t, st.Create(context.Background(), mrs))

	return mr
}

// rotationOptedInAnnotations builds the canonical opt-in map for
// worker + in-place rotation. Tests override individual keys when
// needed.
func rotationOptedInAnnotations() map[string]string {
	return map[string]string{
		AnnotationEnabled:  "true",
		AnnotationRole:     "worker",
		AnnotationStrategy: "in-place",
	}
}

// TestDiscover_NotOptedIn confirms the cheap pre-filter. A MachineClass
// without enabled=true is invisible to the reconciler.
func TestDiscover_NotOptedIn(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	seedClass(t, st, "plain-workers", `{"cpu":2}`, nil)
	seedSet(t, st, "ms-workers", "plain-workers", 3, false)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))

	got, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestDiscover_FreshAndStale walks the happy path: one MachineClass
// opted in with two requests, one fresh and one stale (provider data
// changed). The reconciler should see one Candidate with the stale
// request flagged.
func TestDiscover_FreshAndStale(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)

	seedClass(t, st, "workers", `{"cpu":4}`, rotationOptedInAnnotations())
	seedSet(t, st, "ms-workers", "workers", 2, false)

	// One request matches current spec (fresh), one carries the old
	// spec (stale).
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))

	got, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)

	c := got[0]
	assert.Len(t, c.Requests, 2)
	assert.Len(t, c.StaleRequests(), 1)
	assert.Equal(t, "mr-stale", c.StaleRequests()[0].Request.Metadata().ID())
	assert.Equal(t, 1, c.FreshCount())
}

// TestPlan_AllFresh returns ActionNone — nothing to do.
func TestPlan_AllFresh(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	seedClass(t, st, "workers", `{"cpu":4}`, rotationOptedInAnnotations())
	seedSet(t, st, "ms-workers", "workers", 1, false)
	seedRequest(t, st, "mr1", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	candidates, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	// Stamp the class-generation up-front so the engine doesn't try to
	// stamp it before noticing all-fresh.
	stampGenerationOnClass(t, st, "workers", candidates[0].CurrentGeneration)
	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&candidates[0])

	assert.Equal(t, ActionNone, plan.Action, "plan: %+v", plan)
}

// TestPlan_NeedsStamp emits the stamp-generation step when the class
// hasn't been stamped yet.
func TestPlan_NeedsStamp(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	seedClass(t, st, "workers", `{"cpu":4}`, rotationOptedInAnnotations())
	seedSet(t, st, "ms-workers", "workers", 1, false)
	seedRequest(t, st, "mr1", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	candidates, err := d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&candidates[0])

	assert.Equal(t, ActionStampGeneration, plan.Action)
}

// TestPlan_InPlaceTeardown — class-gen stamped, one stale request,
// healthy budget allows the teardown.
func TestPlan_InPlaceTeardown(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := rotationOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 2, false)
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	candidates, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	stampGenerationOnClass(t, st, "workers", candidates[0].CurrentGeneration)
	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&candidates[0])

	assert.Equal(t, ActionTeardownStale, plan.Action)
	assert.Equal(t, "mr-stale", plan.TargetRequestID)
}

// TestPlan_MinHealthyFloor — a single-machine MachineSet with
// MinHealthy=1 cannot in-place rotate: tearing the lone Machine down
// would drop healthy to 0.
func TestPlan_MinHealthyFloor(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := rotationOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 1, false)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	candidates, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	stampGenerationOnClass(t, st, "workers", candidates[0].CurrentGeneration)
	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&candidates[0])

	assert.Equal(t, ActionMinHealthyFloor, plan.Action)
}

// TestPlan_WaitingForProvisioning — the engine refuses to start a new
// teardown while a previous request is still in PROVISIONING.
func TestPlan_WaitingForProvisioning(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := rotationOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 3, false)
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-provisioning", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONING)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	candidates, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	stampGenerationOnClass(t, st, "workers", candidates[0].CurrentGeneration)
	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&candidates[0])

	assert.Equal(t, ActionWaitingForReady, plan.Action)
}

// TestPlan_SurgeIdleStartsCycle — at idle (no surge-phase annotation)
// with stale requests, the engine plans a SurgeUp to bump MachineCount
// and stamp wait-up.
func TestPlan_SurgeIdleStartsCycle(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := rotationOptedInAnnotations()
	ann[AnnotationStrategy] = "surge"
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 2, false)
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	candidates, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	stampGenerationOnClass(t, st, "workers", candidates[0].CurrentGeneration)
	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&candidates[0])

	assert.Equal(t, ActionSurgeUp, plan.Action)
	assert.Equal(t, SurgePhaseWaitUp, plan.surgeNext.Phase)
	assert.Equal(t, 2, plan.surgeNext.OriginalCount)
}

// TestPlan_LockHeldByPriorStep — when the MachineSet carries an
// unexpired rotation-state annotation, the engine defers.
func TestPlan_LockHeldByPriorStep(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := rotationOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	ms := seedSet(t, st, "ms-workers", "workers", 2, false)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	// Pre-stamp the class-gen so Plan reaches the lock branch (otherwise
	// the stamp-generation branch fires first now that it runs before
	// the lock check — metadata-only stamping doesn't need to wait
	// behind a lock from another reconciler).
	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	candidates, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	stampGenerationOnClass(t, st, "workers", candidates[0].CurrentGeneration)

	// Stamp a lock 30s ago — well within DefaultLockTTL.
	now := time.Unix(2000, 0)
	_, err = safe.StateUpdateWithConflicts[*omni.MachineSet](
		context.Background(), st,
		omni.NewMachineSet(ms.Metadata().ID()).Metadata(),
		func(m *omni.MachineSet) error {
			m.Metadata().Annotations().Set(AnnotationRotationState,
				formatRotationStateAnnotation("dummy-gen", now.Add(-30*time.Second)))
			return nil
		},
	)
	require.NoError(t, err)

	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return now })
	plan := engine.Plan(&candidates[0])

	assert.Equal(t, ActionLockedByPriorStep, plan.Action)
}

// TestExecute_StampGeneration verifies the class gets the
// AnnotationClassGeneration annotation after Execute.
func TestExecute_StampGeneration(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := rotationOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 1, false)
	seedRequest(t, st, "mr1", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	candidates, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&candidates[0])
	require.Equal(t, ActionStampGeneration, plan.Action)

	require.NoError(t, engine.Execute(context.Background(), plan))

	mc, err := safe.StateGetByID[*omni.MachineClass](context.Background(), st, "workers")
	require.NoError(t, err)

	got, _ := mc.Metadata().Annotations().Get(AnnotationClassGeneration)
	assert.Equal(t, candidates[0].CurrentGeneration, got)
}

// TestExecute_TeardownStale verifies the stale MachineRequest is
// destroyed and the lock annotation is set+cleared.
func TestExecute_TeardownStale(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := rotationOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 2, false)
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	candidates, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	// Stamp class-gen first, so subsequent Plan returns Teardown.
	stampGenerationOnClass(t, st, "workers", candidates[0].CurrentGeneration)
	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&candidates[0])
	require.Equal(t, ActionTeardownStale, plan.Action)

	require.NoError(t, engine.Execute(context.Background(), plan))

	// Verify the MachineRequest is gone (Destroy on COSI is sync for
	// inmem state because there are no finalizers to drain).
	_, err = safe.StateGetByID[*infraresources.MachineRequest](context.Background(), st, "mr-stale")
	assert.Error(t, err, "stale request should be destroyed")

	// Verify the lock was cleared.
	ms, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "ms-workers")
	require.NoError(t, err)
	_, hasLock := ms.Metadata().Annotations().Get(AnnotationRotationState)
	assert.False(t, hasLock, "lock annotation should be cleared after successful teardown")
}

// TestExecute_SurgeUp_AnnotationsAndCountLandAtomically pins the
// surge-step atomicity fix. Earlier the annotation write and the
// MachineCount bump were separate CAS calls; a crash between them
// would leave phase=WaitUp with the count still at OriginalCount,
// forcing planSurge to abort the cycle via AbortKindCountDriftWaitUp
// on the next tick. Both mutations now live in one closure — after
// SurgeUp, EITHER neither annotation nor count moved (CAS failed) OR
// both moved (CAS committed). The intermediate "phase set, count
// still N" state is unreachable.
func TestExecute_SurgeUp_AnnotationsAndCountLandAtomically(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := rotationOptedInAnnotations()
	ann[AnnotationStrategy] = string(StrategySurge)
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 3, false)
	seedRequest(t, st, "mr-stale-1", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale-2", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale-3", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	candidates, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", candidates[0].CurrentGeneration)

	candidates, err = d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&candidates[0])
	require.Equal(t, ActionSurgeUp, plan.Action, "reason: %s", plan.Reason)

	require.NoError(t, engine.Execute(context.Background(), plan))

	ms, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "ms-workers")
	require.NoError(t, err)

	assert.Equal(t, uint32(4), ms.TypedSpec().Value.MachineAllocation.MachineCount,
		"MachineCount must be bumped 3→4 in the same CAS that wrote the surge annotations")

	_, hasLock := ms.Metadata().Annotations().Get(AnnotationRotationState)
	assert.True(t, hasLock, "rotation-state lock must be set in the same CAS as the count bump")

	phaseRaw, hasPhase := ms.Metadata().Annotations().Get(AnnotationSurgePhase)
	assert.True(t, hasPhase, "surge-phase annotation must be set in the same CAS as the count bump")
	assert.Contains(t, phaseRaw, "wait-up", "surge phase should advance to wait-up after SurgeUp")
}

// stampGenerationOnClass is a test helper to set the class-generation
// annotation directly so Plan skips the stamp step and reaches the
// teardown/none branches.
func stampGenerationOnClass(t *testing.T, st state.State, classID, gen string) {
	t.Helper()

	_, err := safe.StateUpdateWithConflicts[*omni.MachineClass](
		context.Background(), st,
		omni.NewMachineClass(classID).Metadata(),
		func(mc *omni.MachineClass) error {
			mc.Metadata().Annotations().Set(AnnotationClassGeneration, gen)
			return nil
		},
	)
	require.NoError(t, err)
}

// TestRotationLockAnnotationRoundtrip — the format we write must be
// the format the autoscaler parses. Pins the contract.
func TestRotationLockAnnotationRoundtrip(t *testing.T) {
	t.Parallel()

	now := time.Unix(1700000000, 0)
	raw := formatRotationStateAnnotation("abc12345", now)

	gen, ts, ok := parseRotationStateAnnotation(raw)
	require.True(t, ok)
	assert.Equal(t, "abc12345", gen)
	assert.Equal(t, now, ts)
}

// silence unused-import warning if test helpers shrink in the future.
var _ resource.Resource = (*omni.MachineClass)(nil)
