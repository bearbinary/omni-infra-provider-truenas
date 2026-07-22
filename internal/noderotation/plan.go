package noderotation

import (
	"fmt"
	"time"

	"github.com/siderolabs/omni/client/api/omni/specs"
)

// Plan inspects a Candidate and decides what action to take this tick
// without performing any state writes. Pure function over the
// Candidate + clock.
func (e *Engine) Plan(c *Candidate) Decision {
	if c == nil {
		return Decision{Reason: "nil candidate"}
	}

	d := Decision{Candidate: c}

	// Snapshot the clock once per Plan call. Lock check + surge-cycle
	// start timestamp + cycle-complete duration calc all use the same
	// `now`, so a per-call vDSO call is enough.
	now := e.now()

	// Stamp generation if the class doesn't carry one yet, or carries
	// the wrong one (operator hand-edit, prior reconciler crash). This
	// is a metadata-only write — never touches a Machine. Done before
	// the lock check so operators see the class-generation update
	// promptly even if a prior surge cycle's lock is still draining.
	if existing, _ := c.MachineClass.Metadata().Annotations().Get(AnnotationClassGeneration); existing != c.CurrentGeneration {
		d.Action = ActionStampGeneration
		d.PreviousGeneration = existing
		d.Reason = fmt.Sprintf("class-generation annotation %q != computed %q", existing, c.CurrentGeneration)

		return d
	}

	// Foreign lock check: if the lock is held but no surge phase is
	// in flight and the strategy is in-place, another reconciler is
	// taking an in-place step. Defer.
	if c.HasActiveLock(now, e.lockTTL) {
		if _, hasPhase := c.SurgePhase(); !hasPhase {
			d.Action = ActionLockedByPriorStep
			d.Reason = "rotation-state lock not yet expired; deferring step"

			return d
		}
		// Lock + surge phase present: that's our own cycle, fall
		// through to the surge state machine which knows how to
		// refresh the lock on its own ticks.
	}

	stale := c.StaleRequests()

	switch c.Config.Strategy {
	case StrategyInPlace:
		return e.planInPlace(c, stale, d)
	case StrategySurge:
		return e.planSurge(c, stale, d, now)
	default:
		d.Action = ActionNone
		d.Reason = fmt.Sprintf("unknown strategy %q (parse should have rejected this)", c.Config.Strategy)

		return d
	}
}

// planInPlace decides the next step for an in-place candidate. CP role
// is refused at parse time, so this only runs for worker sets.
func (e *Engine) planInPlace(c *Candidate, stale []RequestStatus, d Decision) Decision {
	if len(stale) == 0 {
		d.Action = ActionNone
		d.Reason = fmt.Sprintf("all %d requests at generation %s", len(c.Requests), c.CurrentGeneration)

		return d
	}

	if hasInFlightProvisioning(c.Requests) {
		d.Action = ActionWaitingForReady
		d.Reason = "a MachineRequest is still PROVISIONING — letting it land before next step"

		return d
	}

	freshProvisioned := c.FreshCount()

	// Teardown removes one healthy member temporarily. Refuse if the
	// post-teardown healthy count would drop below MinHealthy.
	healthyAfter := freshProvisioned
	if stale[0].Stage == specs.MachineRequestStatusSpec_PROVISIONED {
		healthyAfter = freshProvisioned + 1 - 1
	}

	if healthyAfter < c.Config.MinHealthy {
		d.Action = ActionMinHealthyFloor
		d.Reason = fmt.Sprintf("teardown would leave %d healthy, below min-healthy=%d; scale the MachineSet up or lower %s",
			healthyAfter, c.Config.MinHealthy, AnnotationMinHealthy)

		return d
	}

	victim := stale[0]
	d.Action = ActionTeardownStale
	d.Reason = fmt.Sprintf("in-place: tearing down oldest stale request %q; MachineRequestSet controller will recreate at current generation",
		victim.Request.Metadata().ID())
	d.TargetRequestID = victim.Request.Metadata().ID()

	return d
}

