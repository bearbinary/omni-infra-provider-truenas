package autoscaler

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Rotation-lock coupling. The node-rotation reconciler
// (internal/noderotation) writes the rotationLockAnnotation onto a
// MachineSet for the duration of one rotation step. The autoscaler
// reads it here and clamps Min == Max == CurrentSize for the affected
// node group so Cluster Autoscaler doesn't scale a MachineSet that's
// mid-rotation.
//
// The two packages share only this string-annotation contract: there
// is intentionally no Go-type dependency from autoscaler to
// noderotation, because the autoscaler subcommand can be deployed
// without the rotation reconciler also being deployed (and vice
// versa).
const (
	// rotationLockAnnotation is the MachineSet annotation the rotation
	// reconciler writes. Value format: "<class-generation>:<unix-ts>".
	// Kept in lockstep with noderotation.AnnotationRotationState — a
	// drift between the two constants would silently break the
	// pause coupling, which is why both sides also live in code under
	// test (autoscaler/discovery_test, noderotation/engine_test).
	rotationLockAnnotation = "node-rotation.omni/rotation-state"

	// rotationLockTTL bounds how long an annotation is honored before
	// the autoscaler treats it as stale. Matches
	// noderotation.DefaultLockTTL. Choosing a value here that's
	// SHORTER than the rotation engine's TTL would resume scaling
	// before the engine releases the lock, defeating the coupling;
	// LONGER would extend pauses if the engine crashes mid-step.
	// Same constant on both sides is the right call.
	rotationLockTTL = 5 * time.Minute
)

// isRotationLockActive parses the rotationLockAnnotation value and
// reports whether the lock is still within TTL. A malformed value is
// treated as "not active" so a typo or stale format never indefinitely
// freezes the node group — the rotation engine will re-stamp a valid
// lock on its next step.
//
// Defensive: negative timestamps (operator paste mishap, clock skew on
// the writer) are treated as malformed rather than as "very old"
// timestamps that would still parse as expired-by-TTL — keeps the
// reading consistent with the engine-side parser.
func isRotationLockActive(raw string, now time.Time, ttl time.Duration) bool {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return false
	}

	secs, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || secs < 0 {
		return false
	}

	return now.Sub(time.Unix(secs, 0)) < ttl
}

// recordRotationPause increments the metric used by operator
// dashboards to track how often the autoscaler is held back by a
// rotation step. A persistently nonzero value means rotation is
// constantly in flight and operators can correlate against the
// reconciler's own progress counter.
//
// Lazily initialized via InitMetrics — nil-safe so unit tests can skip
// metric setup entirely.
//
// Labels: machineset + cluster. Bounded by the deployment scale (low
// dozens per Omni instance) — see the cardinality budget comment in
// InitMetrics.
func recordRotationPause(ctx context.Context, machineSetID, cluster string) {
	if AutoscalerPausedForRotation == nil {
		return
	}

	AutoscalerPausedForRotation.Add(ctx, 1, metric.WithAttributes(
		attribute.String("machineset", machineSetID),
		attribute.String("cluster", cluster),
	))
}

// AutoscalerPausedForRotation counts how many times a NodeGroup was
// returned to CAS with Min==Max==CurrentSize because a node-rotation
// step holds the lock. One increment per per-tick observation, so a
// 30s-interval reconciler holding the lock for 5 minutes produces ~10
// increments — gives the dashboard a "rotation in progress" signal
// without overcounting.
var AutoscalerPausedForRotation metric.Int64Counter
