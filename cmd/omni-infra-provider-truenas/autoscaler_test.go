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

// TestRunAutoscaler_ShutsDownCleanlyOnContextCancel verifies the
// Phase 1 hold-open loop returns nil when its parent context is
// cancelled. Catches regressions where a later phase adds a blocking
// call that ignores ctx.
func TestRunAutoscaler_ShutsDownCleanlyOnContextCancel(t *testing.T) {
	t.Setenv(autoscaler.EnvClusterName, "test-cluster")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel so runAutoscaler returns immediately.

	err := runAutoscaler(ctx)

	// Either nil (clean shutdown) OR a context-canceled passthrough is
	// acceptable; what's NOT acceptable is a config-parse error or a
	// panic. The runAutoscaler impl treats context.Canceled as a clean
	// shutdown signal and returns nil.
	assert.NoError(t, err, "pre-cancelled context must shut down cleanly")
}
