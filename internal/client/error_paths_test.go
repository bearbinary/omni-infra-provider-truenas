package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateVM_InvalidName(t *testing.T) {
	c := newMockClient(t, func(method string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, &jsonRPCError{Code: ErrCodeInvalid, Message: "vm_create.name: Only alphanumeric characters are allowed"}
	})

	_, err := c.CreateVM(context.Background(), CreateVMRequest{
		Name: "invalid-name-with-hyphens",
	})
	assert.Error(t, err)

	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "Invalid VM name")
}

func TestCreateZvol_NoSpace(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, &jsonRPCError{Code: ErrCodeNoSpace, Message: "[ENOSPC] pool is full"}
	})

	_, err := c.CreateZvol(context.Background(), "tank/test", 999)
	assert.Error(t, err)

	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "pool is full")
}

func TestAddNIC_InvalidAttach(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, &jsonRPCError{Code: ErrCodeInvalid, Message: "nic_attach: br999 not found"}
	})

	_, err := c.AddNIC(context.Background(), 1, "br999")
	assert.Error(t, err)

	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "network interface not found")
}

func TestGetVM_Unauthorized(t *testing.T) {
	c := newMockClient(t, func(_ string, _ json.RawMessage) (any, *jsonRPCError) {
		return nil, &jsonRPCError{Code: ErrCodeDenied, Message: "permission denied"}
	})

	_, err := c.GetVM(context.Background(), 1)
	assert.Error(t, err)

	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "permission denied")
}

func TestPing_ConnectionRefused(t *testing.T) {
	msg := UserFriendlyError(assert.AnError)
	assert.NotEmpty(t, msg, "should return the error message for unknown errors")
}

func TestIsAlreadyExists_EFAULT(t *testing.T) {
	// TrueNAS sometimes returns EFAULT (14) with "already exists" message
	err := &APIError{Code: 14, Message: "Failed to create dataset: cannot create 'tank/test': dataset already exists"}
	assert.True(t, IsAlreadyExists(err), "EFAULT with 'already exists' message should be treated as already exists")
}

func TestMultipleErrorTypes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{"not found", &APIError{Code: ErrCodeNotFound, Message: "not found"}, "not found"},
		{"already exists", &APIError{Code: ErrCodeExists, Message: "exists"}, "exists"},
		{"no space", &APIError{Code: ErrCodeNoSpace, Message: "no space"}, "pool is full"},
		{"denied", &APIError{Code: ErrCodeDenied, Message: "denied"}, "permission denied"},
		{"unknown code", &APIError{Code: 999, Message: "something broke"}, "something broke"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := UserFriendlyError(tt.err)
			assert.Contains(t, msg, tt.contains)
		})
	}
}

func TestRateLimitSemaphore_NotNil(t *testing.T) {
	c := NewMockClient(func(_ string, _ json.RawMessage) (any, error) {
		return nil, nil
	})

	require.NotNil(t, c.semaphore, "client should have semaphore initialized")
	assert.Equal(t, defaultMaxConcurrentCalls, cap(c.semaphore))
}

func TestCustomMaxConcurrent(t *testing.T) {
	c := newClient(&MockTransport{}, 16)
	assert.Equal(t, 16, cap(c.semaphore))
}

func TestZeroMaxConcurrent_UsesDefault(t *testing.T) {
	c := newClient(&MockTransport{}, 0)
	assert.Equal(t, defaultMaxConcurrentCalls, cap(c.semaphore))
}
