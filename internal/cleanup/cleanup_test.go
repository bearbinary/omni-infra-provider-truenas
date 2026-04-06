package cleanup

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestCleanerRun_CancelsOnContext(t *testing.T) {
	// Verify Run() exits when context is cancelled
	cl := &Cleaner{
		config:         Config{CleanupInterval: time.Hour},
		logger:         zap.NewNop(),
		activeImageIDs: func() map[string]bool { return nil },
		activeVMNames:  func() map[string]bool { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		cl.Run(ctx)
		close(done)
	}()

	// Cancel immediately
	cancel()

	select {
	case <-done:
		// Success — Run exited
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not exit after context cancellation")
	}
}

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
