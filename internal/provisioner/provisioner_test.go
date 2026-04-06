package provisioner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProvisioner_TrackingConcurrency(t *testing.T) {
	p := NewProvisioner(nil, ProviderConfig{DefaultPool: "tank"})

	// Track some resources
	p.TrackImageID("abc123")
	p.TrackImageID("def456")
	p.TrackVMName("omni_test_vm_1")
	p.TrackVMName("omni_test_vm_2")

	// Verify snapshots
	imageIDs := p.ActiveImageIDs()
	assert.True(t, imageIDs["abc123"])
	assert.True(t, imageIDs["def456"])
	assert.False(t, imageIDs["nonexistent"])

	vmNames := p.ActiveVMNames()
	assert.True(t, vmNames["omni_test_vm_1"])
	assert.True(t, vmNames["omni_test_vm_2"])

	// Untrack
	p.UntrackVMName("omni_test_vm_1")
	vmNames = p.ActiveVMNames()
	assert.False(t, vmNames["omni_test_vm_1"])
	assert.True(t, vmNames["omni_test_vm_2"])

	// Snapshot is independent of original map
	p.TrackImageID("ghi789")
	assert.False(t, imageIDs["ghi789"])          // old snapshot
	assert.True(t, p.ActiveImageIDs()["ghi789"]) // new snapshot
}
