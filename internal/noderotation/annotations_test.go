package noderotation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseMachineClassRotationConfig covers every branch of the
// rotation parser. Treats both the strict opt-in semantics
// (enabled=true required) and the CP+in-place refusal as load-
// bearing — a regression here either silently rotates a class the
// operator didn't ask to rotate, or silently allows a CP gap that
// could drop quorum.
func TestParseMachineClassRotationConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		annotations map[string]string
		want        *Config
		wantErr     string
	}{
		{
			name:        "not opted in — empty",
			annotations: map[string]string{},
			want:        nil,
		},
		{
			name: "not opted in — unrelated annotations",
			annotations: map[string]string{
				"example.com/other": "42",
			},
			want: nil,
		},
		{
			name: "not opted in — enabled missing even with role+strategy",
			annotations: map[string]string{
				AnnotationRole:     "worker",
				AnnotationStrategy: "in-place",
			},
			want: nil,
		},
		{
			name: "not opted in — enabled=false",
			annotations: map[string]string{
				AnnotationEnabled:  "false",
				AnnotationRole:     "worker",
				AnnotationStrategy: "in-place",
			},
			want: nil,
		},
		{
			name: "not opted in — enabled=yes (strict parse)",
			annotations: map[string]string{
				AnnotationEnabled:  "yes",
				AnnotationRole:     "worker",
				AnnotationStrategy: "in-place",
			},
			want: nil,
		},
		{
			name: "opted in — worker + in-place, defaults",
			annotations: map[string]string{
				AnnotationEnabled:  "true",
				AnnotationRole:     "worker",
				AnnotationStrategy: "in-place",
			},
			want: &Config{
				Role:       RoleWorker,
				Strategy:   StrategyInPlace,
				MinHealthy: DefaultMinHealthyWorker,
			},
		},
		{
			name: "opted in — worker + surge, defaults",
			annotations: map[string]string{
				AnnotationEnabled:  "true",
				AnnotationRole:     "worker",
				AnnotationStrategy: "surge",
			},
			want: &Config{
				Role:       RoleWorker,
				Strategy:   StrategySurge,
				MinHealthy: DefaultMinHealthyWorker,
			},
		},
		{
			name: "opted in — controlplane + surge, defaults",
			annotations: map[string]string{
				AnnotationEnabled:  "true",
				AnnotationRole:     "controlplane",
				AnnotationStrategy: "surge",
			},
			want: &Config{
				Role:       RoleControlPlane,
				Strategy:   StrategySurge,
				MinHealthy: DefaultMinHealthyControlPlane,
			},
		},
		{
			name: "REFUSED — controlplane + in-place",
			annotations: map[string]string{
				AnnotationEnabled:  "true",
				AnnotationRole:     "controlplane",
				AnnotationStrategy: "in-place",
			},
			wantErr: "control-plane",
		},
		{
			name: "opted in — explicit min-healthy override",
			annotations: map[string]string{
				AnnotationEnabled:    "true",
				AnnotationRole:       "worker",
				AnnotationStrategy:   "in-place",
				AnnotationMinHealthy: "3",
			},
			want: &Config{
				Role:       RoleWorker,
				Strategy:   StrategyInPlace,
				MinHealthy: 3,
			},
		},
		{
			name: "opted in — case-insensitive enable + role + strategy",
			annotations: map[string]string{
				AnnotationEnabled:  "True",
				AnnotationRole:     "WORKER",
				AnnotationStrategy: "In-Place",
			},
			want: &Config{
				Role:       RoleWorker,
				Strategy:   StrategyInPlace,
				MinHealthy: DefaultMinHealthyWorker,
			},
		},
		{
			name: "error — enabled but role missing",
			annotations: map[string]string{
				AnnotationEnabled:  "true",
				AnnotationStrategy: "in-place",
			},
			wantErr: AnnotationRole,
		},
		{
			name: "error — enabled but strategy missing",
			annotations: map[string]string{
				AnnotationEnabled: "true",
				AnnotationRole:    "worker",
			},
			wantErr: AnnotationStrategy,
		},
		{
			name: "error — invalid role",
			annotations: map[string]string{
				AnnotationEnabled:  "true",
				AnnotationRole:     "gateway",
				AnnotationStrategy: "in-place",
			},
			wantErr: "gateway",
		},
		{
			name: "error — invalid strategy",
			annotations: map[string]string{
				AnnotationEnabled:  "true",
				AnnotationRole:     "worker",
				AnnotationStrategy: "rolling",
			},
			wantErr: "rolling",
		},
		{
			name: "error — non-integer min-healthy",
			annotations: map[string]string{
				AnnotationEnabled:    "true",
				AnnotationRole:       "worker",
				AnnotationStrategy:   "in-place",
				AnnotationMinHealthy: "two",
			},
			wantErr: AnnotationMinHealthy,
		},
		{
			name: "error — negative min-healthy",
			annotations: map[string]string{
				AnnotationEnabled:    "true",
				AnnotationRole:       "worker",
				AnnotationStrategy:   "in-place",
				AnnotationMinHealthy: "-1",
			},
			wantErr: AnnotationMinHealthy,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseMachineClassRotationConfig(tc.annotations)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr, "error should mention %q", tc.wantErr)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestIsRotationOptIn is the cheap pre-filter. Verifies the strict
// "true" parse so a typo doesn't accidentally enroll a class.
func TestIsRotationOptIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		annotations map[string]string
		want        bool
	}{
		{map[string]string{}, false},
		{map[string]string{AnnotationEnabled: ""}, false},
		{map[string]string{AnnotationEnabled: "false"}, false},
		{map[string]string{AnnotationEnabled: "1"}, false},
		{map[string]string{AnnotationEnabled: "yes"}, false},
		{map[string]string{AnnotationEnabled: "true"}, true},
		{map[string]string{AnnotationEnabled: "TRUE"}, true},
		{map[string]string{AnnotationEnabled: "  true  "}, true},
	}

	for _, tc := range tests {
		t.Run(strings.ReplaceAll(tc.annotations[AnnotationEnabled], " ", "_"), func(t *testing.T) {
			assert.Equal(t, tc.want, IsRotationOptIn(tc.annotations))
		})
	}
}
