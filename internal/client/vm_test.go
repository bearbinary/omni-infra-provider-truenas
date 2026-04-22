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
