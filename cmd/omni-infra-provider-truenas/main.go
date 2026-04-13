// Package main is the entry point for the TrueNAS Omni infrastructure provider.
//
// This provider requires TrueNAS SCALE 25.04+ (JSON-RPC 2.0 API).
// The legacy REST v2.0 API is NOT supported.
package main

import (
	"context"
	_ "embed" // Required for //go:embed directives (schema.json, icon.svg)
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/siderolabs/omni/client/pkg/client"
	"github.com/siderolabs/omni/client/pkg/client/omni"
	"github.com/siderolabs/omni/client/pkg/infra"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/cleanup"
	truenasclient "github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/health"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/monitor"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/provisioner"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources/meta"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/singleton"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

// version is set at build time via -ldflags.
var version = "dev"

//go:embed data/schema.json
var schema string

//go:embed data/icon.svg
var icon []byte

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load .env file if present — does not override existing env vars.
	// Silently ignored if .env doesn't exist (Docker/k8s set env vars directly).
	//
	// SECURITY: godotenv loads from the working directory. If an attacker can write
	// an .env file (e.g., via volume mount misconfiguration), they could override
	// TRUENAS_HOST, TRUENAS_API_KEY, or OMNI_ENDPOINT. Mitigations:
	//   - Docker: read_only: true in the TrueNAS app template
	//   - Kubernetes: readOnlyRootFilesystem: true in the deployment securityContext
	//   - godotenv does NOT override existing env vars (existing values take precedence)
	_ = godotenv.Load()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer cancel()

	loggerConfig := zap.NewProductionConfig()

	logLevel := os.Getenv("LOG_LEVEL")
	switch logLevel {
	case "debug":
		loggerConfig.Level.SetLevel(zap.DebugLevel)
	case "warn":
		loggerConfig.Level.SetLevel(zap.WarnLevel)
	case "error":
		loggerConfig.Level.SetLevel(zap.ErrorLevel)
	default:
		loggerConfig.Level.SetLevel(zap.InfoLevel)
	}

	logger, err := loggerConfig.Build(zap.AddStacktrace(zapcore.ErrorLevel))
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize telemetry (noop if OTEL_EXPORTER_OTLP_ENDPOINT is not set)
	telemetryShutdown, err := telemetry.Init(ctx, telemetry.Config{
		OTELEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OTELInsecure:   envBool("OTEL_EXPORTER_OTLP_INSECURE", true),
		OTELHeaders:    parseHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")),
		OTELProtocol:   envString("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc"),
		PyroscopeURL:   os.Getenv("PYROSCOPE_URL"),
		PyroscopeUser:  os.Getenv("PYROSCOPE_BASIC_AUTH_USER"),
		PyroscopePass:  os.Getenv("PYROSCOPE_BASIC_AUTH_PASSWORD"),
		ServiceName:    envString("OTEL_SERVICE_NAME", "omni-infra-provider-truenas"),
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize telemetry: %w", err)
	}
	defer func() { _ = telemetryShutdown(ctx) }()

	if version == "dev" && os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		logger.Warn("running with version='dev' while OTEL is enabled — " +
			"telemetry data will not be correlated to a release. " +
			"Build with -ldflags=\"-X main.version=vX.Y.Z\" for production.")
	}

	// Add otelzap bridge for log-trace correlation (logs include trace_id/span_id)
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		otelCore := otelzap.NewCore("omni-infra-provider-truenas")
		logger = logger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, otelCore)
		}))
	}

	// Read configuration from environment variables
	omniEndpoint := os.Getenv("OMNI_ENDPOINT")
	if omniEndpoint == "" {
		return fmt.Errorf("OMNI_ENDPOINT is required")
	}

	omniServiceAccountKey := truenasclient.NewSecretString(os.Getenv("OMNI_SERVICE_ACCOUNT_KEY"))

	providerID := os.Getenv("PROVIDER_ID")
	if providerID != "" {
		meta.ProviderID = providerID
	}

	defaultPool := envString("DEFAULT_POOL", "default")
	defaultNetworkInterface := envString("DEFAULT_NETWORK_INTERFACE", "")
	defaultBootMethod := envString("DEFAULT_BOOT_METHOD", "UEFI")
	concurrency := envInt("CONCURRENCY", 4)

	// Create TrueNAS client — auto-detects transport:
	//   1. Unix socket (if /var/run/middleware/middlewared.sock exists) — no API key needed
	//   2. WebSocket (requires TRUENAS_HOST + TRUENAS_API_KEY)
	tnClient, err := truenasclient.New(truenasclient.Config{
		Host:               os.Getenv("TRUENAS_HOST"),
		APIKey:             os.Getenv("TRUENAS_API_KEY"),
		InsecureSkipVerify: envBool("TRUENAS_INSECURE_SKIP_VERIFY", false),
		SocketPath:         os.Getenv("TRUENAS_SOCKET_PATH"),
		MaxConcurrentCalls: envInt("TRUENAS_MAX_CONCURRENT_CALLS", 8),
	})
	if err != nil {
		return fmt.Errorf("failed to create TrueNAS client: %w", err)
	}
	defer func() { _ = tnClient.Close() }()

	logger.Info("TrueNAS client connected",
		zap.String("transport", tnClient.TransportName()),
	)

	// Create provisioner
	prov := provisioner.NewProvisioner(tnClient, provisioner.ProviderConfig{
		DefaultPool:             defaultPool,
		DefaultNetworkInterface: defaultNetworkInterface,
		DefaultBootMethod:       defaultBootMethod,
		GracefulShutdownTimeout: time.Duration(envInt("GRACEFUL_SHUTDOWN_TIMEOUT", 30)) * time.Second,
		MaxErrorRecoveries:      envInt("MAX_ERROR_RECOVERIES", 5),
	})

	// Create infra provider
	//goland:noinspection ALL — false positive: Go compiler infers generic type params correctly
	ip, err := infra.NewProvider(meta.ProviderID, prov, infra.ProviderConfig{
		Name:        envString("PROVIDER_NAME", "TrueNAS"),
		Description: envString("PROVIDER_DESCRIPTION", "TrueNAS SCALE infrastructure provider"),
		Icon:        base64.RawStdEncoding.EncodeToString(icon),
		Schema:      schema,
	})
	if err != nil {
		return fmt.Errorf("failed to create infra provider: %w", err)
	}

	if err := runStartupChecks(ctx, logger, tnClient, defaultPool, defaultNetworkInterface); err != nil {
		return err
	}

	// Start background cleanup for stale ISOs and orphan VMs/zvols.
	// Orphan detection uses TrueNAS state (zvol existence) rather than in-memory
	// tracking, so it's safe across provider restarts.
	cleaner := cleanup.New(tnClient, cleanup.Config{
		Pool: defaultPool,
	}, logger, prov.ActiveImageIDs)

	go cleaner.Run(ctx)

	// Start host health monitor (publishes OTEL gauges)
	hostMonitor := monitor.New(tnClient, monitor.Config{}, logger)

	go hostMonitor.Run(ctx)

	// Start HTTP health endpoint for Kubernetes probes
	healthAddr := envString("HEALTH_LISTEN_ADDR", ":8081")
	healthSrv := health.NewServer(newHealthCheck(tnClient, defaultPool, defaultNetworkInterface), logger)

	go func() {
		if err := healthSrv.Run(ctx, healthAddr); err != nil {
			logger.Error("health server failed", zap.Error(err))
		}
	}()

	logger.Info("starting TrueNAS infra provider",
		zap.String("provider_id", meta.ProviderID),
		zap.String("omni_endpoint", omniEndpoint),
		zap.String("default_pool", defaultPool),
		zap.String("default_network_interface", defaultNetworkInterface),
	)

	// Build the Omni client ourselves (rather than letting infra.Provider.Run
	// build it) so the singleton lease can access the COSI state before ip.Run
	// starts. ip.Run accepts our state via infra.WithState.
	clientOptions := []client.Option{
		client.WithInsecureSkipTLSVerify(envBool("OMNI_INSECURE_SKIP_VERIFY", false)),
		// Matches the SDK's internal behavior when it builds the client itself:
		// the infra provider ID is sent as gRPC metadata on every call.
		client.WithOmniClientOptions(omni.WithProviderID(meta.ProviderID)),
	}

	if !omniServiceAccountKey.IsEmpty() {
		clientOptions = append(clientOptions, client.WithServiceAccount(omniServiceAccountKey.Reveal()))
	}

	omniClient, err := client.New(omniEndpoint, clientOptions...)
	if err != nil {
		return fmt.Errorf("failed to create Omni client: %w", err)
	}
	defer func() { _ = omniClient.Close() }()

	omniState := omniClient.Omni().State()

	// Acquire the singleton lease before ip.Run so we fail fast if another
	// instance is already serving this provider ID. Races on provision steps
	// (VM create, zvol create, ISO upload) across two processes are
	// effectively impossible to recover from, so we'd rather crashloop loudly.
	var lease *singleton.Lease

	if envBool("PROVIDER_SINGLETON_ENABLED", true) {
		lease, err = singleton.New(omniState, singleton.Config{
			ProviderID:      meta.ProviderID,
			RefreshInterval: envDuration("PROVIDER_SINGLETON_REFRESH_INTERVAL", singleton.DefaultRefreshInterval),
			StaleAfter:      envDuration("PROVIDER_SINGLETON_STALE_AFTER", singleton.DefaultStaleAfter),
			OnRefreshError: func() {
				if telemetry.SingletonRefreshErrors != nil {
					telemetry.SingletonRefreshErrors.Add(ctx, 1)
				}
			},
		}, logger)
		if err != nil {
			return fmt.Errorf("failed to build singleton lease: %w", err)
		}

		if err := lease.Acquire(ctx); err != nil {
			return fmt.Errorf("singleton lease acquire failed: %w", err)
		}

		// Track lease acquisition
		if telemetry.SingletonLeaseHeld != nil {
			telemetry.SingletonLeaseHeld.Record(ctx, 1)
		}

		if lease.WasTakeover() && telemetry.SingletonTakeovers != nil {
			telemetry.SingletonTakeovers.Add(ctx, 1)
		}

		defer func() {
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer releaseCancel()
			lease.Release(releaseCtx)

			if telemetry.SingletonLeaseHeld != nil {
				telemetry.SingletonLeaseHeld.Record(releaseCtx, 0)
			}
		}()

		go func() {
			if runErr := lease.Run(ctx); runErr != nil {
				logger.Error("singleton lease refresh loop exited with error", zap.Error(runErr))
			}
		}()

		go func() {
			select {
			case <-ctx.Done():
			case <-lease.Lost():
				logger.Error("singleton lease lost — cancelling root context to shut down")

				if telemetry.SingletonLeaseHeld != nil {
					telemetry.SingletonLeaseHeld.Record(ctx, 0)
				}

				cancel()
			}
		}()
	} else {
		logger.Warn("singleton enforcement disabled via PROVIDER_SINGLETON_ENABLED=false — " +
			"running multiple instances with the same PROVIDER_ID will cause provisioning races")
	}

	return ip.Run(ctx, logger,
		infra.WithState(omniState),
		infra.WithEncodeRequestIDsIntoTokens(),
		infra.WithConcurrency(uint(concurrency)),
		infra.WithHealthCheckFunc(newHealthCheck(tnClient, defaultPool, defaultNetworkInterface)),
	)
}

