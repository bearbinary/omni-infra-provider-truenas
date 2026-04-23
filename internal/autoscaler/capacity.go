package autoscaler

import (
	"context"
	"errors"
	"fmt"
)

// Capacity gate — decides whether a scale-up request on a specific
// MachineSet should be allowed given current TrueNAS state. Returns a
// structured Decision so the gRPC handler can emit the right metric
// label and log line without re-deriving the reason from a bare error.
//
// The gate is deliberately cheap: two reads (pool and system.info) that
// we already make elsewhere in the provider. Callers should cache
// results for a short TTL (~30s) in front of this package — this file
// keeps the policy pure, no caching concerns.

// CapacityQuery is the narrow interface the gate needs. Decoupling
// autoscaler from internal/client means a future extraction to a
// standalone repo can swap in an interface satisfied by a generic
// infra-provider capacity source rather than our TrueNAS client.
type CapacityQuery interface {
	// PoolFreeBytes reports available bytes on the named pool.
	PoolFreeBytes(ctx context.Context, pool string) (int64, error)

	// HostFreeMemoryBytes reports free bytes on the TrueNAS host's
	// physical memory. Not "total - allocated-to-VMs"; this is the
	// kernel's view of available memory, which already accounts for
	// running VMs, caches, etc.
	HostFreeMemoryBytes(ctx context.Context) (int64, error)
}

// Outcome is the three-value result of a capacity check. The enum is
// exported so the metrics layer can use it as a label value without
// stringifying through the Decision.Reason field.
type Outcome int

const (
	// OutcomeAllowed: gate is satisfied; scale-up may proceed.
	OutcomeAllowed Outcome = iota

	// OutcomeDeniedHard: gate is hard-configured and below threshold —
	// reject the scale-up and surface the reason to the cluster-
	// autoscaler sidecar. CAS will mark the node group capacity-
	// exceeded and stop retrying until pressure clears.
	OutcomeDeniedHard

	// OutcomeWarnedSoft: gate is soft-configured and would have been
	// denied in hard mode. Scale-up proceeds but the subcommand emits
	// a warn log + metric so operators can graph soft-gate near-misses.
	OutcomeWarnedSoft

	// OutcomeErrored: a capacity query (pool or host-mem) itself
	// failed. Treated like OutcomeDeniedHard so a transient TrueNAS
	// outage doesn't let the autoscaler race ahead of visibility —
	// the gate fails closed.
	OutcomeErrored
)

// String gives humans + log fields a readable outcome. Exported-form is
// snake-case so it doubles as a metric label value.
func (o Outcome) String() string {
	switch o {
	case OutcomeAllowed:
		return "allowed"
	case OutcomeDeniedHard:
		return "denied_hard"
	case OutcomeWarnedSoft:
		return "warned_soft"
	case OutcomeErrored:
		return "errored"
	default:
		return "unknown"
	}
}

// Decision is the full capacity-check result. Returned as a struct
// rather than (Outcome, error) so the soft-warn case (scale-up proceeds
// but we still want the reason) fits naturally — an error-return would
// force the caller to branch on "is this a hard fail or a soft warn".
type Decision struct {
	Outcome Outcome
	Reason  string // empty when OutcomeAllowed
}

// Allowed is a terse predicate for the common branch.
func (d Decision) Allowed() bool {
	return d.Outcome == OutcomeAllowed || d.Outcome == OutcomeWarnedSoft
}

const bytesPerGiB = 1024 * 1024 * 1024

// CheckCapacity evaluates the gate for a single MachineSet scale-up
// request. Returns a Decision describing the outcome; never returns an
// error — query failures surface as OutcomeErrored so the caller's
// happy-path branching stays linear.
//
// Parameters:
//   - cfg: parsed per-MachineClass autoscaler config (from annotations)
//   - pool: the TrueNAS pool the MachineSet's VMs are provisioned against
//
// Behavior by CapacityGate:
//   - Soft: always returns OutcomeAllowed or OutcomeWarnedSoft. Queries
//     still run so the warn log + metric record the near-miss; operators
//     can graph these to tune thresholds before flipping the gate hard.
//   - Hard: returns OutcomeDeniedHard on threshold breach, OutcomeErrored
//     on query failure.
//
// A zero threshold (MinPoolFreeGiB == 0 or MinHostMemGiB == 0) disables
// that half of the check — operators who only care about one dimension
// can set the other to 0 without disabling the gate entirely.
func CheckCapacity(ctx context.Context, q CapacityQuery, cfg Config, pool string) Decision {
	reasons := []string{}

	if cfg.MinPoolFreeGiB > 0 {
		reason, outcome := checkPool(ctx, q, pool, cfg.MinPoolFreeGiB)
		if outcome == OutcomeErrored {
			return Decision{Outcome: OutcomeErrored, Reason: reason}
		}

		if reason != "" {
			reasons = append(reasons, reason)
		}
	}

	if cfg.MinHostMemGiB > 0 {
		reason, outcome := checkHostMem(ctx, q, cfg.MinHostMemGiB)
		if outcome == OutcomeErrored {
			return Decision{Outcome: OutcomeErrored, Reason: reason}
		}

		if reason != "" {
			reasons = append(reasons, reason)
		}
	}

	if len(reasons) == 0 {
		return Decision{Outcome: OutcomeAllowed}
	}

	reason := joinReasons(reasons)

	if cfg.CapacityGate == CapacityGateSoft {
		return Decision{Outcome: OutcomeWarnedSoft, Reason: reason}
	}

	return Decision{Outcome: OutcomeDeniedHard, Reason: reason}
}

// checkPool returns (reason, outcome). A non-empty reason with
// OutcomeAllowed means "below threshold but query succeeded" — the
// caller aggregates reasons across both dimensions before deciding
// hard-vs-soft. An Errored outcome short-circuits the whole check.
func checkPool(ctx context.Context, q CapacityQuery, pool string, thresholdGiB int) (string, Outcome) {
	free, err := q.PoolFreeBytes(ctx, pool)
	if err != nil {
		return fmt.Sprintf("pool %q capacity query failed: %s", pool, err.Error()), OutcomeErrored
	}

	threshold := int64(thresholdGiB) * bytesPerGiB
	if free >= threshold {
		return "", OutcomeAllowed
	}

	return fmt.Sprintf("pool %q free %d GiB < threshold %d GiB", pool, free/bytesPerGiB, thresholdGiB), OutcomeAllowed
}

func checkHostMem(ctx context.Context, q CapacityQuery, thresholdGiB int) (string, Outcome) {
	free, err := q.HostFreeMemoryBytes(ctx)
	if err != nil {
		return "host memory capacity query failed: " + err.Error(), OutcomeErrored
	}

	threshold := int64(thresholdGiB) * bytesPerGiB
	if free >= threshold {
		return "", OutcomeAllowed
	}

	return fmt.Sprintf("host free memory %d GiB < threshold %d GiB", free/bytesPerGiB, thresholdGiB), OutcomeAllowed
}

func joinReasons(rs []string) string {
	switch len(rs) {
	case 0:
		return ""
	case 1:
		return rs[0]
	default:
		return rs[0] + "; " + joinReasons(rs[1:])
	}
}

// ErrCapacityUnknown is returned by higher-level callers (in later
// phases) when a capacity query failed and the caller wants a sentinel
// rather than inspecting a Decision. Not returned by CheckCapacity
// itself — CheckCapacity uses OutcomeErrored so the caller can still
// see the human-readable Reason.
var ErrCapacityUnknown = errors.New("capacity state unknown")
