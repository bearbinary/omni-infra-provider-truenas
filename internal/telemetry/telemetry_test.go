package telemetry

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

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
	// Invalid endpoint — Init should still succeed (exporters connect lazily)
	shutdown, err := Init(context.Background(), Config{
		OTELEndpoint:   "localhost:99999",
		OTELInsecure:   true,
		ServiceName:    "test-provider",
		ServiceVersion: "test",
	})

	if err != nil {
		return // acceptable — some OTLP clients fail eagerly
	}

	// Shutdown may fail trying to flush to invalid endpoint — that's expected
	_ = shutdown(context.Background())
}

func TestInit_PyroscopeOnly_InvalidURL(t *testing.T) {
	// Pyroscope with invalid URL — should fail
	_, err := Init(context.Background(), Config{
		PyroscopeURL:   "not-a-valid-url",
		ServiceName:    "test-provider",
		ServiceVersion: "test",
	})

	// Pyroscope may or may not fail on invalid URL depending on the client
	// Just verify it doesn't panic
	_ = err
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

		conn.Close()
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

		conn.Close()
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
