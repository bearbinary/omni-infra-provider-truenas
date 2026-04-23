package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/siderolabs/omni/client/pkg/client"
	"github.com/siderolabs/omni/client/pkg/client/omni"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/autoscaler"
	truenasclient "github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources/meta"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/singleton"
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

	// Register OTel instruments early so every subsequent decision
	// emits metrics. Safe to call before config load — InitMetrics
	// uses the global MeterProvider which is either the real OTLP
	// exporter (when OTEL_EXPORTER_OTLP_ENDPOINT is set) or a no-op.
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

	// Build TrueNAS client for the capacity gate. Reuses the same env
	// vars the provisioner's main.go consumes — matches operator
	// expectations and avoids documentation drift.
	//
	// Host is optional: when unset, the capacity gate is disabled
	// (CapacityQuery passed as nil to the Server). That mode is
	// useful for dry-run deploys where the operator wants to observe
	// NodeGroups discovery + write-path-idempotency without risking
	// gate misconfiguration blocking everything.
	var (
		gate        autoscaler.CapacityQuery
		tnClient    *truenasclient.Client
		defaultPool string
	)

	truenasHost := os.Getenv("TRUENAS_HOST")
	if truenasHost != "" {
		tnClient, err = truenasclient.New(truenasclient.Config{
			Host:               truenasHost,
			APIKey:             consumeSecretEnv("TRUENAS_API_KEY"),
			InsecureSkipVerify: envBool("TRUENAS_INSECURE_SKIP_VERIFY", false),
			MaxConcurrentCalls: envInt("TRUENAS_MAX_CONCURRENT_CALLS", 4),
		})
		if err != nil {
			return fmt.Errorf("autoscaler: build TrueNAS client: %w", err)
		}

		defer func() { _ = tnClient.Close() }()

		gate = autoscaler.NewTrueNASCapacityAdapter(tnClient)
		defaultPool = envString("DEFAULT_POOL", "default")

		logger.Info("autoscaler: TrueNAS capacity gate enabled",
			zap.String("default_pool", defaultPool),
		)
	} else {
		logger.Warn("autoscaler: TRUENAS_HOST unset — capacity gate disabled; scale-up decisions will proceed without pool/host-memory checks")
	}

	discoverer := autoscaler.NewDiscoverer(omniState, cfg.ClusterName, logger)
	writer := autoscaler.NewScaleWriter(omniState)

	server := autoscaler.NewServer(logger, cfg, gate, discoverer, writer).WithDefaultPool(defaultPool)

	ctx, stop := signal.NotifyContext(baseCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Singleton lease prevents two autoscaler Deployments (e.g.,
	// during a rolling restart or a misconfigured HA deploy) from
	// concurrently writing MachineAllocation.MachineCount on the
	// same cluster. Omni's UpdateWithConflicts already makes
	// concurrent writes *correct* (one wins, the other retries), but
	// the lease prevents duplicate API traffic + duplicate structured
	// logs that would otherwise confuse operators reading pod logs.
	//
	// Namespaced ProviderID ("autoscaler-<cluster>") so the lease
	// doesn't collide with the provisioner's provider-id lease. Two
	// autoscalers managing different clusters can coexist; two
	// autoscalers managing the same cluster block on this lease.
	//
	// Disabled when AUTOSCALER_SINGLETON_ENABLED=false — useful for
	// operators deploying `replicas: 0`-style dry-runs where the
	// lease would block a manual test run.
	if envBool("AUTOSCALER_SINGLETON_ENABLED", true) {
		leaseID := "autoscaler-" + cfg.ClusterName

		lease, leaseErr := singleton.New(omniState, singleton.Config{
			ProviderID:      leaseID,
			RefreshInterval: envDuration("AUTOSCALER_SINGLETON_REFRESH_INTERVAL", singleton.DefaultRefreshInterval),
			StaleAfter:      envDuration("AUTOSCALER_SINGLETON_STALE_AFTER", 45*time.Second),
		}, logger)
		if leaseErr != nil {
			return fmt.Errorf("construct autoscaler singleton lease: %w", leaseErr)
		}

		if acquireErr := lease.Acquire(ctx); acquireErr != nil {
			// A context-cancellation during Acquire means the process
			// is already shutting down before the lease was even
			// probed — don't treat that as an error path.
			if errors.Is(acquireErr, context.Canceled) {
				logger.Info("autoscaler shutting down before lease acquired")
				return nil
			}

			return fmt.Errorf("acquire autoscaler singleton lease for %q: %w", leaseID, acquireErr)
		}

		defer lease.Release(context.Background())

		go func() {
			if err := lease.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("autoscaler singleton lease lost", zap.Error(err))
				stop()
			}
		}()

		logger.Info("autoscaler singleton lease acquired", zap.String("lease_id", leaseID))
	} else {
		logger.Warn("autoscaler singleton lease DISABLED — concurrent Deployments can race on MachineAllocation writes (UpdateWithConflicts still prevents incorrect state, but expect duplicate log/metric traffic)")
	}

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

// newOmniClient constructs an Omni SDK client from the same env vars
// the provisioner consumes (OMNI_ENDPOINT, OMNI_SERVICE_ACCOUNT_KEY,
// PROVIDER_ID, OMNI_INSECURE_SKIP_VERIFY). Kept as a subcommand-
// private helper rather than calling main.go's client-build path
// because the subcommands have different defaults — the autoscaler
// doesn't need PROVIDER_ID at all but does need a valid endpoint +
// service-account key. Keeping the two paths separate lets each
// subcommand fail with error messages scoped to its own requirements.
func newOmniClient(logger *zap.Logger) (*client.Client, error) {
	omniEndpoint := os.Getenv("OMNI_ENDPOINT")
	if omniEndpoint == "" {
		return nil, errors.New("OMNI_ENDPOINT is required for the autoscaler subcommand — set it to the Omni cluster-API endpoint the provisioner uses")
	}

	sa := truenasclient.NewSecretString(consumeSecretEnv("OMNI_SERVICE_ACCOUNT_KEY"))

	providerID := os.Getenv("PROVIDER_ID")
	if providerID != "" {
		meta.ProviderID = providerID
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

	logger.Debug("autoscaler: Omni client connected",
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
