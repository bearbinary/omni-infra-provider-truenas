package noderotation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerationStability pins the contract that two parties hashing
// the same logical inputs produce identical hashes — that's the whole
// point of the canonical hash, because the discovery loop runs the
// MachineClass-side hash and stale-detection runs the MachineRequest-
// side hash. A drift between the two would make every Machine appear
// stale forever and trigger unbounded rotation.
func TestGenerationStability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fromClass  func() string
		fromReq    func() string
		shouldHash bool // sanity: both produce same non-empty hash
	}{
		{
			name: "identical provider data",
			fromClass: func() string {
				h, _ := MachineClassGeneration(true, `{"cpu":2,"mem":4}`, "GRPC_TUNNEL_DISABLED", nil, nil)
				return h
			},
			fromReq: func() string {
				h, _ := MachineRequestGeneration(`{"cpu":2,"mem":4}`, "GRPC_TUNNEL_DISABLED", nil, nil)
				return h
			},
			shouldHash: true,
		},
		{
			name: "with kernel args",
			fromClass: func() string {
				h, _ := MachineClassGeneration(true, `{"cpu":2}`, "", []string{"console=ttyS0", "init=/sbin/init"}, nil)
				return h
			},
			fromReq: func() string {
				h, _ := MachineRequestGeneration(`{"cpu":2}`, "", []string{"console=ttyS0", "init=/sbin/init"}, nil)
				return h
			},
			shouldHash: true,
		},
		{
			name: "with meta values",
			fromClass: func() string {
				h, _ := MachineClassGeneration(true, "", "", nil, []string{"1=foo", "2=bar"})
				return h
			},
			fromReq: func() string {
				h, _ := MachineRequestGeneration("", "", nil, []string{"1=foo", "2=bar"})
				return h
			},
			shouldHash: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fromClass := tc.fromClass()
			fromReq := tc.fromReq()

			assert.Equal(t, fromClass, fromReq, "MachineClass and MachineRequest hashes must agree on identical inputs")
			if tc.shouldHash {
				assert.NotEmpty(t, fromClass)
				assert.NotEmpty(t, fromReq)
			}
		})
	}
}

// TestGenerationChangesOnContentChange covers the inverse — anything
// that should rotate should produce a different hash.
func TestGenerationChangesOnContentChange(t *testing.T) {
	t.Parallel()

	base, err := MachineClassGeneration(true, `{"cpu":2}`, "", nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, base)

	cases := map[string]func() (string, error){
		"provider data changes": func() (string, error) {
			return MachineClassGeneration(true, `{"cpu":4}`, "", nil, nil)
		},
		"kernel args added": func() (string, error) {
			return MachineClassGeneration(true, `{"cpu":2}`, "", []string{"quiet"}, nil)
		},
		"meta values added": func() (string, error) {
			return MachineClassGeneration(true, `{"cpu":2}`, "", nil, []string{"1=x"})
		},
		"grpc tunnel toggle": func() (string, error) {
			return MachineClassGeneration(true, `{"cpu":2}`, "GRPC_TUNNEL_ENABLED", nil, nil)
		},
	}

	for name, gen := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := gen()
			require.NoError(t, err)
			assert.NotEqual(t, base, got, "expected a different hash when %s", name)
		})
	}
}

// TestGenerationAbsentAutoProvision is the explicit "operator-managed
// allocation gets ignored" contract. Reconciler skips classes with no
// AutoProvision block so a hand-curated cluster doesn't get its
// requests mass-rotated.
func TestGenerationAbsentAutoProvision(t *testing.T) {
	t.Parallel()

	gen, err := MachineClassGeneration(false, `{"cpu":2}`, "", nil, nil)
	require.NoError(t, err)
	assert.Empty(t, gen, "absent AutoProvision should yield empty generation, signaling 'not a rotation candidate'")
}

// TestGenerationGoldenHash pins the byte-level hash output for a few
// canonical inputs. Catches encoder regressions — a stdlib JSON escape
// tweak, a struct-field-reorder refactor, or a switch to a different
// hash function would silently invalidate every persisted generation
// in production and trigger mass rotation. If this test fails after a
// deliberate change, update the goldens AND bump
// docs/node-rotation.md noting the rotation-affecting change.
func TestGenerationGoldenHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   generationInputs
		want string
	}{
		{
			name: "minimal — provider data only",
			in:   generationInputs{ProviderData: `{"cpu":2}`},
			want: "c30017923d88e857",
		},
		{
			name: "full surface — provider data + kernel + meta + grpc",
			in: generationInputs{
				ProviderData: `{"cpu":4,"mem":8}`,
				KernelArgs:   []string{"console=ttyS0", "quiet"},
				MetaValues:   []string{"1=foo", "2=bar"},
				GrpcTunnel:   "GRPC_TUNNEL_ENABLED",
			},
			want: "51a2de233bf937df",
		},
		{
			name: "zero-value inputs",
			in:   generationInputs{},
			want: "104fd644825f22a8",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ComputeGeneration(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got,
				"generation hash changed for canonical input; either revert the change or update goldens + docs/node-rotation.md")
		})
	}
}

// TestGenerationHashLengthIsStable pins the truncation width. Drift
// here would either reduce collision resistance silently (shorter
// output) or push the annotation value past the COSI 256-byte cap
// (much longer output).
func TestGenerationHashLengthIsStable(t *testing.T) {
	t.Parallel()

	got, err := ComputeGeneration(generationInputs{ProviderData: "anything"})
	require.NoError(t, err)
	assert.Len(t, got, generationHashBytes*2, "expected 2*generationHashBytes hex characters")
}
