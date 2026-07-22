package autoscaler

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/omni/client/api/omni/specs"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestIsRotationLockActive covers the lock-format parser. Pins the
// "<gen>:<unix-ts>" shape the noderotation engine writes so a future
// schema change on either side has to update both — otherwise the
// autoscaler would silently stop pausing and start racing with the
// rotation engine.
func TestIsRotationLockActive(t *testing.T) {
	t.Parallel()

	now := time.Unix(1700000000, 0)

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "fresh lock — 30s ago",
			raw:  "abc12345:" + strconv.FormatInt(now.Add(-30*time.Second).Unix(), 10),
			want: true,
		},
		{
			name: "expired lock — 10 min ago",
			raw:  "abc12345:" + strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10),
			want: false,
		},
		{
			name: "malformed — no colon",
			raw:  "abc12345",
			want: false,
		},
		{
			name: "malformed — non-int timestamp",
			raw:  "abc12345:not-a-number",
			want: false,
		},
		{
			name: "empty string",
			raw:  "",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isRotationLockActive(tc.raw, now, rotationLockTTL))
		})
	}
}

// TestDiscover_PausesOnRotationLock verifies the autoscaler clamps
// Min/Max to CurrentSize for a MachineSet carrying a fresh
// rotation-state annotation. This is the contract surface that the
// noderotation reconciler relies on to prevent CAS from racing it.
func TestDiscover_PausesOnRotationLock(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "5",
	})

	ms := seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 3, false, specs.MachineSetSpec_MachineAllocation_Static)

	// Stamp the lock 30s ago — well within TTL.
	lockTS := time.Now().Add(-30 * time.Second).Unix()
	_, err := safe.StateUpdateWithConflicts[*omni.MachineSet](
		context.Background(), st,
		omni.NewMachineSet(ms.Metadata().ID()).Metadata(),
		func(m *omni.MachineSet) error {
			m.Metadata().Annotations().Set(rotationLockAnnotation, "abc12345:"+strconv.FormatInt(lockTS, 10))
			return nil
		},
	)
	require.NoError(t, err)

	d := NewDiscoverer(st, "talos-home", zaptest.NewLogger(t))

	groups, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)

	g := groups[0]
	// Paused groups report Min == Max == CurrentSize so CAS treats the
	// group as at-the-cap-and-at-the-floor for this refresh cycle.
	assert.Equal(t, g.CurrentSize, g.Config.Min, "Min should clamp to CurrentSize while rotation lock is held")
	assert.Equal(t, g.CurrentSize, g.Config.Max, "Max should clamp to CurrentSize while rotation lock is held")
}

// TestDiscover_IgnoresExpiredRotationLock verifies an expired lock is
// honored as absent. The TTL is the safety net: if the rotation
// reconciler crashed mid-step, the lock would otherwise freeze
// autoscaling forever.
func TestDiscover_IgnoresExpiredRotationLock(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "5",
	})

	ms := seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 3, false, specs.MachineSetSpec_MachineAllocation_Static)

	// Stamp the lock far enough in the past to be definitely-expired.
	staleTS := time.Now().Add(-2 * rotationLockTTL).Unix()
	_, err := safe.StateUpdateWithConflicts[*omni.MachineSet](
		context.Background(), st,
		omni.NewMachineSet(ms.Metadata().ID()).Metadata(),
		func(m *omni.MachineSet) error {
			m.Metadata().Annotations().Set(rotationLockAnnotation, "abc12345:"+strconv.FormatInt(staleTS, 10))
			return nil
		},
	)
	require.NoError(t, err)

	d := NewDiscoverer(st, "talos-home", zaptest.NewLogger(t))

	groups, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)

	g := groups[0]
	// No pause: original Min/Max preserved.
	assert.Equal(t, 1, g.Config.Min)
	assert.Equal(t, 5, g.Config.Max)
}
