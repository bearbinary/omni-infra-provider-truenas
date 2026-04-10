package provisioner

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

func TestProvisioner_ConcurrentTrackAndRead(t *testing.T) {
	p := NewProvisioner(nil, ProviderConfig{DefaultPool: "tank"})

	// Race detector will catch unsafe concurrent access
	var wg sync.WaitGroup

	// Writer goroutines
	for i := range 100 {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			name := fmt.Sprintf("omni_vm_%d", idx)
			p.TrackVMName(name)
			p.TrackImageID(fmt.Sprintf("image_%d", idx))

			if idx%3 == 0 {
				p.UntrackVMName(name)
			}
		}(i)
	}

	// Reader goroutines (concurrent with writers)
	for range 50 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_ = p.ActiveVMNames()
			_ = p.ActiveImageIDs()
		}()
	}

	wg.Wait()

	// Verify final state is consistent
	vms := p.ActiveVMNames()
	images := p.ActiveImageIDs()
	assert.NotNil(t, vms)
	assert.NotNil(t, images)
	assert.Equal(t, 100, len(images))
	// ~67 VMs should remain (100 - 34 that were untracked where idx%3==0)
	assert.InDelta(t, 66, len(vms), 2)
}

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

func TestProvisionSteps_ReturnsCorrectSteps(t *testing.T) {
	t.Parallel()

	p := NewProvisioner(nil, ProviderConfig{DefaultPool: "tank"})
	steps := p.ProvisionSteps()

	assert.Len(t, steps, 5, "should return exactly 5 provision steps")

	expectedNames := []string{
		"createSchematic",
		"uploadISO",
		"createVM",
		"configureStorage",
		"healthCheck",
	}

	for i, step := range steps {
		assert.Equal(t, expectedNames[i], step.Name(), "step %d should be %q", i, expectedNames[i])
	}
}

func TestIsAlreadyExists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{"nil error", nil, false},
		{"EEXIST code", &client.APIError{Code: client.ErrCodeExists, Message: "already exists"}, true},
		{"message contains already exists", &client.APIError{Code: 14, Message: "dataset already exists"}, true},
		{"message contains Already exists", &client.APIError{Code: 11, Message: "Already exists"}, true},
		{"unrelated error", &client.APIError{Code: 99, Message: "something else"}, false},
		{"non-API error", fmt.Errorf("connection refused"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expect, isAlreadyExists(tc.err))
		})
	}
}

func TestIsNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{"nil error", nil, false},
		{"ENOENT code", &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}, true},
		{"different code", &client.APIError{Code: 99, Message: "not found"}, false},
		{"non-API error", fmt.Errorf("not found"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expect, isNotFound(tc.err))
		})
	}
}
