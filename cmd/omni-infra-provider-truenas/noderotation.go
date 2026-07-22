package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cosi-project/runtime/pkg/state"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/health"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/noderotation"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources/meta"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/singleton"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

// runNodeRotation is the entry point for the `omni-infra-provider-truenas
// node-rotation` subcommand. The reconciler watches MachineClass changes
// and rotates members of the matching MachineSets so they boot against
// the current spec.
//
// DARK LAUNCH — this code ships in the binary but the subcommand
// refuses to start unless `NODE_ROTATION_DARK_LAUNCH=true` is set. The
// feature is not yet approved for use; the gate exists so we can ship
// the reconciler + autoscaler coupling to main and iterate on it
// without any user accidentally opting in via just the annotation set.
// Remove the gate (and this comment) when the feature is released.
//
// EXPERIMENTAL when enabled. Both in-place (worker) and surge (worker +
// control-plane) strategies are supported.
//
// The reconciler does not need a TrueNAS client: it operates only on
// Omni COSI state (MachineClass, MachineSet, MachineRequest). Keeping
// the subcommand separate from the provisioner means a misbehaving
// reconciler cannot stall provisioning, and operators can deploy one
// without the other.
func runNodeRotation(baseCtx context.Context) error {
	if !envBool("NODE_ROTATION_DARK_LAUNCH", false) {
		return errors.New("node-rotation is dark-launched — code ships in the binary but the subcommand is disabled by default; " +
			"set NODE_ROTATION_DARK_LAUNCH=true only if you are an approved test operator and have read docs/node-rotation.md; " +
			"this gate will be removed when the feature is released")
	}

	logger, err := newLogger()
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}

	defer func() { _ = logger.Sync() }()

	logger.Warn("node-rotation DARK LAUNCH — feature not yet released; you have opted in via NODE_ROTATION_DARK_LAUNCH=true")

	// Telemetry first so every subsequent log + metric exports through
	// the same pipeline as the provisioner. Without this, OTel meters
	// are bound to the no-op global MeterProvider and every
	// `truenas.node_rotation.*` sample is silently dropped — leaving
	// on-call unable to debug a wedged reconciler at 3 AM.
	otelHeaders := consumeSecretEnv("OTEL_EXPORTER_OTLP_HEADERS")

	telemetryShutdown, err := telemetry.Init(baseCtx, telemetry.Config{
		OTELEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OTELInsecure:   envBool("OTEL_EXPORTER_OTLP_INSECURE", true),
		OTELHeaders:    parseHeaders(otelHeaders),
		OTELProtocol:   envString("OTEL_EXPORTER_OTLP_PROTOCOL", defaultOTELProtocol),
		OTELConsole:    envBool("OTEL_CONSOLE_EXPORT", false),
		ServiceName:    envString("OTEL_SERVICE_NAME", "omni-infra-provider-truenas-node-rotation"),
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("initialize telemetry: %w", err)
	}

	defer func() { _ = telemetryShutdown(baseCtx) }()

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		otelCore := otelzap.NewCore("omni-infra-provider-truenas-node-rotation")
		logger = logger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, otelCore)
		}))
	}

	clusterName := strings.TrimSpace(os.Getenv("NODE_ROTATION_CLUSTER"))
	if clusterName == "" {
		return errors.New("NODE_ROTATION_CLUSTER is required — set it to the Omni cluster name whose MachineSets this reconciler should rotate")
	}

	interval := envDuration("NODE_ROTATION_REFRESH_INTERVAL", noderotation.DefaultRefreshInterval)

	rawLockTTL := envDuration("NODE_ROTATION_LOCK_TTL", noderotation.DefaultLockTTL)
	lockTTL := rawLockTTL
	if lockTTL < noderotation.MinLockTTL {
		logger.Warn("NODE_ROTATION_LOCK_TTL below floor; clamping to safe minimum",
			zap.Duration("operator_value", rawLockTTL),
			zap.Duration("min_lock_ttl", noderotation.MinLockTTL),
		)

		lockTTL = noderotation.MinLockTTL
	}

	trustDeclaredRole := envBool("NODE_ROTATION_TRUST_DECLARED_ROLE", false)

	logger.Info("node-rotation EXPERIMENTAL — see docs/node-rotation.md",
		zap.String("cluster", clusterName),
		zap.Duration("refresh_interval", interval),
		zap.Duration("lock_ttl", lockTTL),
		zap.Bool("trust_declared_role", trustDeclaredRole),
		zap.String("version", version),
	)

	omniClient, err := newOmniClient(logger)
	if err != nil {
		return fmt.Errorf("node-rotation: build Omni client: %w", err)
	}

	defer func() { _ = omniClient.Close() }()

	omniState := omniClient.Omni().State()

	ctx, stop := signal.NotifyContext(baseCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	release, err := acquireNodeRotationLease(ctx, baseCtx, logger, omniState, clusterName, stop)
	if errors.Is(err, errNodeRotationLeaseShutdownDuringAcquire) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()

	metrics, roleMismatchCounts := buildNodeRotationMetrics()

	disc := noderotation.NewDiscoverer(omniState, clusterName, meta.ProviderID, logger.Named("discover")).
		WithTrustDeclaredRole(trustDeclaredRole).
		WithRoleMismatchMetrics(roleMismatchCounts)

	engine := noderotation.NewEngine(omniState, logger.Named("engine")).
		WithLockTTL(lockTTL).
		WithProviderID(meta.ProviderID)

	reconciler := noderotation.NewReconciler(disc, engine, logger.Named("reconciler"), interval).
		WithMetrics(metrics)

	// Health endpoint so k8s liveness/readiness probes can detect a
	// wedged reconciler (panicked tick, stuck Omni call). The lease
	// guarantees uniqueness, not progress — without this, k8s would
	// only restart the pod when the lease expired five minutes later.
	healthAddr := envString("NODE_ROTATION_HEALTH_LISTEN_ADDR", ":8082")
	healthSrv := health.NewServer(reconcilerHealthCheck(reconciler), logger.Named("health"))

	go func() {
		if healthErr := healthSrv.Run(ctx, healthAddr); healthErr != nil {
			logger.Error("health server failed", zap.Error(healthErr))
		}
	}()

	if err := reconciler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("node-rotation reconciler: %w", err)
	}

	logger.Info("node-rotation shutting down")

	return nil
}

