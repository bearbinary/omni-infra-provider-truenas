package provisioner

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
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
		// `config_invalid` — MachineClass validation failures wrapped via
		// "invalid MachineClass config: %w" must NOT route to nic_invalid even
		// when the inner message mentions additional_nics. Regression guard
		// against "operator typo pages the same alert as hypervisor regression".
		{"config_invalid CIDR typo", `invalid MachineClass config: additional_nics[0].addresses[0]: "10.20.0.5" is not a valid CIDR`, "config_invalid"},
		{"config_invalid gateway typo", `invalid MachineClass config: additional_nics[0].gateway: "not-an-ip" is not a valid IP address`, "config_invalid"},
		{"config_invalid disk size", `invalid MachineClass config: disk_size must be >= 20 GiB`, "config_invalid"},
		// `config_patch` — CreateConfigPatch failures across all five patch
		// kinds. Without this, they fall to "unknown" and on-call can't
		// attribute which patch broke.
		{"config_patch build nic-interfaces", "failed to build additional-NIC interfaces config patch: invalid MAC", "config_patch"},
		{"config_patch apply nic-interfaces", "failed to apply additional-NIC interfaces config patch: resource conflict", "config_patch"},
		{"config_patch apply data-volumes", "failed to apply data-volumes config patch: connection refused", "config_patch"},
		{"config_patch apply longhorn-ops", "failed to apply longhorn-ops config patch: timeout", "config_patch"},
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
	recordProvisionError(context.Background(), nil, nil)
}

// TestRecordProvisionError_RequeueUnwrap verifies that RequeueError is handled
// correctly. Without unwrapping, every step-wait signal would land as an
// Error-level log line with error_category=unknown and bump the
// truenas_provision_errors_total counter — a regression introduced in the
// initial v0.15.0 recordProvisionError change and fixed in v0.15.1.
func TestRecordProvisionError_RequeueUnwrap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantLogs int    // number of error-level log lines expected
		wantMsg  string // substring that must appear in the one log line, when wantLogs==1
	}{
		{
			name:     "pure requeue with nil inner",
			err:      controller.NewRequeueError(nil, 15*time.Second),
			wantLogs: 0,
		},
		{
			name:     "requeue wrapping a real error",
			err:      controller.NewRequeueError(errors.New("pool \"tank\" not found"), 15*time.Second),
			wantLogs: 1,
			wantMsg:  `pool "tank" not found`,
		},
		{
			name:     "non-requeue error passes through",
			err:      errors.New("failed to delete VM 42"),
			wantLogs: 1,
			wantMsg:  "failed to delete VM 42",
		},
		{
			name:     "context.Canceled alone is treated as shutdown, not failure",
			err:      context.Canceled,
			wantLogs: 0,
		},
		{
			name:     "context.Canceled wrapped in RequeueError is also shutdown",
			err:      controller.NewRequeueError(context.Canceled, 15*time.Second),
			wantLogs: 0,
		},
		{
			name:     "wrapped context.Canceled with more context — still shutdown",
			err:      fmt.Errorf("cleanupVM: %w", context.Canceled),
			wantLogs: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			core, sink := observer.New(zap.ErrorLevel)
			logger := zap.New(core)

			recordProvisionError(context.Background(), logger, tc.err)

			entries := sink.FilterMessage("provision error").All()
			assert.Len(t, entries, tc.wantLogs)

			if tc.wantLogs == 1 {
				assert.Contains(t, entries[0].ContextMap()["error"], tc.wantMsg)
			}
		})
	}
}
