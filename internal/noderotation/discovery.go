package noderotation

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/omni/client/api/omni/specs"
	infraresources "github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"go.uber.org/zap"
)

// Candidate is the resolved view of one rotation-eligible MachineSet:
// the MachineSet itself, the MachineClass it allocates from, the
// validated rotation Config, the current MachineClass generation, and
// the per-MachineRequest stale/fresh classification. The reconciler
// consumes []Candidate one item at a time and decides whether to take
// a rotation step on each.
//
// Per-MachineSet rather than per-MachineClass because two MachineSets
// in the same cluster can reference the same MachineClass (worker
// pools split by zone, for example). Each gets its own surge / in-place
// step budget and its own rotation-state lock annotation.
type Candidate struct {
	// MachineSet is the live resource. The reconciler reads its
	// MachineCount + rotation-state lock and may write to it during a
	// rotation step. Never nil for a returned Candidate.
	MachineSet *omni.MachineSet

	// MachineClass is the live resource the MachineSet allocates from.
	// Carries the AnnotationClassGeneration value (after the reconciler
	// has stamped it). Never nil for a returned Candidate.
	MachineClass *omni.MachineClass

	// Config is the parsed rotation policy. Never nil for a returned
	// Candidate — discovery filters out classes that aren't opted in
	// before constructing one.
	Config *Config

	// CurrentGeneration is the canonical hash computed from
	// MachineClass.AutoProvision at discovery time. Stamped onto the
	// MachineClass as AnnotationClassGeneration once the reconciler
	// processes this candidate.
	CurrentGeneration string

	// MachineCount is MachineSet.MachineAllocation.MachineCount at
	// discovery time. Used for the min-healthy floor check and for
	// surge bookkeeping.
	MachineCount int

	// Requests is the per-MachineRequest classification. Sorted by
	// creation timestamp ascending so the engine's "pick the oldest
	// stale" victim selection is deterministic and prefers replacing
	// the longest-running stale member first.
	Requests []RequestStatus
}

// RequestStatus is one MachineRequest's freshness classification.
type RequestStatus struct {
	// Request is the live MachineRequest resource.
	Request *infraresources.MachineRequest

	// Generation is the canonical hash computed from this
	// MachineRequest's provider_data + Talos boot inputs. Compared
	// against the parent Candidate.CurrentGeneration to decide
	// fresh-vs-stale.
	Generation string

	// Stale reports Generation != Candidate.CurrentGeneration. Stale
	// requests are rotation candidates; fresh ones are left alone.
	Stale bool

	// Stage is the MachineRequestStatus stage at discovery time when
	// available. The reconciler refuses to count a PROVISIONING or
	// FAILED request toward the healthy floor — they aren't yet
	// serving workload. UNKNOWN when no MachineRequestStatus has been
	// written yet (very early in a request's life).
	Stage specs.MachineRequestStatusSpec_Stage

	// CreatedAt is the request's COSI creation timestamp. Used to sort
	// Candidate.Requests in stable order and to break ties when
	// multiple stale requests are equally rotation-eligible.
	CreatedAt time.Time
}

// Discoverer enumerates rotation-eligible MachineSets for one Omni
// cluster. Kept as a struct mirroring autoscaler.Discoverer so the
// reconciler holds a single long-lived reference rather than re-
// plumbing the state / logger / cluster on every tick.
type Discoverer struct {
	st                 state.State
	cluster            string
	providerID         string
	logger             *zap.Logger
	trustDeclaredRole  bool
	roleMismatchCounts *RoleMismatchCounts
}

// RoleMismatchCounts is an optional metrics hook used to surface
// role-mismatch refusals (and overridden warnings) to operators. Tests
// pass nil; cmd/.../noderotation.go wires real OTel counters.
type RoleMismatchCounts struct {
	// OnRefused is invoked once per discovery pass per MachineSet that
	// was excluded because its LabelControlPlaneRole disagreed with the
	// MachineClass-declared role and the escape-hatch is off.
	OnRefused func(machineSetID, declaredRole string)
}

// NewDiscoverer constructs a Discoverer scoped to one Omni cluster and
// one infra provider ID. The providerID filters MachineRequest lookups
// — we only rotate MachineRequests this provider owns, never another
// provider's resources, because the rotation step (delete MachineRequest)
// would otherwise corrupt foreign state.
func NewDiscoverer(st state.State, cluster, providerID string, logger *zap.Logger) *Discoverer {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Discoverer{
		st:         st,
		cluster:    cluster,
		providerID: providerID,
		logger:     logger,
	}
}

