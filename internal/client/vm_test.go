package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testVMName = "omni-test-vm"

func TestCreateVM_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.create", method)

		return VM{ID: 42, Name: testVMName, VCPUs: 2, Memory: 4096}, nil
	})

	vm, err := c.CreateVM(context.Background(), CreateVMRequest{
		Name:       testVMName,
		VCPUs:      2,
		Memory:     4096,
		Bootloader: "UEFI",
	})

	require.NoError(t, err)
	assert.Equal(t, 42, vm.ID)
	assert.Equal(t, testVMName, vm.Name)
}

func TestGetVM_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, methodVMQuery, method)

		return VM{ID: 42, Name: "omni-test", Status: VMStatus{State: "RUNNING"}}, nil
	})

	vm, err := c.GetVM(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, "RUNNING", vm.Status.State)
}

func TestStartVM_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.start", method)

		return true, nil
	})

	err := c.StartVM(context.Background(), 42)
	require.NoError(t, err)
}

// TestRunningGuestsMemoryMiB sums RUNNING guests only. STOPPED guests don't
// hold host memory until they boot, so including them in the pre-flight
// would refuse provisioning on hosts that are nominally over-committed but
// fine in practice (the dominant homelab pattern: one or two big VMs
// usually-stopped, plus a fleet of small always-on ones).
func TestRunningGuestsMemoryMiB_OnlyCountsRunning(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, methodVMQuery, method)

		return []VM{
			{ID: 1, Name: "running-a", Memory: 4096, Status: VMStatus{State: "RUNNING"}},
			{ID: 2, Name: "stopped-b", Memory: 8192, Status: VMStatus{State: "STOPPED"}},
			{ID: 3, Name: "running-c", Memory: 2048, Status: VMStatus{State: "RUNNING"}},
		}, nil
	})

	total, err := c.RunningGuestsMemoryMiB(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(6144), total, "should sum 4096 + 2048 (RUNNING only)")
}

func TestRunningGuestsMemoryMiB_EmptyHost(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return []VM{}, nil
	})

	total, err := c.RunningGuestsMemoryMiB(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
}

// TestIsNoMemory pins both detection paths: TrueNAS error code 12 (the
// well-formed case) and the message-based fallback (observed in the wild
// on TrueNAS 25.04, where libvirt errors arrive with a different code but
// the [ENOMEM] string intact).
func TestIsNoMemory(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"code 12 ENOMEM", &APIError{Code: ErrCodeNoMemory, Message: "kvm out of memory"}, true},
		{"message [ENOMEM]", &APIError{Code: 1, Message: "[ENOMEM] Cannot guarantee memory for guest foo"}, true},
		{"message Cannot guarantee", &APIError{Code: 1, Message: "Cannot guarantee memory for guest foo"}, true},
		{"code 28 ENOSPC is not ENOMEM", &APIError{Code: ErrCodeNoSpace, Message: "no space"}, false},
		{"non-API error", assert.AnError, false},
		{"nil", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsNoMemory(tc.err))
		})
	}
}

func TestStopVM_Force(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.stop", method)

		// params should be [42, {"force": true}]
		assert.Contains(t, string(params), `"force":true`)

		return true, nil
	})

	err := c.StopVM(context.Background(), 42, true)
	require.NoError(t, err)
}

func TestDeleteVM_Success(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.delete", method)

		// Pin the exact param shape TrueNAS 25.10 accepts. A prior version
		// shipped {"force":true, "force_after_timeout":true} and was rejected
		// with EINVAL "Extra inputs are not permitted", stopping every VM it
		// tried to delete without ever completing the delete.
		var payload []json.RawMessage
		require.NoError(t, json.Unmarshal(params, &payload))
		require.Len(t, payload, 2, "vm.delete expects [id, opts]")
		assert.JSONEq(t, `{"force":true}`, string(payload[1]))

		return true, nil
	})

	err := c.DeleteVM(context.Background(), 42)
	require.NoError(t, err)
}

func TestDeleteVM_NotFound(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, notFoundErr()
	})

	err := c.DeleteVM(context.Background(), 42)
	require.NoError(t, err) // not found is not an error on delete
}

func TestFindVMByName_Found(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, methodVMQuery, method)

		return []VM{{ID: 2, Name: "omni-target"}}, nil
	})

	vm, err := c.FindVMByName(context.Background(), "omni-target")
	require.NoError(t, err)
	require.NotNil(t, vm)
	assert.Equal(t, 2, vm.ID)
}

func TestFindVMByName_NotFound(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return []VM{}, nil
	})

	vm, err := c.FindVMByName(context.Background(), "omni-missing")
	require.NoError(t, err)
	assert.Nil(t, vm)
}

func TestListVMs(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, methodVMQuery, method)

		return []VM{{ID: 1, Name: "vm-1"}, {ID: 2, Name: "vm-2"}}, nil
	})

	vms, err := c.ListVMs(context.Background())
	require.NoError(t, err)
	assert.Len(t, vms, 2)
}
