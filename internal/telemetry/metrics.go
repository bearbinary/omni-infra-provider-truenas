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

// Pre-defined metric instruments for the provider.
var (
	VMsProvisioned      metric.Int64Counter
	VMsDeprovisioned    metric.Int64Counter
	VMsErrored          metric.Int64Counter
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
}
