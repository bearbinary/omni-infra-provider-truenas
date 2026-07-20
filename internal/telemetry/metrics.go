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

// WithPatchKind returns a metric option with the patch_kind attribute,
// used to disambiguate `truenas.config_patch.duration` records across the
// five ConfigPatchRequest kinds the provider emits (data-volumes,
// longhorn-ops, nic-mtu, nic-interfaces, advertised-subnets).
func WithPatchKind(kind string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("patch_kind", kind))
}

// Error category strings used as label values on truenas.provision.errors.
// Centralizing these as constants keeps the categorizer (provisioner.categorizeError),
// the dashboards/alerts that filter by these strings, and any test that pins
// a category from drifting independently. Renaming a category here is now a
// single edit; before, renaming required grep across Go code + Grafana JSON +
// rules.yml.
const (
	ErrCategoryUnknown       = "unknown"
	ErrCategoryPoolNotFound  = "pool_not_found"
	ErrCategoryPoolFull      = "pool_full"
	ErrCategoryConfigInvalid = "config_invalid"
	ErrCategoryConfigPatch   = "config_patch"
	ErrCategoryNICInvalid    = "nic_invalid"
	ErrCategoryConnection    = "connection"
	ErrCategoryAuth          = "auth"
	ErrCategoryTimeout       = "timeout"
	ErrCategoryHostOOM       = "host_oom"
	ErrCategoryMemory        = "memory"
	ErrCategoryImage         = "image"
	// ErrCategoryTOFURead covers the TOFU baseline / cached-ISO stat
	// failure modes added with the cache-hit re-verification work. These
	// errors used to fall through to ErrCategoryImage via a substring
	// match on "ISO", which lumped them with Talos-factory download
	// failures and made a degrading TrueNAS property RPC indistinguishable
	// from a flaky Image Factory on the dashboard.
	ErrCategoryTOFURead = "tofu_read"
)

