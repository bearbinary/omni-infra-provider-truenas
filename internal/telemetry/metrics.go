package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// WithMethod returns a metric option with the method attribute.
func WithMethod(method string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("method", method))
}

// WithStep returns a metric option with the step attribute.
func WithStep(step string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("step", step))
}

// WithErrorCategory returns a metric option with the error category attribute.
func WithErrorCategory(category string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("error_category", category))
}

// WithPool returns a metric option with the pool attribute.
func WithPool(pool string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("pool", pool))
}

// Pre-defined metric instruments for the provider.
var (
	VMsProvisioned   metric.Int64Counter
	VMsDeprovisioned metric.Int64Counter
	VMsErrored       metric.Int64Counter
	VMsAutoReplaced  metric.Int64Counter
	ZvolsResized     metric.Int64Counter

	// Host health gauges
	HostCPUCores      metric.Int64Gauge
	HostMemoryTotal   metric.Int64Gauge
	HostPoolFreeBytes metric.Int64Gauge
	HostPoolUsedBytes metric.Int64Gauge
	HostPoolHealthy   metric.Int64Gauge
	HostDisksTotal    metric.Int64Gauge
	HostVMsRunning    metric.Int64Gauge

	// Duration histograms
	ProvisionDuration   metric.Float64Histogram
	DeprovisionDuration metric.Float64Histogram
	APICallDuration     metric.Float64Histogram
	ISODownloadDuration metric.Float64Histogram

	// Connection & resilience
	WSReconnects       metric.Int64Counter
	RateLimitQueueSize metric.Int64Gauge

	// Cleanup
	CleanupISOsRemoved metric.Int64Counter
	CleanupOrphanVMs   metric.Int64Counter
	CleanupOrphanZvols metric.Int64Counter

	// Provision steps (individual durations)
	StepDuration metric.Float64Histogram

	// Error categorization
	ProvisionErrors metric.Int64Counter

	// ISO cache
	ISOCacheHits   metric.Int64Counter
	ISOCacheMisses metric.Int64Counter

	// File upload
	ISOUploadBytes metric.Int64Counter

	// Health check
	HealthCheckErrors metric.Int64Counter

	// Graceful shutdown outcomes
	GracefulShutdownSuccess metric.Int64Counter
	GracefulShutdownTimeout metric.Int64Counter

	// Deprovision step durations
	DeprovisionStepDuration metric.Float64Histogram
)

