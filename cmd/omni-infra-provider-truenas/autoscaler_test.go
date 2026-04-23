package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/autoscaler"
)

// TestRunAutoscaler_MissingClusterName mirrors the provisioner's
// TestSmoke_MissingOmniEndpoint shape — the subcommand must fail with a
// recognizable error string so deploy manifests that forget
// OMNI_CLUSTER_NAME surface that fact in pod logs rather than looking
// like an opaque startup hang.
func TestRunAutoscaler_MissingClusterName(t *testing.T) {
	// Cannot use t.Parallel — t.Setenv mutates process env.
	t.Setenv(autoscaler.EnvClusterName, "")

	err := runAutoscaler(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), autoscaler.EnvClusterName,
		"error must name the missing env var so the operator knows what to fix")
}

// TestRunAutoscaler_MissingOmniEndpoint pins the fail-fast behavior
// when OMNI_ENDPOINT isn't set. Operators deploying the autoscaler
// subcommand must see a named-env-var error in pod logs rather than
// the subcommand entering a partial state (cluster name validated,
// then a silent hang on Omni-client construction).
func TestRunAutoscaler_MissingOmniEndpoint(t *testing.T) {
	t.Setenv(autoscaler.EnvClusterName, "test-cluster")
	t.Setenv("OMNI_ENDPOINT", "")

	err := runAutoscaler(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "OMNI_ENDPOINT",
		"error must name OMNI_ENDPOINT so operators can diagnose the missing env var")
}

// TestRunAutoscaler_ShutsDownCleanlyOnContextCancel verifies the
// Phase 1 hold-open loop returns nil when its parent context is
// cancelled. Catches regressions where a later phase adds a blocking
// call that ignores ctx.
func TestRunAutoscaler_ShutsDownCleanlyOnContextCancel(t *testing.T) {
	t.Setenv(autoscaler.EnvClusterName, "test-cluster")
	// Ephemeral port so the test doesn't collide with a running
	// autoscaler Deployment on the dev machine or with another test
	// binary holding the default :8086.
	t.Setenv(autoscaler.EnvListenAddress, "127.0.0.1:0")
	// Localhost endpoint so client.New succeeds (it doesn't actually
	// dial at construction time). TRUENAS_HOST left unset to
	// exercise the "capacity gate disabled" branch — the test only
	// cares that the subcommand sets up and shuts down cleanly on a
	// pre-cancelled context.
	t.Setenv("OMNI_ENDPOINT", "http://localhost:0")
	t.Setenv("OMNI_SERVICE_ACCOUNT_KEY", "")
	t.Setenv("TRUENAS_HOST", "")
	// Disable the singleton lease — the test has no live Omni
	// backing store to read/write against. Shutdown-path coverage
	// doesn't need the lease.
	t.Setenv("AUTOSCALER_SINGLETON_ENABLED", "false")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel so runAutoscaler returns immediately.

	err := runAutoscaler(ctx)

	// Either nil (clean shutdown) OR a context-canceled passthrough is
	// acceptable; what's NOT acceptable is a config-parse error or a
	// panic. The runAutoscaler impl treats context.Canceled as a clean
	// shutdown signal and returns nil.
	assert.NoError(t, err, "pre-cancelled context must shut down cleanly")
}