func envDuration(key string, defaultVal time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return defaultVal
	}

	return d
}

func runStartupChecks(ctx context.Context, logger *zap.Logger, tnClient *truenasclient.Client, pool, networkInterface string) error {
	if err := tnClient.Ping(ctx); err != nil {
		return fmt.Errorf("startup check failed — TrueNAS API unreachable: %w", err)
	}

	// Verify TrueNAS version is 25.04+ (JSON-RPC 2.0 required)
	ver, err := tnClient.SystemVersion(ctx)
	if err != nil {
		logger.Warn("could not check TrueNAS version", zap.Error(err))
	} else {
		logger.Info("TrueNAS version", zap.String("version", ver))

		if !isSupportedTrueNASVersion(ver) {
			return fmt.Errorf("startup check failed — TrueNAS SCALE 25.04+ (Fangtooth) required, found %q. "+
				"This provider uses JSON-RPC 2.0 which is not available on older versions", ver)
		}
	}

	if exists, err := tnClient.PoolExists(ctx, pool); err != nil {
		return fmt.Errorf("startup check failed — cannot verify pool %q: %w", pool, err)
	} else if !exists {
		return fmt.Errorf("startup check failed — pool %q not found on TrueNAS", pool)
	}

	if networkInterface != "" {
		if valid, err := tnClient.NetworkInterfaceValid(ctx, networkInterface); err != nil {
			return fmt.Errorf("startup check failed — cannot verify network interface %q: %w", networkInterface, err)
		} else if !valid {
			choices, _ := tnClient.NetworkInterfaceChoices(ctx)
			return fmt.Errorf("startup check failed — network interface %q not found on TrueNAS. Available: %v", networkInterface, choices)
		}
	} else {
		logger.Warn("DEFAULT_NETWORK_INTERFACE not set — MachineClass configs must specify network_interface")
	}

	logger.Info("startup checks passed",
		zap.String("transport", tnClient.TransportName()),
		zap.String("pool", pool),
		zap.String("network_interface", networkInterface),
	)

	return nil
}

