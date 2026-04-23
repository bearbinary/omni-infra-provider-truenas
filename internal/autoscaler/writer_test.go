package autoscaler

import (
	"context"
	"errors"
	"testing"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/omni/client/api/omni/specs"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScaleWriter_IncreaseMachineCount_HappyPath pins the core
// write: current + delta becomes the new MachineCount on the Omni
// MachineSet, and the function returns the new size.
func TestScaleWriter_IncreaseMachineCount_HappyPath(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "10",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 3, false, specs.MachineSetSpec_MachineAllocation_Static)

	writer := NewScaleWriter(st)

	group := NodeGroup{
		ID:               "talos-home-workers",
		MachineClassName: "workers",
		CurrentSize:      3,
		Config:           &Config{Min: 1, Max: 10},
	}

	newSize, err := writer.IncreaseMachineCount(context.Background(), group, 2)
	require.NoError(t, err)
	assert.Equal(t, 5, newSize)

	// Verify the write actually landed in state.
	got, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "talos-home-workers")
	require.NoError(t, err)
	assert.Equal(t, uint32(5), got.TypedSpec().Value.MachineAllocation.MachineCount)
}

// TestScaleWriter_IncreaseMachineCount_RejectsNonPositiveDelta
// pins the defensive input check. Cluster-autoscaler should never
// send delta<=0 on a scale-up RPC, but if a sidecar bug or a future
// CAS version does, we must reject instead of degenerating the
// write semantics.
func TestScaleWriter_IncreaseMachineCount_RejectsNonPositiveDelta(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)
	writer := NewScaleWriter(st)

	group := NodeGroup{
		ID:          "x",
		CurrentSize: 3,
		Config:      &Config{Min: 1, Max: 10},
	}

	for _, delta := range []int{0, -1, -100} {
		_, err := writer.IncreaseMachineCount(context.Background(), group, delta)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "delta must be > 0")
	}
}

// TestScaleWriter_IncreaseMachineCount_RejectsAboveMax pins the
// upper-bound enforcement at pre-check time (before the state
// lookup). Returns ErrAtOrAboveMax so the gRPC handler can map to
// codes.ResourceExhausted — the signal cluster-autoscaler uses to
// stop retrying a capped node group.
func TestScaleWriter_IncreaseMachineCount_RejectsAboveMax(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "5",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 4, false, specs.MachineSetSpec_MachineAllocation_Static)

	writer := NewScaleWriter(st)

	group := NodeGroup{
		ID:          "talos-home-workers",
		CurrentSize: 4,
		Config:      &Config{Min: 1, Max: 5},
	}

	_, err := writer.IncreaseMachineCount(context.Background(), group, 3) // 4 + 3 = 7 > 5
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAtOrAboveMax), "error must wrap ErrAtOrAboveMax so gRPC handler can map to ResourceExhausted")
}

// TestScaleWriter_IncreaseMachineCount_RechecksMaxAgainstLiveState
// guards against the race where CurrentSize in the NodeGroup struct
// is stale by the time IncreaseMachineCount runs. The inner mutator
// re-reads MachineAllocation.MachineCount live and re-applies the
// Max check. Simulates the race by bumping MachineCount in state
// between NodeGroup capture and the write call.
func TestScaleWriter_IncreaseMachineCount_RechecksMaxAgainstLiveState(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "5",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 2, false, specs.MachineSetSpec_MachineAllocation_Static)

	writer := NewScaleWriter(st)

	// NodeGroup captures current=2 (the stale value).
	group := NodeGroup{
		ID:          "talos-home-workers",
		CurrentSize: 2,
		Config:      &Config{Min: 1, Max: 5},
	}

	// Simulate an out-of-band edit: someone scales the MachineSet to
	// 4 between our discovery read and our write.
	_, err := safe.StateUpdateWithConflicts[*omni.MachineSet](context.Background(), st,
		omni.NewMachineSet("talos-home-workers").Metadata(),
		func(ms *omni.MachineSet) error {
			ms.TypedSpec().Value.MachineAllocation.MachineCount = 4
			return nil
		})
	require.NoError(t, err)

	// Delta that looks fine against stale current (2+3=5) but not
	// against live (4+3=7 > 5).
	_, err = writer.IncreaseMachineCount(context.Background(), group, 3)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAtOrAboveMax), "live re-check must fire even when stale pre-check passed")
}

// TestScaleWriter_IncreaseMachineCount_MissingMachineSet surfaces
// the not-found case with a clear error. Shouldn't happen in normal
// operation (the MachineSet ID comes from the same Discoverer the
// caller already used), but a race against a cluster teardown could
// see the MachineSet vanish between discovery and write.
func TestScaleWriter_IncreaseMachineCount_MissingMachineSet(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)
	writer := NewScaleWriter(st)

	group := NodeGroup{
		ID:          "nonexistent-set",
		CurrentSize: 2,
		Config:      &Config{Min: 1, Max: 10},
	}

	_, err := writer.IncreaseMachineCount(context.Background(), group, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent-set")
}
