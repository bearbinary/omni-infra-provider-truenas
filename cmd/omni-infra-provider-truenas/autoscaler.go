package main

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/autoscaler"
)

// runAutoscaler is the entry point for the `omni-infra-provider-truenas
// autoscaler` subcommand. Phase 1 scope is deliberately narrow: load
// env-var config, log the experimental banner, and block on ctx so a
// Deployment can run us without crash-looping.
//
// Subsequent phases add:
//   - Phase 2: gRPC server + MachineSet discovery + capacity gate (no writes)
//   - Phase 3: MachineAllocation writes behind a singleton lease
//   - Phase 4: Helm chart under deploy/autoscaler/ and operator docs
//
// Keep the public surface small: returning a plain error lets main.go
// print and exit without the subcommand needing its own os.Exit path.
func runAutoscaler(baseCtx context.Context) error {
	logger, err := newLogger()
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}

	defer func() { _ = logger.Sync() }()

	cfg, err := autoscaler.LoadSubcommandConfig()
	if err != nil {
		return fmt.Errorf("load autoscaler config: %w", err)
	}

	// Single conspicuous banner so operators grepping logs for this
	// feature can confirm the subcommand is live AND that they've
	// opted into experimental behavior. Written once at boot, not per
	// request.
	logger.Info("autoscaler EXPERIMENTAL — see docs/autoscaler.md; this feature may change without semver guarantees",
		zap.String("cluster", cfg.ClusterName),
		zap.String("listen", cfg.ListenAddress),
		zap.Duration("refresh_interval", cfg.RefreshInterval),
		zap.String("version", version),
	)

	// Phase 3a: boot the gRPC server so the sidecar can reach us. The
	// capacity gate wires in alongside the server but every handler
	// still returns codes.Unimplemented — real MachineSet discovery
	// and MachineAllocation writes land in phases 3b and 3d.
	//
	// The TrueNAS client isn't constructed yet in this subcommand
	// (phases 3b+ will build it from the same env vars the provisioner
	// uses). For now we pass a nil gate to NewServer — server.go
	// tolerates that and will panic only if a handler that needs the
	// gate is called, which can't happen while all handlers are
	// Unimplemented.
	server := autoscaler.NewServer(logger, cfg, nil)

	ctx, stop := signal.NotifyContext(baseCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