// reconcilerHealthCheck returns a Checker that reports unhealthy if
// the reconciler's last tick was more than 3× the refresh interval
// ago. The 3× buffer absorbs a single missed tick caused by a slow
// Omni list; persistent staleness signals a stuck tick goroutine or a
// dead Omni connection.
func reconcilerHealthCheck(r *noderotation.Reconciler) func(context.Context) error {
	return func(_ context.Context) error {
		last := r.LastTickAt()
		if last.IsZero() {
			// Pre-first-tick: report healthy. The reconciler runs an
			// immediate tick on Run, so this window is sub-second.
			return nil
		}

		max := 3 * r.RefreshInterval()
		if since := time.Since(last); since > max {
			return fmt.Errorf("no successful tick in %s (max=%s) — reconciler may be stuck", since.Round(time.Second), max)
		}

		return nil
	}
}

// errNodeRotationLeaseShutdownDuringAcquire mirrors the autoscaler's
// shutdown-during-acquire sentinel. Treated by the caller as a clean
// exit so the process doesn't crashloop when k8s tells it to stop
// mid-acquire.
var errNodeRotationLeaseShutdownDuringAcquire = errors.New("node-rotation shutting down before lease acquired")

// acquireNodeRotationLease constructs and acquires the singleton lease
// for the node-rotation subcommand. Two reconcilers writing
// concurrently to the same MachineSet would race on the rotation lock
// and could double-teardown — fail-fast acquisition is cheap insurance.
//
// Lease scope is per-cluster, mirroring the autoscaler. The cluster
// name suffix makes "rotation for cluster A" and "rotation for cluster
// B" cohabit one Omni instance.
//
// Disabling the lease requires a double opt-out
// (NODE_ROTATION_SINGLETON_ENABLED=false AND
// NODE_ROTATION_SINGLETON_FORCE_DISABLE=true) so a single fat-fingered
// env var doesn't silently turn off the only protection against
// duplicate destructive writes.
func acquireNodeRotationLease(ctx, baseCtx context.Context, logger *zap.Logger, omniState state.State, clusterName string, stop context.CancelFunc) (release func(), err error) {
	noop := func() {}
	if !envBool("NODE_ROTATION_SINGLETON_ENABLED", true) {
		if !envBool("NODE_ROTATION_SINGLETON_FORCE_DISABLE", false) {
			return nil, errors.New("NODE_ROTATION_SINGLETON_ENABLED=false but NODE_ROTATION_SINGLETON_FORCE_DISABLE is not set — set both to consciously opt out (concurrent reconcilers will race on destructive MachineRequest writes)")
		}

		logger.Error("NODE_ROTATION_SINGLETON_ENABLED=false confirmed via FORCE_DISABLE — concurrent reconcilers can race on lock writes; expect duplicate teardowns and conflicting MachineCount edits")

		return noop, nil
	}

	leaseID := "node-rotation-" + clusterName

	lease, leaseErr := singleton.New(omniState, singleton.Config{
		ProviderID:      leaseID,
		RefreshInterval: envDuration("NODE_ROTATION_SINGLETON_REFRESH_INTERVAL", singleton.DefaultRefreshInterval),
		StaleAfter:      envDuration("NODE_ROTATION_SINGLETON_STALE_AFTER", 45*time.Second),
	}, logger)
	if leaseErr != nil {
		return nil, fmt.Errorf("construct node-rotation singleton lease: %w", leaseErr)
	}

	if acquireErr := lease.Acquire(ctx); acquireErr != nil {
		if errors.Is(acquireErr, context.Canceled) {
			logger.Info("node-rotation shutting down before lease acquired")
			return nil, errNodeRotationLeaseShutdownDuringAcquire
		}

		return nil, fmt.Errorf("acquire node-rotation singleton lease for %q: %w", leaseID, acquireErr)
	}

	release = func() {
		relCtx, relCancel := context.WithTimeout(context.WithoutCancel(baseCtx), 5*time.Second)
		defer relCancel()
		lease.Release(relCtx)
	}

	go func() {
		if runErr := lease.Run(ctx); runErr != nil && !errors.Is(runErr, context.Canceled) {
			logger.Error("node-rotation singleton lease lost", zap.Error(runErr))
			stop()
		}
	}()

	logger.Info("node-rotation singleton lease acquired", zap.String("lease_id", leaseID))

	return release, nil
}

