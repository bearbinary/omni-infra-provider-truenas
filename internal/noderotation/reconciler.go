package noderotation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// DefaultRefreshInterval is the poll cadence the reconciler uses
// between Discover passes. Bound by the fact that COSI watches are
// per-type, not per-namespace; for v1 we poll on a timer rather than
// wire up the watch plumbing. 30s strikes a balance between "noticeable
// rotation lag on operator edits" and "load on the Omni API for
// otherwise quiet clusters".
const DefaultRefreshInterval = 30 * time.Second

// Reconciler is the long-running loop that drives rotation. Each tick:
//  1. Discover candidates.
//  2. For each candidate, Plan a decision.
//  3. Execute at most one decision per candidate per tick.
//
// The "at most one" rule is what keeps rotation safe: between ticks,
// the autoscaler has a chance to scale, the MachineRequestSet
// controller has a chance to act on the lock + count edits, and the
// new fresh Machine has a chance to reach Ready before the next
// teardown.
type Reconciler struct {
	disc     *Discoverer
	engine   *Engine
	logger   *zap.Logger
	interval time.Duration
	metrics  Metrics

	// lastTickAt is the last time a tick completed (success or otherwise).
	// Exposed via LastTickAt for the health probe; updated atomically
	// since the health goroutine reads concurrently with the tick loop.
	lastTickAt atomic.Int64

	// jitterOnce stamps a per-process random offset so multiple
	// reconcilers deployed simultaneously across clusters don't
	// hammer the Omni API on identical tick boundaries.
	jitterOnce sync.Once
	jitter     time.Duration
}

// Metrics is the callback surface for reconciler observability. Kept as
// a plain struct of optional func fields so the package has zero hard
// dependency on OTel — production wires real callbacks, tests pass an
// empty Metrics{} and pin no expectations.
type Metrics struct {
	// OnTick is called once per Discover cycle with the number of
	// candidates returned.
	OnTick func(ctx context.Context, candidates int)

	// OnTickDuration is called once per Discover→plan→execute cycle
	// with the wall-clock duration. Wired to a histogram on the
	// reconciler side so on-call has p99 latency on the rotation
	// tick.
	OnTickDuration func(ctx context.Context, seconds float64)

	// OnDecision is called for every Plan result, keyed by action.
	// Used to surface "how many sets are locked / waiting / refused"
	// per tick.
	OnDecision func(ctx context.Context, candidate *Candidate, decision Decision)

	// OnExecuteError is called when Execute returns a non-nil, non-
	// ErrCandidateNotActionable error. Surface to a counter so
	// operators can alert on persistent write failures.
	OnExecuteError func(ctx context.Context, candidate *Candidate, decision Decision, err error)

	// OnRotationProgress is called when a teardown succeeds. The
	// counter feeds a "rotation rate" view in operator dashboards.
	OnRotationProgress func(ctx context.Context, candidate *Candidate, decision Decision)

	// OnSurgeCycleComplete records the end-to-end duration of a
	// surge cycle. Wired to a histogram so on-call can graph p99
	// rotation cycle duration per role/strategy.
	OnSurgeCycleComplete func(ctx context.Context, candidate *Candidate, decision Decision, durationSeconds float64)
}

// NewReconciler wires a Discoverer + Engine + interval into a runnable
// loop. interval is clamped to a sane lower bound to prevent
// accidental hot-loops on operator misconfiguration.
func NewReconciler(disc *Discoverer, engine *Engine, logger *zap.Logger, interval time.Duration) *Reconciler {
	if logger == nil {
		logger = zap.NewNop()
	}

	if interval < time.Second {
		interval = DefaultRefreshInterval
	}

	return &Reconciler{
		disc:     disc,
		engine:   engine,
		logger:   logger,
		interval: interval,
	}
}

// WithMetrics installs the metrics callbacks. Returns the receiver for
// fluent setup.
func (r *Reconciler) WithMetrics(m Metrics) *Reconciler {
	r.metrics = m
	return r
}

// LastTickAt returns the wall-clock time of the most recent tick
// completion. Returns the zero time before the first tick has run.
// Used by the health probe to declare unhealthy when ticks fall
// behind by more than 3× the refresh interval.
func (r *Reconciler) LastTickAt() time.Time {
	v := r.lastTickAt.Load()
	if v == 0 {
		return time.Time{}
	}

	return time.Unix(0, v)
}

// RefreshInterval returns the configured tick cadence. Used by the
// health probe to size its staleness budget.
func (r *Reconciler) RefreshInterval() time.Duration {
	return r.interval
}

