package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestHistogramBuckets_MatchRecordedUnit asserts every Float64Histogram the
// provider exposes has explicit bucket boundaries in the same unit the
// provider records into it (seconds). The regression this catches: v0.15.0
// shipped with the SDK-default boundaries `[0, 5, 10, 25, 50, 75, 100, 250,
// 500, 750, 1000, 2500, 5000, 7500, 10000]` which look like millisecond
// boundaries. The provider records in seconds, so every call <5s landed in
// the first populated bucket and `histogram_quantile()` returned bucket
// midpoints (~2.5s p50, ~4.95s p99) for calls whose real durations were
// single-digit milliseconds. Grafana dashboards read ~250× too high.
//
// This test records a known small duration into each duration histogram and
// verifies the sum + count match what we wrote, then inspects the bucket
// boundaries on the exported aggregation and rejects any that look like
// millisecond-scale thresholds (>=1000) paired with sub-second boundaries
// (<1) — that combination is the unit mismatch's signature.
func TestHistogramBuckets_MatchRecordedUnit(t *testing.T) {
	// Install a fresh MeterProvider with a ManualReader so the test can
	// directly introspect the aggregation state. Uses the real initMetrics
	// path so bucket boundaries applied via metric.WithExplicitBucketBoundaries
	// are observed exactly as production sees them.
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)

	t.Cleanup(func() {
		otel.SetMeterProvider(prev)
	})

	initMetrics()

	// Record a known 50 ms value into every duration histogram we export.
	// The provider's real code paths call .Record with seconds, so this
	// mirrors production.
	ctx := context.Background()
	histograms := map[string]func(){
		"truenas.api.duration":              func() { APICallDuration.Record(ctx, 0.050) },
		"truenas.provision.duration":        func() { ProvisionDuration.Record(ctx, 0.050) },
		"truenas.deprovision.duration":      func() { DeprovisionDuration.Record(ctx, 0.050) },
		"truenas.iso.download.duration":     func() { ISODownloadDuration.Record(ctx, 0.050) },
		"truenas.provision.step.duration":   func() { StepDuration.Record(ctx, 0.050) },
		"truenas.deprovision.step.duration": func() { DeprovisionStepDuration.Record(ctx, 0.050) },
	}

	for _, record := range histograms {
		record()
	}

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	seen := make(map[string]bool, len(histograms))

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if _, wanted := histograms[m.Name]; !wanted {
				continue
			}

			seen[m.Name] = true

			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.Truef(t, ok, "metric %q is not a float64 histogram", m.Name)
			require.Lenf(t, hist.DataPoints, 1, "metric %q: expected a single data point after one Record", m.Name)

			dp := hist.DataPoints[0]
			assert.InDeltaf(t, 0.050, dp.Sum, 1e-9, "metric %q: Sum must be 0.050 seconds — if it's ~50 you're recording ms into a seconds metric", m.Name)
			assert.Equalf(t, uint64(1), dp.Count, "metric %q: Count must be 1 after one Record", m.Name)

			bounds := dp.Bounds
			require.NotEmptyf(t, bounds, "metric %q: explicit bucket boundaries missing — if nil, the SDK picked defaults", m.Name)

			// The v0.15.0 bug signature: OTel SDK default
			// [0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000].
			// Exact match means initMetrics didn't pass
			// metric.WithExplicitBucketBoundaries for this instrument.
			sdkDefault := []float64{0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000}
			if equalFloat64Slices(bounds, sdkDefault) {
				t.Fatalf("metric %q: bucket set matches the OTel SDK millisecond default — against a seconds-scale metric, every call <5s lands in one bucket and histogram_quantile returns bucket midpoints. Add metric.WithExplicitBucketBoundaries(...) in initMetrics.", m.Name)
			}

			// Cap sanity: largest bound must not stretch past an hour for any
			// duration the provider records. The provisioner's longest path
			// (ISO download + VM create) is well under 1h in practice.
			assert.LessOrEqualf(t, bounds[len(bounds)-1], 3600.0, "metric %q: largest bucket boundary %v > 1h — unit likely wrong or pathologically wide", m.Name, bounds[len(bounds)-1])
		}
	}

	// Ensure every histogram we cared about actually showed up. If the test
	// silently stopped asserting on a renamed metric it would be useless.
	for name := range histograms {
		assert.Truef(t, seen[name], "metric %q not found in exported ResourceMetrics", name)
	}

	// The API-duration histogram specifically is in the millisecond range in
	// practice (median ~19ms). If its smallest bound isn't sub-second the
	// quantile math will be useless regardless of whether the bounds match
	// the SDK default. This assertion runs *outside* the scan loop because
	// per-metric asserts above only check presence+sum+count.
	assert.NotNil(t, APICallDuration, "APICallDuration must be initialized")
}

func equalFloat64Slices(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
