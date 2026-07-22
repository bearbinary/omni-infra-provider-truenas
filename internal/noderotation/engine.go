package noderotation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	infraresources "github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"go.uber.org/zap"
)

// DefaultLockTTL bounds how long a rotation-state annotation is honored
// by the autoscaler before it's treated as a stale dead-reconciler
// artifact. Picked at 5 minutes to span a normal Talos boot + Ready
// cycle on TrueNAS hardware (~1–2 minutes) with headroom for retry,
// without freezing scaling for an absurd amount of time if the
// reconciler panics mid-step.
//
// Surge cycles can exceed a single TTL window — the engine refreshes
// the lock's timestamp on every tick while a cycle is in flight so
// the autoscaler stays paused for the whole multi-step cycle.
const DefaultLockTTL = 5 * time.Minute

// MinLockTTL is the floor an operator-supplied lock TTL is clamped to.
// Below this, the autoscaler-pause coupling becomes a no-op (every
// freshly written lock would read as expired on the next autoscaler
// tick), which defeats the whole point of the coupling.
const MinLockTTL = 30 * time.Second

// Engine executes one rotation step against an Omni state client. The
// step is the smallest unit of progress: stamp generation, take /
// refresh / release the lock, delete a stale MachineRequest, or write
// MachineAllocation.MachineCount. The reconciler calls Execute once
// per Candidate per tick; multiple ticks together complete a full
// rotation cycle.
//
// Engine deliberately does NOT loop. Looping inside one Execute call
// would prevent the lock TTL from acting as a safety net and would
// make the per-step timing invisible to metrics.
type Engine struct {
	st         state.State
	logger     *zap.Logger
	now        func() time.Time
	lockTTL    time.Duration
	providerID string
}

// NewEngine constructs an Engine bound to an Omni state client. now is
// injected so tests can pin timestamps; production passes time.Now.
func NewEngine(st state.State, logger *zap.Logger) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Engine{
		st:      st,
		logger:  logger,
		now:     time.Now,
		lockTTL: DefaultLockTTL,
	}
}

// WithClock overrides the clock used for lock-timestamp generation +
// staleness checks. Test-support.
func (e *Engine) WithClock(now func() time.Time) *Engine {
	e.now = now
	return e
}

// WithLockTTL overrides the lock TTL, clamping operator-supplied
// values to MinLockTTL. Production uses DefaultLockTTL.
func (e *Engine) WithLockTTL(ttl time.Duration) *Engine {
	if ttl < MinLockTTL {
		ttl = MinLockTTL
	}

	e.lockTTL = ttl
	return e
}

// WithProviderID enables a defense-in-depth refetch on teardownRequest:
// before destroying a MachineRequest, the engine verifies it still
// carries the expected provider-ID label. Prevents the case where an
// operator relabels a request between Plan and Execute.
func (e *Engine) WithProviderID(providerID string) *Engine {
	e.providerID = providerID
	return e
}

// Execute applies a planned decision to Omni state. Acquires / refreshes
// / releases the rotation-state lock as appropriate for the action.
//
// Returns ErrCandidateNotActionable for ActionNone / waiting actions —
// reconciler treats as success and moves to next candidate. Returns
// any real Omni write error verbatim.
func (e *Engine) Execute(ctx context.Context, d Decision) error {
	if d.Candidate == nil {
		return errors.New("execute: nil candidate")
	}

	switch d.Action {
	case ActionNone, ActionLockedByPriorStep,
		ActionWaitingForReady, ActionWaitingForTeardown,
		ActionMinHealthyFloor:
		// Wait-style actions in the middle of a surge cycle still
		// need a lock refresh so the autoscaler stays paused while
		// the cycle drags on.
		if _, inSurge := d.Candidate.SurgePhase(); inSurge {
			if err := e.setLock(ctx, d.Candidate, e.now()); err != nil {
				return fmt.Errorf("refresh rotation lock during surge wait: %w", err)
			}
		}

		return ErrCandidateNotActionable

	case ActionRefreshLock:
		return e.setLock(ctx, d.Candidate, e.now())

	case ActionStampGeneration:
		return e.stampGeneration(ctx, d.Candidate)

	case ActionTeardownStale:
		return e.withLock(ctx, d.Candidate, func() error {
			return e.teardownRequest(ctx, d.Candidate.MachineSet.Metadata().ID(), d.TargetRequestID)
		})

	case ActionSurgeUp:
		return e.withSurgeStep(ctx, d, +1)

	case ActionSurgeDown:
		return e.withSurgeStep(ctx, d, -1)

	case ActionSurgeCycleComplete, ActionSurgeAborted:
		return e.clearSurgeAndLock(ctx, d.Candidate)

	default:
		return fmt.Errorf("execute: unknown action %q", d.Action)
	}
}

