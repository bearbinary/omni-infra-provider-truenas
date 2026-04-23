package autoscaler

import (
	"context"
	"testing"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/omni/client/api/omni/specs"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// newInMemOmniState builds the same style of COSI state used by the
// singleton integration tests (state.WrapCore over a namespaced
// in-memory backend). Every test gets a fresh state so there's no
// cross-test bleed.
func newInMemOmniState(t *testing.T) state.State {
	t.Helper()

	return state.WrapCore(namespaced.NewState(inmem.Build))
}

// seedMachineClass creates a MachineClass in the Omni default
// namespace with the provided autoscaler annotations.
func seedMachineClass(t *testing.T, st state.State, name string, annotations map[string]string) {
	t.Helper()

	mc := omni.NewMachineClass(name)

	for k, v := range annotations {
		mc.Metadata().Annotations().Set(k, v)
	}

	require.NoError(t, st.Create(context.Background(), mc))
}

// seedMachineSet creates a MachineSet with the given cluster label,
// role (true for CP), and allocation spec. The MachineSet ID comes
// from cluster+name so tests can reference it back cleanly.
func seedMachineSet(t *testing.T, st state.State, cluster, id, machineClass string, count uint32, isCP bool, allocType specs.MachineSetSpec_MachineAllocation_Type) *omni.MachineSet {
	t.Helper()

	ms := omni.NewMachineSet(id)
	ms.Metadata().Labels().Set(omni.LabelCluster, cluster)

	if isCP {
		ms.Metadata().Labels().Set(omni.LabelControlPlaneRole, "")
	} else {
		ms.Metadata().Labels().Set(omni.LabelWorkerRole, "")
	}

	ms.TypedSpec().Value.MachineAllocation = &specs.MachineSetSpec_MachineAllocation{
		Name:           machineClass,
		MachineCount:   count,
		AllocationType: allocType,
	}

	require.NoError(t, st.Create(context.Background(), ms))

	return ms
}

// TestDiscover_EmptyCluster returns zero NodeGroups, not an error,
// when the cluster has no MachineSets. The cluster-autoscaler sidecar
// treats an empty list as normal.
func TestDiscover_EmptyCluster(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)
	d := NewDiscoverer(st, "talos-home", zaptest.NewLogger(t))

	groups, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, groups)
}

// TestDiscover_IgnoresOtherClusters ensures label-query scoping is
// correct — only MachineSets labeled with the target cluster should
// appear in the result.
func TestDiscover_IgnoresOtherClusters(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "home-workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "5",
	})
	seedMachineClass(t, st, "preview-workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "5",
	})

	seedMachineSet(t, st, "talos-home", "talos-home-workers", "home-workers", 2, false, specs.MachineSetSpec_MachineAllocation_Static)
	seedMachineSet(t, st, "talos-preview", "talos-preview-workers", "preview-workers", 2, false, specs.MachineSetSpec_MachineAllocation_Static)

	d := NewDiscoverer(st, "talos-home", zaptest.NewLogger(t))

	groups, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "talos-home-workers", groups[0].ID)
}

// TestDiscover_SkipsControlPlaneRole pins the no-CP-autoscaling rule.
// The gRPC surface would accept CP MachineSets if we returned them,
// but cluster-autoscaler's scheduling logic assumes only workers grow.
func TestDiscover_SkipsControlPlaneRole(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "home-cp", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "3",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-control-planes", "home-cp", 3, true, specs.MachineSetSpec_MachineAllocation_Static)

	d := NewDiscoverer(st, "talos-home", zaptest.NewLogger(t))

	groups, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, groups, "CP MachineSets must never appear in node-group discovery even when annotated")
}

// TestDiscover_SkipsNonOptedInMachineClass verifies the opt-in
// semantics. A worker MachineSet that references a MachineClass
// without the autoscale-* annotations is legitimately non-autoscaled.
func TestDiscover_SkipsNonOptedInMachineClass(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "plain-workers", map[string]string{})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "plain-workers", 2, false, specs.MachineSetSpec_MachineAllocation_Static)

	d := NewDiscoverer(st, "talos-home", zaptest.NewLogger(t))

	groups, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, groups)
}

