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

// WithPool returns a metric option with the pool attribute.
func WithPool(pool string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("pool", pool))
}

// Pre-defined metric instruments for the provider.
var (
	VMsProvisioned      metric.Int64Counter
	VMsDeprovisioned    metric.Int64Counter
	VMsErrored          metric.Int64Counter
	ZvolsResized        metric.Int64Counter
	SnapshotsCreated    metric.Int64Counter
	SnapshotsRolledBack metric.Int64Counter

	// Host health gauges
	HostCPUCores        metric.Int64Gauge
	HostMemoryTotal     metric.Int64Gauge
	HostPoolFreeBytes   metric.Int64Gauge
	HostPoolUsedBytes   metric.Int64Gauge
	HostPoolHealthy     metric.Int64Gauge
	HostDisksTotal      metric.Int64Gauge
	HostVMsRunning      metric.Int64Gauge
	ProvisionDuration   metric.Float64Histogram
	DeprovisionDuration metric.Float64Histogram
	APICallDuration     metric.Float64Histogram
	ISODownloadDuration metric.Float64Histogram
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
	ZvolsResized, _ = meter.Int64Counter("truenas.zvols.resized",
		metric.WithDescription("Total zvols resized"),
	)
	SnapshotsCreated, _ = meter.Int64Counter("truenas.snapshots.created",
		metric.WithDescription("Total ZFS snapshots created"),
	)
	SnapshotsRolledBack, _ = meter.Int64Counter("truenas.snapshots.rolled_back",
		metric.WithDescription("Total ZFS snapshot rollbacks"),
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
