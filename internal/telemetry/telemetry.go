// Package telemetry provides OpenTelemetry and Pyroscope initialization.
// All telemetry is opt-in — when OTEL_EXPORTER_OTLP_ENDPOINT is not set,
// no SDK is initialized and there is zero overhead.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/grafana/pyroscope-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds telemetry configuration.
type Config struct {
	OTELEndpoint   string            // OTLP gRPC endpoint (e.g., "otel-collector:4317" or Grafana Cloud OTLP endpoint)
	OTELInsecure   bool              // Disable TLS for OTLP exporter (false for Grafana Cloud)
	OTELHeaders    map[string]string // Extra headers for OTLP exporter (e.g., Authorization for Grafana Cloud)
	OTELProtocol   string            // "grpc" (default) or "http/protobuf"
	OTELConsole    bool              // If true, also emit traces/metrics/logs to stdout (verbose — opt-in for local debugging)
	PyroscopeURL   string            // Pyroscope server URL (e.g., "http://pyroscope:4040" or Grafana Cloud endpoint)
	PyroscopeUser  string            // Basic auth user (Grafana Cloud instance ID)
	PyroscopePass  string            // Basic auth password (Grafana Cloud API token)
	ServiceName    string            // Defaults to "omni-infra-provider-truenas"
	ServiceVersion string            // Injected at build time
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
			return errors.Join(errs...)
		}

		return nil
	}

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
		fns, err := initOTEL(ctx, cfg, res)
		if err != nil {
			return shutdown, err
		}

		shutdownFuncs = append(shutdownFuncs, fns...)
	}

	if cfg.PyroscopeURL != "" {
		fn, err := initPyroscope(cfg)
		if err != nil {
			return shutdown, err
		}

		shutdownFuncs = append(shutdownFuncs, fn)
	}

	return shutdown, nil
}

func initOTEL(ctx context.Context, cfg Config, res *resource.Resource) ([]func(context.Context) error, error) {
	var shutdownFuncs []func(context.Context) error

	traceExporter, metricExporter, logExporter, err := buildOTLPExporters(ctx, cfg)
	if err != nil {
		return nil, err
	}

	traceOpts2 := []sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	}

	if cfg.OTELConsole {
		consoleTraceExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("failed to create console trace exporter: %w", err)
		}

		traceOpts2 = append(traceOpts2, sdktrace.WithBatcher(consoleTraceExporter))
	}

	tp := sdktrace.NewTracerProvider(traceOpts2...)
	otel.SetTracerProvider(tp)
	shutdownFuncs = append(shutdownFuncs, tp.Shutdown)

	metricProviderOpts := []sdkmetric.Option{
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(15*time.Second))),
		sdkmetric.WithResource(res),
	}

	if cfg.OTELConsole {
		consoleMetricExporter, err := stdoutmetric.New()
		if err != nil {
			return nil, fmt.Errorf("failed to create console metric exporter: %w", err)
		}

		metricProviderOpts = append(metricProviderOpts,
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(consoleMetricExporter, sdkmetric.WithInterval(60*time.Second))),
		)
	}

	mp := sdkmetric.NewMeterProvider(metricProviderOpts...)
	otel.SetMeterProvider(mp)
	shutdownFuncs = append(shutdownFuncs, mp.Shutdown)

	logProviderOpts := []sdklog.LoggerProviderOption{
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	}

	if cfg.OTELConsole {
		consoleLogExporter, err := stdoutlog.New(stdoutlog.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("failed to create console log exporter: %w", err)
		}

		logProviderOpts = append(logProviderOpts, sdklog.WithProcessor(sdklog.NewBatchProcessor(consoleLogExporter)))
	}

	lp := sdklog.NewLoggerProvider(logProviderOpts...)
	global.SetLoggerProvider(lp)
	shutdownFuncs = append(shutdownFuncs, lp.Shutdown)

	initMetrics()

	return shutdownFuncs, nil
}

