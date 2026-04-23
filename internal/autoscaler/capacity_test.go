package autoscaler

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeCapacityQuery lets each test swap out pool/host-memory responses
// independently. Nil funcs default to "not called" errors so a test
// that unexpectedly reaches a disabled check fails loudly.
type fakeCapacityQuery struct {
	pool func(pool string) (int64, error)
	mem  func() (int64, error)
}

func (f fakeCapacityQuery) PoolFreeBytes(_ context.Context, pool string) (int64, error) {
	if f.pool == nil {
		return 0, errors.New("fakeCapacityQuery.PoolFreeBytes unexpectedly called")
	}

	return f.pool(pool)
}

func (f fakeCapacityQuery) HostFreeMemoryBytes(_ context.Context) (int64, error) {
	if f.mem == nil {
		return 0, errors.New("fakeCapacityQuery.HostFreeMemoryBytes unexpectedly called")
	}

	return f.mem()
}

// TestCheckCapacity covers the full decision table for the gate. Named
// numbers chosen so threshold breaches are obvious at a glance:
// 100 GiB threshold, queries return 50 GiB (breach) or 200 GiB (pass).
func TestCheckCapacity(t *testing.T) {
	t.Parallel()

	gib := int64(bytesPerGiB)

	tests := []struct {
		name        string
		cfg         Config
		pool        func(string) (int64, error)
		mem         func() (int64, error)
		wantOutcome Outcome
		wantReason  string // substring the reason must contain; "" means Reason must be empty
	}{
		{
			name:        "both thresholds pass",
			cfg:         Config{MinPoolFreeGiB: 100, MinHostMemGiB: 8, CapacityGate: CapacityGateHard},
			pool:        func(string) (int64, error) { return 200 * gib, nil },
			mem:         func() (int64, error) { return 16 * gib, nil },
			wantOutcome: OutcomeAllowed,
		},
		{
			name:        "pool breach under hard gate → denied",
			cfg:         Config{MinPoolFreeGiB: 100, MinHostMemGiB: 8, CapacityGate: CapacityGateHard},
			pool:        func(string) (int64, error) { return 50 * gib, nil },
			mem:         func() (int64, error) { return 16 * gib, nil },
			wantOutcome: OutcomeDeniedHard,
			wantReason:  "pool",
		},
		{
			name:        "host-mem breach under hard gate → denied",
			cfg:         Config{MinPoolFreeGiB: 100, MinHostMemGiB: 8, CapacityGate: CapacityGateHard},
			pool:        func(string) (int64, error) { return 200 * gib, nil },
			mem:         func() (int64, error) { return 4 * gib, nil },
			wantOutcome: OutcomeDeniedHard,
			wantReason:  "host free memory",
		},
		{
			name:        "both breach under hard gate → denied, reason names both",
			cfg:         Config{MinPoolFreeGiB: 100, MinHostMemGiB: 8, CapacityGate: CapacityGateHard},
			pool:        func(string) (int64, error) { return 50 * gib, nil },
			mem:         func() (int64, error) { return 4 * gib, nil },
			wantOutcome: OutcomeDeniedHard,
			wantReason:  "pool",
		},
		{
			name:        "pool breach under soft gate → warned, still allowed",
			cfg:         Config{MinPoolFreeGiB: 100, MinHostMemGiB: 8, CapacityGate: CapacityGateSoft},
			pool:        func(string) (int64, error) { return 50 * gib, nil },
			mem:         func() (int64, error) { return 16 * gib, nil },
			wantOutcome: OutcomeWarnedSoft,
			wantReason:  "pool",
		},
		{
			name:        "pool query fails → errored (fails closed)",
			cfg:         Config{MinPoolFreeGiB: 100, MinHostMemGiB: 8, CapacityGate: CapacityGateHard},
			pool:        func(string) (int64, error) { return 0, errors.New("boom") },
			mem:         func() (int64, error) { return 16 * gib, nil },
			wantOutcome: OutcomeErrored,
			wantReason:  "boom",
		},
		{
			name:        "pool threshold disabled (0) — skip pool check",
			cfg:         Config{MinPoolFreeGiB: 0, MinHostMemGiB: 8, CapacityGate: CapacityGateHard},
			pool:        nil, // would error if called
			mem:         func() (int64, error) { return 16 * gib, nil },
			wantOutcome: OutcomeAllowed,
		},
		{
			name:        "host-mem threshold disabled (0) — skip host check",
			cfg:         Config{MinPoolFreeGiB: 100, MinHostMemGiB: 0, CapacityGate: CapacityGateHard},
			pool:        func(string) (int64, error) { return 200 * gib, nil },
			mem:         nil, // would error if called
			wantOutcome: OutcomeAllowed,
		},
		{
			name:        "both thresholds disabled (0) — always allow",
			cfg:         Config{MinPoolFreeGiB: 0, MinHostMemGiB: 0, CapacityGate: CapacityGateHard},
			pool:        nil,
			mem:         nil,
			wantOutcome: OutcomeAllowed,
		},
		{
			name:        "exactly at threshold — allowed (>= semantics)",
			cfg:         Config{MinPoolFreeGiB: 100, MinHostMemGiB: 8, CapacityGate: CapacityGateHard},
			pool:        func(string) (int64, error) { return 100 * gib, nil },
			mem:         func() (int64, error) { return 8 * gib, nil },
			wantOutcome: OutcomeAllowed,
		},
		{
			name:        "soft gate, both pass → allowed (no warn)",
			cfg:         Config{MinPoolFreeGiB: 100, MinHostMemGiB: 8, CapacityGate: CapacityGateSoft},
			pool:        func(string) (int64, error) { return 200 * gib, nil },
			mem:         func() (int64, error) { return 16 * gib, nil },
			wantOutcome: OutcomeAllowed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			q := fakeCapacityQuery{pool: tc.pool, mem: tc.mem}
			got := CheckCapacity(context.Background(), q, tc.cfg, "tank")

			assert.Equal(t, tc.wantOutcome, got.Outcome, "outcome mismatch; reason=%q", got.Reason)

			if tc.wantReason == "" {
				assert.Empty(t, got.Reason, "reason must be empty when outcome is allowed")
			} else {
				assert.Contains(t, got.Reason, tc.wantReason)
			}
		})
	}
}

// TestDecision_Allowed pins the caller-side predicate used by gRPC
// handlers to branch quickly. Soft-warn must count as allowed so
// scale-up proceeds; everything else blocks.
func TestDecision_Allowed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		outcome Outcome
		want    bool
	}{
		{"allowed", OutcomeAllowed, true},
		{"warned soft", OutcomeWarnedSoft, true},
		{"denied hard", OutcomeDeniedHard, false},
		{"errored", OutcomeErrored, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := Decision{Outcome: tc.outcome}
			assert.Equal(t, tc.want, d.Allowed())
		})
	}
}

// TestOutcome_String pins the metric-label form of each outcome so a
// rename can't silently break existing dashboards + alerts. The
// snake_case form is load-bearing — Prometheus rejects labels with
// spaces or uppercase, and dashboards reference these exact strings.
func TestOutcome_String(t *testing.T) {
	t.Parallel()

	cases := map[Outcome]string{
		OutcomeAllowed:    "allowed",
		OutcomeDeniedHard: "denied_hard",
		OutcomeWarnedSoft: "warned_soft",
		OutcomeErrored:    "errored",
	}

	for outcome, want := range cases {
		assert.Equal(t, want, outcome.String())
	}
}