// WithTrustDeclaredRole disables the role-mismatch refusal and reverts
// to the original warn-and-honor behavior. Provided as an explicit
// opt-in for operators running hand-crafted clusters whose
// LabelControlPlaneRole isn't trustworthy. Fail-closed by default.
func (d *Discoverer) WithTrustDeclaredRole(trust bool) *Discoverer {
	d.trustDeclaredRole = trust
	return d
}

// WithRoleMismatchMetrics wires a metrics callback so refusal counts
// land on operator dashboards. Pass nil to skip metric emission.
func (d *Discoverer) WithRoleMismatchMetrics(counts *RoleMismatchCounts) *Discoverer {
	d.roleMismatchCounts = counts
	return d
}

// Discover enumerates rotation candidates for the configured cluster.
// Per-class and per-MachineSet failures are logged at warn and the
// offending entry is skipped — one bad annotation must never take out
// rotation for unrelated sets.
//
// Returns an error only on total enumeration failure (Omni API
// unreachable), giving the caller a clear signal to back off rather
// than silently treat an outage as "no work to do".
func (d *Discoverer) Discover(ctx context.Context) ([]Candidate, error) {
	classes, err := safe.StateListAll[*omni.MachineClass](ctx, d.st)
	if err != nil {
		return nil, fmt.Errorf("list MachineClasses: %w", err)
	}

	// Build the opted-in class index up front. Most clusters have only
	// a handful of classes, so the O(N×M) join against MachineSets is
	// cheap compared to the Omni API round-trips.
	type optedInClass struct {
		class      *omni.MachineClass
		config     *Config
		generation string
	}

	optedIn := make(map[string]optedInClass, classes.Len())

	classIt := classes.Iterator() //nolint:staticcheck // see discovery.go in autoscaler for the iter.Seq migration note
	for classIt.Next() {
		mc := classIt.Value()

		// Parser reads from mc.Metadata().Annotations().Raw() directly
		// — the contract on KV.Raw() is "do not mutate", and the parser
		// is read-only over the map. Skipping the per-tick map copy
		// removes one allocation per opted-in class per tick.
		cfg, parseErr := ParseMachineClassRotationConfig(mc.Metadata().Annotations().Raw())
		if parseErr != nil {
			d.logger.Warn("skipping MachineClass: rotation annotations invalid",
				zap.String("machineclass", mc.Metadata().ID()),
				zap.Error(parseErr),
			)

			continue
		}

		if cfg == nil {
			continue
		}

		gen, genErr := generationFromMachineClass(mc)
		if genErr != nil {
			d.logger.Warn("skipping MachineClass: generation hash failed",
				zap.String("machineclass", mc.Metadata().ID()),
				zap.Error(genErr),
			)

			continue
		}

		if gen == "" {
			d.logger.Warn("skipping MachineClass: no AutoProvision block, nothing to rotate against",
				zap.String("machineclass", mc.Metadata().ID()),
			)

			continue
		}

		optedIn[mc.Metadata().ID()] = optedInClass{class: mc, config: cfg, generation: gen}
	}

	if len(optedIn) == 0 {
		return nil, nil
	}

	sets, err := safe.StateListAll[*omni.MachineSet](
		ctx,
		d.st,
		state.WithLabelQuery(resource.LabelEqual(omni.LabelCluster, d.cluster)),
	)
	if err != nil {
		return nil, fmt.Errorf("list MachineSets for cluster %q: %w", d.cluster, err)
	}

	// One ListAll of MachineRequestStatuses for this provider per tick.
	// Replaces a per-request StateGetByID call (the N+1 pattern) — for
	// R total requests on the provider, drops API round-trips from O(R)
	// to O(1). Build an ID→Stage map; classifyRequests does map lookups.
	stageByID, err := d.listRequestStages(ctx)
	if err != nil {
		// Treat ListAll failure as non-fatal: every request becomes
		// UNKNOWN. The reconciler tick logs the cause and retries.
		d.logger.Warn("MachineRequestStatus listing failed; treating all requests as UNKNOWN this tick",
			zap.Error(err),
		)

		stageByID = nil
	}

	candidates := make([]Candidate, 0, sets.Len())

	setIt := sets.Iterator() //nolint:staticcheck
	for setIt.Next() {
		ms := setIt.Value()

		allocation := ms.TypedSpec().Value.GetMachineAllocation()
		if allocation == nil {
			continue
		}

		entry, ok := optedIn[allocation.GetName()]
		if !ok {
			continue
		}

		// Role-mismatch refusal: a MachineSet carrying
		// LabelControlPlaneRole whose MachineClass declares role=worker
		// would be in-place-rotated with MinHealthy=1 — that drops a
		// CP below quorum and bricks etcd. Refuse the candidate unless
		// the operator explicitly opts in via WithTrustDeclaredRole.
		if !d.checkRoleMismatch(ms, entry.config.Role) {
			continue
		}

		reqs, err := d.classifyRequests(ctx, ms.Metadata().ID(), entry.generation, stageByID)
		if err != nil {
			d.logger.Warn("skipping MachineSet: request enumeration failed",
				zap.String("machineset", ms.Metadata().ID()),
				zap.Error(err),
			)

			continue
		}

		candidates = append(candidates, Candidate{
			MachineSet:        ms,
			MachineClass:      entry.class,
			Config:            entry.config,
			CurrentGeneration: entry.generation,
			MachineCount:      int(allocation.GetMachineCount()),
			Requests:          reqs,
		})
	}

	return candidates, nil
}

