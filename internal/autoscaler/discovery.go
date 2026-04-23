package autoscaler

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/omni/client/api/omni/specs"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"go.uber.org/zap"
)

// NodeGroup is the parsed, validated, and role-filtered view of a
// single autoscaler-managed MachineSet. One NodeGroup per MachineSet
// that (1) belongs to the configured cluster, (2) is a worker (CP
// scaling is out of scope), (3) uses a static MachineAllocation, and
// (4) references a MachineClass carrying the
// `bearbinary.com/autoscale-*` annotations.
//
// The fields here are the minimum the gRPC handlers need to answer
// NodeGroups / NodeGroupIncreaseSize. Deliberately does not expose
// the raw MachineSet/MachineClass — callers that need those can
// look them back up via ID to keep the surface narrow.
type NodeGroup struct {
	// ID is the MachineSet resource ID (e.g., "talos-home-workers").
	// The external-gRPC protocol uses this as the node-group key.
	ID string

	// MachineClassName is the MachineClass resource ID the MachineSet
	// allocates from. Stored so the gRPC server can re-read the
	// annotations each tick without re-running the full cluster scan.
	MachineClassName string

	// Pool is the TrueNAS pool backing this node group's VMs. Read
	// from the MachineClass spec's provider-data payload when
	// available; empty when the MachineClass doesn't declare a pool
	// (capacity gate falls back to the autoscaler's
	// DEFAULT_POOL / DEFAULT_POOL_AUTOSCALER env var in later phases).
	Pool string

	// CurrentSize is the MachineAllocation.MachineCount from when
	// Discover ran. The gRPC server uses this as the "target size"
	// answer for NodeGroupTargetSize.
	CurrentSize int

	// Config is the parsed autoscaling configuration from the
	// MachineClass annotations. Populated by
	// ParseMachineClassAutoscaleConfig; non-nil for every NodeGroup
	// that Discover returns.
	Config *Config
}

// Discoverer resolves a cluster's autoscaler-managed MachineSets from
// an Omni state source. Kept as a struct (rather than a free
// function) so Phase 3c's gRPC handlers can hold a reference and
// periodically refresh without re-plumbing the state/logger/cluster
// parameters.
type Discoverer struct {
	st      state.State
	cluster string
	logger  *zap.Logger
}

// NewDiscoverer constructs a Discoverer scoped to one Omni cluster.
// The state argument is the Omni client's COSI state
// (omni.State().Omni()); tests pass a `state.WrapCore(inmem.NewState)`
// with pre-seeded MachineSet + MachineClass fixtures.
func NewDiscoverer(st state.State, cluster string, logger *zap.Logger) *Discoverer {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Discoverer{
		st:      st,
		cluster: cluster,
		logger:  logger,
	}
}

// Discover enumerates autoscaler-managed node groups for the
// configured cluster. Per-MachineSet parsing errors are logged at
// warn and the offending MachineSet is skipped — one bad annotation
// should never take out scaling for the whole cluster, and the
// cluster-autoscaler sidecar treats a shrinking NodeGroups response
// as normal (it re-polls on every refresh).
//
// Returns an error only on total enumeration failure (e.g., Omni API
// unreachable) — that's the signal that warrants an Unavailable gRPC
// status rather than an empty list.
func (d *Discoverer) Discover(ctx context.Context) ([]NodeGroup, error) {
	sets, err := safe.StateListAll[*omni.MachineSet](
		ctx,
		d.st,
		state.WithLabelQuery(resource.LabelEqual(omni.LabelCluster, d.cluster)),
	)
	if err != nil {
		return nil, fmt.Errorf("list MachineSets for cluster %q: %w", d.cluster, err)
	}

	groups := make([]NodeGroup, 0, sets.Len())

	for i := 0; i < sets.Len(); i++ {
		ms := sets.Get(i)

		group, err := d.classify(ctx, ms)
		if err != nil {
			d.logger.Warn("skipping MachineSet: classification failed",
				zap.String("machineset", ms.Metadata().ID()),
				zap.Error(err),
			)

			continue
		}

		if group == nil {
			// Not opted in — expected for most MachineSets. Debug
			// level so operators can confirm the discovery loop sees
			// the MachineSet but recognizes it as out-of-scope.
			d.logger.Debug("skipping MachineSet: not opted in to autoscaling",
				zap.String("machineset", ms.Metadata().ID()),
			)

			continue
		}

		groups = append(groups, *group)
	}

	return groups, nil
}

// classify turns one MachineSet into a NodeGroup or an explicit "not
// opted in" signal. Returns (nil, nil) when the MachineSet is valid
// but isn't autoscaled — the most common outcome. Returns (nil, err)
// when something's structurally wrong (missing MachineClass, bad
// annotations) so the caller can log + skip without a second
// MachineSet's error taking down the whole list.
//
// Role filter: MachineSets carrying LabelControlPlaneRole are
// skipped wholesale. Cluster-autoscaler upstream doesn't support CP
// scaling; annotating a CP MachineClass would be either a typo or an
// operator expecting a behavior we don't deliver, and silently
// ignoring it surfaces in logs.
func (d *Discoverer) classify(ctx context.Context, ms *omni.MachineSet) (*NodeGroup, error) {
	if _, isCP := ms.Metadata().Labels().Get(omni.LabelControlPlaneRole); isCP {
		return nil, nil
	}

	spec := ms.TypedSpec().Value

	allocation := spec.MachineAllocation
	if allocation == nil {
		return nil, nil // MachineSet without static allocation — not autoscalable via this path
	}

	if allocation.AllocationType != specs.MachineSetSpec_MachineAllocation_Static {
		return nil, fmt.Errorf("MachineSet uses allocation_type=%v; only Static is supported — set `size: <N>` in the cluster template, not `size: unlimited`", allocation.AllocationType)
	}

	mc, err := safe.StateGetByID[*omni.MachineClass](ctx, d.st, allocation.Name)
	if err != nil {
		return nil, fmt.Errorf("get MachineClass %q: %w", allocation.Name, err)
	}

	annotations := allAnnotations(mc)

	cfg, err := ParseMachineClassAutoscaleConfig(annotations)
	if err != nil {
		return nil, fmt.Errorf("MachineClass %q annotations: %w", allocation.Name, err)
	}

	if cfg == nil {
		// MachineClass isn't opted in — legitimate non-autoscaled
		// worker pool.
		return nil, nil
	}

	if int(allocation.MachineCount) < cfg.Min || int(allocation.MachineCount) > cfg.Max {
		d.logger.Warn("MachineSet MachineCount is outside annotated bounds",
			zap.String("machineset", ms.Metadata().ID()),
			zap.Uint32("current", allocation.MachineCount),
			zap.Int("min", cfg.Min),
			zap.Int("max", cfg.Max),
		)
		// Keep the node group in the result — the autoscaler will
		// correct it toward the [min, max] window via CAS's natural
		// scaling behavior. Silently dropping would strand the
		// MachineSet in the out-of-bounds state with no autoscaling.
	}

	return &NodeGroup{
		ID:               ms.Metadata().ID(),
		MachineClassName: allocation.Name,
		CurrentSize:      int(allocation.MachineCount),
		Config:           cfg,
	}, nil
}

// allAnnotations returns a shallow copy of the resource's annotation
// map. Wraps KV.Raw() (which returns the backing map that callers
// must not mutate) so callers receive a safe-to-modify map and so
// tests can pass a plain map[string]string into
// ParseMachineClassAutoscaleConfig without depending on COSI types.
func allAnnotations(r resource.Resource) map[string]string {
	raw := r.Metadata().Annotations().Raw()

	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = v
	}

	return out
}
