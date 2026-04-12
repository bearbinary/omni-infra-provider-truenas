package singleton_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cosiresource "github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/singleton"
)

const testProviderID = "truenas-test"

// fakeClock is a manually-advanced clock for deterministic time in tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

func newTestState() state.State {
	return state.WrapCore(namespaced.NewState(inmem.Build))
}

func newLease(t *testing.T, st state.State, instanceID string, clock singleton.Clock) *singleton.Lease {
	t.Helper()

	l, err := singleton.New(st, singleton.Config{
		ProviderID:      testProviderID,
		InstanceID:      instanceID,
		RefreshInterval: 10 * time.Second,
		StaleAfter:      30 * time.Second,
		Clock:           clock,
	}, zaptest.NewLogger(t))
	require.NoError(t, err)

	return l
}

func getStatus(t *testing.T, st state.State) *infra.ProviderStatus {
	t.Helper()

	res, err := st.Get(context.Background(), infra.NewProviderStatus(testProviderID).Metadata())
	require.NoError(t, err)

	ps, ok := res.(*infra.ProviderStatus)
	require.True(t, ok, "expected *infra.ProviderStatus, got %T", res)

	return ps
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	st := newTestState()

	t.Run("missing provider id", func(t *testing.T) {
		t.Parallel()

		_, err := singleton.New(st, singleton.Config{}, logger)
		require.Error(t, err)
	})

	t.Run("stale-after too short", func(t *testing.T) {
		t.Parallel()

		_, err := singleton.New(st, singleton.Config{
			ProviderID:      testProviderID,
			RefreshInterval: 30 * time.Second,
			StaleAfter:      30 * time.Second,
		}, logger)
		require.Error(t, err)
	})

	t.Run("auto instance id", func(t *testing.T) {
		t.Parallel()

		l, err := singleton.New(st, singleton.Config{ProviderID: testProviderID}, logger)
		require.NoError(t, err)
		assert.NotEmpty(t, l.InstanceID())
	})
}

func TestAcquireFreshCreate(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := newLease(t, st, "inst-A", clock)

	require.NoError(t, l.Acquire(context.Background()))

	ps := getStatus(t, st)
	owner, ok := ps.Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	require.True(t, ok)
	assert.Equal(t, "inst-A", owner)

	heartbeat, ok := ps.Metadata().Annotations().Get(singleton.AnnotationHeartbeat)
	require.True(t, ok)

	parsed, err := time.Parse(time.RFC3339Nano, heartbeat)
	require.NoError(t, err)
	assert.True(t, parsed.Equal(clock.Now()), "heartbeat %s should equal clock %s", parsed, clock.Now())
}

func TestAcquireTakesOverStale(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	// Seed an old lease.
	first := newLease(t, st, "inst-old", clock)
	require.NoError(t, first.Acquire(context.Background()))

	// Advance past StaleAfter (30s).
	clock.advance(60 * time.Second)

	second := newLease(t, st, "inst-new", clock)
	require.NoError(t, second.Acquire(context.Background()))

	ps := getStatus(t, st)
	owner, _ := ps.Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	assert.Equal(t, "inst-new", owner)
}

func TestAcquireReentrantRefreshesHeartbeat(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := newLease(t, st, "inst-A", clock)

	require.NoError(t, l.Acquire(context.Background()))
	firstHB, _ := getStatus(t, st).Metadata().Annotations().Get(singleton.AnnotationHeartbeat)

	clock.advance(5 * time.Second)
	require.NoError(t, l.Acquire(context.Background()))
	secondHB, _ := getStatus(t, st).Metadata().Annotations().Get(singleton.AnnotationHeartbeat)

	assert.NotEqual(t, firstHB, secondHB, "heartbeat should have advanced on re-acquire")
}

func TestAcquireFailsWhenFreshLeaseHeld(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	first := newLease(t, st, "inst-A", clock)
	require.NoError(t, first.Acquire(context.Background()))

	// Second attempt, same clock — heartbeat is 0s old.
	second := newLease(t, st, "inst-B", clock)
	err := second.Acquire(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, singleton.ErrLeaseHeld)

	var held *singleton.LeaseHeldError

	require.True(t, errors.As(err, &held))
	assert.Equal(t, "inst-A", held.OtherInstanceID)
	assert.Equal(t, testProviderID, held.ProviderID)

	// Confirm the server state was NOT mutated — inst-A still owns it.
	owner, _ := getStatus(t, st).Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	assert.Equal(t, "inst-A", owner)
}