// listRequestStages fetches every MachineRequestStatus owned by this
// provider and returns an ID→Stage map. Used by Discover to replace
// the per-MachineRequest StateGetByID call inside classifyRequests
// (the N+1 pattern that, on a 30-request cluster ticking every 30s,
// produced ~120 extra Get round-trips per minute).
func (d *Discoverer) listRequestStages(ctx context.Context) (map[string]specs.MachineRequestStatusSpec_Stage, error) {
	statuses, err := safe.StateListAll[*infraresources.MachineRequestStatus](
		ctx,
		d.st,
		state.WithLabelQuery(resource.LabelEqual(omni.LabelInfraProviderID, d.providerID)),
	)
	if err != nil {
		return nil, fmt.Errorf("list MachineRequestStatuses: %w", err)
	}

	out := make(map[string]specs.MachineRequestStatusSpec_Stage, statuses.Len())

	it := statuses.Iterator() //nolint:staticcheck
	for it.Next() {
		mrs := it.Value()
		out[mrs.Metadata().ID()] = mrs.TypedSpec().Value.GetStage()
	}

	return out, nil
}

// classifyRequests lists this provider's MachineRequests scoped to the
// given MachineSet and hashes each one's spec to decide fresh-vs-stale.
// Sorts the result by CreatedAt ascending so the engine's victim picker
// gets oldest-first ordering for free.
//
// stageByID is the per-tick pre-fetched ID→Stage map from
// listRequestStages. Pass nil to default every request to UNKNOWN
// (used when the listing failed earlier in Discover).
func (d *Discoverer) classifyRequests(ctx context.Context, machineSetID, currentGen string, stageByID map[string]specs.MachineRequestStatusSpec_Stage) ([]RequestStatus, error) {
	reqs, err := safe.StateListAll[*infraresources.MachineRequest](
		ctx,
		d.st,
		state.WithLabelQuery(
			resource.LabelEqual(omni.LabelMachineRequestSet, machineSetID),
			resource.LabelEqual(omni.LabelInfraProviderID, d.providerID),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("list MachineRequests: %w", err)
	}

	out := make([]RequestStatus, 0, reqs.Len())

	reqIt := reqs.Iterator() //nolint:staticcheck
	for reqIt.Next() {
		mr := reqIt.Value()

		gen, err := generationFromMachineRequest(mr)
		if err != nil {
			d.logger.Warn("skipping MachineRequest: hash failed",
				zap.String("request", mr.Metadata().ID()),
				zap.Error(err),
			)

			continue
		}

		stage := specs.MachineRequestStatusSpec_UNKNOWN
		if stageByID != nil {
			if s, ok := stageByID[mr.Metadata().ID()]; ok {
				stage = s
			}
		}

		out = append(out, RequestStatus{
			Request:    mr,
			Generation: gen,
			Stale:      gen != currentGen,
			Stage:      stage,
			CreatedAt:  mr.Metadata().Created(),
		})
	}

	// Stable oldest-first. Ties broken by ID so two requests created
	// in the same microsecond don't oscillate between ticks.
	sortRequestsByCreatedAt(out)

	return out, nil
}

// checkRoleMismatch validates the operator-declared role against the
// MachineSet's LabelControlPlaneRole. Returns true when the candidate
// should proceed, false when it must be skipped.
//
// CP label present + declared worker is the dangerous case: in-place
// rotation under that misconfiguration would tear CP members down
// with MinHealthy=1 (worker default), dropping etcd below quorum.
// Refused by default; trustDeclaredRole flips the refusal back to the
// pre-fix warn-and-honor behavior for hand-crafted clusters.
//
// The inverse (no CP label, declared CP) is logged at warn but
// proceeds — the upside-down case can only over-protect, not destroy.
func (d *Discoverer) checkRoleMismatch(ms *omni.MachineSet, declared Role) bool {
	_, isCPLabeled := ms.Metadata().Labels().Get(omni.LabelControlPlaneRole)

	switch {
	case isCPLabeled && declared != RoleControlPlane:
		if d.trustDeclaredRole {
			d.logger.Warn("MachineSet carries control-plane label but rotation role is declared worker; honoring declared role per NODE_ROTATION_TRUST_DECLARED_ROLE",
				zap.String("machineset", ms.Metadata().ID()),
				zap.String("declared_role", string(declared)),
			)

			return true
		}

		d.logger.Error("REFUSING rotation: MachineSet carries control-plane label but MachineClass declares role=worker — in-place rotation here would drop CP below quorum and brick etcd. Fix the annotation or set NODE_ROTATION_TRUST_DECLARED_ROLE=true if Omni's labels are wrong on this cluster.",
			zap.String("machineset", ms.Metadata().ID()),
			zap.String("declared_role", string(declared)),
		)

		if d.roleMismatchCounts != nil && d.roleMismatchCounts.OnRefused != nil {
			d.roleMismatchCounts.OnRefused(ms.Metadata().ID(), string(declared))
		}

		return false
	case !isCPLabeled && declared == RoleControlPlane:
		d.logger.Warn("MachineSet lacks control-plane label but rotation role is declared controlplane",
			zap.String("machineset", ms.Metadata().ID()),
		)

		return true
	default:
		return true
	}
}

// FreshCount returns the number of fresh, provisioned requests in the
// candidate. Used by the engine to enforce min-healthy before taking a
// destructive step.
func (c Candidate) FreshCount() int {
	n := 0

	for _, r := range c.Requests {
		if !r.Stale && r.Stage == specs.MachineRequestStatusSpec_PROVISIONED {
			n++
		}
	}

	return n
}

// StaleRequests returns the subset of Requests that are stale. Order is
// preserved (oldest-first).
func (c Candidate) StaleRequests() []RequestStatus {
	out := make([]RequestStatus, 0, len(c.Requests))

	for _, r := range c.Requests {
		if r.Stale {
			out = append(out, r)
		}
	}

	return out
}

// HasActiveLock reports whether the MachineSet currently carries a
// rotation-state lock that is still within its TTL. The autoscaler
// uses the same logic to decide whether to pause scaling for this
// node group.
func (c Candidate) HasActiveLock(now time.Time, ttl time.Duration) bool {
	raw, ok := c.MachineSet.Metadata().Annotations().Get(AnnotationRotationState)
	if !ok {
		return false
	}

	_, ts, parseOK := parseRotationStateAnnotation(raw)
	if !parseOK {
		return false
	}

	return now.Sub(ts) < ttl
}

// parseRotationStateAnnotation splits the "<gen>:<unix-ts>" value
// written to AnnotationRotationState. Returns (gen, ts, true) on
// success; (_, _, false) on any parse failure so callers treat
// malformed locks as absent rather than indefinitely held.
func parseRotationStateAnnotation(raw string) (gen string, ts time.Time, ok bool) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return "", time.Time{}, false
	}

	secs, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", time.Time{}, false
	}

	return parts[0], time.Unix(secs, 0), true
}