// buildNodeRotationMetrics wires the reconciler's callback surface
// onto a fresh OTel meter. Kept here (rather than inside the package)
// so the package stays free of OTel imports — tests and embedders that
// don't want OTel can construct a Reconciler without paying for it.
//
// Counter naming follows the truenas_node_rotation_* prefix so
// Prometheus dashboards can grep all rotation metrics in one regex.
//
// Cardinality budget:
//   - action × strategy × role = ~40 combinations on `decisions`
//   - strategy × role = 4 on `progress`
//   - action × role × abort_kind × strategy = ~80 worst-case on `errors`
//
// All well within Prometheus comfort. No machineset-labelled counters
// here — that would cross into unbounded territory on multi-cluster
// Omni instances. The autoscaler-side paused-for-rotation counter has
// per-machineset labels because its cardinality is bounded by deploy
// scale; do not copy that pattern into this surface.
func buildNodeRotationMetrics() (noderotation.Metrics, *noderotation.RoleMismatchCounts) {
	meter := otel.Meter("omni-infra-provider-truenas-node-rotation")

	candidates, _ := meter.Int64Histogram("truenas.node_rotation.candidates",
		metric.WithDescription("Number of rotation candidates returned per Discover tick"),
	)

	decisions, _ := meter.Int64Counter("truenas.node_rotation.decisions",
		metric.WithDescription("Reconciler decisions by action label"),
	)

	progress, _ := meter.Int64Counter("truenas.node_rotation.progress",
		metric.WithDescription("Successful rotation steps (teardown-stale, surge-down)"),
	)

	execErrors, _ := meter.Int64Counter("truenas.node_rotation.errors",
		metric.WithDescription("Rotation step execution errors"),
	)

	roleMismatchRefused, _ := meter.Int64Counter("truenas.node_rotation.role_mismatch_refused",
		metric.WithDescription("Candidates refused because LabelControlPlaneRole disagreed with the MachineClass-declared role"),
	)

	tickDuration, _ := meter.Float64Histogram("truenas.node_rotation.tick.duration",
		metric.WithDescription("Duration of one Discover→Plan→Execute pass in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)

	surgeCycleDuration, _ := meter.Float64Histogram("truenas.node_rotation.surge.cycle.duration",
		metric.WithDescription("Wall-clock duration of one completed surge cycle in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(30, 60, 120, 300, 600, 1200, 1800, 3600),
	)

	actionKey := attribute.Key("action")
	strategyKey := attribute.Key("strategy")
	roleKey := attribute.Key("role")
	abortKindKey := attribute.Key("abort_kind")

	m := noderotation.Metrics{
		OnTick: func(ctx context.Context, n int) {
			if candidates != nil {
				candidates.Record(ctx, int64(n))
			}
		},
		OnTickDuration: func(ctx context.Context, seconds float64) {
			if tickDuration != nil {
				tickDuration.Record(ctx, seconds)
			}
		},
		OnDecision: func(ctx context.Context, c *noderotation.Candidate, d noderotation.Decision) {
			if decisions == nil {
				return
			}

			action := d.Action
			if action == "" {
				action = "none"
			}

			decisions.Add(ctx, 1, metric.WithAttributes(
				actionKey.String(action),
				strategyKey.String(string(c.Config.Strategy)),
				roleKey.String(string(c.Config.Role)),
			))
		},
		OnExecuteError: func(ctx context.Context, c *noderotation.Candidate, d noderotation.Decision, _ error) {
			if execErrors == nil {
				return
			}

			abortKind := string(d.AbortKind)
			if abortKind == "" {
				abortKind = "none"
			}

			execErrors.Add(ctx, 1, metric.WithAttributes(
				actionKey.String(d.Action),
				roleKey.String(string(c.Config.Role)),
				strategyKey.String(string(c.Config.Strategy)),
				abortKindKey.String(abortKind),
			))
		},
		OnRotationProgress: func(ctx context.Context, c *noderotation.Candidate, _ noderotation.Decision) {
			if progress == nil {
				return
			}

			progress.Add(ctx, 1, metric.WithAttributes(
				strategyKey.String(string(c.Config.Strategy)),
				roleKey.String(string(c.Config.Role)),
			))
		},
		OnSurgeCycleComplete: func(ctx context.Context, c *noderotation.Candidate, _ noderotation.Decision, seconds float64) {
			if surgeCycleDuration == nil {
				return
			}

			surgeCycleDuration.Record(ctx, seconds, metric.WithAttributes(
				strategyKey.String(string(c.Config.Strategy)),
				roleKey.String(string(c.Config.Role)),
			))
		},
	}

	counts := &noderotation.RoleMismatchCounts{
		OnRefused: func(machineSetID, declaredRole string) {
			if roleMismatchRefused == nil {
				return
			}

			roleMismatchRefused.Add(context.Background(), 1, metric.WithAttributes(
				attribute.String("machineset", machineSetID),
				roleKey.String(declaredRole),
			))
		},
	}

	return m, counts
}
