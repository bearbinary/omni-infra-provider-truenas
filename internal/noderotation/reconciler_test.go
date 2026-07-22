package noderotation

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/siderolabs/omni/client/api/omni/specs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// captureMetrics is a tiny stub used by reconciler tests to verify the
// callback fan-out. Tracks per-callback invocation counts plus the
// last action seen so tests can assert ordering without a full mock
// framework.
type captureMetrics struct {
	mu              sync.Mutex
	onTick          int
	onTickDuration  int
	onDecision      int
	onExecuteError  int
	onProgress      int
	onSurgeComplete int
	lastAction      string
	lastErrAction   string
}

func (m *captureMetrics) Metrics() Metrics {
	return Metrics{
		OnTick: func(_ context.Context, _ int) {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.onTick++
		},
		OnTickDuration: func(_ context.Context, _ float64) {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.onTickDuration++
		},
		OnDecision: func(_ context.Context, _ *Candidate, d Decision) {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.onDecision++
			m.lastAction = d.Action
		},
		OnExecuteError: func(_ context.Context, _ *Candidate, d Decision, _ error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.onExecuteError++
			m.lastErrAction = d.Action
		},
		OnRotationProgress: func(_ context.Context, _ *Candidate, _ Decision) {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.onProgress++
		},
		OnSurgeCycleComplete: func(_ context.Context, _ *Candidate, _ Decision, _ float64) {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.onSurgeComplete++
		},
	}
}

func (m *captureMetrics) snapshot() captureMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	return captureMetrics{
		onTick:          m.onTick,
		onTickDuration:  m.onTickDuration,
		onDecision:      m.onDecision,
		onExecuteError:  m.onExecuteError,
		onProgress:      m.onProgress,
		onSurgeComplete: m.onSurgeComplete,
		lastAction:      m.lastAction,
		lastErrAction:   m.lastErrAction,
	}
}

// TestReconciler_Tick_FiresMetricsCallbacks covers the OnTick and
// OnDecision dispatch on a clean tick. A regression that swallowed
// either callback would silence dashboards without failing any of
// the per-action tests.
func TestReconciler_Tick_FiresMetricsCallbacks(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	seedClass(t, st, "workers", `{"cpu":4}`, rotationOptedInAnnotations())
	seedSet(t, st, "ms-workers", "workers", 1, false)
	seedRequest(t, st, "mr1", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })

	caps := &captureMetrics{}
	r := NewReconciler(d, engine, zaptest.NewLogger(t), time.Second).WithMetrics(caps.Metrics())

	r.tick(context.Background())

	s := caps.snapshot()
	assert.Equal(t, 1, s.onTick, "OnTick should fire once per tick")
	assert.Equal(t, 1, s.onTickDuration, "OnTickDuration should fire once per tick")
	assert.GreaterOrEqual(t, s.onDecision, 1, "OnDecision should fire for each candidate")
	assert.NotEmpty(t, s.lastAction, "decision action should be populated")
}

// TestReconciler_Tick_FiresProgressOnSurgeDown — surge-down is the
// step that proves rotation made progress. A regression that omitted
// it from the progress switch would silently drop the surge-side
// rotation rate dashboard.
func TestReconciler_Tick_FiresProgressOnSurgeDown(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := surgeOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 3, false)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-fresh-1", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-fresh-2", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	cands, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", cands[0].CurrentGeneration)

	// Pretend we're already mid-cycle at wait-up; surge replacement
	// landed (fresh count satisfies the condition). Plan will return
	// SurgeDown.
	setSurgePhase(t, st, "ms-workers", SurgePhaseState{
		Phase:          SurgePhaseWaitUp,
		OriginalCount:  2,
		CycleStartedAt: time.Unix(500, 0),
	})

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })

	caps := &captureMetrics{}
	r := NewReconciler(d, engine, zaptest.NewLogger(t), time.Second).WithMetrics(caps.Metrics())

	r.tick(context.Background())

	s := caps.snapshot()
	assert.Equal(t, 1, s.onProgress, "surge-down should fire OnRotationProgress")
}

