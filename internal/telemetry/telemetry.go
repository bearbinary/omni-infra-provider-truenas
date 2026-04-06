// Package telemetry provides OpenTelemetry and Pyroscope initialization.
// All telemetry is opt-in — when OTEL_EXPORTER_OTLP_ENDPOINT is not set,
// no SDK is initialized and there is zero overhead.
package telemetry

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/grafana/pyroscope-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds telemetry configuration.
type Config struct {
	OTELEndpoint   string // OTLP gRPC endpoint (e.g., "otel-collector:4317")
	OTELInsecure   bool   // Disable TLS for OTLP exporter
	PyroscopeURL   string // Pyroscope server URL (e.g., "http://pyroscope:4040")
	ServiceName    string // Defaults to "omni-infra-provider-truenas"
	ServiceVersion string // Injected at build time
}

// Init initializes OpenTelemetry and Pyroscope. Returns a shutdown function
// that must be called on graceful exit to flush pending telemetry.
// If OTELEndpoint is empty, returns a noop shutdown (no SDK initialized).
func Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "omni-infra-provider-truenas"
	}

	var shutdownFuncs []func(context.Context) error

	shutdown = func(ctx context.Context) error {
		var errs []error
		for _, fn := range shutdownFuncs {
			if err := fn(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("telemetry shutdown errors: %v", errs)
		}
		return nil
	}

	// If no OTEL endpoint, skip SDK initialization entirely
	if cfg.OTELEndpoint == "" && cfg.PyroscopeURL == "" {
		return shutdown, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return shutdown, fmt.Errorf("failed to create resource: %w", err)
	}

	if cfg.OTELEndpoint != "" {
		// Trace provider
		traceOpts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.OTELEndpoint),
		}
		if cfg.OTELInsecure {
			traceOpts = append(traceOpts, otlptracegrpc.WithInsecure())
		}

		traceExporter, err := otlptracegrpc.New(ctx, traceOpts...)
		if err != nil {
			return shutdown, fmt.Errorf("failed to create trace exporter: %w", err)
		}

		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExporter),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(tp)
		shutdownFuncs = append(shutdownFuncs, tp.Shutdown)

		// Metric provider
		metricOpts := []otlpmetricgrpc.Option{
			otlpmetricgrpc.WithEndpoint(cfg.OTELEndpoint),
		}
		if cfg.OTELInsecure {
			metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
		}

		metricExporter, err := otlpmetricgrpc.New(ctx, metricOpts...)
		if err != nil {
			return shutdown, fmt.Errorf("failed to create metric exporter: %w", err)
		}

		mp := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(15*time.Second))),
			sdkmetric.WithResource(res),
		)
		otel.SetMeterProvider(mp)
		shutdownFuncs = append(shutdownFuncs, mp.Shutdown)

		// Log provider
		logOpts := []otlploggrpc.Option{
			otlploggrpc.WithEndpoint(cfg.OTELEndpoint),
		}
		if cfg.OTELInsecure {
			logOpts = append(logOpts, otlploggrpc.WithInsecure())
		}

		logExporter, err := otlploggrpc.New(ctx, logOpts...)
		if err != nil {
			return shutdown, fmt.Errorf("failed to create log exporter: %w", err)
		}

		lp := sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
			sdklog.WithResource(res),
		)
		global.SetLoggerProvider(lp)
		shutdownFuncs = append(shutdownFuncs, lp.Shutdown)

		initMetrics()
	}

	// Pyroscope
	if cfg.PyroscopeURL != "" {
		profiler, err := pyroscope.Start(pyroscope.Config{
			ApplicationName: cfg.ServiceName,
			ServerAddress:   cfg.PyroscopeURL,
			Tags:            map[string]string{"version": cfg.ServiceVersion},
			ProfileTypes: []pyroscope.ProfileType{
				pyroscope.ProfileCPU,
				pyroscope.ProfileAllocObjects,
				pyroscope.ProfileAllocSpace,
				pyroscope.ProfileInuseObjects,
				pyroscope.ProfileInuseSpace,
				pyroscope.ProfileGoroutines,
			},
		})
		if err != nil {
			return shutdown, fmt.Errorf("failed to start pyroscope: %w", err)
		}

		shutdownFuncs = append(shutdownFuncs, func(_ context.Context) error {
			return profiler.Stop()
		})

		// Enable mutex and block profiling for richer flame graphs
		runtime.SetMutexProfileFraction(5)
		runtime.SetBlockProfileRate(5)
	}

	return shutdown, nil
}
