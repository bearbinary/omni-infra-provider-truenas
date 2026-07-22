// Package noderotation implements MachineClass-driven node rotation. When
// an operator edits the provider-data on an opted-in MachineClass, the
// reconciler replaces members of the matching MachineSet so they boot
// against the new spec, one Machine at a time, while honoring a min-
// healthy floor that protects etcd quorum on control-plane sets.
//
// Status: EXPERIMENTAL. The annotation schema and the reconciler's
// observable side effects (MachineSet count edits, rotation-state lock
// annotation) are subject to breaking changes until the feature is
// promoted to stable.
//
// Design notes:
//   - Opt-in per MachineClass via `node-rotation.omni/*` annotations. A
//     MachineClass without the enabled+role+strategy triad is never
//     touched — zero blast radius for clusters that don't opt in.
//   - Reconciler does not depend on the TrueNAS client. It only reads
//     and writes Omni COSI state. Subcommand wiring keeps it as its
//     own process so a misbehaving reconciler cannot stall the
//     provisioner.
//   - The autoscaler (internal/autoscaler) reads the rotation-state lock
//     annotation this package writes and pauses scaling for the affected
//     node group during a rotation step. The two packages share no Go
//     types — coordination is purely through the lock annotation key.
package noderotation

import (
	"fmt"
	"strconv"
	"strings"
)

// Annotation keys applied to Omni MachineClass and MachineSet resources
// to declare rotation policy and to coordinate with the autoscaler.
// All keys live under the node-rotation.omni/ namespace. Exported so
// docgen, tests, and the autoscaler's pause-check share one source of
// truth.
const (
	// AnnotationEnabled is the master opt-in. Must be "true" for the
	// reconciler to consider the MachineClass; any other value
	// (including missing) leaves the class out of the rotation
	// candidate set. Required when opting in.
	AnnotationEnabled = "node-rotation.omni/enabled"

	// AnnotationRole declares whether the operator intends this
	// MachineClass to back a control-plane or worker MachineSet.
	// Required when opting in. Values: "controlplane" or "worker".
	// User-declared rather than inferred — a mismatch with the
	// MachineSet's LabelControlPlaneRole is logged at warn but honored,
	// because Omni's labels can be wrong on hand-crafted clusters and
	// the operator's explicit intent is the safer source of truth.
	AnnotationRole = "node-rotation.omni/role"

	// AnnotationStrategy selects how rotation steps progress. Required
	// when opting in. Values: "surge" or "in-place".
	//
	//   surge:    +1 new Machine → wait Ready → -1 stale. Needs free
	//             capacity in the pool; protected by the autoscaler's
	//             capacity gate when both features are co-deployed.
	//   in-place: -1 stale → wait gone → +1 new. No capacity needed.
	//             Workload gap on the rotated node until the
	//             replacement schedules. REFUSED when role=controlplane
	//             because dropping a CP member below quorum during the
	//             gap risks the etcd cluster.
	AnnotationStrategy = "node-rotation.omni/strategy"

	// AnnotationMinHealthy is the floor on healthy Machine count
	// during rotation. Optional. Defaults: role=controlplane → 2,
	// role=worker → 1. The reconciler refuses to step a MachineSet
	// below this number of healthy members; a CP set at MachineCount=3
	// with min-healthy=2 means surge by one, then tear down one, never
	// dropping below 2 healthy. Cannot exceed the MachineSet's
	// MachineCount (enforced at step time, not at parse time, because
	// MachineCount can drift independently of the annotation).
	AnnotationMinHealthy = "node-rotation.omni/min-healthy"

	// AnnotationClassGeneration is stamped on the MachineClass by the
	// reconciler after it observes a new spec hash. The value is the
	// canonical hex hash of the inputs that matter for rotation (see
	// generation.go). MachineRequests whose own hash differs from this
	// generation are stale and will be rotated. Operator-readable so
	// `omnictl get machineclass -o yaml | grep generation` gives a quick
	// "are my nodes up to date" answer.
	AnnotationClassGeneration = "node-rotation.omni/class-generation"

	// AnnotationRotationState is stamped on a MachineSet for the
	// duration of a single rotation step (in-place) OR for the entire
	// surge cycle. Value format: "<class-generation>:<unix-ts>". The
	// autoscaler reads this key and, if present and not stale-by-TTL,
	// pauses scaling for the node group (returns min==max==current to
	// CAS). TTL bounds the failure mode: a dead reconciler cannot
	// freeze the set forever — the surge state machine refreshes the
	// timestamp on every tick it observes the cycle in flight so the
	// autoscaler stays paused while progress is being made.
	AnnotationRotationState = "node-rotation.omni/rotation-state"

	// AnnotationSurgePhase tracks where in a multi-step surge cycle a
	// MachineSet currently is. Set when the engine bumps MachineCount
	// to land a replacement; cleared when the cycle returns to the
	// operator-configured nominal count with all stale members drained.
	//
	// Value format: "<phase>:<original-count>:<unix-ts-cycle-start>".
	//
	// Phase = SurgePhaseWaitUp while the engine waits for the new
	// fresh replacement to reach PROVISIONED. Phase = SurgePhaseWaitDown
	// while the MRS controller is destroying the stale member after
	// the count drop. original-count is the MachineCount value the
	// operator configured (preserved across the cycle so a crash mid-
	// surge restores correctly). unix-ts-cycle-start lets operators
	// see how long a surge has been running in `omnictl get ms -o yaml`.
	//
	// Kept separate from AnnotationRotationState so the autoscaler's
	// 2-segment "<gen>:<ts>" lock parser stays backward-compatible.
	AnnotationSurgePhase = "node-rotation.omni/surge-phase"
)

