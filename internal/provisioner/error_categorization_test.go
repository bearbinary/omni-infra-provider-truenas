package provisioner

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecordProvisionError_Categories(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		errMsg  string
		wantCat string
	}{
		{"pool not found", `pool "tank" not found`, "pool_not_found"},
		{"pool full ENOSPC", "ENOSPC: pool is full", "pool_full"},
		{"nic invalid nic_attach", "nic_attach: br999 not found", "nic_invalid"},
		{"nic invalid NIC", "invalid NIC configuration", "nic_invalid"},
		{"network_interface invalid", "network_interface contains unsafe characters", "nic_invalid"},
		{"connection reconnect", "reconnect failed after 3 attempts", "connection"},
		{"connection unreachable", "TrueNAS is unreachable", "connection"},
		{"auth permission", "permission denied", "auth"},
		{"auth EACCES", "EACCES: access denied", "auth"},
		{"timeout", "context deadline exceeded: timeout", "timeout"},
		{"memory", "host has 8192 MiB total memory but VM requests 32768 MiB", "memory"},
		{"memory RAM", "not enough RAM", "memory"},
		{"image schematic", "failed to generate schematic", "image"},
		{"image ISO", "failed to download ISO", "image"},
		{"unknown error", "something completely different", "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// recordProvisionError calls telemetry which may be nil in tests.
			// We test the categorization logic by extracting it.
			cat := categorizeError(fmt.Errorf("%s", tc.errMsg))
			assert.Equal(t, tc.wantCat, cat)
		})
	}
}

func TestRecordProvisionError_NilError(t *testing.T) {
	t.Parallel()
	// Should not panic
	recordProvisionError(context.Background(), nil)
}
