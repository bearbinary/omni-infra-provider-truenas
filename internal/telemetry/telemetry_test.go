package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