func initMetrics() {
	meter := otel.Meter("omni-infra-provider-truenas")

	VMsProvisioned, _ = meter.Int64Counter("truenas.vms.provisioned",
		metric.WithDescription("Total VMs successfully provisioned"),
	)
	VMsDeprovisioned, _ = meter.Int64Counter("truenas.vms.deprovisioned",
		metric.WithDescription("Total VMs successfully deprovisioned"),
	)
	VMsErrored, _ = meter.Int64Counter("truenas.vms.errored",
		metric.WithDescription("Total VM provision/deprovision errors"),
	)
	VMsAutoReplaced, _ = meter.Int64Counter("truenas.vms.auto_replaced",
		metric.WithDescription("Total VMs auto-deprovisioned by circuit breaker after exceeding max error recoveries"),
	)
	ZvolsResized, _ = meter.Int64Counter("truenas.zvols.resized",
		metric.WithDescription("Total zvols resized"),
	)
	ProvisionDuration, _ = meter.Float64Histogram("truenas.provision.duration",
		metric.WithDescription("Duration of full VM provision in seconds"),
		metric.WithUnit("s"),
	)
	DeprovisionDuration, _ = meter.Float64Histogram("truenas.deprovision.duration",
		metric.WithDescription("Duration of full VM deprovision in seconds"),
		metric.WithUnit("s"),
	)
	APICallDuration, _ = meter.Float64Histogram("truenas.api.duration",
		metric.WithDescription("Duration of TrueNAS API calls in seconds"),
		metric.WithUnit("s"),
	)
	ISODownloadDuration, _ = meter.Float64Histogram("truenas.iso.download.duration",
		metric.WithDescription("Duration of ISO download in seconds"),
		metric.WithUnit("s"),
	)

	// Connection & resilience
	WSReconnects, _ = meter.Int64Counter("truenas.ws.reconnects",
		metric.WithDescription("Total WebSocket reconnection attempts"),
	)
	RateLimitQueueSize, _ = meter.Int64Gauge("truenas.ratelimit.queue_size",
		metric.WithDescription("Current number of API calls waiting for a rate limit slot"),
	)

	// Cleanup
	CleanupISOsRemoved, _ = meter.Int64Counter("truenas.cleanup.isos_removed",
		metric.WithDescription("Total stale ISOs removed by cleanup"),
	)
	CleanupOrphanVMs, _ = meter.Int64Counter("truenas.cleanup.orphan_vms",
		metric.WithDescription("Total orphan VMs removed by cleanup"),
	)
	CleanupOrphanZvols, _ = meter.Int64Counter("truenas.cleanup.orphan_zvols",
		metric.WithDescription("Total orphan zvols removed by cleanup"),
	)

	// Per-step duration
	StepDuration, _ = meter.Float64Histogram("truenas.provision.step.duration",
		metric.WithDescription("Duration of individual provision steps"),
		metric.WithUnit("s"),
	)

	// Error categorization
	ProvisionErrors, _ = meter.Int64Counter("truenas.provision.errors",
		metric.WithDescription("Provision errors by category"),
	)

	// ISO cache
	ISOCacheHits, _ = meter.Int64Counter("truenas.iso.cache.hits",
		metric.WithDescription("ISO cache hits (download skipped)"),
	)
	ISOCacheMisses, _ = meter.Int64Counter("truenas.iso.cache.misses",
		metric.WithDescription("ISO cache misses (download required)"),
	)

	// File upload
	ISOUploadBytes, _ = meter.Int64Counter("truenas.iso.upload.bytes",
		metric.WithDescription("Total bytes uploaded to TrueNAS (ISOs)"),
		metric.WithUnit("By"),
	)

	// Health check
	HealthCheckErrors, _ = meter.Int64Counter("truenas.healthcheck.errors",
		metric.WithDescription("Total health check failures"),
	)

	// Graceful shutdown outcomes
	GracefulShutdownSuccess, _ = meter.Int64Counter("truenas.shutdown.graceful",
		metric.WithDescription("VMs that shut down gracefully via ACPI"),
	)
	GracefulShutdownTimeout, _ = meter.Int64Counter("truenas.shutdown.forced",
		metric.WithDescription("VMs that required force stop after graceful timeout"),
	)

	// Deprovision step durations
	DeprovisionStepDuration, _ = meter.Float64Histogram("truenas.deprovision.step.duration",
		metric.WithDescription("Duration of individual deprovision steps"),
		metric.WithUnit("s"),
	)

	// Host health gauges
	HostCPUCores, _ = meter.Int64Gauge("truenas.host.cpu_cores",
		metric.WithDescription("Number of CPU cores on TrueNAS host"),
	)
	HostMemoryTotal, _ = meter.Int64Gauge("truenas.host.memory_total_bytes",
		metric.WithDescription("Total physical memory on TrueNAS host"),
		metric.WithUnit("By"),
	)
	HostPoolFreeBytes, _ = meter.Int64Gauge("truenas.host.pool_free_bytes",
		metric.WithDescription("Free space per ZFS pool"),
		metric.WithUnit("By"),
	)
	HostPoolUsedBytes, _ = meter.Int64Gauge("truenas.host.pool_used_bytes",
		metric.WithDescription("Used space per ZFS pool"),
		metric.WithUnit("By"),
	)
	HostPoolHealthy, _ = meter.Int64Gauge("truenas.host.pool_healthy",
		metric.WithDescription("Pool health (1=healthy, 0=degraded/faulted)"),
	)
	HostDisksTotal, _ = meter.Int64Gauge("truenas.host.disks_total",
		metric.WithDescription("Total number of disks"),
	)
	HostVMsRunning, _ = meter.Int64Gauge("truenas.host.vms_running",
		metric.WithDescription("Number of running VMs"),
	)
}
