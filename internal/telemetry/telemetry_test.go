package telemetry

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

// TestSignalEndpoint_AppendsPath pins the URL-join behavior that fixes the
// 404s seen with Grafana Cloud OTLP. The v0.14.1–v0.14.4 implementation
// forwarded OTEL_EXPORTER_OTLP_ENDPOINT verbatim through WithEndpointURL,
// which the SDK uses as a per-signal URL with no path appending. For a
// user-set base URL like .../otlp, requests hit .../otlp and return 404.
// This test asserts we now append /v1/<signal> so requests reach the
// signal-specific endpoints Grafana Cloud serves.
func TestSignalEndpoint_AppendsPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		base     string
		signal   string
		expected string
	}{
		{
			name:     "grafana cloud base with /otlp suffix (the bug in v0.14.1-v0.14.4)",
			base:     "https://otlp-gateway-prod-us-east-3.grafana.net/otlp",
			signal:   "/v1/traces",
			expected: "https://otlp-gateway-prod-us-east-3.grafana.net/otlp/v1/traces",
		},
		{
			name:     "grafana cloud base metrics",
			base:     "https://otlp-gateway-prod-us-east-3.grafana.net/otlp",
			signal:   "/v1/metrics",
			expected: "https://otlp-gateway-prod-us-east-3.grafana.net/otlp/v1/metrics",
		},
		{
			name:     "grafana cloud base logs",
			base:     "https://otlp-gateway-prod-us-east-3.grafana.net/otlp",
			signal:   "/v1/logs",
			expected: "https://otlp-gateway-prod-us-east-3.grafana.net/otlp/v1/logs",
		},
		{
			name:     "trailing slash in base is normalized",
			base:     "https://otlp.example.com/otlp/",
			signal:   "/v1/traces",
			expected: "https://otlp.example.com/otlp/v1/traces",
		},
		{
			name:     "host-only base (no path) just gets the signal path",
			base:     "https://otel-collector.default.svc:4318",
			signal:   "/v1/traces",
			expected: "https://otel-collector.default.svc:4318/v1/traces",
		},
		{
			name:     "root-path base gets the signal path appended cleanly",
			base:     "http://localhost:4318/",
			signal:   "/v1/traces",
			expected: "http://localhost:4318/v1/traces",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := signalEndpoint(tc.base, tc.signal)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// TestSignalEndpoint_InvalidURL_PassesThrough keeps the fallback contract:
// a bad URL comes back unchanged instead of silently becoming something
// weird. The OTLP exporter will raise a clear error at connection time,
// which is more actionable than a mangled URL.
func TestSignalEndpoint_InvalidURL_PassesThrough(t *testing.T) {
	t.Parallel()

	got := signalEndpoint("not a url at all", "/v1/traces")
	assert.Equal(t, "not a url at all", got)
}

func TestInit_NoConfig(t *testing.T) {
	shutdown, err := Init(context.Background(), Config{})
	require.NoError(t, err)

	// Should return a noop shutdown
	err = shutdown(context.Background())
	assert.NoError(t, err)
}

func TestInit_DefaultServiceName(t *testing.T) {
	cfg := Config{}

	// Init with empty config — should not panic, should use default service name
	shutdown, err := Init(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, shutdown(context.Background()))
}

func TestInit_OTELOnly_InvalidEndpoint(t *testing.T) {
	// Invalid endpoint — Init must still succeed (exporters connect lazily;
	// the gRPC OTLP client dials on first export, not during construction).
	// Without this contract, a mistyped OTEL_EXPORTER_OTLP_ENDPOINT would
	// take down the provider on startup instead of just losing telemetry.
	shutdown, err := Init(context.Background(), Config{
		OTELEndpoint:   "localhost:99999",
		OTELInsecure:   true,
		ServiceName:    "test-provider",
		ServiceVersion: "test",
	})
	require.NoError(t, err, "Init must not fail on unreachable OTLP endpoint — exporter connects lazily so bad config can't wedge startup")
	require.NotNil(t, shutdown)

	// Shutdown flush will fail trying to reach the invalid endpoint; the
	// important contract is that it returns rather than hanging forever.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = shutdown(ctx)
}

func TestInit_PyroscopeOnly_InvalidURL(t *testing.T) {
	// An invalid Pyroscope URL must either fail fast at Init or succeed and
	// defer the failure to flush time — either way, no panic. Asserting one
	// of those two branches instead of the older `_ = err` no-op.
	shutdown, err := Init(context.Background(), Config{
		PyroscopeURL:   "not-a-valid-url",
		ServiceName:    "test-provider",
		ServiceVersion: "test",
	})

	if err != nil {
		assert.Nil(t, shutdown, "failed Init must not return a shutdown func — caller has nothing to tear down")
		return
	}

	require.NotNil(t, shutdown)
	_ = shutdown(context.Background())
}

func TestInit_BothConfigured(t *testing.T) {
	// Both OTEL and Pyroscope — with invalid endpoints, just verify no panic
	shutdown, err := Init(context.Background(), Config{
		OTELEndpoint:   "localhost:4317",
		OTELInsecure:   true,
		PyroscopeURL:   "http://localhost:4040",
		ServiceName:    "test-provider",
		ServiceVersion: "v0.0.0-test",
	})

	if err != nil {
		return // acceptable if services aren't running
	}

	// Shutdown should not panic
	_ = shutdown(context.Background())
}

func TestInit_RealOTELCollector(t *testing.T) {
	// This test verifies telemetry works against a real OTEL collector.
	// Requires the observability stack running: docker compose -f deploy/observability/docker-compose.yaml up -d
	endpoint := os.Getenv("OTEL_TEST_ENDPOINT")
	if endpoint == "" {
		// Try localhost:4317 (default from our observability stack)
		conn, err := net.DialTimeout("tcp", "localhost:4317", 2*time.Second)
		if err != nil {
			t.Skip("No OTEL collector available — set OTEL_TEST_ENDPOINT or start deploy/observability stack")
		}

		_ = conn.Close()
		endpoint = "localhost:4317"
	}

	shutdown, err := Init(context.Background(), Config{
		OTELEndpoint:   endpoint,
		OTELInsecure:   true,
		ServiceName:    "test-provider-e2e",
		ServiceVersion: "v0.0.0-test",
	})
	require.NoError(t, err, "Init with real OTEL collector should succeed")

	// Create a span to verify traces flow
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	span.End()

	// Record a metric
	meter := otel.Meter("test")
	counter, err := meter.Int64Counter("test.counter")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)

	// Flush
	err = shutdown(context.Background())
	assert.NoError(t, err, "shutdown should flush successfully to real collector")
}

func TestInit_RealPyroscope(t *testing.T) {
	pyroscopeURL := os.Getenv("PYROSCOPE_TEST_URL")
	if pyroscopeURL == "" {
		conn, err := net.DialTimeout("tcp", "localhost:4040", 2*time.Second)
		if err != nil {
			t.Skip("No Pyroscope available — set PYROSCOPE_TEST_URL or start deploy/observability stack")
		}

		_ = conn.Close()
		pyroscopeURL = "http://localhost:4040"
	}

	shutdown, err := Init(context.Background(), Config{
		PyroscopeURL:   pyroscopeURL,
		ServiceName:    "test-provider-e2e",
		ServiceVersion: "v0.0.0-test",
	})
	require.NoError(t, err, "Init with real Pyroscope should succeed")

	// Do some CPU work to generate profile data
	sum := 0
	for i := range 1000000 {
		sum += i
	}

	_ = sum

	err = shutdown(context.Background())
	assert.NoError(t, err, "shutdown should flush successfully to real Pyroscope")
}

func TestInit_HTTPProtobuf_DeliversToEndpoint(t *testing.T) {
	// Verifies that setting OTELProtocol="http/protobuf" routes traffic through
	// the HTTP exporter and hits /v1/traces etc. Regression test: previously the
	// Protocol field was declared but ignored, so http/protobuf silently fell
	// back to gRPC and failed with "name resolver error" against HTTPS URLs.
	var hits atomic.Int32

	var (
		mu       sync.Mutex
		gotPaths []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)

		mu.Lock()
		gotPaths = append(gotPaths, r.URL.Path)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	shutdown, err := Init(context.Background(), Config{
		OTELEndpoint:   srv.URL,
		OTELProtocol:   "http/protobuf",
		OTELInsecure:   true,
		ServiceName:    "test-provider-http",
		ServiceVersion: "v0.0.0-test",
	})
	require.NoError(t, err)

	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	span.End()

	require.NoError(t, shutdown(context.Background()))

	assert.Positive(t, hits.Load(), "HTTP exporter should have made at least one request")

	mu.Lock()
	pathsCopy := append([]string(nil), gotPaths...)
	mu.Unlock()

	var sawV1Traces bool

	for _, p := range pathsCopy {
		if strings.HasSuffix(p, "/v1/traces") {
			sawV1Traces = true
		}
	}

	assert.True(t, sawV1Traces, "expected a request to .../v1/traces, got paths: %v", pathsCopy)
}

func TestInit_UnsupportedProtocol(t *testing.T) {
	_, err := Init(context.Background(), Config{
		OTELEndpoint:   "http://example:4318",
		OTELProtocol:   "bogus",
		ServiceName:    "test",
		ServiceVersion: "test",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported OTEL_EXPORTER_OTLP_PROTOCOL")
}

func TestInit_ShutdownFlushesAll(t *testing.T) {
	// With no endpoints, shutdown should be a clean noop
	shutdown, err := Init(context.Background(), Config{
		ServiceName: "test",
	})
	require.NoError(t, err)

	// Call shutdown multiple times — should be safe
	assert.NoError(t, shutdown(context.Background()))
	assert.NoError(t, shutdown(context.Background()))
}

// TestBuildOTLPExporters_ProtocolSelection pins the gRPC vs HTTP branch in
// buildOTLPExporters. The v0.14.1 fix was supposed to honor
// OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf, but the http/protobuf branch
// fell back to gRPC silently for nearly a year (well — for a few releases).
// If anyone reverts the switch statement, this test fails immediately.
//
// We compare exporter package paths because the SDK exposes only interface
// types from the public API. otlptracegrpc.Exporter.PkgPath() ends in
// "otlptracegrpc"; otlptracehttp ends in "otlptracehttp".
func TestBuildOTLPExporters_ProtocolSelection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		protocol          string
		expectExporterPkg string // substring expected in exporter type's PkgPath
	}{
		{"empty defaults to gRPC", "", "otlpmetricgrpc"},
		{"explicit grpc", "grpc", "otlpmetricgrpc"},
		{"http/protobuf selects HTTP exporter", "http/protobuf", "otlpmetrichttp"},
		{"http alias also selects HTTP exporter", "http", "otlpmetrichttp"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := Config{
				OTELEndpoint: "https://otlp.example.com/otlp",
				OTELProtocol: tc.protocol,
				OTELInsecure: false,
			}

			// Use metric exporter for the type check — the trace exporter
			// public type is *otlptrace.Exporter regardless of transport
			// (it wraps an unexported client). The metric exporter, by
			// contrast, has a transport-specific concrete type:
			// otlpmetricgrpc.Exporter vs otlpmetrichttp.Exporter.
			_, metricExp, _, err := buildOTLPExporters(context.Background(), cfg)
			require.NoError(t, err, "exporter construction shouldn't fail with valid endpoint URL")

			pkg := reflect.TypeOf(metricExp).Elem().PkgPath()
			assert.Contains(t, pkg, tc.expectExporterPkg,
				"protocol %q should produce exporter from %s, got from %s", tc.protocol, tc.expectExporterPkg, pkg)
		})
	}
}

