package noderotation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAnnotationKeyLiterals pins every exported Annotation* constant
// against its literal string. The constants are the contract surface
// operators write into MachineClass / MachineSet YAML, and the
// autoscaler's lock parser uses the same literal in
// internal/autoscaler/rotation_lock.go. A typo here (the original
// `strategyegy` regression that shipped in v0.17.0-rc.x) silently
// breaks the opt-in for any operator following the docs — this test
// catches the next one before it ships.
func TestAnnotationKeyLiterals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		got  string
		want string
	}{
		{AnnotationEnabled, "node-rotation.omni/enabled"},
		{AnnotationRole, "node-rotation.omni/role"},
		{AnnotationStrategy, "node-rotation.omni/strategy"},
		{AnnotationMinHealthy, "node-rotation.omni/min-healthy"},
		{AnnotationClassGeneration, "node-rotation.omni/class-generation"},
		{AnnotationRotationState, "node-rotation.omni/rotation-state"},
		{AnnotationSurgePhase, "node-rotation.omni/surge-phase"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.got)
		})
	}
}
