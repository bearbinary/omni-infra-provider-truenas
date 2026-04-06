package client

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserFriendlyError_Nil(t *testing.T) {
	assert.Equal(t, "", UserFriendlyError(nil))
}

func TestUserFriendlyError_NoSpace(t *testing.T) {
	err := &APIError{Code: ErrCodeNoSpace, Message: "[ENOSPC] pool is full"}
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "pool is full")
}

func TestUserFriendlyError_Denied(t *testing.T) {
	err := &APIError{Code: ErrCodeDenied, Message: "permission denied"}
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "permission denied")
}

func TestUserFriendlyError_InvalidNIC(t *testing.T) {
	err := &APIError{Code: ErrCodeInvalid, Message: "nic_attach: br999 not found"}
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "NIC attach target not found")
}

func TestUserFriendlyError_InvalidName(t *testing.T) {
	err := &APIError{Code: ErrCodeInvalid, Message: "vm_create.name: Only alphanumeric characters are allowed"}
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "Invalid VM name")
}

func TestUserFriendlyError_ConnectionError(t *testing.T) {
	err := fmt.Errorf("failed to send request (reconnect failed): connection refused")
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "TrueNAS is unreachable")
}

func TestUserFriendlyError_AuthError(t *testing.T) {
	err := fmt.Errorf("authentication failed: check TRUENAS_API_KEY")
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "authentication failed")
}

func TestUserFriendlyError_GenericAPIError(t *testing.T) {
	err := &APIError{Code: 99, Message: "something unexpected"}
	msg := UserFriendlyError(err)
	assert.Contains(t, msg, "something unexpected")
}