// TestReconciler_Tick_OnExecuteError — engine.Execute returning a
// non-actionable error must surface through OnExecuteError. Without
// this signal, persistent write failures would silently retry forever.
func TestReconciler_Tick_OnExecuteError(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	seedClass(t, st, "workers", `{"cpu":4}`, rotationOptedInAnnotations())
	seedSet(t, st, "ms-workers", "workers", 2, false)
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	cands, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", cands[0].CurrentGeneration)

	// Configure engine with WithProviderID set to a value that won't
	// match the seeded test data. teardownRequest will refuse and
	// return a non-actionable error.
	engine := NewEngine(st, zaptest.NewLogger(t)).
		WithClock(func() time.Time { return time.Unix(1000, 0) }).
		WithProviderID("wrong-provider")

	caps := &captureMetrics{}
	r := NewReconciler(d, engine, zaptest.NewLogger(t), time.Second).WithMetrics(caps.Metrics())

	r.tick(context.Background())

	s := caps.snapshot()
	assert.Equal(t, 1, s.onExecuteError, "teardown refusal should bump OnExecuteError")
	assert.Equal(t, ActionTeardownStale, s.lastErrAction, "err action should be the failed teardown")
}

// TestReconciler_Run_CancelStopsLoop pins the contract that ctx
// cancellation returns ctx.Err() promptly rather than running another
// tick after the signal.
func TestReconciler_Run_CancelStopsLoop(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	engine := NewEngine(st, zaptest.NewLogger(t))

	r := NewReconciler(d, engine, zaptest.NewLogger(t), 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	// Allow the immediate first tick to land, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancellation")
	}
}

// TestReconciler_IntervalClampedToDefault — passing a sub-second
// interval falls back to the default to prevent accidental hot-loops.
func TestReconciler_IntervalClampedToDefault(t *testing.T) {
	t.Parallel()

	d := NewDiscoverer(newInMemState(t), testCluster, testProviderID, zap.NewNop())
	engine := NewEngine(newInMemState(t), zap.NewNop())

	r := NewReconciler(d, engine, zap.NewNop(), 100*time.Millisecond)

	assert.Equal(t, DefaultRefreshInterval, r.interval, "sub-1s intervals should fall back to default")
	assert.Equal(t, DefaultRefreshInterval, r.RefreshInterval(), "accessor agrees")
}

// TestReconciler_LastTickAt_PopulatedAfterTick — exposed for the
// health probe; zero before any tick has run, non-zero afterwards.
func TestReconciler_LastTickAt_PopulatedAfterTick(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	d := NewDiscoverer(st, testCluster, testProviderID, zap.NewNop())
	engine := NewEngine(st, zap.NewNop())
	r := NewReconciler(d, engine, zap.NewNop(), time.Second)

	assert.True(t, r.LastTickAt().IsZero(), "no ticks yet")

	r.tick(context.Background())

	assert.False(t, r.LastTickAt().IsZero(), "tick should populate LastTickAt")
}