// formatRotationStateAnnotation produces the canonical "<gen>:<ts>"
// value the reconciler writes when taking a step.
func formatRotationStateAnnotation(generation string, ts time.Time) string {
	return generation + ":" + strconv.FormatInt(ts.Unix(), 10)
}

// SurgePhaseState is the parsed view of AnnotationSurgePhase.
type SurgePhaseState struct {
	// Phase is the current leg of the surge cycle.
	Phase SurgePhase

	// OriginalCount is the operator-configured nominal MachineCount,
	// preserved across the cycle so a reconciler crash can resume
	// without rediscovering the operator's intent.
	OriginalCount int

	// CycleStartedAt is when the engine first set the surge phase
	// annotation. Surfaced in logs so operators can spot a cycle
	// stuck longer than the typical 2–3 minute boot window.
	CycleStartedAt time.Time
}

// parseSurgePhaseAnnotation reads the "<phase>:<count>:<ts>" value
// written to AnnotationSurgePhase. Returns (_, false) on any malformed
// value so a corrupted annotation reads as "no surge in progress" —
// the engine will then re-plan from current state, which is safer than
// trusting partial data.
func parseSurgePhaseAnnotation(raw string) (SurgePhaseState, bool) {
	parts := strings.SplitN(raw, ":", 3)
	if len(parts) != 3 {
		return SurgePhaseState{}, false
	}

	phase := SurgePhase(parts[0])

	switch phase {
	case SurgePhaseWaitUp, SurgePhaseWaitDown:
		// valid
	default:
		return SurgePhaseState{}, false
	}

	origCount, err := strconv.Atoi(parts[1])
	if err != nil || origCount < 0 {
		return SurgePhaseState{}, false
	}

	secs, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return SurgePhaseState{}, false
	}

	return SurgePhaseState{
		Phase:          phase,
		OriginalCount:  origCount,
		CycleStartedAt: time.Unix(secs, 0),
	}, true
}