// planSurge decides the next step for a surge candidate. Three states:
//
//	idle      — no surge in flight. Stale present → SurgeUp.
//	wait-up   — bumped count, waiting for replacement → SurgeDown when ready.
//	wait-down — dropped count, waiting for MRS to drain stale → cycle complete when drained.
//
// The MachineRequestSet controller's deterministic oldest-first
// scale-down picker is what makes this loop converge: when we drop
// count by 1, MRS sorts members (in-use first, non-CP first, oldest
// first) and tears down the oldest — which is one of the stale
// members by construction, since the surge replacement is the
// newest.
func (e *Engine) planSurge(c *Candidate, stale []RequestStatus, d Decision, now time.Time) Decision {
	phase, hasPhase := c.SurgePhase()

	// IDLE state: no surge phase annotation.
	if !hasPhase {
		if len(stale) == 0 {
			d.Action = ActionNone
			d.Reason = fmt.Sprintf("all %d requests at generation %s", len(c.Requests), c.CurrentGeneration)

			return d
		}

		// Min-healthy check before starting a cycle. Surge does NOT
		// drop healthy below the current count (it adds one, then
		// removes one stale), so the floor only bites if the current
		// fresh count is already at-or-below the floor and we'd be
		// asked to wait for a replacement before counting fresh.
		// Phrase as "current fresh+stale provisioned should be ≥
		// MinHealthy" since we're not destroying anyone yet.
		if freshSum := c.FreshCount() + countStaleProvisioned(stale); freshSum < c.Config.MinHealthy {
			d.Action = ActionMinHealthyFloor
			d.Reason = fmt.Sprintf("currently %d provisioned, below min-healthy=%d; surge needs a healthy base before bumping",
				freshSum, c.Config.MinHealthy)

			return d
		}

		d.Action = ActionSurgeUp
		d.Reason = fmt.Sprintf("starting surge cycle: bumping MachineCount %d→%d to land replacement for oldest stale %q",
			c.MachineCount, c.MachineCount+1, stale[0].Request.Metadata().ID())
		d.surgeNext = SurgePhaseState{
			Phase:          SurgePhaseWaitUp,
			OriginalCount:  c.MachineCount,
			CycleStartedAt: now,
		}

		return d
	}

	// PHASE wait-up: MachineCount should be OriginalCount + 1.
	// Waiting for the new request to reach PROVISIONED.
	if phase.Phase == SurgePhaseWaitUp {
		// Sanity: live count drift means operator (or another writer)
		// mutated MachineCount. Abort the cycle and replan.
		if c.MachineCount != phase.OriginalCount+1 {
			d.Action = ActionSurgeAborted
			d.AbortKind = AbortKindCountDriftWaitUp
			d.LiveCount = c.MachineCount
			d.ExpectedCount = phase.OriginalCount + 1
			d.Reason = fmt.Sprintf("wait-up: live MachineCount=%d but annotation expected %d (orig %d + 1) — aborting cycle, replanning next tick",
				c.MachineCount, phase.OriginalCount+1, phase.OriginalCount)

			return d
		}

		// Did the new fresh land? We expect fresh-provisioned count
		// to be original + 1 (or higher if multiple replacements
		// raced, which shouldn't happen but isn't harmful).
		freshProv := c.FreshCount()
		expectedFresh := phase.OriginalCount - len(stale) + 1
		if expectedFresh < 1 {
			expectedFresh = 1
		}

		if freshProv < expectedFresh {
			d.Action = ActionWaitingForReady
			d.Reason = fmt.Sprintf("wait-up: %d fresh provisioned, need %d before drop", freshProv, expectedFresh)

			return d
		}

		// Replacement landed. Drop count by 1 → MRS picks oldest
		// (a stale) to destroy.
		d.Action = ActionSurgeDown
		d.Reason = fmt.Sprintf("surge replacement landed (fresh-provisioned=%d); dropping MachineCount %d→%d",
			freshProv, c.MachineCount, c.MachineCount-1)
		d.surgeNext = SurgePhaseState{
			Phase:          SurgePhaseWaitDown,
			OriginalCount:  phase.OriginalCount,
			CycleStartedAt: phase.CycleStartedAt,
		}

		return d
	}

	// PHASE wait-down: MachineCount should be back at OriginalCount.
	// Waiting for MRS to finish destroying the oldest stale.
	if phase.Phase == SurgePhaseWaitDown {
		if c.MachineCount != phase.OriginalCount {
			d.Action = ActionSurgeAborted
			d.AbortKind = AbortKindCountDriftWaitDown
			d.LiveCount = c.MachineCount
			d.ExpectedCount = phase.OriginalCount
			d.Reason = fmt.Sprintf("wait-down: live MachineCount=%d but annotation expected %d — aborting cycle, replanning next tick",
				c.MachineCount, phase.OriginalCount)

			return d
		}

		// Total request count back to OriginalCount means the
		// teardown finished. MRS removed the stale member, leaving
		// OriginalCount machines (with one more fresh than at cycle
		// start).
		if len(c.Requests) > phase.OriginalCount {
			d.Action = ActionWaitingForTeardown
			d.Reason = fmt.Sprintf("wait-down: %d requests remain, expecting %d after teardown",
				len(c.Requests), phase.OriginalCount)

			return d
		}

		// Cycle complete. Clear annotation. Next tick will replan
		// from idle — if more stale remain, a new cycle begins.
		d.Action = ActionSurgeCycleComplete
		d.Reason = fmt.Sprintf("surge cycle done in %s; %d stale remaining for future cycles",
			now.Sub(phase.CycleStartedAt).Round(time.Second), len(stale))
		d.clearSurge = true

		return d
	}

	// Defensive — parseSurgePhaseAnnotation should reject anything
	// else.
	d.Action = ActionSurgeAborted
	d.AbortKind = AbortKindUnknownPhase
	d.Reason = fmt.Sprintf("unknown surge phase %q", phase.Phase)

	return d
}

// hasInFlightProvisioning reports whether any request in the slice
// is currently PROVISIONING. Pulled out as a free function so
// engine.go and plan.go can share without re-walking from a method
// receiver.
func hasInFlightProvisioning(reqs []RequestStatus) bool {
	for _, r := range reqs {
		if r.Stage == specs.MachineRequestStatusSpec_PROVISIONING {
			return true
		}
	}

	return false
}

// countStaleProvisioned returns the number of stale requests currently
// in the PROVISIONED stage — i.e., stale members that are actually
// serving workload. Used to size the floor check for surge's idle
// state: surge doesn't destroy anyone before adding a replacement, so
// a class with 3 stale provisioned can surge even at MinHealthy=2.
func countStaleProvisioned(stale []RequestStatus) int {
	n := 0
	for _, r := range stale {
		if r.Stage == specs.MachineRequestStatusSpec_PROVISIONED {
			n++
		}
	}

	return n
}