// TestDiscover_RejectsUnlimitedAllocation pins the requirement from
// rothgar's upstream README — only Static machine allocation is
// supported. Unlimited gets skipped with a structured warning log
// (not a Discover error) so the rest of the cluster's node groups
// still come back.
func TestDiscover_RejectsUnlimitedAllocation(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "unlimited-workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "5",
	})
	seedMachineClass(t, st, "static-workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "5",
	})

	seedMachineSet(t, st, "talos-home", "talos-home-unlimited", "unlimited-workers", 10, false, specs.MachineSetSpec_MachineAllocation_Unlimited)
	seedMachineSet(t, st, "talos-home", "talos-home-static", "static-workers", 2, false, specs.MachineSetSpec_MachineAllocation_Static)

	d := NewDiscoverer(st, "talos-home", zaptest.NewLogger(t))

	groups, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "talos-home-static", groups[0].ID, "only Static-allocation MachineSets should survive discovery")
}

// TestDiscover_BadAnnotationsSkipsOne verifies one malformed
// annotation on one MachineClass doesn't kill discovery for the
// whole cluster — a production value, since an operator editing
// annotations shouldn't accidentally disable scaling on unrelated
// MachineSets.
func TestDiscover_BadAnnotationsSkipsOne(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "broken", map[string]string{
		AnnotationAutoscaleMin: "not-a-number",
		AnnotationAutoscaleMax: "5",
	})
	seedMachineClass(t, st, "good", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "5",
	})

	seedMachineSet(t, st, "talos-home", "talos-home-broken", "broken", 2, false, specs.MachineSetSpec_MachineAllocation_Static)
	seedMachineSet(t, st, "talos-home", "talos-home-good", "good", 2, false, specs.MachineSetSpec_MachineAllocation_Static)

	d := NewDiscoverer(st, "talos-home", zaptest.NewLogger(t))

	groups, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "talos-home-good", groups[0].ID)
}

// TestDiscover_PopulatesConfig verifies the parsed config reaches
// the NodeGroup struct intact — this is the handoff point between
// annotation parsing and the gRPC server's handlers.
func TestDiscover_PopulatesConfig(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "tunable-workers", map[string]string{
		AnnotationAutoscaleMin:            "2",
		AnnotationAutoscaleMax:            "8",
		AnnotationAutoscaleCapacityGate:   "soft",
		AnnotationAutoscaleMinPoolFreeGiB: "250",
		AnnotationAutoscaleMinHostMemGiB:  "0",
	})

	seedMachineSet(t, st, "talos-home", "talos-home-workers", "tunable-workers", 4, false, specs.MachineSetSpec_MachineAllocation_Static)

	d := NewDiscoverer(st, "talos-home", zaptest.NewLogger(t))

	groups, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)

	g := groups[0]
	assert.Equal(t, "talos-home-workers", g.ID)
	assert.Equal(t, "tunable-workers", g.MachineClassName)
	assert.Equal(t, 4, g.CurrentSize)

	require.NotNil(t, g.Config)
	assert.Equal(t, 2, g.Config.Min)
	assert.Equal(t, 8, g.Config.Max)
	assert.Equal(t, CapacityGateSoft, g.Config.CapacityGate)
	assert.Equal(t, 250, g.Config.MinPoolFreeGiB)
	assert.Equal(t, 0, g.Config.MinHostMemGiB)
}

// TestDiscover_MissingMachineClassSkips handles a MachineSet that
// references a MachineClass that no longer exists (e.g., operator
// deleted the class before removing the MachineSet). Must skip the
// MachineSet with a warn log rather than failing discovery for the
// whole cluster.
func TestDiscover_MissingMachineClassSkips(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "present", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "5",
	})

	seedMachineSet(t, st, "talos-home", "talos-home-ghost", "ghost-class", 2, false, specs.MachineSetSpec_MachineAllocation_Static)
	seedMachineSet(t, st, "talos-home", "talos-home-real", "present", 2, false, specs.MachineSetSpec_MachineAllocation_Static)

	d := NewDiscoverer(st, "talos-home", zaptest.NewLogger(t))

	groups, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "talos-home-real", groups[0].ID)
}

// TestDiscover_CurrentSizeOutOfBoundsStillIncluded verifies the node
// group comes back even when the current count is outside the
// [min, max] window. Cluster-autoscaler needs to know about it to
// correct it toward the window — silently dropping would strand the
// MachineSet.
func TestDiscover_CurrentSizeOutOfBoundsStillIncluded(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "tight", map[string]string{
		AnnotationAutoscaleMin: "2",
		AnnotationAutoscaleMax: "4",
	})
	// current = 10, outside [2,4]
	seedMachineSet(t, st, "talos-home", "talos-home-fat", "tight", 10, false, specs.MachineSetSpec_MachineAllocation_Static)

	d := NewDiscoverer(st, "talos-home", zaptest.NewLogger(t))

	groups, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, 10, groups[0].CurrentSize,
		"out-of-bounds MachineSet must still be reported so CAS can correct it")
}
