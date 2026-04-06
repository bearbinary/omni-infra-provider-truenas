package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateDevice_Success(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.device.update", method)
		assert.Contains(t, string(params), `"path":"/mnt/new.iso"`)

		return Device{ID: 1, VM: 42, Attributes: map[string]any{"dtype": "CDROM", "path": "/mnt/new.iso"}}, nil
	})

	dev, err := c.UpdateDevice(context.Background(), 1, map[string]any{
		"dtype": "CDROM",
		"path":  "/mnt/new.iso",
	})
	require.NoError(t, err)
	assert.Equal(t, "/mnt/new.iso", dev.Attributes["path"])
}

func TestListDevices_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.device.query", method)

		return []Device{
			{ID: 1, VM: 42, Attributes: map[string]any{"dtype": "CDROM"}},
			{ID: 2, VM: 42, Attributes: map[string]any{"dtype": "DISK"}},
			{ID: 3, VM: 42, Attributes: map[string]any{"dtype": "NIC"}},
		}, nil
	})

	devices, err := c.ListDevices(context.Background(), 42)
	require.NoError(t, err)
	assert.Len(t, devices, 3)
}

func TestFindCDROM_Found(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return []Device{
			{ID: 1, VM: 42, Attributes: map[string]any{"dtype": "DISK"}},
			{ID: 2, VM: 42, Attributes: map[string]any{"dtype": "CDROM", "path": "/mnt/old.iso"}},
		}, nil
	})

	cdrom, err := c.FindCDROM(context.Background(), 42)
	require.NoError(t, err)
	require.NotNil(t, cdrom)
	assert.Equal(t, 2, cdrom.ID)
	assert.Equal(t, "CDROM", cdrom.Attributes["dtype"])
}

func TestFindCDROM_NotFound(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return []Device{
			{ID: 1, VM: 42, Attributes: map[string]any{"dtype": "DISK"}},
		}, nil
	})

	cdrom, err := c.FindCDROM(context.Background(), 42)
	require.NoError(t, err)
	assert.Nil(t, cdrom)
}

func TestSwapCDROM_UpdateExisting(t *testing.T) {
	callCount := 0
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		callCount++
		if method == "vm.device.query" {
			return []Device{
				{ID: 5, VM: 42, Attributes: map[string]any{"dtype": "CDROM", "path": "/mnt/old.iso"}},
			}, nil
		}

		if method == "vm.device.update" {
			return Device{ID: 5, VM: 42, Attributes: map[string]any{"dtype": "CDROM", "path": "/mnt/new.iso"}}, nil
		}

		return nil, nil
	})

	dev, err := c.SwapCDROM(context.Background(), 42, "/mnt/new.iso")
	require.NoError(t, err)
	assert.Equal(t, 5, dev.ID)
}

func TestSwapCDROM_AddNew(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		if method == "vm.device.query" {
			return []Device{
				{ID: 1, VM: 42, Attributes: map[string]any{"dtype": "DISK"}},
			}, nil
		}

		if method == "vm.device.create" {
			return Device{ID: 10, VM: 42, Attributes: map[string]any{"dtype": "CDROM", "path": "/mnt/new.iso"}}, nil
		}

		return nil, nil
	})

	dev, err := c.SwapCDROM(context.Background(), 42, "/mnt/new.iso")
	require.NoError(t, err)
	assert.Equal(t, 10, dev.ID)
}

func TestResetVMNVRAM_Success(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "vm.update", method)
		assert.Contains(t, string(params), `"remove_nvram":true`)

		return nil, nil
	})

	err := c.ResetVMNVRAM(context.Background(), 42)
	require.NoError(t, err)
}
