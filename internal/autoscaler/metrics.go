package autoscaler

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics is the autoscaler's OTel instrument set. Lazily initialized
// on first use so tests that don't need metrics don't pay for them —
// the subcommand calls InitMetrics() once at boot, but unit tests can
// exercise the server handlers without an OTel provider configured.
//
// Kept as package-level globals for the same reason internal/telemetry
// uses globals: the instrument surface is stable, there are no
// configuration knobs, and threading a Metrics struct through every
// handler would clutter signatures for no readability win.
//
// Naming follows the `truenas_autoscaler_*` prefix so a Prometheus
// dashboard can select all autoscaler metrics with a single regex.
// Seconds-unit histograms use `_seconds` suffix by convention — the
// provider-wide histogram bucket smoke test (internal/telemetry/histogram_buckets_test.go)
// catches unit/bucket mismatches before they ship.
var (
	initOnce sync.Once

	// ScaleUpRequests counts NodeGroupIncreaseSize RPCs by outcome.
	// Labels:
	//   - result: "succeeded" | "denied_capacity" | "rejected_bounds" |
	//             "rejected_invalid" | "rejected_not_found" |
	//             "errored_internal"
	// Intentionally mirrors the gRPC status codes the server returns
	// so an operator can correlate metric labels with log lines.
	ScaleUpRequests metric.Int64Counter

	// CapacityDenials counts hard-gate rejections separately from the
	// generic ScaleUpRequests counter. Labels:
	//   - reason: "pool_free_low" | "host_mem_low" | "query_failed"
	// Lets dashboards graph "capacity pressure" independently of
	// "bad requests" — the former is an infrastructure signal, the
	// latter is a Cluster Autoscaler mis-config.
	CapacityDenials metric.Int64Counter

	// MachineSetRefreshDuration times each Discoverer.Discover call.
	// Useful for spotting Omni API slowness — a sudden p99 spike in
	// this histogram typically precedes an autoscaler that's too slow
	// to respond to pending pods.
	MachineSetRefreshDuration metric.Float64Histogram
)

// InitMetrics builds the instrument set against the global
// MeterProvider. Safe to call multiple times (sync.Once guard); safe
// to skip entirely in tests (handlers nil-check before recording).
func InitMetrics() {
	initOnce.Do(func() {
		meter := otel.Meter("omni-infra-provider-truenas-autoscaler")

		ScaleUpRequests, _ = meter.Int64Counter("truenas.autoscaler.scaleup.requests",
			metric.WithDescription("NodeGroupIncreaseSize RPC calls by outcome"),
		)

		CapacityDenials, _ = meter.Int64Counter("truenas.autoscaler.capacity.denials",
			metric.WithDescription("Hard-gate capacity denials by reason"),
		)

		MachineSetRefreshDuration, _ = meter.Float64Histogram("truenas.autoscaler.machineset.refresh.duration",
			metric.WithDescription("Duration of Discoverer.Discover calls in seconds"),
			metric.WithUnit("s"),
			// Boundaries match the shape of a typical Omni state list
			// (expect < 1s in steady state; a slow API pushes toward
			// 10s before CAS starts re-dialing). Same bucket family
			// as the provisioner's API histogram so dashboards can
			// reuse the existing bucket-aware panels.
			metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
		)
	})
}

// result/reason label helpers — kept as typed functions so a future
// rename of a label value surfaces as a compile error instead of a
// silently-renamed metric that breaks dashboards.
var (
	resultKey = attribute.Key("result")
	reasonKey = attribute.Key("reason")
)

// ResultLabel values exposed as typed helpers. Prometheus labels are
// strings; keeping the label values in one place makes the metric
// contract searchable by grep.
const (
	ResultSucceeded        = "succeeded"
	ResultDeniedCapacity   = "denied_capacity"
	ResultRejectedBounds   = "rejected_bounds"
	ResultRejectedInvalid  = "rejected_invalid"
	ResultRejectedNotFound = "rejected_not_found"
	ResultErroredInternal  = "errored_internal"
)

// DenialReason values.
const (
	ReasonPoolFreeLow = "pool_free_low"
	ReasonHostMemLow  = "host_mem_low"
	ReasonQueryFailed = "query_failed"
)

// recordScaleUpResult increments ScaleUpRequests with the supplied
// result label. Nil-safe — skips if InitMetrics hasn't been called
// (i.e., in unit tests).
func recordScaleUpResult(ctx context.Context, result string) {
	if ScaleUpRequests == nil {
		return
	}

	ScaleUpRequests.Add(ctx, 1, metric.WithAttributes(resultKey.String(result)))
}

// recordCapacityDenial increments CapacityDenials with the supplied
// reason label.
func recordCapacityDenial(ctx context.Context, reason string) {
	if CapacityDenials == nil {
		return
	}

	CapacityDenials.Add(ctx, 1, metric.WithAttributes(reasonKey.String(reason)))
}

// recordRefreshDuration records a Discoverer.Discover call latency
// in seconds.
func recordRefreshDuration(ctx context.Context, seconds float64) {
	if MachineSetRefreshDuration == nil {
		return
	}

	MachineSetRefreshDuration.Record(ctx, seconds)
}

// categorizeDenialReason maps a free-form decision reason string from
// CheckCapacity to one of the exported DenialReason constants.
// Substring-based so the parser isn't coupled to the exact phrasing
// the gate's reason builder uses — if the gate rephrases "pool %q
// free %d GiB" → "pool %q low (%d GiB)", this still routes to the
// right metric label as long as the word "pool" appears.
//
// Falls through to ReasonPoolFreeLow as a conservative default so a
// change in reason text never silently drops samples.
func categorizeDenialReason(reason string) string {
	switch {
	case contains(reason, "host"):
		return ReasonHostMemLow
	case contains(reason, "query failed"):
		return ReasonQueryFailed
	default:
		return ReasonPoolFreeLow
	}
}

// contains is a case-insensitive substring check used by
// categorizeDenialReason. Kept private to this file because it's a
// one-off — the stdlib strings.Contains is case-sensitive, which
// bites when a future gate rephrasing capitalizes "Host" or
// "Query".
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}

	if len(haystack) < len(needle) {
		return false
	}

	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true

		for j := 0; j < len(needle); j++ {
			h := haystack[i+j]
			n := needle[j]

			if h >= 'A' && h <= 'Z' {
				h += 'a' - 'A'
			}

			if n >= 'A' && n <= 'Z' {
				n += 'a' - 'A'
			}

			if h != n {
				match = false

				break
			}
		}

		if match {
			return true
		}
	}

	return false
}
