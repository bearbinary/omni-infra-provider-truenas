package singleton_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	cosiresource "github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/singleton"
)

// wallclockProvider satisfies singleton.Clock using real wall time. Used by
// tests that rely on the state impl's server-set Updated() matching "now".
type wallclockProvider struct{}

func (wallclockProvider) Now() time.Time { return time.Now().UTC() }

// TestLease_FirstAcquireStartsEpoch1 pins the contract that a fresh resource
// (no prior ProviderStatus) starts at epoch 1. Observers can treat epoch 0 as
// "pre-fencing era" and epoch ≥ 1 as "fencing-aware v0.15+".
func TestLease_FirstAcquireStartsEpoch1(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := newLease(t, st, "inst-A", clock)

	require.NoError(t, l.Acquire(context.Background()))
	assert.Equal(t, uint64(1), l.Epoch(), "first holder starts at epoch 1")

	ps := getStatus(t, st)
	raw, ok := ps.Metadata().Annotations().Get(singleton.AnnotationEpoch)
	require.True(t, ok, "epoch annotation must be written on first acquire")
	assert.Equal(t, "1", raw)
}

// TestLease_TakeoverBumpsEpoch verifies the core fencing invariant: every
// takeover advances the epoch by one. Future fencing-aware downstream writers
// can reject mutations tagged with a lower epoch.
func TestLease_TakeoverBumpsEpoch(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	// Seed first holder (epoch 1).
	first := newLease(t, st, "inst-old", clock)
	require.NoError(t, first.Acquire(context.Background()))
	require.Equal(t, uint64(1), first.Epoch())

	// Let the lease go stale, then take over.
	clock.advance(60 * time.Second)

	second := newLease(t, st, "inst-new", clock)
	require.NoError(t, second.Acquire(context.Background()))

	assert.Equal(t, uint64(2), second.Epoch(), "takeover must bump epoch by exactly 1")

	ps := getStatus(t, st)
	raw, _ := ps.Metadata().Annotations().Get(singleton.AnnotationEpoch)
	assert.Equal(t, "2", raw, "the written annotation must match the holder's in-memory epoch")
}

// TestLease_UnclaimedTakeoverBumpsEpoch pins behavior for the case where the
// ProviderStatus resource exists but has no instance-id annotation. This
// happens on the Release path and must still advance the epoch so observers
// can distinguish the new holder's generation from the previous one.
func TestLease_UnclaimedTakeoverBumpsEpoch(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	// Seed a resource with a pre-existing epoch but no owner.
	seed := infra.NewProviderStatus(testProviderID)
	seed.Metadata().Annotations().Set(singleton.AnnotationEpoch, "7")
	require.NoError(t, st.Create(context.Background(), seed))

	l := newLease(t, st, "inst-takeover", clock)
	require.NoError(t, l.Acquire(context.Background()))

	assert.Equal(t, uint64(8), l.Epoch(),
		"unclaimed-but-existing resource should bump epoch from the last recorded value")
}

// TestLease_MalformedHeartbeatTakeoverBumps exercises the safety branch:
// when the heartbeat annotation is corrupted, the Lease takes over AND bumps
// the epoch rather than silently preserving the broken state.
func TestLease_MalformedHeartbeatTakeoverBumps(t *testing.T) {
	t.Parallel()

	st := newTestState()
	ctx := context.Background()

	// Seed with a valid instance-id + epoch but unparseable heartbeat.
	seed := infra.NewProviderStatus(testProviderID)
	seed.Metadata().Annotations().Set(singleton.AnnotationInstanceID, "inst-old")
	seed.Metadata().Annotations().Set(singleton.AnnotationHeartbeat, "not-a-timestamp")
	seed.Metadata().Annotations().Set(singleton.AnnotationEpoch, "3")
	require.NoError(t, st.Create(ctx, seed))

	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := newLease(t, st, "inst-new", clock)
	require.NoError(t, l.Acquire(ctx))

	assert.Equal(t, uint64(4), l.Epoch(),
		"malformed-heartbeat takeover must still advance the epoch")
}

// TestLease_ReentrantAcquirePreservesEpoch pins the contract that a second
// Acquire() by the SAME instance does not bump the epoch. Otherwise a slow
// retry loop would appear to observers as repeated takeovers.
func TestLease_ReentrantAcquirePreservesEpoch(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := newLease(t, st, "inst-A", clock)

	require.NoError(t, l.Acquire(context.Background()))
	initialEpoch := l.Epoch()

	// Advance clock within StaleAfter so the lease is still considered fresh.
	clock.advance(5 * time.Second)

	require.NoError(t, l.Acquire(context.Background()))
	assert.Equal(t, initialEpoch, l.Epoch(),
		"re-entrant acquire by the same instance must not bump the epoch")
}

// TestLease_RefreshDetectsEpochAdvanceAsStolen exercises the fence-under-us
// branch: if an out-of-band writer bumps the on-disk epoch while we think we
// own the lease, the next refresh call marks us as stolen.
//
// We drive this through Run() with a very short refresh interval and assert
// that Run returns ErrLeaseHeld within the test deadline.
func TestLease_RefreshDetectsEpochAdvanceAsStolen(t *testing.T) {
	t.Parallel()

	st := newTestState()
	ctx := context.Background()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	// Use a fast refresh interval so Run() ticks within our test window.
	l, err := singleton.New(st, singleton.Config{
		ProviderID:      testProviderID,
		InstanceID:      "inst-A",
		RefreshInterval: 50 * time.Millisecond,
		StaleAfter:      200 * time.Millisecond,
		Clock:           clock,
	}, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, l.Acquire(ctx))

	// Out-of-band bump: advance the epoch annotation without changing the
	// instance id. The next Run refresh should detect the epoch jump.
	ps := getStatus(t, st)
	current, _ := strconv.ParseUint(mustAnnotation(t, ps, singleton.AnnotationEpoch), 10, 64)
	ps.Metadata().Annotations().Set(singleton.AnnotationEpoch, strconv.FormatUint(current+5, 10))
	require.NoError(t, st.Update(ctx, ps))

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err = l.Run(runCtx)

	assert.ErrorIs(t, err, singleton.ErrLeaseHeld,
		"refresh that sees a higher on-disk epoch should report the lease stolen")
}