// stampGeneration writes AnnotationClassGeneration onto the
// MachineClass.
func (e *Engine) stampGeneration(ctx context.Context, c *Candidate) error {
	ptr := omni.NewMachineClass(c.MachineClass.Metadata().ID()).Metadata()

	_, err := safe.StateUpdateWithConflicts[*omni.MachineClass](ctx, e.st, ptr, func(mc *omni.MachineClass) error {
		mc.Metadata().Annotations().Set(AnnotationClassGeneration, c.CurrentGeneration)
		return nil
	})
	if err != nil {
		return fmt.Errorf("stamp class-generation on %q: %w", c.MachineClass.Metadata().ID(), err)
	}

	return nil
}

// teardownRequest deletes a MachineRequest. When the Engine was
// constructed with WithProviderID, the request is refetched first and
// destroyed only when its LabelInfraProviderID + LabelMachineRequestSet
// labels still match the values Plan saw. This closes the small
// window where an operator relabels a request between Plan and
// Execute, redirecting the destroy to a foreign provider's resource.
func (e *Engine) teardownRequest(ctx context.Context, machineSetID, requestID string) error {
	ptr := infraresources.NewMachineRequest(requestID).Metadata()

	if e.providerID != "" {
		mr, getErr := safe.StateGetByID[*infraresources.MachineRequest](ctx, e.st, requestID)
		if getErr != nil {
			return fmt.Errorf("refetch MachineRequest %q before teardown: %w", requestID, getErr)
		}

		if pid, _ := mr.Metadata().Labels().Get(omni.LabelInfraProviderID); pid != e.providerID {
			return fmt.Errorf("refusing to destroy MachineRequest %q: live LabelInfraProviderID=%q does not match engine providerID=%q",
				requestID, pid, e.providerID)
		}

		if msID, _ := mr.Metadata().Labels().Get(omni.LabelMachineRequestSet); msID != machineSetID {
			return fmt.Errorf("refusing to destroy MachineRequest %q: live LabelMachineRequestSet=%q does not match expected machineset=%q",
				requestID, msID, machineSetID)
		}
	}

	if err := e.st.Destroy(ctx, ptr); err != nil {
		return fmt.Errorf("destroy MachineRequest %q: %w", requestID, err)
	}

	return nil
}

// withLock writes the rotation-state annotation, runs the action, then
// clears the annotation. Used by single-step actions (in-place
// teardown) where the lock window covers exactly one mutation.
func (e *Engine) withLock(ctx context.Context, c *Candidate, action func() error) error {
	now := e.now()

	if err := e.setLock(ctx, c, now); err != nil {
		return fmt.Errorf("acquire rotation lock on MachineSet %q: %w", c.MachineSet.Metadata().ID(), err)
	}

	if err := action(); err != nil {
		// Leave the lock; TTL will release. Returning the underlying
		// error preserves Engine→reconciler error mapping.
		return err
	}

	if err := e.clearLock(ctx, c); err != nil {
		e.logger.Warn("rotation lock clear failed; relying on TTL to release",
			zap.String("machineset", c.MachineSet.Metadata().ID()),
			zap.Error(err),
		)
	}

	return nil
}