// SurgePhase tracks the leg of a surge cycle the reconciler is on.
// String values are persisted to AnnotationSurgePhase, so a rename
// here would silently strand any in-flight cycle — change with care.
type SurgePhase string

const (
	// SurgePhaseWaitUp — MachineCount has been bumped by 1; waiting
	// for the MRS controller to spawn the replacement and for the
	// replacement's MachineRequestStatus to reach PROVISIONED.
	SurgePhaseWaitUp SurgePhase = "wait-up"

	// SurgePhaseWaitDown — MachineCount has been dropped back to
	// original; waiting for the MRS controller to finish destroying
	// the oldest stale request (MRS's deterministic oldest-first
	// scale-down picks our stale member).
	SurgePhaseWaitDown SurgePhase = "wait-down"
)

// Role declares the operator's intent for the MachineSets backed by an
// opted-in MachineClass. Persisted as the AnnotationRole value.
type Role string

const (
	// RoleControlPlane marks the MachineClass as backing a control-plane
	// MachineSet. Strategy=in-place is refused for this role; the
	// reconciler force-promotes to surge or skips.
	RoleControlPlane Role = "controlplane"

	// RoleWorker marks the MachineClass as backing a worker MachineSet.
	// Both strategies are allowed.
	RoleWorker Role = "worker"
)

// Strategy selects how the reconciler progresses a rotation step.
type Strategy string

const (
	// StrategySurge scales the MachineSet up by one, waits for the new
	// Machine to reach Ready, then tears down a stale Machine. Bounded
	// by pool capacity.
	StrategySurge Strategy = "surge"

	// StrategyInPlace tears down a stale Machine first, then scales
	// back up. No capacity requirement. Workload gap on the rotated
	// node until the replacement schedules.
	StrategyInPlace Strategy = "in-place"
)

// Default min-healthy values applied when the operator omits the
// annotation. Conservative for CP (quorum), permissive for worker
// (single-node workloads can re-schedule).
const (
	DefaultMinHealthyControlPlane = 2
	DefaultMinHealthyWorker       = 1
)

// Config is the parsed and validated rotation configuration for one
// MachineClass. All fields are populated by ParseMachineClassRotationConfig
// before the struct is returned — callers can trust the values without
// re-validating.
type Config struct {
	// Role is the operator-declared role (required when opted in).
	Role Role

	// Strategy is the operator-declared rotation strategy (required
	// when opted in). ParseMachineClassRotationConfig refuses
	// in-place when Role is RoleControlPlane.
	Strategy Strategy

	// MinHealthy is the floor on healthy Machines maintained during
	// rotation. Substituted from the role-default when the annotation
	// is absent. Always ≥ 0.
	MinHealthy int
}

