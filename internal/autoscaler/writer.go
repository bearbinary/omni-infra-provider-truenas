package autoscaler

import (
	"context"
	"errors"
	"fmt"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
)

// ScaleWriter performs the `MachineAllocation.MachineCount` write
// that actually grows a node group. Decoupled from the gRPC handler
// so tests can exercise the write semantics (conflict retry,
// out-of-bounds reject) without going through a full listener.
//
// Kept as a thin struct over the state client rather than free
// functions so phase 4's Helm-chart-driven deploy can swap in a
// rate-limited / retry-wrapped variant via the same interface if we
// need it.
type ScaleWriter struct {
	st state.State
}

// NewScaleWriter constructs a ScaleWriter bound to an Omni state
// client. The passed state must be writeable — the autoscaler's
// Omni client is constructed with full-access credentials because
// MachineAllocation updates require Admin scope on the service
// account.
func NewScaleWriter(st state.State) *ScaleWriter {
	return &ScaleWriter{st: st}
}

// ErrAtOrAboveMax is returned when IncreaseMachineCount is called
// with a delta that would push the target above the node group's
// configured max. Exported so the gRPC handler can map it to
// codes.ResourceExhausted, which is the signal cluster-autoscaler
// uses to stop retrying a full node group and try a different one.
var ErrAtOrAboveMax = errors.New("target size would exceed node group max")

// ErrBelowMin is returned when IncreaseMachineCount is called on a
// node group whose current size is already below min. Shouldn't
// happen in normal operation (discovery reports such groups so CAS
// corrects them), but explicit sentinel makes the write path self-
// documenting.
var ErrBelowMin = errors.New("current size is below node group min — cluster-autoscaler should correct before scaling")

// IncreaseMachineCount writes MachineAllocation.MachineCount =
// currentSize + delta for the given MachineSet, conditioned on:
//
//   - Delta > 0 (caller responsibility; we reject otherwise).
//   - Current + delta <= group.Config.Max.
//   - The MachineSet's version hasn't changed between the discovery
//     read and this write (UpdateWithConflicts handles this — if
//     another writer races us, we surface the error and let the
//     caller decide whether to retry).
//
// Returns the new target size on success. Does NOT run the capacity
// gate — callers MUST invoke CheckCapacity separately before calling
// this method so gate failures emit the right error codes and
// metrics without entangling the write path.
func (w *ScaleWriter) IncreaseMachineCount(ctx context.Context, group NodeGroup, delta int) (int, error) {
	if delta <= 0 {
		return 0, fmt.Errorf("delta must be > 0, got %d", delta)
	}

	if group.Config == nil {
		return 0, fmt.Errorf("group %q: missing autoscaler config — discovery bug", group.ID)
	}

	newSize := group.CurrentSize + delta
	if newSize > group.Config.Max {
		return 0, fmt.Errorf("%w: group %q current=%d delta=%d would give %d, max=%d",
			ErrAtOrAboveMax, group.ID, group.CurrentSize, delta, newSize, group.Config.Max)
	}

	ptr := omni.NewMachineSet(group.ID).Metadata()

	updated, err := safe.StateUpdateWithConflicts[*omni.MachineSet](ctx, w.st, ptr, func(ms *omni.MachineSet) error {
		spec := ms.TypedSpec().Value

		if spec.MachineAllocation == nil {
			return fmt.Errorf("group %q: MachineAllocation missing on MachineSet — out-of-band edit?", group.ID)
		}

		// Re-check the Max bound against the live MachineCount inside
		// the mutator. Between discovery.Read and this callback the
		// MachineCount could have been changed by a manual edit; we
		// must not scale above Max even if our pre-check approved the
		// delta based on a stale current.
		liveCurrent := int(spec.MachineAllocation.MachineCount)

		liveNew := liveCurrent + delta
		if liveNew > group.Config.Max {
			return fmt.Errorf("%w: group %q live_current=%d delta=%d would give %d, max=%d",
				ErrAtOrAboveMax, group.ID, liveCurrent, delta, liveNew, group.Config.Max)
		}

		spec.MachineAllocation.MachineCount = uint32(liveNew)

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("update MachineSet %q: %w", group.ID, err)
	}

	return int(updated.TypedSpec().Value.MachineAllocation.MachineCount), nil
}