func newHealthCheck(tnClient *truenasclient.Client, pool, networkInterface string) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := tnClient.Ping(ctx); err != nil {
			recordHealthCheckError(ctx)

			return fmt.Errorf("TrueNAS API unreachable: %w", err)
		}

		exists, err := tnClient.PoolExists(ctx, pool)
		if err != nil {
			recordHealthCheckError(ctx)

			return fmt.Errorf("failed to check pool %q: %w", pool, err)
		}

		if !exists {
			recordHealthCheckError(ctx)

			return fmt.Errorf("pool %q not found on TrueNAS", pool)
		}

		if networkInterface != "" {
			valid, nicErr := tnClient.NetworkInterfaceValid(ctx, networkInterface)
			if nicErr != nil {
				recordHealthCheckError(ctx)

				return fmt.Errorf("failed to validate network interface %q: %w", networkInterface, nicErr)
			}

			if !valid {
				recordHealthCheckError(ctx)

				return fmt.Errorf("network interface %q not found on TrueNAS", networkInterface)
			}
		}

		return nil
	}
}

func recordHealthCheckError(ctx context.Context) {
	if telemetry.HealthCheckErrors != nil {
		telemetry.HealthCheckErrors.Add(ctx, 1)
	}
}

// isSupportedTrueNASVersion checks if the version string indicates 25.x or later.
// Extracts the major version number and compares >= 25.
func isSupportedTrueNASVersion(ver string) bool {
	// Version format: "TrueNAS-SCALE-25.04.0" or similar
	// Extract digits after the last dash
	parts := strings.Split(ver, "-")
	for _, part := range parts {
		if len(part) > 0 && part[0] >= '0' && part[0] <= '9' {
			// Found the version number part (e.g., "25.04.0")
			dotParts := strings.Split(part, ".")
			if len(dotParts) >= 1 {
				major, err := strconv.Atoi(dotParts[0])
				if err == nil {
					return major >= 25
				}
			}
		}
	}

	// Can't parse — assume supported (don't block on unexpected format)
	return true
}

// parseHeaders parses the OTEL_EXPORTER_OTLP_HEADERS format: "key=value,key2=value2".
func parseHeaders(raw string) map[string]string {
	if raw == "" {
		return nil
	}

	headers := make(map[string]string)

	for _, pair := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if ok && k != "" {
			headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}

	if len(headers) == 0 {
		return nil
	}

	return headers
}

func envString(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return defaultVal
}

func envBool(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}

	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}

	return b
}

func envInt(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}

	i, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}

	return i
}
