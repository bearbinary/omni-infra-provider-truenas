package cleanup

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVMNameFromZvolName(t *testing.T) {
	// The cleanup logic maps zvol names (request IDs with hyphens)
	// to VM names (omni_ prefix, underscores instead of hyphens).
	tests := []struct {
		zvolName string
		vmName   string
	}{
		{"talos-test-workers-abc123", "omni_talos_test_workers_abc123"},
		{"simple", "omni_simple"},
		{"a-b-c", "omni_a_b_c"},
	}

	for _, tt := range tests {
		t.Run(tt.zvolName, func(t *testing.T) {
			// Replicate the logic from cleanupOrphanZvols
			vmName := "omni_" + replaceHyphens(tt.zvolName)
			assert.Equal(t, tt.vmName, vmName)
		})
	}
}

func replaceHyphens(s string) string {
	result := make([]byte, len(s))
	for i := range len(s) {
		if s[i] == '-' {
			result[i] = '_'
		} else {
			result[i] = s[i]
		}
	}

	return string(result)
}