// Pre-defined metric instruments for the provider.
var (
	VMsProvisioned   metric.Int64Counter
	VMsDeprovisioned metric.Int64Counter
	VMsErrored       metric.Int64Counter
	VMsAutoReplaced  metric.Int64Counter
	VMsOOMPermanent  metric.Int64Counter
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
	ConfigPatchDuration metric.Float64Histogram

	// Connection & resilience
	WSReconnects       metric.Int64Counter
	RateLimitQueueSize metric.Int64Gauge

	// WSCloseOutcome tracks the exit path of every wsTransport.Close call:
	// clean (wg.Wait drained before deadline + underlying Close returned),
	// inflight_timeout (wg.Wait hit the closeTimeout — Call/UploadFile were
	// still in-flight when Close's grace budget expired), close_timeout
	// (underlying conn.Close hit the closeTimeout — TCP was half-open or
	// otherwise wedged), already_closed (second-or-later Close on the same
	// transport — no-op short-circuit).
	//
	// Before this counter existed, the WaitGroup-reuse panic that motivated
	// the concurrency-model fix was not observable in telemetry — there was
	// no way to see from Grafana that Close was even being called during a
	// panic loop.
	WSCloseOutcome metric.Int64Counter

	// WSCallRejected counts Call/UploadFile rejections at the
	// closed-transport gate. Label `reason=closed` today; leaves room for
	// future gates (e.g. reason=not_authed) without a breaking rename.
	// A sustained non-zero rate here means callers are still driving the
	// transport after shutdown — often a shutdown-order bug in the caller.
	WSCallRejected metric.Int64Counter

	// WSGoroutinePanics counts recovered panics in the three long-lived
	// wsTransport goroutines (label `site=close_wait|read_loop|close_conn`).
	// The recover site re-panics after logging + incrementing this counter
	// so the process still crash-loops — but the next same-class regression
	// is now diagnosable from Grafana instead of a raw pod log.
	WSGoroutinePanics metric.Int64Counter

	// Cleanup
	CleanupISOsRemoved metric.Int64Counter
	CleanupOrphanVMs   metric.Int64Counter
	CleanupOrphanZvols metric.Int64Counter

	// Provision steps (individual durations)
	StepDuration metric.Float64Histogram

	// Error categorization
	ProvisionErrors metric.Int64Counter

	// PreflightDegraded counts how many times the createVM pre-flight had to
	// fall back to the single-VM ceiling because RunningGuestsMemoryMiB
	// failed. Each increment means the free-RAM safety net is silently off
	// for that provision. A sustained non-zero rate is the leading indicator
	// for the original ENOMEM-loop bug to reappear (pre-flight passes by
	// only checking total RAM, vm.start then ENOMEMs).
	PreflightDegraded metric.Int64Counter

	// ISO cache
	ISOCacheHits   metric.Int64Counter
	ISOCacheMisses metric.Int64Counter

	// File upload
	ISOUploadBytes metric.Int64Counter

	// Supply-chain: TOFU hash mismatches on Talos ISO download. A non-zero
	// value indicates either a factory.talos.dev compromise, a MITM on the
	// download path, or a legitimate image re-publish under the same URL —
	// all cases require operator attention. Labelled by detection_path
	// ("download" vs "cache_hit_metadata") so the runbook for "factory
	// rotated under the same URL" stays separate from "bytes were tampered
	// at rest on TrueNAS after the baseline was recorded".
	ISOHashMismatches metric.Int64Counter

	// Supply-chain: TOFU POISON marker writes that exhausted retries. A
	// non-zero value means a confirmed-bad ISO is still on disk with the
	// trusted baseline in place — a future cache hit will silently reuse
	// the bad bytes. Manual cleanup is required; the corresponding log
	// line carries the path. Critical-severity alert.
	ISOPoisonMarkerWriteFailed metric.Int64Counter

	// Supply-chain: per-attempt counter for failed POISON marker writes.
	// Distinct from ISOPoisonMarkerWriteFailed (which fires once per
	// exhaustion) — this lets a degrading TrueNAS property RPC show up
	// as a leading indicator before retries exhaust.
	ISOPoisonMarkerRetries metric.Int64Counter

	// Supply-chain: TOFU companion property (size, mtime, hash) writes
	// that failed at upload-time. Each failure silently degrades the next
	// cache-hit verification to "first use" for the missing field.
	// Labelled by property={size|mtime|hash}.
	ISOTOFUMetadataWriteFailed metric.Int64Counter

	// Health check
	HealthCheckErrors metric.Int64Counter

	// Graceful shutdown outcomes
	GracefulShutdownSuccess metric.Int64Counter
	GracefulShutdownTimeout metric.Int64Counter

	// Deprovision step durations
	DeprovisionStepDuration metric.Float64Histogram

	// Singleton lease
	SingletonLeaseHeld     metric.Int64Gauge
	SingletonRefreshErrors metric.Int64Counter
	SingletonTakeovers     metric.Int64Counter

	// Storage disks
	AdditionalDisksTotal metric.Int64Gauge
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
	VMsOOMPermanent, _ = meter.Int64Counter("truenas.vms.oom_permanent",
		metric.WithDescription("VMs that hit MaxStartOOMAttempts and returned a permanent error — terminal signal that host RAM exhausted the OOM retry budget"),
	)
	ZvolsResized, _ = meter.Int64Counter("truenas.zvols.resized",
		metric.WithDescription("Total zvols resized"),
	)
	ProvisionDuration, _ = meter.Float64Histogram("truenas.provision.duration",
		metric.WithDescription("Duration of full VM provision in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(5, 10, 30, 60, 120, 300, 600, 900, 1800, 3600),
	)
	DeprovisionDuration, _ = meter.Float64Histogram("truenas.deprovision.duration",
		metric.WithDescription("Duration of full VM deprovision in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 30, 60, 120, 300, 600),
	)
	APICallDuration, _ = meter.Float64Histogram("truenas.api.duration",
		metric.WithDescription("Duration of TrueNAS API calls in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)
	// ConfigPatchDuration tracks the per-RPC latency of pctx.CreateConfigPatch.
	// Labeled by patch_kind so operators can isolate slow/failing patch types
	// ("nic-interfaces is slow today" vs "Omni-side is slow for everything")
	// without spelunking step-duration logs. Bucket boundaries mirror
	// APICallDuration since both surface RPC-level latency.
	ConfigPatchDuration, _ = meter.Float64Histogram("truenas.config_patch.duration",
		metric.WithDescription("Duration of pctx.CreateConfigPatch calls by patch_kind"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	ISODownloadDuration, _ = meter.Float64Histogram("truenas.iso.download.duration",
		metric.WithDescription("Duration of ISO download in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 30, 60, 120, 300, 600, 900),
	)
	ISOHashMismatches, _ = meter.Int64Counter("truenas.iso.hash_mismatches",
		metric.WithDescription("Total ISO TOFU mismatches by detection_path — indicates possible supply-chain compromise"),
	)
	ISOPoisonMarkerWriteFailed, _ = meter.Int64Counter("truenas.iso.poison_marker_write_failed",
		metric.WithDescription("POISON marker writes that exhausted retries — confirmed-bad ISO remains accessible until manual cleanup"),
	)
	ISOPoisonMarkerRetries, _ = meter.Int64Counter("truenas.iso.poison_marker_retries",
		metric.WithDescription("Per-attempt counter for failed POISON marker writes — leading indicator for degrading TrueNAS property RPC"),
	)
	ISOTOFUMetadataWriteFailed, _ = meter.Int64Counter("truenas.iso.tofu_metadata_write_failed",
		metric.WithDescription("TOFU companion property writes (hash/size/mtime) that failed at upload-time, labelled by property"),
	)

	// Connection & resilience
	WSReconnects, _ = meter.Int64Counter("truenas.ws.reconnects",
		metric.WithDescription("Total WebSocket reconnection attempts"),
	)
	WSCloseOutcome, _ = meter.Int64Counter("truenas.ws.close_outcome",
		metric.WithDescription("wsTransport.Close outcomes by exit path (clean, inflight_timeout, close_timeout, already_closed)"),
	)
	WSCallRejected, _ = meter.Int64Counter("truenas.ws.call_rejected",
		metric.WithDescription("Call/UploadFile rejected at the transport gate, labelled by reason (closed)"),
	)
	WSGoroutinePanics, _ = meter.Int64Counter("truenas.ws.goroutine_panics",
		metric.WithDescription("Recovered panics in wsTransport goroutines, labelled by site (close_wait, read_loop, close_conn)"),
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
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300),
	)

	// Error categorization
	ProvisionErrors, _ = meter.Int64Counter("truenas.provision.errors",
		metric.WithDescription("Provision errors by category"),
	)
	PreflightDegraded, _ = meter.Int64Counter("truenas.preflight.degraded",
		metric.WithDescription("Pre-flight memory check fell back to single-VM ceiling because the running-guest aggregate query failed — free-RAM safety net is silently off"),
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
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120),
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

	// Singleton lease
	SingletonLeaseHeld, _ = meter.Int64Gauge("truenas.singleton.lease_held",
		metric.WithDescription("Whether this instance holds the singleton lease (1=held, 0=not held)"),
	)
	SingletonRefreshErrors, _ = meter.Int64Counter("truenas.singleton.refresh_errors",
		metric.WithDescription("Total singleton lease refresh failures"),
	)
	SingletonTakeovers, _ = meter.Int64Counter("truenas.singleton.takeovers",
		metric.WithDescription("Total singleton lease takeovers (stale lease acquired from another instance)"),
	)

	// Storage disks
	AdditionalDisksTotal, _ = meter.Int64Gauge("truenas.vms.additional_disks",
		metric.WithDescription("Total additional data disks across all provisioned VMs"),
	)
}