// buildOTLPExporters creates trace/metric/log exporters for either gRPC or HTTP
// protocol based on cfg.OTELProtocol. Empty or "grpc" selects gRPC; "http/protobuf"
// (also "http") selects HTTP. HTTP accepts a full URL in OTELEndpoint — required
// for Grafana Cloud OTLP which serves on https://<host>/otlp.
func buildOTLPExporters(ctx context.Context, cfg Config) (sdktrace.SpanExporter, sdkmetric.Exporter, sdklog.Exporter, error) {
	switch cfg.OTELProtocol {
	case "", "grpc":
		return buildGRPCExporters(ctx, cfg)
	case "http/protobuf", "http":
		return buildHTTPExporters(ctx, cfg)
	default:
		return nil, nil, nil, fmt.Errorf("unsupported OTEL_EXPORTER_OTLP_PROTOCOL %q (want \"grpc\" or \"http/protobuf\")", cfg.OTELProtocol)
	}
}

func buildGRPCExporters(ctx context.Context, cfg Config) (sdktrace.SpanExporter, sdkmetric.Exporter, sdklog.Exporter, error) {
	traceOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTELEndpoint)}
	metricOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.OTELEndpoint)}
	logOpts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(cfg.OTELEndpoint)}

	if cfg.OTELInsecure {
		traceOpts = append(traceOpts, otlptracegrpc.WithInsecure())
		metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
		logOpts = append(logOpts, otlploggrpc.WithInsecure())
	}

	if len(cfg.OTELHeaders) > 0 {
		traceOpts = append(traceOpts, otlptracegrpc.WithHeaders(cfg.OTELHeaders))
		metricOpts = append(metricOpts, otlpmetricgrpc.WithHeaders(cfg.OTELHeaders))
		logOpts = append(logOpts, otlploggrpc.WithHeaders(cfg.OTELHeaders))
	}

	traceExp, err := otlptracegrpc.New(ctx, traceOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create grpc trace exporter: %w", err)
	}

	metricExp, err := otlpmetricgrpc.New(ctx, metricOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create grpc metric exporter: %w", err)
	}

	logExp, err := otlploggrpc.New(ctx, logOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create grpc log exporter: %w", err)
	}

	return traceExp, metricExp, logExp, nil
}

func buildHTTPExporters(ctx context.Context, cfg Config) (sdktrace.SpanExporter, sdkmetric.Exporter, sdklog.Exporter, error) {
	// WithEndpointURL accepts a full URL (scheme + host + path). The exporter
	// appends /v1/traces, /v1/metrics, /v1/logs to the path — Grafana Cloud's
	// /otlp base path becomes /otlp/v1/traces etc., which is correct.
	traceOpts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(cfg.OTELEndpoint)}
	metricOpts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(cfg.OTELEndpoint)}
	logOpts := []otlploghttp.Option{otlploghttp.WithEndpointURL(cfg.OTELEndpoint)}

	if cfg.OTELInsecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
		logOpts = append(logOpts, otlploghttp.WithInsecure())
	}

	if len(cfg.OTELHeaders) > 0 {
		traceOpts = append(traceOpts, otlptracehttp.WithHeaders(cfg.OTELHeaders))
		metricOpts = append(metricOpts, otlpmetrichttp.WithHeaders(cfg.OTELHeaders))
		logOpts = append(logOpts, otlploghttp.WithHeaders(cfg.OTELHeaders))
	}

	traceExp, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create http trace exporter: %w", err)
	}

	metricExp, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create http metric exporter: %w", err)
	}

	logExp, err := otlploghttp.New(ctx, logOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create http log exporter: %w", err)
	}

	return traceExp, metricExp, logExp, nil
}

func initPyroscope(cfg Config) (func(context.Context) error, error) {
	profiler, err := pyroscope.Start(pyroscope.Config{
		ApplicationName:   cfg.ServiceName,
		ServerAddress:     cfg.PyroscopeURL,
		BasicAuthUser:     cfg.PyroscopeUser,
		BasicAuthPassword: cfg.PyroscopePass,
		Tags:              map[string]string{"version": cfg.ServiceVersion},
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
		return nil, fmt.Errorf("failed to start pyroscope: %w", err)
	}

	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(5)

	return func(_ context.Context) error { return profiler.Stop() }, nil
}
