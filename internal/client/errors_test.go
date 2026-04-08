package client

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserFriendlyError_Nil(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", UserFriendlyError(nil))
}

func TestUserFriendlyError_NoSpace(t *testing.T) {
	t.Parallel()
	err := &APIError{Code: ErrCodeNoSpace, Message: "[ENOSPC] pool is full"}
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "pool is full")
}

func TestUserFriendlyError_Denied(t *testing.T) {
	t.Parallel()
	err := &APIError{Code: ErrCodeDenied, Message: "permission denied"}
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "permission denied")
}

func TestUserFriendlyError_InvalidNIC(t *testing.T) {
	t.Parallel()
	err := &APIError{Code: ErrCodeInvalid, Message: "nic_attach: br999 not found"}
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "network interface not found")
}

func TestUserFriendlyError_InvalidName(t *testing.T) {
	t.Parallel()
	err := &APIError{Code: ErrCodeInvalid, Message: "vm_create.name: Only alphanumeric characters are allowed"}
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "Invalid VM name")
}

func TestUserFriendlyError_ConnectionError(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("failed to send request (reconnect failed): connection refused")
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "TrueNAS is unreachable")
}

func TestUserFriendlyError_AuthError(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("authentication failed: check TRUENAS_API_KEY")
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "authentication failed")
}

func TestUserFriendlyError_InvalidGeneric(t *testing.T) {
	t.Parallel()
	err := &APIError{Code: ErrCodeInvalid, Message: "invalid value for field xyz"}
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "Invalid configuration")
}

func TestUserFriendlyError_AuthKeyword(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("auth: token expired")
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "authentication failed")
}

func TestUserFriendlyError_GenericAPIError(t *testing.T) {
	t.Parallel()
	err := &APIError{Code: 99, Message: "something unexpected"}
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "something unexpected")
}

func TestUserFriendlyError_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		err     error
		contain string
	}{
		{"nil", nil, ""},
		{"no space", &APIError{Code: ErrCodeNoSpace, Message: "pool full"}, "pool is full"},
		{"denied", &APIError{Code: ErrCodeDenied, Message: "denied"}, "permission denied"},
		{"invalid NIC", &APIError{Code: ErrCodeInvalid, Message: "nic_attach: not found"}, "network interface not found"},
		{"invalid NIC caps", &APIError{Code: ErrCodeInvalid, Message: "NIC not configured"}, "network interface not found"},
		{"invalid name", &APIError{Code: ErrCodeInvalid, Message: "name: bad chars"}, "Invalid VM name"},
		{"invalid generic", &APIError{Code: ErrCodeInvalid, Message: "bad field"}, "Invalid configuration"},
		{"unknown code", &APIError{Code: 42, Message: "custom error"}, "custom error"},
		{"reconnect failed", fmt.Errorf("reconnect failed: conn refused"), "TrueNAS is unreachable"},
		{"failed to connect", fmt.Errorf("failed to connect to host"), "TrueNAS is unreachable"},
		{"auth keyword", fmt.Errorf("auth failed"), "authentication failed"},
		{"authentication keyword", fmt.Errorf("authentication error"), "authentication failed"},
		{"plain error", fmt.Errorf("something random"), "something random"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := UserFriendlyError(tc.err)
			if tc.contain == "" {
				assert.Equal(t, "", msg)
			} else {
				assert.Contains(t, msg, tc.contain)
			}
		})
	}
}
