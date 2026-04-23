package autoscaler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseMachineClassAutoscaleConfig covers every branch of the
// annotation parser. The parser is load-bearing — a false positive
// (accepting malformed annotations and silently defaulting) would let
// the autoscaler scale a MachineSet to a number the operator didn't
// ask for, and a false negative (rejecting valid annotations) makes
// the experimental feature look broken. Both failure modes hit users
// quickly, so the parse surface is kept narrow and every edge has a
// test.
func TestParseMachineClassAutoscaleConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		annotations map[string]string
		want        *Config
		wantErr     string // substring the error must contain; empty means no error
	}{
		{
			name:        "not opted in — no annotations",
			annotations: map[string]string{},
			want:        nil,
		},
		{
			name: "not opted in — only unrelated annotations",
			annotations: map[string]string{
				"example.com/other": "42",
			},
			want: nil,
		},
		{
			name: "min+max only — defaults fill the rest",
			annotations: map[string]string{
				AnnotationAutoscaleMin: "2",
				AnnotationAutoscaleMax: "10",
			},
			want: &Config{
				Min:            2,
				Max:            10,
				CapacityGate:   CapacityGateHard,
				MinPoolFreeGiB: DefaultMinPoolFreeGiB,
				MinHostMemGiB:  DefaultMinHostMemGiB,
			},
		},
		{
			name: "all fields set explicitly",
			annotations: map[string]string{
				AnnotationAutoscaleMin:            "1",
				AnnotationAutoscaleMax:            "5",
				AnnotationAutoscaleCapacityGate:   "soft",
				AnnotationAutoscaleMinPoolFreeGiB: "200",
				AnnotationAutoscaleMinHostMemGiB:  "32",
			},
			want: &Config{
				Min:            1,
				Max:            5,
				CapacityGate:   CapacityGateSoft,
				MinPoolFreeGiB: 200,
				MinHostMemGiB:  32,
			},
		},
		{
			name: "thresholds explicitly disabled with 0",
			annotations: map[string]string{
				AnnotationAutoscaleMin:            "0",
				AnnotationAutoscaleMax:            "3",
				AnnotationAutoscaleMinPoolFreeGiB: "0",
				AnnotationAutoscaleMinHostMemGiB:  "0",
			},
			want: &Config{
				Min:            0,
				Max:            3,
				CapacityGate:   CapacityGateHard,
				MinPoolFreeGiB: 0,
				MinHostMemGiB:  0,
			},
		},
		{
			name: "capacity-gate is case-insensitive",
			annotations: map[string]string{
				AnnotationAutoscaleMin:          "1",
				AnnotationAutoscaleMax:          "2",
				AnnotationAutoscaleCapacityGate: "HARD",
			},
			want: &Config{
				Min:            1,
				Max:            2,
				CapacityGate:   CapacityGateHard,
				MinPoolFreeGiB: DefaultMinPoolFreeGiB,
				MinHostMemGiB:  DefaultMinHostMemGiB,
			},
		},
		{
			name: "capacity-gate tolerates whitespace",
			annotations: map[string]string{
				AnnotationAutoscaleMin:          "1",
				AnnotationAutoscaleMax:          "2",
				AnnotationAutoscaleCapacityGate: "  soft  ",
			},
			want: &Config{
				Min:            1,
				Max:            2,
				CapacityGate:   CapacityGateSoft,
				MinPoolFreeGiB: DefaultMinPoolFreeGiB,
				MinHostMemGiB:  DefaultMinHostMemGiB,
			},
		},
		{
			name: "min without max → error",
			annotations: map[string]string{
				AnnotationAutoscaleMin: "2",
			},
			wantErr: AnnotationAutoscaleMax + " is required",
		},
		{
			name: "max without min → error",
			annotations: map[string]string{
				AnnotationAutoscaleMax: "5",
			},
			wantErr: AnnotationAutoscaleMin + " is required",
		},
		{
			name: "non-integer min",
			annotations: map[string]string{
				AnnotationAutoscaleMin: "two",
				AnnotationAutoscaleMax: "5",
			},
			wantErr: AnnotationAutoscaleMin,
		},
		{
			name: "negative min",
			annotations: map[string]string{
				AnnotationAutoscaleMin: "-1",
				AnnotationAutoscaleMax: "5",
			},
			wantErr: "must be ≥ 0",
		},
		{
			name: "min > max",
			annotations: map[string]string{
				AnnotationAutoscaleMin: "10",
				AnnotationAutoscaleMax: "2",
			},
			wantErr: "must be ≥",
		},
		{
			name: "min == max is allowed",
			annotations: map[string]string{
				AnnotationAutoscaleMin: "3",
				AnnotationAutoscaleMax: "3",
			},
			want: &Config{
				Min:            3,
				Max:            3,
				CapacityGate:   CapacityGateHard,
				MinPoolFreeGiB: DefaultMinPoolFreeGiB,
				MinHostMemGiB:  DefaultMinHostMemGiB,
			},
		},
		{
			name: "unknown capacity-gate mode → error, don't silently default",
			annotations: map[string]string{
				AnnotationAutoscaleMin:          "1",
				AnnotationAutoscaleMax:          "2",
				AnnotationAutoscaleCapacityGate: "strict",
			},
			wantErr: AnnotationAutoscaleCapacityGate,
		},
		{
			name: "negative pool threshold",
			annotations: map[string]string{
				AnnotationAutoscaleMin:            "1",
				AnnotationAutoscaleMax:            "2",
				AnnotationAutoscaleMinPoolFreeGiB: "-10",
			},
			wantErr: "must be ≥ 0",
		},
		{
			name: "non-integer host-mem threshold",
			annotations: map[string]string{
				AnnotationAutoscaleMin:           "1",
				AnnotationAutoscaleMax:           "2",
				AnnotationAutoscaleMinHostMemGiB: "a lot",
			},
			wantErr: AnnotationAutoscaleMinHostMemGiB,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseMachineClassAutoscaleConfig(tc.annotations)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Containsf(t, err.Error(), tc.wantErr, "error %q missing expected substring %q", err, tc.wantErr)
				assert.Nil(t, got, "config must be nil when parse fails")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestIsAutoscaleOptIn is the cheap pre-filter the MachineClass discovery
// loop runs before the full parse. Kept separate from the parser tests
// because a caller is allowed to treat it as a pure opt-in check —
// returning true on a malformed class is fine as long as the subsequent
// parse call rejects it.
func TestIsAutoscaleOptIn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{"no annotations", map[string]string{}, false},
		{"only unrelated annotations", map[string]string{"x": "y"}, false},
		{"only min", map[string]string{AnnotationAutoscaleMin: "1"}, true},
		{"only max", map[string]string{AnnotationAutoscaleMax: "5"}, true},
		{"both min and max", map[string]string{
			AnnotationAutoscaleMin: "1",
			AnnotationAutoscaleMax: "5",
		}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsAutoscaleOptIn(tc.annotations))
		})
	}
}

// TestAnnotationPrefixes pins the `bearbinary.com/` prefix on every
// exported annotation key. Keeping the vendor namespace consistent
// matters: operators who already have ConfigMap-based tooling that
// filters on `bearbinary.com/` will lose the autoscaler annotations if
// someone refactors these keys under a different prefix.
func TestAnnotationPrefixes(t *testing.T) {
	t.Parallel()

	keys := []string{
		AnnotationAutoscaleMin,
		AnnotationAutoscaleMax,
		AnnotationAutoscaleCapacityGate,
		AnnotationAutoscaleMinPoolFreeGiB,
		AnnotationAutoscaleMinHostMemGiB,
	}

	for _, k := range keys {
		assert.Truef(t, strings.HasPrefix(k, "bearbinary.com/"),
			"annotation %q must be under the bearbinary.com/ namespace", k)
	}
}
