package noderotation

import "errors"

// Decision is the outcome of a single Candidate evaluation. The
// reconciler logs every decision and selectively acts on those whose
// Action is non-empty.
type Decision struct {
	// Candidate is the MachineSet the decision applies to. Always the
	// same pointer the caller passed in.
	Candidate *Candidate

	// Action is the human-readable label for what the engine intends
	// to do. Empty (ActionNone) when no action is appropriate this
	// tick — steady-state, locked, waiting, or refused. Values are
	// constants below.
	Action string

	// Reason explains why the engine chose this action (or why it
	// declined to act). Surfaced in logs at Info level.
	Reason string

	// TargetRequestID is the MachineRequest the action mutates, when
	// applicable. Empty for actions that touch the MachineSet but no
	// specific request (e.g., surge bump/drop write MachineSet, not a
	// request).
	TargetRequestID string

	// PreviousGeneration is the prior class-generation hash this
	// rotation step is moving away from. Populated by Plan when
	// Action is ActionStampGeneration so on-call sees the before/after
	// without having to regex-parse the Reason string.
	PreviousGeneration string

	// AbortKind classifies an ActionSurgeAborted decision so dashboards
	// can split aborts by cause. Empty for non-abort actions.
	AbortKind AbortKind

	// LiveCount is the MachineSet's live MachineAllocation.MachineCount
	// at decision time. Set on abort decisions so the operator-edit
	// drift case carries the actual observed count as a structured
	// field (the human-readable Reason string still holds the same
	// info for quick reading).
	LiveCount int

	// ExpectedCount is what the surge state machine expected the live
	// MachineCount to be given the persisted phase annotation. Set on
	// abort decisions; zero for non-abort.
	ExpectedCount int

	// surgeNext is the SurgePhaseState the action should write when
	// it succeeds. Unexported — only Execute consumes it. Empty
	// SurgePhase means "clear the surge-phase annotation".
	surgeNext SurgePhaseState

	// clearSurge tells Execute to delete the surge-phase annotation
	// after the action succeeds. Used when the cycle completes (no
	// more stale, count restored).
	clearSurge bool
}

// AbortKind categorizes an ActionSurgeAborted decision. Exported as
// a typed enum so metric label sets stay constrained and so a
// switch over abort kinds compiles cleanly.
type AbortKind string

const (
	// AbortKindNone is the zero value used for non-abort decisions.
	AbortKindNone AbortKind = ""

	// AbortKindCountDriftWaitUp — wait-up phase observed a live
	// MachineCount that doesn't match OriginalCount+1. Operator edit
	// or external writer.
	AbortKindCountDriftWaitUp AbortKind = "count-drift-waitup"

	// AbortKindCountDriftWaitDown — same shape for wait-down.
	AbortKindCountDriftWaitDown AbortKind = "count-drift-waitdown"

	// AbortKindUnknownPhase — persisted surge-phase annotation parsed
	// to a phase the state machine doesn't know. Should be unreachable
	// (parseSurgePhaseAnnotation rejects unknown phases), but if it
	// happens the cycle aborts cleanly rather than panic.
	AbortKindUnknownPhase AbortKind = "unknown-phase"
)

// Action labels. Exported so tests and metrics can pin them without
// duplicating the string literals.
const (
	// ActionNone — no rotation needed; all requests fresh and the
	// MachineSet is at nominal count.
	ActionNone = ""

	// ActionLockedByPriorStep — another instance holds the lock (or
	// the previous step of this reconciler hasn't released yet).
	// Defer.
	ActionLockedByPriorStep = "locked-by-prior-step"

	// ActionWaitingForReady — a replacement MachineRequest hasn't
	// reached PROVISIONED yet. In-place: the MRS-spawned replacement
	// for a torn-down stale. Surge wait-up: the surge replacement
	// landing in the +1 slot.
	ActionWaitingForReady = "waiting-for-ready"

	// ActionWaitingForTeardown — the engine has dropped MachineCount
	// during a surge cycle and is waiting for the MRS controller to
	// destroy the oldest stale member (MRS's deterministic
	// oldest-first scale-down picks our stale member).
	ActionWaitingForTeardown = "waiting-for-teardown"

	// ActionMinHealthyFloor — taking the next step would drop healthy
	// count below MinHealthy. Refuse; operator must scale up the set
	// or relax min-healthy.
	ActionMinHealthyFloor = "min-healthy-floor"

	// ActionTeardownStale — delete a stale MachineRequest directly
	// (in-place strategy). The MachineRequestSet controller will
	// spawn a replacement at the current MachineClass spec.
	ActionTeardownStale = "teardown-stale"

	// ActionSurgeUp — bump MachineCount by 1 to let the MRS
	// controller spawn a fresh replacement before any stale is torn
	// down (surge strategy). Sets surge-phase = wait-up.
	ActionSurgeUp = "surge-up"

	// ActionSurgeDown — drop MachineCount by 1 after the surge
	// replacement has landed. MRS picks the oldest member to destroy
	// (which is one of our stale members by construction). Sets
	// surge-phase = wait-down.
	ActionSurgeDown = "surge-down"

	// ActionSurgeCycleComplete — the previous surge cycle wrapped up
	// (no stale remaining at the moment, count restored). Clears the
	// surge-phase annotation and releases the lock. Reconciler's next
	// tick replans from idle state — if more stale exist (multi-
	// machine class), a new cycle begins.
	ActionSurgeCycleComplete = "surge-cycle-complete"

	// ActionSurgeAborted — the engine detected a corrupted surge
	// state (operator-edited MachineCount mid-cycle, or impossible
	// live-count vs annotation values). Clears the surge-phase
	// annotation and releases the lock so the next tick replans from
	// scratch.
	ActionSurgeAborted = "surge-aborted"

	// ActionStampGeneration — MachineClass's class-generation
	// annotation is missing or out-of-date. Stamp it so operators have
	// a readable "current gen" without spelunking the hash function.
	ActionStampGeneration = "stamp-generation"

	// ActionRefreshLock — a surge cycle is in flight, no other action
	// is appropriate this tick, but the lock timestamp needs
	// refreshing so the autoscaler keeps pausing the node group.
	ActionRefreshLock = "refresh-lock"
)

// Errors returned by Engine.Execute. Exported so reconciler tests can
// distinguish "the step legitimately couldn't proceed" from "the Omni
// state write failed".
var (
	// ErrCandidateNotActionable means no rotation step was appropriate
	// this tick. The reconciler treats it as success and moves on.
	ErrCandidateNotActionable = errors.New("no rotation action needed")
)