func TestBuildOTLPExporters_UnsupportedProtocolFailsFast(t *testing.T) {
	t.Parallel()

	cfg := Config{
		OTELEndpoint: "https://otlp.example.com/otlp",
		OTELProtocol: "websocket-over-carrier-pigeon",
	}

	_, _, _, err := buildOTLPExporters(context.Background(), cfg)
	require.Error(t, err, "unknown protocol must fail fast — silent fallback was the v0.14.1–v0.14.4 OTEL bug")
	assert.Contains(t, err.Error(), "unsupported")
}

// TestBuildHTTPExporters_UsesSignalEndpointWiring is a source-grep guard. The
// signalEndpoint() helper is unit-tested separately for path-joining, but
// nothing else asserts that buildHTTPExporters actually calls it. If a
// future refactor reverts to bare WithEndpointURL(cfg.OTELEndpoint), unit
// tests on signalEndpoint still pass and the v0.14.5 OTLP 404 bug returns
// silently.
func TestBuildHTTPExporters_UsesSignalEndpointWiring(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("telemetry.go")
	require.NoError(t, err)

	body := string(src)

	for _, signal := range []string{"/v1/traces", "/v1/metrics", "/v1/logs"} {
		expected := `signalEndpoint(cfg.OTELEndpoint, "` + signal + `")`
		assert.Contains(t, body, expected,
			"telemetry.go must call %s — without it, OTEL_EXPORTER_OTLP_ENDPOINT base URLs (Grafana Cloud /otlp) hit the wrong path and return 404",
			expected)
	}
}
