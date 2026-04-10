package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateNFSShare_Success(t *testing.T) {
	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "sharing.nfs.create", method)
		assert.Contains(t, string(params), "/mnt/default/omni-nfs/cluster-abc")

		return NFSShare{ID: 1, Path: "/mnt/default/omni-nfs/cluster-abc", Enabled: true}, nil
	})

	share, err := c.CreateNFSShare(context.Background(), CreateNFSShareRequest{
		Path:    "/mnt/default/omni-nfs/cluster-abc",
		Comment: "Omni cluster: cluster-abc",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, share.ID)
	assert.Equal(t, "/mnt/default/omni-nfs/cluster-abc", share.Path)
}

func TestGetNFSShareByPath_Found(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "sharing.nfs.query", method)

		return []NFSShare{{ID: 5, Path: "/mnt/pool/nfs", Enabled: true}}, nil
	})

	share, err := c.GetNFSShareByPath(context.Background(), "/mnt/pool/nfs")
	require.NoError(t, err)
	require.NotNil(t, share)
	assert.Equal(t, 5, share.ID)
}

func TestGetNFSShareByPath_NotFound(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return []NFSShare{}, nil
	})

	share, err := c.GetNFSShareByPath(context.Background(), "/mnt/nonexistent")
	require.NoError(t, err)
	assert.Nil(t, share)
}

func TestDeleteNFSShare_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		assert.Equal(t, "sharing.nfs.delete", method)

		return nil, nil
	})

	err := c.DeleteNFSShare(context.Background(), 42)
	require.NoError(t, err)
}

func TestDeleteNFSShare_NotFound_NoError(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, &jsonRPCError{Code: ErrCodeNotFound, Message: "not found"}
	})

	err := c.DeleteNFSShare(context.Background(), 99)
	require.NoError(t, err, "deleting non-existent share should not error")
}

func TestListNFSShares_Success(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return []NFSShare{
			{ID: 1, Path: "/mnt/pool/share1"},
			{ID: 2, Path: "/mnt/pool/share2"},
		}, nil
	})

	shares, err := c.ListNFSShares(context.Background())
	require.NoError(t, err)
	assert.Len(t, shares, 2)
}

func TestEnsureNFSService_AlreadyRunning(t *testing.T) {
	var startCalled bool
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		switch method {
		case "service.query":
			return struct {
				State string `json:"state"`
			}{State: "RUNNING"}, nil
		case "service.start":
			startCalled = true

			return nil, nil
		}

		return nil, nil
	})

	err := c.EnsureNFSService(context.Background())
	require.NoError(t, err)
	assert.False(t, startCalled, "should not start service if already running")
}

func TestEnsureNFSService_NotRunning_Starts(t *testing.T) {
	var startCalled bool
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		switch method {
		case "service.query":
			return struct {
				State string `json:"state"`
			}{State: "STOPPED"}, nil
		case "service.start":
			startCalled = true

			return nil, nil
		}

		return nil, nil
	})

	err := c.EnsureNFSService(context.Background())
	require.NoError(t, err)
	assert.True(t, startCalled, "should start service when not running")
}