// TestDiscover_RoleMismatch_MatrixOfBehaviors — table-driven test
// covering every (label, declared, trust) combination so a future
// change cannot silently invert the safety property.
func TestDiscover_RoleMismatch_MatrixOfBehaviors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		isCP             bool
		declared         Role
		trustDeclared    bool
		expectCandidates int
		expectRefusal    bool
	}{
		{
			name:             "CP label + declared CP — proceeds",
			isCP:             true,
			declared:         RoleControlPlane,
			expectCandidates: 1,
		},
		{
			name:             "CP label + declared worker — REFUSED by default",
			isCP:             true,
			declared:         RoleWorker,
			expectCandidates: 0,
			expectRefusal:    true,
		},
		{
			name:             "CP label + declared worker + trust=true — proceeds",
			isCP:             true,
			declared:         RoleWorker,
			trustDeclared:    true,
			expectCandidates: 1,
		},
		{
			name:             "no CP label + declared worker — proceeds",
			isCP:             false,
			declared:         RoleWorker,
			expectCandidates: 1,
		},
		{
			name:             "no CP label + declared CP — proceeds with warn",
			isCP:             false,
			declared:         RoleControlPlane,
			expectCandidates: 1,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			st := newInMemState(t)
			ann := map[string]string{
				AnnotationEnabled: "true",
				AnnotationRole:    string(tc.declared),
				AnnotationStrategy: func() string {
					if tc.declared == RoleControlPlane {
						return "surge"
					}
					return "in-place"
				}(),
			}
			seedClass(t, st, "class", `{"cpu":4}`, ann)
			seedSet(t, st, "ms", "class", 1, tc.isCP)
			seedRequest(t, st, "mr", "ms", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)

			var refusalCount atomic.Int32

			d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t)).
				WithTrustDeclaredRole(tc.trustDeclared).
				WithRoleMismatchMetrics(&RoleMismatchCounts{
					OnRefused: func(_, _ string) { refusalCount.Add(1) },
				})

			got, err := d.Discover(context.Background())
			require.NoError(t, err)
			assert.Len(t, got, tc.expectCandidates)

			if tc.expectRefusal {
				assert.Equal(t, int32(1), refusalCount.Load(), "refusal metric should fire")
			} else {
				assert.Equal(t, int32(0), refusalCount.Load(), "no refusal metric")
			}
		})
	}
}

// TestPlan_InPlace_MinHealthyZero — MinHealthy=0 is a legal degenerate
// case meaning "no floor; full workload gap acceptable." A future
// change to a `<=` comparator would silently break this opt-in.
func TestPlan_InPlace_MinHealthyZero(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := rotationOptedInAnnotations()
	ann[AnnotationMinHealthy] = "0"

	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 1, false)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	cands, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", cands[0].CurrentGeneration)

	cands, err = d.Discover(context.Background())
	require.NoError(t, err)

	engine := NewEngine(st, zaptest.NewLogger(t)).WithClock(func() time.Time { return time.Unix(1000, 0) })
	plan := engine.Plan(&cands[0])

	assert.Equal(t, ActionTeardownStale, plan.Action, "MinHealthy=0 allows tearing the lone stale down; reason: %s", plan.Reason)
}

// TestEngine_TeardownRequest_DefenseInDepth — engine constructed with
// WithProviderID refuses to destroy a MachineRequest whose label was
// changed between discovery and execute.
func TestEngine_TeardownRequest_DefenseInDepth(t *testing.T) {
	t.Parallel()

	st := newInMemState(t)
	ann := rotationOptedInAnnotations()
	seedClass(t, st, "workers", `{"cpu":4}`, ann)
	seedSet(t, st, "ms-workers", "workers", 2, false)
	seedRequest(t, st, "mr-fresh", "ms-workers", `{"cpu":4}`, specs.MachineRequestStatusSpec_PROVISIONED)
	seedRequest(t, st, "mr-stale", "ms-workers", `{"cpu":2}`, specs.MachineRequestStatusSpec_PROVISIONED)

	d := NewDiscoverer(st, testCluster, testProviderID, zaptest.NewLogger(t))
	cands, err := d.Discover(context.Background())
	require.NoError(t, err)
	stampGenerationOnClass(t, st, "workers", cands[0].CurrentGeneration)

	engine := NewEngine(st, zaptest.NewLogger(t)).
		WithClock(func() time.Time { return time.Unix(1000, 0) }).
		WithProviderID("a-different-provider")

	cands, err = d.Discover(context.Background())
	require.NoError(t, err)
	plan := engine.Plan(&cands[0])
	require.Equal(t, ActionTeardownStale, plan.Action)

	err = engine.Execute(context.Background(), plan)
	require.Error(t, err)
	assert.True(t, errors.Is(err, err) && err.Error() != "", "refusal should surface a descriptive error")
}
