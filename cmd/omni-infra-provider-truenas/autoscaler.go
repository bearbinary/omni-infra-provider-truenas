package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/omni/client/pkg/client"
	"github.com/siderolabs/omni/client/pkg/client/omni"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/autoscaler"
	truenasclient "github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources/meta"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/singleton"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

// runAutoscaler is the entry point for the `omni-infra-provider-truenas
// autoscaler` subcommand. Phase 4 wires:
//   - Omni client (state for discovery + MachineAllocation writes)
//   - TrueNAS client (capacity gate queries)
//   - gRPC server answering the external-gRPC cluster-autoscaler contract
//
// Returns a plain error so main.go's switch handler can print and exit
// without the subcommand needing its own os.Exit path.
//
// The default subcommand (no argv) remains the provisioner, so existing
// Deployments bumping image tags see zero behavior drift from this
// feature.
func runAutoscaler(baseCtx context.Context) error {
	logger, err := newLogger()
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}

	defer func() { _ = logger.Sync() }()

	// Telemetry first so InitMetrics() below binds to the real OTLP
	// MeterProvider rather than the no-op global. Without this, every
	// truenas.autoscaler.* metric is silently dropped — leaving on-call
	// without visibility when scaling decisions go wrong.
	otelHeaders := consumeSecretEnv("OTEL_EXPORTER_OTLP_HEADERS")

	telemetryShutdown, telemErr := telemetry.Init(baseCtx, telemetry.Config{
		OTELEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OTELInsecure:   envBool("OTEL_EXPORTER_OTLP_INSECURE", true),
		OTELHeaders:    parseHeaders(otelHeaders),
		OTELProtocol:   envString("OTEL_EXPORTER_OTLP_PROTOCOL", defaultOTELProtocol),
		OTELConsole:    envBool("OTEL_CONSOLE_EXPORT", false),
		ServiceName:    envString("OTEL_SERVICE_NAME", "omni-infra-provider-truenas-autoscaler"),
		ServiceVersion: version,
	})
	if telemErr != nil {
		return fmt.Errorf("initialize telemetry: %w", telemErr)
	}

	defer func() { _ = telemetryShutdown(baseCtx) }()

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		otelCore := otelzap.NewCore("omni-infra-provider-truenas-autoscaler")
		logger = logger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, otelCore)
		}))
	}

	// Register OTel instruments now that the MeterProvider is the real
	// OTLP one. Safe to call multiple times via the sync.Once guard.
	autoscaler.InitMetrics()

	cfg, err := autoscaler.LoadSubcommandConfig()
	if err != nil {
		return fmt.Errorf("load autoscaler config: %w", err)
	}

	// Experimental banner: one line at Info so operators grepping logs
	// can confirm the subcommand is live AND the opt-in has happened.
	logger.Info("autoscaler EXPERIMENTAL — see docs/autoscaler.md; this feature may change without semver guarantees",
		zap.String("cluster", cfg.ClusterName),
		zap.String("listen", cfg.ListenAddress),
		zap.Duration("refresh_interval", cfg.RefreshInterval),
		zap.String("version", version),
	)

	// Build Omni client. Shared env vars with the provisioner —
	// OMNI_ENDPOINT, OMNI_SERVICE_ACCOUNT_KEY, PROVIDER_ID — so one
	// `.env` works for both subcommands if the operator wants to
	// colocate them.
	omniClient, err := newOmniClient(logger)
	if err != nil {
		return fmt.Errorf("autoscaler: build Omni client: %w", err)
	}

	defer func() { _ = omniClient.Close() }()

	omniState := omniClient.Omni().State()

	bundle, err := buildAutoscalerCapacityGate(logger)
	if err != nil {
		return err
	}
	defer bundle.Close()

	discoverer := autoscaler.NewDiscoverer(omniState, cfg.ClusterName, logger)
	writer := autoscaler.NewScaleWriter(omniState)

	server := autoscaler.NewServer(logger, cfg, bundle.Query, discoverer, writer).WithDefaultPool(bundle.DefaultPool)

	ctx, stop := signal.NotifyContext(baseCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	release, err := acquireAutoscalerLease(ctx, baseCtx, logger, omniState, cfg.ClusterName, stop)
	if errors.Is(err, errAutoscalerLeaseShutdownDuringAcquire) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()

	if err := server.Listen(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Info("autoscaler shutting down")
			return nil
		}

		return fmt.Errorf("autoscaler gRPC server: %w", err)
	}

	logger.Info("autoscaler shutting down")

	return nil
}