// TestLease_ReleaseClearsEpochAnnotation verifies clean shutdown erases the
// epoch annotation so the successor's CAS-create/update path sees the slot
// as "unclaimed" and bumps to epoch+1 rather than reusing a dangling value.
func TestLease_ReleaseClearsEpochAnnotation(t *testing.T) {
	t.Parallel()

	st := newTestState()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := newLease(t, st, "inst-A", clock)

	require.NoError(t, l.Acquire(context.Background()))

	// Sanity: epoch annotation is present.
	ps := getStatus(t, st)
	_, hasEpoch := ps.Metadata().Annotations().Get(singleton.AnnotationEpoch)
	require.True(t, hasEpoch, "acquire must set the epoch annotation")

	l.Release(context.Background())

	ps = getStatus(t, st)
	_, hasEpoch = ps.Metadata().Annotations().Get(singleton.AnnotationEpoch)
	assert.False(t, hasEpoch, "Release must delete the epoch annotation")
}

// TestLease_LegacyResourceWithoutEpochAnnotationStillTakesOver pins the
// backward-compat path: a pre-v0.15 resource has no epoch annotation. New
// holders must treat that as epoch 0 → their epoch becomes 1.
func TestLease_LegacyResourceWithoutEpochAnnotationStillTakesOver(t *testing.T) {
	t.Parallel()

	st := newTestState()
	ctx := context.Background()

	seed := infra.NewProviderStatus(testProviderID)
	seed.Metadata().Annotations().Set(singleton.AnnotationInstanceID, "pre-v0.15-holder")
	seed.Metadata().Annotations().Set(singleton.AnnotationHeartbeat,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano))
	// Intentionally no AnnotationEpoch — simulates a v0.14 holder.
	require.NoError(t, st.Create(ctx, seed))

	clock := newFakeClock(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) // well past stale
	l := newLease(t, st, "v0.15-newcomer", clock)
	require.NoError(t, l.Acquire(ctx))

	assert.Equal(t, uint64(1), l.Epoch(),
		"taking over a legacy (no-epoch) resource should start the epoch at 1")
}

// TestLease_HeartbeatMissingUsesUpdatedFallback checks that a ProviderStatus
// resource with no heartbeat annotation (but a real server Updated() time)
// is still ageable. The Lease uses Metadata().Updated() as fallback.
//
// Seed an owned resource with no heartbeat; the state impl sets Updated() to
// real wall time on Create, so "now" ≈ Updated(). A challenger using wall
// clock should see the lease as FRESH (via the Updated() fallback) and be
// rejected, proving the fallback path ran.
func TestLease_HeartbeatMissingUsesUpdatedFallback(t *testing.T) {
	t.Parallel()

	st := newTestState()
	ctx := context.Background()

	seed := infra.NewProviderStatus(testProviderID)
	seed.Metadata().Annotations().Set(singleton.AnnotationInstanceID, "real-holder")
	// No AnnotationHeartbeat — forces the fallback branch.
	require.NoError(t, st.Create(ctx, seed))

	// Use wall clock so "now" is close to the Updated() the state impl set.
	l := newLease(t, st, "challenger", wallclockProvider{})
	err := l.Acquire(ctx)

	require.Error(t, err)
	assert.ErrorIs(t, err, singleton.ErrLeaseHeld,
		"missing heartbeat + fresh Updated() must be treated as a fresh lease")
}

// TestLease_MalformedHeartbeatTakesOverEvenWhenUpdatedIsFresh pins the
// stricter policy for CORRUPTED state: if the heartbeat annotation exists but
// is unparseable, we treat the state as broken and take over, even when
// Updated() would say the resource is fresh.
func TestLease_MalformedHeartbeatTakesOverEvenWhenUpdatedIsFresh(t *testing.T) {
	t.Parallel()

	st := newTestState()
	ctx := context.Background()

	seed := infra.NewProviderStatus(testProviderID)
	seed.Metadata().Annotations().Set(singleton.AnnotationInstanceID, "broken-holder")
	seed.Metadata().Annotations().Set(singleton.AnnotationHeartbeat, "this is not a timestamp")
	require.NoError(t, st.Create(ctx, seed))

	l := newLease(t, st, "recoverer", wallclockProvider{})
	require.NoError(t, l.Acquire(ctx),
		"malformed heartbeat must be treated as takeover-eligible, not just fall back to Updated()")

	assert.True(t, l.WasTakeover())
}

// --- helpers ---

// mustAnnotation returns the value of an annotation or fails the test.
func mustAnnotation(t *testing.T, res cosiresource.Resource, key string) string {
	t.Helper()

	v, ok := res.Metadata().Annotations().Get(key)
	require.True(t, ok, "missing annotation %q", key)

	return v
}

// Ensure state package is used (lint guard).
var _ = state.State(nil)