// Run drives the reconciler until ctx is cancelled. Performs an initial
// tick immediately, then ticks on the configured interval. Returns
// ctx.Err() on cancellation; never returns a tick-level error (those
// are logged + metric'd but don't terminate the loop, because killing
// the whole reconciler over one transient Omni timeout would be worse
// than continuing).
func (r *Reconciler) Run(ctx context.Context) error {
	r.logger.Info("node-rotation reconciler starting",
		zap.Duration("refresh_interval", r.interval),
		zap.Duration("lock_ttl", r.engine.lockTTL),
	)

	// Immediate first tick — operators editing a MachineClass right
	// before deploying the reconciler shouldn't have to wait a full
	// interval for the first rotation step.
	r.tick(ctx)

	ticker := time.NewTicker(r.interval + r.tickJitter())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("node-rotation reconciler shutting down")
			return ctx.Err()
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

// tickJitter returns a per-process random offset added to the tick
// interval so multiple reconcilers running on a fleet (one per
// cluster) don't synchronize their Omni API calls.
func (r *Reconciler) tickJitter() time.Duration {
	r.jitterOnce.Do(func() {
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err != nil {
			r.jitter = 0
			return
		}

		// Bound jitter to ±10% of the interval. Enough to scatter
		// fleet ticks; small enough that the effective cadence stays
		// close to what the operator configured.
		max := int64(r.interval / 10)
		if max <= 0 {
			r.jitter = 0
			return
		}

		v := int64(0)
		for i := 0; i < 8; i++ {
			v = (v << 8) | int64(buf[i])
		}
		if v < 0 {
			v = -v
		}
		r.jitter = time.Duration(v % max)
	})

	return r.jitter
}

// tick runs one Discover→Plan→Execute pass. Errors are logged and
// metric'd but never propagated — a single bad tick should not stall
// the loop.
func (r *Reconciler) tick(ctx context.Context) {
	tickID := newTickID()
	tickStart := time.Now()
	tickLogger := r.logger.With(zap.String("tick_id", tickID), zap.Time("tick_ts", tickStart))

	defer func() {
		r.lastTickAt.Store(time.Now().UnixNano())

		if r.metrics.OnTickDuration != nil {
			r.metrics.OnTickDuration(ctx, time.Since(tickStart).Seconds())
		}
	}()

	candidates, err := r.disc.Discover(ctx)
	if err != nil {
		tickLogger.Warn("discover failed; skipping tick", zap.Error(err))
		return
	}

	if r.metrics.OnTick != nil {
		r.metrics.OnTick(ctx, len(candidates))
	}

	for i := range candidates {
		c := &candidates[i]

		decision := r.engine.Plan(c)

		if r.metrics.OnDecision != nil {
			r.metrics.OnDecision(ctx, c, decision)
		}

		r.logDecision(tickLogger, c, decision)

		if err := r.engine.Execute(ctx, decision); err != nil {
			if errors.Is(err, ErrCandidateNotActionable) {
				// Expected for ActionNone / waiting decisions.
				continue
			}

			tickLogger.Error("rotation step failed",
				zap.String("machineset", c.MachineSet.Metadata().ID()),
				zap.String("action", decision.Action),
				zap.Error(err),
			)

			if r.metrics.OnExecuteError != nil {
				r.metrics.OnExecuteError(ctx, c, decision, err)
			}

			continue
		}

		switch decision.Action {
		case ActionTeardownStale, ActionSurgeDown:
			if r.metrics.OnRotationProgress != nil {
				r.metrics.OnRotationProgress(ctx, c, decision)
			}
		case ActionSurgeCycleComplete:
			if r.metrics.OnSurgeCycleComplete != nil {
				// CycleStartedAt lives on the phase annotation that the
				// engine reads in Plan; reconstruct via the Candidate's
				// SurgePhase view. Falls back to 0 if the annotation
				// races us — better than emitting a wrong duration.
				if phase, ok := c.SurgePhase(); ok {
					r.metrics.OnSurgeCycleComplete(ctx, c, decision, time.Since(phase.CycleStartedAt).Seconds())
				}
			}
		}
	}
}

// logDecision emits a per-decision log entry at a level chosen by the
// action type. Steady-state and "waiting" outcomes log at debug to keep
// production logs quiet; refusals, errors, and actual mutations log at
// info or warn.
func (r *Reconciler) logDecision(tickLogger *zap.Logger, c *Candidate, d Decision) {
	// Bail out before constructing the field slice when the level is
	// filtered. Saves a per-Candidate alloc + walk on steady-state
	// ActionNone ticks (the common case).
	level := decisionLevel(d.Action)
	if !tickLogger.Core().Enabled(level) {
		return
	}

	msID := c.MachineSet.Metadata().ID()

	fields := []zap.Field{
		zap.String("machineset", msID),
		zap.String("machineclass", c.MachineClass.Metadata().ID()),
		zap.String("strategy", string(c.Config.Strategy)),
		zap.String("role", string(c.Config.Role)),
		zap.String("action", d.Action),
		zap.String("reason", d.Reason),
		zap.String("class_generation", c.CurrentGeneration),
		zap.Int("machine_count", c.MachineCount),
	}

	if d.TargetRequestID != "" {
		fields = append(fields, zap.String("target_request", d.TargetRequestID))
	}

	if d.PreviousGeneration != "" {
		fields = append(fields, zap.String("previous_generation", d.PreviousGeneration))
	}

	if d.AbortKind != AbortKindNone {
		fields = append(fields, zap.String("abort_kind", string(d.AbortKind)))
	}

	if d.ExpectedCount != 0 {
		fields = append(fields, zap.Int("expected_count", d.ExpectedCount))
	}

	tickLogger.Log(level, "rotation decision", fields...)
}

// decisionLevel picks the log level for a Decision.Action.
func decisionLevel(action string) zapcore.Level {
	switch action {
	case ActionNone, ActionLockedByPriorStep, ActionWaitingForReady, ActionWaitingForTeardown:
		return zapcore.DebugLevel
	case ActionMinHealthyFloor, ActionSurgeAborted:
		return zapcore.WarnLevel
	default:
		return zapcore.InfoLevel
	}
}

// newTickID returns a short random hex string used to correlate all
// log lines emitted during one Discover→Plan→Execute pass. 8 hex
// chars = 32 random bits — enough entropy to disambiguate two ticks
// in the same dashboard query, short enough that the field doesn't
// dominate the log payload.
func newTickID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Random failure is itself worth surfacing; fall back to a
		// fixed sentinel so logs still tag the tick and an SRE can
		// notice the failure mode.
		return "rng-fail"
	}

	return hex.EncodeToString(buf[:])
}