// CapacityGateBundle wraps the autoscaler's optional TrueNAS capacity gate
// + its TrueNAS client + the default pool, with a single Close() that
// safely no-ops when the gate is disabled. The caller defers bundle.Close()
// unconditionally — no nil-check dance.
type CapacityGateBundle struct {
	Query       autoscaler.CapacityQuery
	DefaultPool string
	close       func()
}

// Close releases the underlying TrueNAS client, or no-ops if the gate was
// disabled via unset TRUENAS_HOST.
func (b *CapacityGateBundle) Close() {
	if b == nil || b.close == nil {
		return
	}
	b.close()
}

// buildAutoscalerCapacityGate constructs the capacity-gate bundle. When
// TRUENAS_HOST is unset (dry-run mode), returns a bundle with a nil Query
// and a no-op Close — the caller never has to check for nil.
func buildAutoscalerCapacityGate(logger *zap.Logger) (*CapacityGateBundle, error) {
	truenasHost := os.Getenv("TRUENAS_HOST")
	if truenasHost == "" {
		logger.Warn("autoscaler: TRUENAS_HOST unset — capacity gate disabled; scale-up decisions will proceed without pool/host-memory checks")
		return &CapacityGateBundle{}, nil
	}

	tnClient, err := truenasclient.New(truenasclient.Config{
		Host:               truenasHost,
		APIKey:             consumeSecretEnv("TRUENAS_API_KEY"),
		InsecureSkipVerify: envBool("TRUENAS_INSECURE_SKIP_VERIFY", false),
		MaxConcurrentCalls: envInt("TRUENAS_MAX_CONCURRENT_CALLS", 4),
	})
	if err != nil {
		return nil, fmt.Errorf("autoscaler: build TrueNAS client: %w", err)
	}

	defaultPool := envString("DEFAULT_POOL", "default")
	logger.Info("autoscaler: TrueNAS capacity gate enabled", zap.String("default_pool", defaultPool))

	return &CapacityGateBundle{
		Query:       autoscaler.NewTrueNASCapacityAdapter(tnClient),
		DefaultPool: defaultPool,
		close:       func() { _ = tnClient.Close() },
	}, nil
}

// errAutoscalerLeaseShutdownDuringAcquire is returned when ctx is cancelled
// before lease.Acquire completes. The caller treats this as a clean exit
// (the process is already shutting down) — distinguished from real
// acquire failures via errors.Is.
var errAutoscalerLeaseShutdownDuringAcquire = errors.New("autoscaler shutting down before lease acquired")

// acquireAutoscalerLease constructs and acquires the singleton lease for
// the autoscaler subcommand. Returns one of:
//
//   - (no-op release, nil)                              — lease disabled via env; run anyway.
//   - (release fn, nil)                                 — acquired; caller defers release.
//   - (nil, errAutoscalerLeaseShutdownDuringAcquire)    — ctx cancelled mid-acquire; bail clean.
//   - (nil, real err)                                   — construction/acquire failed hard.
//
// The returned release is always non-nil when err is nil — the caller can
// `defer release()` unconditionally.
func acquireAutoscalerLease(ctx, baseCtx context.Context, logger *zap.Logger, omniState state.State, clusterName string, stop context.CancelFunc) (release func(), err error) {
	noop := func() {}
	if !envBool("AUTOSCALER_SINGLETON_ENABLED", true) {
		logger.Warn("autoscaler singleton lease DISABLED — concurrent Deployments can race on MachineAllocation writes (UpdateWithConflicts still prevents incorrect state, but expect duplicate log/metric traffic)")
		return noop, nil
	}

	leaseID := "autoscaler-" + clusterName

	lease, leaseErr := singleton.New(omniState, singleton.Config{
		ProviderID:      leaseID,
		RefreshInterval: envDuration("AUTOSCALER_SINGLETON_REFRESH_INTERVAL", singleton.DefaultRefreshInterval),
		StaleAfter:      envDuration("AUTOSCALER_SINGLETON_STALE_AFTER", 45*time.Second),
	}, logger)
	if leaseErr != nil {
		return nil, fmt.Errorf("construct autoscaler singleton lease: %w", leaseErr)
	}

	if acquireErr := lease.Acquire(ctx); acquireErr != nil {
		if errors.Is(acquireErr, context.Canceled) {
			logger.Info("autoscaler shutting down before lease acquired")
			return nil, errAutoscalerLeaseShutdownDuringAcquire
		}

		return nil, fmt.Errorf("acquire autoscaler singleton lease for %q: %w", leaseID, acquireErr)
	}

	// Item 4 (Obs): release runs through a 5s timeout so a slow Omni RPC
	// cannot stall pod shutdown. WithoutCancel preserves trace IDs through
	// the cleanup span.
	release = func() {
		relCtx, relCancel := context.WithTimeout(context.WithoutCancel(baseCtx), 5*time.Second)
		defer relCancel()
		lease.Release(relCtx)
		if telemetry.SingletonLeaseHeld != nil {
			telemetry.SingletonLeaseHeld.Record(relCtx, 0, metric.WithAttributes(attribute.String("scope", leaseID)))
		}
	}

	// Item 5 (Obs): emit the SingletonLeaseHeld gauge on acquire + decrement
	// on lease-lost + release. Provider-side already does this; autoscaler
	// was missing.
	if telemetry.SingletonLeaseHeld != nil {
		telemetry.SingletonLeaseHeld.Record(ctx, 1, metric.WithAttributes(attribute.String("scope", leaseID)))
	}

	go func() {
		if runErr := lease.Run(ctx); runErr != nil && !errors.Is(runErr, context.Canceled) {
			logger.Error("autoscaler singleton lease lost", zap.Error(runErr))
			if telemetry.SingletonLeaseHeld != nil {
				telemetry.SingletonLeaseHeld.Record(ctx, 0, metric.WithAttributes(attribute.String("scope", leaseID)))
			}
			stop()
		}
	}()

	logger.Info("autoscaler singleton lease acquired", zap.String("lease_id", leaseID))

	return release, nil
}