// IsRotationOptIn reports whether a MachineClass carries the master
// AnnotationEnabled annotation with value "true". Cheap pre-filter for
// the reconciler's enumeration so the full parse only runs on opted-in
// classes. Matches IsAutoscaleOptIn's shape in the sibling package.
func IsRotationOptIn(annotations map[string]string) bool {
	v, ok := annotations[AnnotationEnabled]
	if !ok {
		return false
	}

	// Strict parse: only "true" (any case) opts in. A typo like "yes"
	// or "1" leaves the class out, which is the safer default — silent
	// rotation of a misconfigured class would be worse than the
	// operator noticing nothing happened and fixing the annotation.
	return strings.EqualFold(strings.TrimSpace(v), "true")
}

// ParseMachineClassRotationConfig reads the node-rotation.omni/*
// annotations off a MachineClass annotations map and returns the
// validated Config. Returns (nil, nil) when the class is not opted in
// so the caller can cheaply skip it. Returns (nil, err) when opted-in
// but malformed — the reconciler logs and skips rather than partially
// rotating with defaults the operator didn't ask for.
func ParseMachineClassRotationConfig(annotations map[string]string) (*Config, error) {
	if !IsRotationOptIn(annotations) {
		return nil, nil
	}

	role, err := parseRole(annotations[AnnotationRole])
	if err != nil {
		return nil, err
	}

	strategy, err := parseStrategy(annotations[AnnotationStrategy])
	if err != nil {
		return nil, err
	}

	if role == RoleControlPlane && strategy == StrategyInPlace {
		return nil, fmt.Errorf("%s=%q with %s=%q: in-place rotation on a control-plane MachineClass would drop the CP below quorum during the gap — use %q instead",
			AnnotationRole, role, AnnotationStrategy, strategy, StrategySurge)
	}

	minHealthy, err := parseMinHealthy(annotations, role)
	if err != nil {
		return nil, err
	}

	return &Config{
		Role:       role,
		Strategy:   strategy,
		MinHealthy: minHealthy,
	}, nil
}

func parseRole(raw string) (Role, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))

	switch raw {
	case "":
		return "", fmt.Errorf("%s is required when %s=true; valid values: %q or %q",
			AnnotationRole, AnnotationEnabled, RoleControlPlane, RoleWorker)
	case string(RoleControlPlane):
		return RoleControlPlane, nil
	case string(RoleWorker):
		return RoleWorker, nil
	default:
		return "", fmt.Errorf("%s %q: valid values are %q or %q",
			AnnotationRole, raw, RoleControlPlane, RoleWorker)
	}
}

func parseStrategy(raw string) (Strategy, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))

	switch raw {
	case "":
		return "", fmt.Errorf("%s is required when %s=true; valid values: %q or %q",
			AnnotationStrategy, AnnotationEnabled, StrategySurge, StrategyInPlace)
	case string(StrategySurge):
		return StrategySurge, nil
	case string(StrategyInPlace):
		return StrategyInPlace, nil
	default:
		return "", fmt.Errorf("%s %q: valid values are %q or %q",
			AnnotationStrategy, raw, StrategySurge, StrategyInPlace)
	}
}

func parseMinHealthy(annotations map[string]string, role Role) (int, error) {
	raw, ok := annotations[AnnotationMinHealthy]
	if !ok {
		switch role {
		case RoleControlPlane:
			return DefaultMinHealthyControlPlane, nil
		case RoleWorker:
			return DefaultMinHealthyWorker, nil
		}
		// Unreachable — parseRole rejects everything else — but Go's
		// switch exhaustiveness check doesn't know that.
		return 0, fmt.Errorf("unexpected role %q in default-min-healthy resolution", role)
	}

	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", AnnotationMinHealthy, raw, err)
	}

	if n < 0 {
		return 0, fmt.Errorf("%s %q: must be ≥ 0", AnnotationMinHealthy, raw)
	}

	return n, nil
}
