package autoscaler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	truenasclient "github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

// TestCapacityGate_Cassette_Allows exercises the
// TrueNASCapacityAdapter's pool-free check against a hand-crafted
// cassette that matches the exact shape TrueNAS 25.10 returns for
// `pool.query` + `pool.dataset.query`. Complements the mock-based
// TestTrueNASCapacityAdapter_PoolFreeBytes which exercises routing
// logic without pinning the wire format — if TrueNAS changes the
// response shape in a future upgrade, this cassette-based test
// breaks first, and the cassette-age gate (internal/client/cassette_age_test.go)
// will flag staleness after 90 days so operators refresh against a
// live host.
func TestCapacityGate_Cassette_Allows(t *testing.T) {
	t.Parallel()

	replay := truenasclient.NewReplayTransport(t, "testdata/cassettes/TestCapacityGate_PoolFree.json")
	client := truenasclient.NewReplayClient(replay)

	t.Cleanup(func() {
		replay.AssertAllConsumed(t)
	})

	adapter := NewTrueNASCapacityAdapter(client)

	decision := CheckCapacity(context.Background(), adapter, Config{
		MinPoolFreeGiB: 50,
		MinHostMemGiB:  0, // Disabled — adapter returns ErrHostMemNotImplemented.
		CapacityGate:   CapacityGateHard,
	}, "default")

	require.Equal(t, OutcomeAllowed, decision.Outcome,
		"50 GiB threshold with 4.27 TiB free must pass; reason=%q", decision.Reason)
	assert.Empty(t, decision.Reason, "allowed outcomes must not carry a reason string")
}

// TestCapacityGate_Cassette_Denies is the symmetric case: same
// cassette shape, threshold set high enough to deny. Separate test
// (vs a subtest inside TestCapacityGate_Cassette) because the
// ReplayTransport is single-pass — each call to PoolFreeBytes
// consumes one interaction.
func TestCapacityGate_Cassette_Denies(t *testing.T) {
	t.Parallel()

	replay := truenasclient.NewReplayTransport(t, "testdata/cassettes/TestCapacityGate_PoolFree.json")
	client := truenasclient.NewReplayClient(replay)

	t.Cleanup(func() {
		replay.AssertAllConsumed(t)
	})

	adapter := NewTrueNASCapacityAdapter(client)

	const cassetteFreeGiB = 4691248635904 / bytesPerGiB // ~4370 GiB

	decision := CheckCapacity(context.Background(), adapter, Config{
		MinPoolFreeGiB: cassetteFreeGiB + 1, // One GiB over the cassette's free value
		MinHostMemGiB:  0,
		CapacityGate:   CapacityGateHard,
	}, "default")

	assert.Equal(t, OutcomeDeniedHard, decision.Outcome)
	assert.Contains(t, decision.Reason, "pool")
	assert.Contains(t, decision.Reason, "default")
}