// newOmniClient constructs an Omni SDK client from the same env vars
// the provisioner consumes (OMNI_ENDPOINT, OMNI_SERVICE_ACCOUNT_KEY,
// PROVIDER_ID, OMNI_INSECURE_SKIP_VERIFY). Kept as a subcommand-
// private helper rather than calling main.go's client-build path
// because the subcommands have different defaults. The PROVIDER_ID
// guard mirrors the provisioner: refusing a non-localhost endpoint
// with empty PROVIDER_ID prevents cross-tenant lease collisions and
// the LeaseHeldError identity leak on SaaS Omni.
func newOmniClient(logger *zap.Logger) (*client.Client, error) {
	omniEndpoint := os.Getenv("OMNI_ENDPOINT")
	if omniEndpoint == "" {
		return nil, errors.New("OMNI_ENDPOINT is required — set it to the Omni cluster-API endpoint the provisioner uses")
	}

	sa := truenasclient.NewSecretString(consumeSecretEnv("OMNI_SERVICE_ACCOUNT_KEY"))

	providerID := os.Getenv("PROVIDER_ID")
	if providerID != "" {
		meta.ProviderID = providerID
	} else if !isLocalOmniEndpoint(omniEndpoint) {
		// Two tenants that both default to PROVIDER_ID=truenas on the
		// same SaaS Omni would share lease keys and the singleton's
		// LeaseHeldError.OtherInstanceID would leak the other tenant's
		// instance UUID. The node-rotation subcommand additionally reads
		// MachineRequests by LabelInfraProviderID, so a collision could
		// cross-tear-down requests across tenants. Fail fast — same
		// posture as cmd/.../main.go for the provisioner.
		return nil, errors.New("PROVIDER_ID is required when OMNI_ENDPOINT is not localhost — set PROVIDER_ID=<unique-per-tenant-value>")
	}

	opts := []client.Option{
		client.WithInsecureSkipTLSVerify(envBool("OMNI_INSECURE_SKIP_VERIFY", false)),
		client.WithOmniClientOptions(omni.WithProviderID(meta.ProviderID)),
	}

	if !sa.IsEmpty() {
		opts = append(opts, client.WithServiceAccount(sa.Reveal()))
	}

	c, err := client.New(omniEndpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("new Omni client: %w", err)
	}

	logger.Debug("Omni client connected",
		zap.String("endpoint", omniEndpoint),
		zap.Bool("has_service_account", !sa.IsEmpty()),
	)

	return c, nil
}

// newLogger builds the same zap logger used by the provisioner entry
// point so log format, structured metadata, and OTel bridge behavior
// are identical across subcommands. Isolated here (vs calling into
// main.go's buildLogger) to keep the subcommand's surface testable
// without importing the full provisioner wiring.
func newLogger() (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.MessageKey = "msg"

	return cfg.Build()
}
