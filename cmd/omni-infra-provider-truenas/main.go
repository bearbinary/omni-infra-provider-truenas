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
	"syscall"

	"github.com/joho/godotenv"
	"github.com/siderolabs/omni/client/pkg/client"
	"github.com/siderolabs/omni/client/pkg/infra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/cleanup"
	truenasclient "github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/provisioner"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources/meta"
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
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load .env file if present — does not override existing env vars.
	// Silently ignored if .env doesn't exist (Docker/k8s set env vars directly).
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
		PyroscopeURL:   os.Getenv("PYROSCOPE_URL"),
		ServiceName:    envString("OTEL_SERVICE_NAME", "omni-infra-provider-truenas"),
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize telemetry: %w", err)
	}
	defer telemetryShutdown(ctx)

	// Read configuration from environment variables
	omniEndpoint := os.Getenv("OMNI_ENDPOINT")
	if omniEndpoint == "" {
		return fmt.Errorf("OMNI_ENDPOINT is required")
	}

	omniServiceAccountKey := os.Getenv("OMNI_SERVICE_ACCOUNT_KEY")

	providerID := os.Getenv("PROVIDER_ID")
	if providerID != "" {
		meta.ProviderID = providerID
	}

	defaultPool := envString("DEFAULT_POOL", "default")
	defaultNICAttach := envString("DEFAULT_NIC_ATTACH", "")
	defaultBootMethod := envString("DEFAULT_BOOT_METHOD", "UEFI")
	concurrency := envInt("CONCURRENCY", 4)

	// Create TrueNAS client — auto-detects transport:
	//   1. Unix socket (if /var/run/middleware/middlewared.sock exists) — no API key needed
	//   2. WebSocket (requires TRUENAS_HOST + TRUENAS_API_KEY)
	tnClient, err := truenasclient.New(truenasclient.Config{
		Host:               os.Getenv("TRUENAS_HOST"),
		APIKey:             os.Getenv("TRUENAS_API_KEY"),
		InsecureSkipVerify: envBool("TRUENAS_INSECURE_SKIP_VERIFY", true),
		SocketPath:         os.Getenv("TRUENAS_SOCKET_PATH"),
		MaxConcurrentCalls: envInt("TRUENAS_MAX_CONCURRENT_CALLS", 8),
	})
	if err != nil {
		return fmt.Errorf("failed to create TrueNAS client: %w", err)
	}
	defer tnClient.Close()

	logger.Info("TrueNAS client connected",
		zap.String("transport", tnClient.TransportName()),
	)

	// Create provisioner
	prov := provisioner.NewProvisioner(tnClient, provisioner.ProviderConfig{
		DefaultPool:       defaultPool,
		DefaultNICAttach:  defaultNICAttach,
		DefaultBootMethod: defaultBootMethod,
	})

	// Create infra provider
	ip, err := infra.NewProvider(meta.ProviderID, prov, infra.ProviderConfig{
		Name:        envString("PROVIDER_NAME", "TrueNAS"),
		Description: envString("PROVIDER_DESCRIPTION", "TrueNAS SCALE infrastructure provider"),
		Icon:        base64.RawStdEncoding.EncodeToString(icon),
		Schema:      schema,
	})
	if err != nil {
		return fmt.Errorf("failed to create infra provider: %w", err)
	}

	if err := runStartupChecks(ctx, logger, tnClient, defaultPool, defaultNICAttach); err != nil {
		return err
	}

	// Start background cleanup for stale ISOs and orphan VMs/zvols
	cleaner := cleanup.New(tnClient, cleanup.Config{
		Pool: defaultPool,
	}, logger, prov.ActiveImageIDs, prov.ActiveVMNames)

	go cleaner.Run(ctx)

	logger.Info("starting TrueNAS infra provider",
		zap.String("provider_id", meta.ProviderID),
		zap.String("omni_endpoint", omniEndpoint),
		zap.String("default_pool", defaultPool),
		zap.String("default_nic_attach", defaultNICAttach),
	)

	clientOptions := []client.Option{
		client.WithInsecureSkipTLSVerify(envBool("OMNI_INSECURE_SKIP_VERIFY", false)),
	}

	if omniServiceAccountKey != "" {
		clientOptions = append(clientOptions, client.WithServiceAccount(omniServiceAccountKey))
	}

	return ip.Run(ctx, logger,
		infra.WithOmniEndpoint(omniEndpoint),
		infra.WithClientOptions(clientOptions...),
		infra.WithEncodeRequestIDsIntoTokens(),
		infra.WithConcurrency(uint(concurrency)),
		infra.WithHealthCheckFunc(newHealthCheck(tnClient, defaultPool, defaultNICAttach)),
	)
}

func runStartupChecks(ctx context.Context, logger *zap.Logger, tnClient *truenasclient.Client, pool, nicAttach string) error {
	if err := tnClient.Ping(ctx); err != nil {
		return fmt.Errorf("startup check failed — TrueNAS API unreachable: %w", err)
	}

	if exists, err := tnClient.PoolExists(ctx, pool); err != nil {
		return fmt.Errorf("startup check failed — cannot verify pool %q: %w", pool, err)
	} else if !exists {
		return fmt.Errorf("startup check failed — pool %q not found on TrueNAS", pool)
	}

	if nicAttach != "" {
		if valid, err := tnClient.NICAttachValid(ctx, nicAttach); err != nil {
			return fmt.Errorf("startup check failed — cannot verify NIC attach target %q: %w", nicAttach, err)
		} else if !valid {
			choices, _ := tnClient.NICAttachChoices(ctx)
			return fmt.Errorf("startup check failed — NIC attach target %q not found on TrueNAS. Available: %v", nicAttach, choices)
		}
	} else {
		logger.Warn("DEFAULT_NIC_ATTACH not set — MachineClass configs must specify nic_attach")
	}

	logger.Info("startup checks passed",
		zap.String("transport", tnClient.TransportName()),
		zap.String("pool", pool),
		zap.String("nic_attach", nicAttach),
	)

	return nil
}

func newHealthCheck(tnClient *truenasclient.Client, pool, nicAttach string) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := tnClient.Ping(ctx); err != nil {
			return fmt.Errorf("TrueNAS API unreachable: %w", err)
		}

		exists, err := tnClient.PoolExists(ctx, pool)
		if err != nil {
			return fmt.Errorf("failed to check pool %q: %w", pool, err)
		}

		if !exists {
			return fmt.Errorf("pool %q not found on TrueNAS", pool)
		}

		if nicAttach != "" {
			valid, nicErr := tnClient.NICAttachValid(ctx, nicAttach)
			if nicErr != nil {
				return fmt.Errorf("failed to validate NIC attach %q: %w", nicAttach, nicErr)
			}

			if !valid {
				return fmt.Errorf("NIC attach target %q not found on TrueNAS", nicAttach)
			}
		}

		return nil
	}
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