func TestAcquireTakesOverMalformedHeartbeat(t *testing.T) {
	t.Parallel()

	st := newTestState()
	ctx := context.Background()

	// Seed a ProviderStatus with a bogus heartbeat.
	seed := infra.NewProviderStatus(testProviderID)
	seed.Metadata().Annotations().Set(singleton.AnnotationInstanceID, "inst-old")
	seed.Metadata().Annotations().Set(singleton.AnnotationHeartbeat, "not-a-timestamp")
	require.NoError(t, st.Create(ctx, seed))

	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := newLease(t, st, "inst-new", clock)
	require.NoError(t, l.Acquire(ctx))

	owner, _ := getStatus(t, st).Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	assert.Equal(t, "inst-new", owner)
}

func TestRefreshUpdatesHeartbeat(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	l, err := singleton.New(st, singleton.Config{
		ProviderID:      testProviderID,
		InstanceID:      "inst-A",
		RefreshInterval: 20 * time.Millisecond,
		StaleAfter:      1 * time.Second,
		Clock:           clock,
	}, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, l.Acquire(context.Background()))
	firstHB, _ := getStatus(t, st).Metadata().Annotations().Get(singleton.AnnotationHeartbeat)

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)

	go func() { runDone <- l.Run(ctx) }()

	// Wait for at least one tick + advance the clock so the new heartbeat is different.
	require.Eventually(t, func() bool {
		clock.advance(10 * time.Millisecond)

		hb, ok := getStatus(t, st).Metadata().Annotations().Get(singleton.AnnotationHeartbeat)

		return ok && hb != firstHB
	}, time.Second, 5*time.Millisecond, "heartbeat should refresh on tick")

	cancel()
	select {
	case err := <-runDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}

	// Clean shutdown via ctx does NOT count as "lost".
	select {
	case <-l.Lost():
		t.Fatal("Lost channel closed on clean shutdown")
	default:
	}
}