// withSurgeStep is the surge analogue of withLock. In one CAS on the
// MachineSet: the lock is taken/refreshed, the surge-phase annotation
// advances to the next state, and MachineCount is adjusted by delta.
// The lock is NOT cleared on success — the surge cycle spans multiple
// ticks; clearing happens only when the cycle finishes
// (ActionSurgeCycleComplete / ActionSurgeAborted → clearSurgeAndLock).
//
// Atomicity: annotations and MachineCount live on the same resource,
// so a single StateUpdateWithConflicts covers both. An earlier version
// split this into two writes (annotation-set, then a separate
// adjustMachineCount CAS); a crash between them would leave phase=
// WaitUp with the count unbumped, forcing planSurge to abort the
// cycle via AbortKindCountDriftWaitUp and re-plan on the next tick.
// The plan-time drift check is still there as belt-and-suspenders
// against operator/autoscaler races, but the crash window is closed.
func (e *Engine) withSurgeStep(ctx context.Context, d Decision, delta int) error {
	now := e.now()

	ptr := omni.NewMachineSet(d.Candidate.MachineSet.Metadata().ID()).Metadata()

	_, err := safe.StateUpdateWithConflicts[*omni.MachineSet](ctx, e.st, ptr, func(ms *omni.MachineSet) error {
		alloc := ms.TypedSpec().Value.GetMachineAllocation()
		if alloc == nil {
			return fmt.Errorf("MachineSet %q has no MachineAllocation", ms.Metadata().ID())
		}

		next := int(alloc.MachineCount) + delta
		if next < 0 {
			return fmt.Errorf("MachineSet %q: surge would push MachineCount below zero (current=%d, delta=%d)",
				ms.Metadata().ID(), alloc.MachineCount, delta)
		}

		ms.Metadata().Annotations().Set(AnnotationRotationState,
			formatRotationStateAnnotation(d.Candidate.CurrentGeneration, now))

		ms.Metadata().Annotations().Set(AnnotationSurgePhase,
			formatSurgePhaseAnnotation(d.surgeNext))

		alloc.MachineCount = uint32(next)

		return nil
	})
	if err != nil {
		return fmt.Errorf("surge step %s lock+phase+count write on %q (delta=%+d): %w",
			d.Action, d.Candidate.MachineSet.Metadata().ID(), delta, err)
	}

	return nil
}

// clearSurgeAndLock removes both annotations in one write. Used by the
// cycle-complete and cycle-aborted paths.
func (e *Engine) clearSurgeAndLock(ctx context.Context, c *Candidate) error {
	ptr := omni.NewMachineSet(c.MachineSet.Metadata().ID()).Metadata()

	_, err := safe.StateUpdateWithConflicts[*omni.MachineSet](ctx, e.st, ptr, func(ms *omni.MachineSet) error {
		ms.Metadata().Annotations().Delete(AnnotationSurgePhase)
		ms.Metadata().Annotations().Delete(AnnotationRotationState)
		return nil
	})

	return err
}

func (e *Engine) setLock(ctx context.Context, c *Candidate, now time.Time) error {
	ptr := omni.NewMachineSet(c.MachineSet.Metadata().ID()).Metadata()

	_, err := safe.StateUpdateWithConflicts[*omni.MachineSet](ctx, e.st, ptr, func(ms *omni.MachineSet) error {
		ms.Metadata().Annotations().Set(AnnotationRotationState, formatRotationStateAnnotation(c.CurrentGeneration, now))
		return nil
	})

	return err
}

func (e *Engine) clearLock(ctx context.Context, c *Candidate) error {
	ptr := omni.NewMachineSet(c.MachineSet.Metadata().ID()).Metadata()

	_, err := safe.StateUpdateWithConflicts[*omni.MachineSet](ctx, e.st, ptr, func(ms *omni.MachineSet) error {
		ms.Metadata().Annotations().Delete(AnnotationRotationState)
		return nil
	})

	return err
}