// formatSurgePhaseAnnotation produces the canonical persisted value
// for a surge-phase annotation.
func formatSurgePhaseAnnotation(s SurgePhaseState) string {
	return string(s.Phase) + ":" +
		strconv.Itoa(s.OriginalCount) + ":" +
		strconv.FormatInt(s.CycleStartedAt.Unix(), 10)
}

// SurgePhase returns the parsed surge-phase state for this Candidate's
// MachineSet, or (_, false) when no cycle is in flight. Engine.Plan
// uses this to branch into the right surge leg.
func (c Candidate) SurgePhase() (SurgePhaseState, bool) {
	raw, ok := c.MachineSet.Metadata().Annotations().Get(AnnotationSurgePhase)
	if !ok {
		return SurgePhaseState{}, false
	}

	return parseSurgePhaseAnnotation(raw)
}

func sortRequestsByCreatedAt(reqs []RequestStatus) {
	// Insertion sort — request counts are tiny (single-digit to low-
	// dozens per MachineSet in practice), so a simple in-place pass is
	// cheaper than reaching for sort.Slice closure allocation. Compare
	// in place (no per-iteration struct copy) so an already-sorted
	// input does no work beyond the comparisons.
	for i := 1; i < len(reqs); i++ {
		for j := i; j > 0; j-- {
			if reqs[j-1].CreatedAt.Before(reqs[j].CreatedAt) {
				break
			}
			if reqs[j-1].CreatedAt.Equal(reqs[j].CreatedAt) &&
				reqs[j-1].Request.Metadata().ID() <= reqs[j].Request.Metadata().ID() {
				break
			}

			reqs[j-1], reqs[j] = reqs[j], reqs[j-1]
		}
	}
}

// generationFromMachineClass extracts the AutoProvision sub-spec and
// runs the canonical hash. Returns ("", nil) when AutoProvision is
// absent (operator-managed allocation — not in this controller's scope).
func generationFromMachineClass(mc *omni.MachineClass) (string, error) {
	ap := mc.TypedSpec().Value.GetAutoProvision()
	if ap == nil {
		return "", nil
	}

	return MachineClassGeneration(
		true,
		ap.GetProviderData(),
		ap.GetGrpcTunnel().String(),
		ap.GetKernelArgs(),
		metaValuesToStringSlice(ap.GetMetaValues()),
	)
}

// generationFromMachineRequest hashes the same shape from a baked
// MachineRequest. Only the fields that flow from MachineClass.
// AutoProvision are hashed — see generation.go for why TalosVersion /
// Extensions / Overlay are excluded.
func generationFromMachineRequest(mr *infraresources.MachineRequest) (string, error) {
	spec := mr.TypedSpec().Value

	return MachineRequestGeneration(
		spec.GetProviderData(),
		spec.GetGrpcTunnel().String(),
		spec.GetKernelArgs(),
		metaValuesToStringSlice(spec.GetMetaValues()),
	)
}

// metaValuesToStringSlice flattens []*MetaValue into a deterministic
// "<key>=<value>" string slice. The slice is sorted so two equivalent
// metadata sets produce identical hashes regardless of protobuf
// repeated-field ordering.
func metaValuesToStringSlice(values []*specs.MetaValue) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, 0, len(values))
	for _, mv := range values {
		// strconv.Itoa avoids the reflect-driven fmt.Sprintf path; on a
		// per-tick budget across (classes + requests) × meta-values this
		// halves the allocations.
		out = append(out, strconv.Itoa(int(mv.GetKey()))+"="+mv.GetValue())
	}

	sort.Strings(out)

	return out
}