func TestRefreshDetectsSteal(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	l, err := singleton.New(st, singleton.Config{
		ProviderID:      testProviderID,
		InstanceID:      "inst-A",
		RefreshInterval: 20 * time.Millisecond,
		StaleAfter:      1 * time.Second,
		Clock:           clock,
	}, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, l.Acquire(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)

	go func() { runDone <- l.Run(ctx) }()

	// Externally rewrite annotations to simulate a steal.
	stealDone := make(chan struct{})

	go func() {
		defer close(stealDone)

		require.Eventually(t, func() bool {
			_, updateErr := st.UpdateWithConflicts(ctx,
				infra.NewProviderStatus(testProviderID).Metadata(),
				func(r cosiresource.Resource) error {
					ps := r.(*infra.ProviderStatus) //nolint:forcetypeassert
					ps.Metadata().Annotations().Set(singleton.AnnotationInstanceID, "inst-thief")
					ps.Metadata().Annotations().Set(singleton.AnnotationHeartbeat, clock.Now().UTC().Format(time.RFC3339Nano))

					return nil
				})

			return updateErr == nil
		}, time.Second, 5*time.Millisecond)
	}()
	<-stealDone

	select {
	case <-l.Lost():
	case <-time.After(2 * time.Second):
		t.Fatal("Lost channel never closed after steal")
	}

	select {
	case err := <-runDone:
		require.ErrorIs(t, err, singleton.ErrLeaseHeld)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after steal")
	}
}

func TestReleaseClearsAnnotations(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := newLease(t, st, "inst-A", clock)

	require.NoError(t, l.Acquire(context.Background()))
	l.Release(context.Background())

	ps := getStatus(t, st)
	_, hasOwner := ps.Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	_, hasHeartbeat := ps.Metadata().Annotations().Get(singleton.AnnotationHeartbeat)
	assert.False(t, hasOwner, "instance-id annotation should have been cleared")
	assert.False(t, hasHeartbeat, "heartbeat annotation should have been cleared")

	// Successor acquires immediately without waiting for staleAfter.
	successor := newLease(t, st, "inst-B", clock)
	require.NoError(t, successor.Acquire(context.Background()))

	owner, _ := getStatus(t, st).Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	assert.Equal(t, "inst-B", owner)
}

func TestReleaseLeavesForeignLeaseAlone(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	// inst-A takes ownership.
	a := newLease(t, st, "inst-A", clock)
	require.NoError(t, a.Acquire(context.Background()))

	// inst-B attempts release while A still holds it — should be a no-op.
	b := newLease(t, st, "inst-B", clock)
	b.Release(context.Background())

	owner, _ := getStatus(t, st).Metadata().Annotations().Get(singleton.AnnotationInstanceID)
	assert.Equal(t, "inst-A", owner)
}

// TestAcquireConcurrentOnlyOneWins exercises the Create-then-fall-through-to-CAS
// race path. N goroutines call Acquire simultaneously against an empty state.
// Exactly one must return nil; the rest must return LeaseHeldError.
//
// This is the exact scenario that happens if two pods start at the same instant
// during a rolling deploy — we need to guarantee exactly one leader.
func TestAcquireConcurrentOnlyOneWins(t *testing.T) {
	t.Parallel()

	const goroutines = 20

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	var (
		start       = make(chan struct{})
		wg          sync.WaitGroup
		wins        atomic.Int32
		heldErrors  atomic.Int32
		otherErrors atomic.Int32
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			l := newLease(t, st, fmt.Sprintf("inst-%d", id), clock)

			<-start

			switch err := l.Acquire(context.Background()); {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, singleton.ErrLeaseHeld):
				heldErrors.Add(1)
			default:
				otherErrors.Add(1)
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}

	close(start)
	wg.Wait()

	assert.Equal(t, int32(1), wins.Load(), "exactly one Acquire must succeed")
	assert.Equal(t, int32(goroutines-1), heldErrors.Load(), "every other Acquire must see LeaseHeldError")
	assert.Equal(t, int32(0), otherErrors.Load(), "no unexpected errors")
}

// TestStaleBoundary pins the "exactly at staleAfter" behavior. Our code treats
// ages strictly less than StaleAfter as fresh, so an age of exactly StaleAfter
// is eligible for takeover. This locks in the semantic so a future `<=` typo
// doesn't silently change it.
func TestStaleBoundary(t *testing.T) {
	t.Parallel()

	t.Run("just before boundary held", func(t *testing.T) {
		t.Parallel()

		st := newTestState()
		clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

		holder := newLease(t, st, "inst-old", clock)
		require.NoError(t, holder.Acquire(context.Background()))

		// Advance to staleAfter - 1ns. Should still be held.
		clock.advance(30*time.Second - time.Nanosecond)

		challenger := newLease(t, st, "inst-new", clock)
		err := challenger.Acquire(context.Background())
		require.ErrorIs(t, err, singleton.ErrLeaseHeld)
	})

	t.Run("exactly at boundary eligible", func(t *testing.T) {
		t.Parallel()

		st := newTestState()
		clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

		holder := newLease(t, st, "inst-old", clock)
		require.NoError(t, holder.Acquire(context.Background()))

		clock.advance(30 * time.Second) // == staleAfter

		challenger := newLease(t, st, "inst-new", clock)
		require.NoError(t, challenger.Acquire(context.Background()))

		owner, _ := getStatus(t, st).Metadata().Annotations().Get(singleton.AnnotationInstanceID)
		assert.Equal(t, "inst-new", owner)
	})
}

// TestReleaseOnMissingResource covers the path where the ProviderStatus was
// deleted externally (e.g., operator ran omnictl delete). Release must swallow
// the NotFound and not log-and-return a noisy error.
func TestReleaseOnMissingResource(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := newLease(t, st, "inst-A", clock)
	ctx := context.Background()

	require.NoError(t, l.Acquire(ctx))

	// Delete the ProviderStatus externally.
	require.NoError(t, st.Destroy(ctx, infra.NewProviderStatus(testProviderID).Metadata()))

	// Must not panic or error visibly.
	l.Release(ctx)
}

// TestLeaseHeldErrorFormat pins the error string format so support engineers
// can rely on it when diagnosing incidents via logs.
func TestLeaseHeldErrorFormat(t *testing.T) {
	t.Parallel()

	err := &singleton.LeaseHeldError{
		ProviderID:      "truenas-prod",
		OtherInstanceID: "0192abcd-ef01-7000-8000-000000000000",
		HeartbeatAt:     time.Date(2026, 4, 11, 12, 34, 56, 0, time.UTC),
		HeartbeatAge:    7 * time.Second,
	}

	msg := err.Error()
	assert.Contains(t, msg, "truenas-prod")
	assert.Contains(t, msg, "0192abcd-ef01-7000-8000-000000000000")
	assert.Contains(t, msg, "7s")
	assert.Contains(t, msg, "2026-04-11T12:34:56Z")
	assert.Contains(t, msg, "PROVIDER_SINGLETON_ENABLED=false")
}

// --- error-injection test harness -------------------------------------------

// flakyState wraps a state.State and forces UpdateWithConflicts to fail for
// the first N calls. Used to test Run's transient-error recovery and
// abandonment thresholds.
type flakyState struct {
	state.State

	mu              sync.Mutex
	remainingErrors int
	injectedErr     error
}

func (s *flakyState) UpdateWithConflicts(
	ctx context.Context, ptr cosiresource.Pointer, f state.UpdaterFunc, opts ...state.UpdateOption,
) (cosiresource.Resource, error) {
	s.mu.Lock()

	if s.remainingErrors > 0 {
		s.remainingErrors--
		s.mu.Unlock()

		return nil, s.injectedErr
	}

	s.mu.Unlock()

	return s.State.UpdateWithConflicts(ctx, ptr, f, opts...)
}

func TestRunRecoversFromTransientError(t *testing.T) {
	t.Parallel()

	base := newTestState()
	flaky := &flakyState{
		State:           base,
		remainingErrors: 2, // Run survives 2 transient errors (threshold is 3).
		injectedErr:     errors.New("transient gRPC blip"),
	}

	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	// Acquire against the base state (bypasses the flaky wrapper so the
	// initial write happens cleanly — we only want Run's refresh path to
	// exercise the flaky behavior).
	prep := newLease(t, base, "inst-A", clock)
	require.NoError(t, prep.Acquire(context.Background()))

	// Build the Run loop lease against the flaky wrapper.
	l, err := singleton.New(flaky, singleton.Config{
		ProviderID:      testProviderID,
		InstanceID:      "inst-A",
		RefreshInterval: 10 * time.Millisecond,
		StaleAfter:      1 * time.Second,
		Clock:           clock,
	}, zaptest.NewLogger(t))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)

	go func() { runDone <- l.Run(ctx) }()

	// Wait long enough for > 2 refresh ticks. After 2 failures, the 3rd tick
	// must succeed and reset the counter, preventing abandonment.
	require.Eventually(t, func() bool {
		flaky.mu.Lock()
		defer flaky.mu.Unlock()

		return flaky.remainingErrors == 0
	}, time.Second, 5*time.Millisecond, "flaky state should have exhausted injected errors")

	// Give Run a chance to do several more successful refreshes.
	time.Sleep(50 * time.Millisecond)

	// Lost must NOT be closed — transient errors below the threshold should
	// not abandon the lease.
	select {
	case <-l.Lost():
		t.Fatal("Lost channel closed after recoverable errors — threshold logic broken")
	default:
	}

	cancel()

	select {
	case runErr := <-runDone:
		// Clean context cancel → nil return.
		require.NoError(t, runErr)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestRunAbandonsAfterConsecutiveErrors(t *testing.T) {
	t.Parallel()

	base := newTestState()
	flaky := &flakyState{
		State:           base,
		remainingErrors: 1000, // Far more than the threshold — permanent failure.
		injectedErr:     errors.New("permanent gRPC failure"),
	}

	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	prep := newLease(t, base, "inst-A", clock)
	require.NoError(t, prep.Acquire(context.Background()))

	l, err := singleton.New(flaky, singleton.Config{
		ProviderID:      testProviderID,
		InstanceID:      "inst-A",
		RefreshInterval: 10 * time.Millisecond,
		StaleAfter:      1 * time.Second,
		Clock:           clock,
	}, zaptest.NewLogger(t))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)

	go func() { runDone <- l.Run(ctx) }()

	// Lost must close after consecutive failures trip the threshold.
	select {
	case <-l.Lost():
	case <-time.After(2 * time.Second):
		t.Fatal("Lost channel never closed despite permanent refresh failures")
	}

	// Run must return with an error describing the failure.
	select {
	case runErr := <-runDone:
		require.Error(t, runErr)
		assert.Contains(t, runErr.Error(), "refresh failed")
	case <-time.After(time.Second):
		t.Fatal("Run did not return after abandonment")
	}
}
