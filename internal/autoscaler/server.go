package autoscaler

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/bearbinary/omni-infra-provider-truenas/internal/autoscaler/proto/externalgrpc"
)

// Server implements the external-gRPC cluster-autoscaler cloud-provider
// contract. This phase wires the RPC surface but keeps every handler
// returning codes.Unimplemented — the purpose is to let a Deployment
// come up, answer a sidecar's health checks, and log what the CAS
// would be asking us to do, without yet writing to Omni.
//
// Phase 3 swaps the Unimplemented handlers for real implementations
// that (1) enumerate MachineSets, (2) run the capacity gate, and
// (3) update MachineAllocation.MachineCount. Keeping the server skeleton
// separate lets us ship each capability one commit at a time.
//
// Deliberate design constraints:
//   - No blocking operations inside handlers except the ones that call
//     into our own CapacityQuery + (future) Omni client. If we ever
//     need to call out over a slow path, it goes through a context-
//     aware helper so the cluster-autoscaler sidecar's deadline is
//     honored.
//   - Handlers log at debug level unless a decision is denied/errored;
//     those log at warn+ so operators can grep for "autoscaler" in
//     production logs without a wall of info spam.
type Server struct {
	pb.UnimplementedCloudProviderServer

	logger     *zap.Logger
	config     *SubcommandConfig
	gate       CapacityQuery
	discoverer *Discoverer
	writer     *ScaleWriter

	// defaultPool is the TrueNAS pool the capacity gate queries when
	// the NodeGroup's MachineClass didn't set the autoscale-pool
	// annotation. Matches the provisioner's DEFAULT_POOL env var so
	// autoscaled clusters see the same pool selection as newly-
	// provisioned ones without the operator having to duplicate
	// config.
	defaultPool string

	mu  sync.Mutex
	grp *grpc.Server
}

// NewServer constructs a Server bound to the provided logger and
// config. CapacityQuery, Discoverer, and ScaleWriter may be nil
// during early-phase testing: handlers that need them return
// Unimplemented with a message naming the missing dependency, so a
// test that exercises only the Unimplemented surface doesn't have
// to wire Omni state + TrueNAS.
//
// Real deploys pass all three: a *TrueNASCapacityAdapter for the
// gate, a *Discoverer built from the Omni state client, and a
// *ScaleWriter sharing that same state client.
func NewServer(logger *zap.Logger, cfg *SubcommandConfig, gate CapacityQuery, discoverer *Discoverer, writer *ScaleWriter) *Server {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Server{
		logger:      logger,
		config:      cfg,
		gate:        gate,
		discoverer:  discoverer,
		writer:      writer,
		defaultPool: "",
	}
}

// WithDefaultPool configures the fallback pool the capacity gate
// uses when a NodeGroup's MachineClass doesn't set the
// AnnotationAutoscalePool annotation. Returns the Server for
// chaining. Callers that don't set this leave defaultPool empty,
// which causes gate evaluation on pool-less groups to skip the
// pool check (equivalent to MinPoolFreeGiB=0) rather than error.
func (s *Server) WithDefaultPool(pool string) *Server {
	s.defaultPool = pool

	return s
}

// Listen binds the gRPC listener and serves until ctx is cancelled.
// On cancellation, performs a GracefulStop so in-flight sidecar calls
// get a chance to complete before the socket closes.
//
// Returns nil on clean shutdown; returns the first error encountered on
// listener bind or serve.
func (s *Server) Listen(ctx context.Context) error {
	lis, err := net.Listen("tcp", s.config.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen %q: %w", s.config.ListenAddress, err)
	}

	return s.serveOnListener(ctx, lis)
}

// ServeOnListener serves on an already-bound listener. Used by tests
// that need to pre-bind a port to avoid the close-then-rebind race in
// the "pick an ephemeral port, close, let the server rebind" pattern.
// Production callers use Listen; this is test-support surface.
func (s *Server) ServeOnListener(ctx context.Context, lis net.Listener) error {
	return s.serveOnListener(ctx, lis)
}

func (s *Server) serveOnListener(ctx context.Context, lis net.Listener) error {
	grp := grpc.NewServer()
	pb.RegisterCloudProviderServer(grp, s)

	s.mu.Lock()
	s.grp = grp
	s.mu.Unlock()

	s.logger.Info("autoscaler gRPC server listening",
		zap.String("address", lis.Addr().String()),
	)

	errCh := make(chan error, 1)

	go func() {
		if err := grp.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- err
		}

		close(errCh)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("autoscaler gRPC server draining")
		grp.GracefulStop()
		s.logger.Info("autoscaler gRPC server stopped")

		return nil
	case err := <-errCh:
		return err
	}
}

// Stop is a synchronous immediate-stop variant for use in tests. Not
// called by the subcommand's normal shutdown path — that flows through
// ctx cancellation in Listen.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.grp != nil {
		s.grp.Stop()
	}
}

// --- CloudProvider RPC surface -------------------------------------------
//
// Phase 3 scope: every handler returns codes.Unimplemented. The RPC
// surface is defined here so the server registration line
// (`pb.RegisterCloudProviderServer(grp, s)`) compiles against the
// generated contract; subsequent commits flesh out individual handlers.
//
// Keeping them as explicit methods (rather than relying on
// UnimplementedCloudProviderServer's defaults) makes the list of
// capabilities the autoscaler needs to support literal and searchable.

// NodeGroups is called by the sidecar on every refresh cycle to
// enumerate the node groups this autoscaler manages. Translates the
// Discoverer's []NodeGroup into the proto shape — `id`, `minSize`,
// `maxSize`. The `debug` field is populated with a human-readable
// string listing the current size so Cluster Autoscaler's
// verbose-mode logs include enough context to diagnose
// over/under-allocated MachineSets.
//
// A configured Discoverer is required. If one isn't present (e.g.,
// during a partial-boot test) we return Unimplemented rather than
// silently returning an empty list — the silent-empty path would be
// indistinguishable from "this cluster has no opted-in MachineSets",
// which is a legitimate steady state.
func (s *Server) NodeGroups(ctx context.Context, _ *pb.NodeGroupsRequest) (*pb.NodeGroupsResponse, error) {
	if s.discoverer == nil {
		return nil, status.Error(codes.Unimplemented, "autoscaler not fully configured: discoverer missing (boot sequence not complete)")
	}

	groups, err := s.discoverer.Discover(ctx)
	if err != nil {
		s.logger.Warn("NodeGroups: discovery failed", zap.Error(err))

		return nil, status.Errorf(codes.Unavailable, "discover node groups: %v", err)
	}

	resp := &pb.NodeGroupsResponse{NodeGroups: make([]*pb.NodeGroup, 0, len(groups))}

	for _, g := range groups {
		resp.NodeGroups = append(resp.NodeGroups, toProtoNodeGroup(g))
	}

	s.logger.Debug("NodeGroups", zap.Int("count", len(groups)))

	return resp, nil
}

// NodeGroupForNode maps a Kubernetes node back to its managing node
// group. The sidecar calls this when deciding whether a node it sees
// in the K8s API is something we can scale.
//
// Uses the node's providerID label (set by Talos via the Omni
// machine infra-id) to find the MachineSetNode and, from there, the
// MachineSet. Phase 3c ships a minimal implementation that resolves
// via label walk; phase 4 can swap to a ClusterMachine-keyed index
// if the walk turns out to be slow at scale.
//
// Returns an empty NodeGroup (not an error) when the node doesn't
// belong to any autoscaler-managed MachineSet — that's the Cluster
// Autoscaler's signal to leave the node alone. An Unimplemented /
// Unavailable error would make CAS refuse to manage the cluster at
// all, which is not what we want for non-opted-in nodes.
func (s *Server) NodeGroupForNode(ctx context.Context, req *pb.NodeGroupForNodeRequest) (*pb.NodeGroupForNodeResponse, error) {
	if s.discoverer == nil {
		return nil, status.Error(codes.Unimplemented, "autoscaler not fully configured: discoverer missing")
	}

	if req.GetNode() == nil {
		return nil, status.Error(codes.InvalidArgument, "NodeGroupForNode: request missing node payload")
	}

	// CAS-sidecar contract: when the node doesn't belong to any of our
	// node groups, return a response with a nil NodeGroup — that's
	// "not mine, leave it". Phase 3c deliberately always returns
	// "not ours": the node → node-group mapping requires watching
	// MachineSetNode / ClusterMachine relationships, which is a
	// bigger slice of Omni state than discovery, and the
	// scale-up-only experimental scope doesn't need it (scale-down
	// is where node-group membership matters).
	//
	// Phase 3d / post-experimental re-implements this properly. The
	// nil-NodeGroup answer is safe: CAS only calls NodeGroupForNode
	// during scale-down decisions, which are disabled at multiple
	// layers in the experimental phase.
	s.logger.Debug("NodeGroupForNode: returning 'not-ours' (phase 3c scope)",
		zap.String("node", req.GetNode().GetName()),
		zap.String("providerID", req.GetNode().GetProviderID()),
	)

	return &pb.NodeGroupForNodeResponse{}, nil
}

// NodeGroupTargetSize answers the current MachineCount for a given
// node group. Backed by discovery — the current count is read from
// MachineAllocation.MachineCount so this number matches the next
// refresh's NodeGroups response.
func (s *Server) NodeGroupTargetSize(ctx context.Context, req *pb.NodeGroupTargetSizeRequest) (*pb.NodeGroupTargetSizeResponse, error) {
	if s.discoverer == nil {
		return nil, status.Error(codes.Unimplemented, "autoscaler not fully configured: discoverer missing")
	}

	id := req.GetId()
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeGroupTargetSize: missing id")
	}

	groups, err := s.discoverer.Discover(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "discover: %v", err)
	}

	for _, g := range groups {
		if g.ID == id {
			return &pb.NodeGroupTargetSizeResponse{TargetSize: int32(g.CurrentSize)}, nil
		}
	}

	return nil, status.Errorf(codes.NotFound, "NodeGroupTargetSize: node group %q not found (not opted in or no longer exists)", id)
}

// NodeGroupIncreaseSize is the write path — the only RPC in the
// experimental phase that mutates Omni state. Flow:
//
//  1. Validate request (positive delta, non-empty id).
//  2. Discover (fresh, every call) so the current-size value used
//     in bounds/capacity decisions is the live value, not a cached
//     one that could be stale relative to a concurrent manual edit.
//  3. Find the target group by id; NotFound if it's missing or no
//     longer opted in.
//  4. Capacity gate (when a CapacityQuery is wired). Gate runs
//     against the pool named in the MachineClass annotation, or
//     the server's defaultPool when the annotation is absent.
//  5. ScaleWriter.IncreaseMachineCount performs the
//     MachineAllocation update under Omni's optimistic concurrency.
//
// No explicit lease: the UpdateWithConflicts inside ScaleWriter
// rejects stale writes so two autoscaler replicas writing in
// parallel can't both land an update — the loser retries on the
// next CAS refresh. Operators should still deploy `replicas: 1` in
// the Helm chart to avoid wasted API calls, but correctness does
// not depend on it.
func (s *Server) NodeGroupIncreaseSize(ctx context.Context, req *pb.NodeGroupIncreaseSizeRequest) (*pb.NodeGroupIncreaseSizeResponse, error) {
	if s.discoverer == nil || s.writer == nil {
		recordScaleUpResult(ctx, ResultErroredInternal)

		return nil, status.Error(codes.Unimplemented, "autoscaler not fully configured: discoverer or writer missing — check the Deployment's boot sequence")
	}

	id := req.GetId()
	if id == "" {
		recordScaleUpResult(ctx, ResultRejectedInvalid)

		return nil, status.Error(codes.InvalidArgument, "NodeGroupIncreaseSize: missing id")
	}

	delta := int(req.GetDelta())
	if delta <= 0 {
		recordScaleUpResult(ctx, ResultRejectedInvalid)

		return nil, status.Errorf(codes.InvalidArgument, "NodeGroupIncreaseSize: delta must be > 0, got %d", delta)
	}

	groups, err := s.discoverer.Discover(ctx)
	if err != nil {
		recordScaleUpResult(ctx, ResultErroredInternal)

		return nil, status.Errorf(codes.Unavailable, "discover: %v", err)
	}

	var group *NodeGroup

	for i := range groups {
		if groups[i].ID == id {
			group = &groups[i]

			break
		}
	}

	if group == nil {
		recordScaleUpResult(ctx, ResultRejectedNotFound)

		return nil, status.Errorf(codes.NotFound, "NodeGroupIncreaseSize: node group %q not found", id)
	}

	// Capacity gate — only when one is wired. Early experimental
	// deploys pass a nil CapacityQuery to Server; skipping the gate
	// is operationally equivalent to MinPoolFreeGiB=0 +
	// MinHostMemGiB=0.
	if s.gate != nil {
		pool := group.Pool
		if pool == "" {
			pool = s.defaultPool
		}

		decision := CheckCapacity(ctx, s.gate, *group.Config, pool)

		switch decision.Outcome {
		case OutcomeDeniedHard:
			s.logger.Warn("NodeGroupIncreaseSize: capacity gate denied",
				zap.String("group", id),
				zap.Int("delta", delta),
				zap.String("reason", decision.Reason),
			)

			recordScaleUpResult(ctx, ResultDeniedCapacity)
			recordCapacityDenial(ctx, categorizeDenialReason(decision.Reason))

			return nil, status.Errorf(codes.ResourceExhausted, "capacity gate denied: %s", decision.Reason)
		case OutcomeErrored:
			s.logger.Warn("NodeGroupIncreaseSize: capacity gate errored (fails closed)",
				zap.String("group", id),
				zap.String("reason", decision.Reason),
			)

			recordScaleUpResult(ctx, ResultErroredInternal)
			recordCapacityDenial(ctx, ReasonQueryFailed)

			return nil, status.Errorf(codes.Unavailable, "capacity gate query failed: %s", decision.Reason)
		case OutcomeWarnedSoft:
			s.logger.Warn("NodeGroupIncreaseSize: capacity gate soft-warn, proceeding",
				zap.String("group", id),
				zap.Int("delta", delta),
				zap.String("reason", decision.Reason),
			)
		case OutcomeAllowed:
			// Nominal path — no log, the post-write log below covers it.
		}
	}

	newSize, err := s.writer.IncreaseMachineCount(ctx, *group, delta)
	if err != nil {
		if errors.Is(err, ErrAtOrAboveMax) {
			recordScaleUpResult(ctx, ResultRejectedBounds)

			return nil, status.Errorf(codes.ResourceExhausted, "%v", err)
		}

		recordScaleUpResult(ctx, ResultErroredInternal)

		return nil, status.Errorf(codes.Unavailable, "increase machine count: %v", err)
	}

	s.logger.Info("NodeGroupIncreaseSize: scaled up",
		zap.String("group", id),
		zap.Int("delta", delta),
		zap.Int("old_size", group.CurrentSize),
		zap.Int("new_size", newSize),
	)

	recordScaleUpResult(ctx, ResultSucceeded)

	return &pb.NodeGroupIncreaseSizeResponse{}, nil
}

// NodeGroupDecreaseTargetSize and NodeGroupDeleteNodes are the
// scale-down RPCs. Explicitly overridden here (rather than relying
// on the generated UnimplementedCloudProviderServer default) so the
// "scale-down disabled for the experimental phase" contract is
// literal in the code and the rejection log records an operator-
// actionable message.
//
// Two independent layers block scale-down:
//
//  1. The Helm chart runs the sidecar with `--scale-down-enabled=false`
//     (see deploy/helm/omni-autoscaler/values.yaml). Cluster Autoscaler
//     never issues these RPCs when that flag is set.
//
//  2. Even if an operator overrides the sidecar flag, these handlers
//     return codes.Unimplemented with a pointer at docs/autoscaler.md,
//     so the machine never gets stopped at our layer.
//
// Removing these overrides is a deliberate act: scale-down enablement
// is the graduation criterion from experimental, and doing it in two
// steps (re-enable sidecar flag first, remove Unimplemented guard
// second) means we can observe scale-down RPCs hitting the server
// for a few days before actually acting on them.
func (s *Server) NodeGroupDecreaseTargetSize(_ context.Context, req *pb.NodeGroupDecreaseTargetSizeRequest) (*pb.NodeGroupDecreaseTargetSizeResponse, error) {
	s.logger.Warn("NodeGroupDecreaseTargetSize called but scale-down is disabled",
		zap.String("group", req.GetId()),
		zap.Int32("delta", req.GetDelta()),
	)

	return nil, status.Error(codes.Unimplemented,
		"scale-down is disabled in the experimental phase — see docs/autoscaler.md. "+
			"If Cluster Autoscaler is reaching this RPC, verify the sidecar is running with --scale-down-enabled=false.")
}

func (s *Server) NodeGroupDeleteNodes(_ context.Context, req *pb.NodeGroupDeleteNodesRequest) (*pb.NodeGroupDeleteNodesResponse, error) {
	s.logger.Warn("NodeGroupDeleteNodes called but scale-down is disabled",
		zap.String("group", req.GetId()),
		zap.Int("node_count", len(req.GetNodes())),
	)

	return nil, status.Error(codes.Unimplemented,
		"node deletion is disabled in the experimental phase — see docs/autoscaler.md. "+
			"If Cluster Autoscaler is reaching this RPC, verify the sidecar is running with --scale-down-enabled=false.")
}

// NodeGroupDecreaseTargetSize / NodeGroupDeleteNodes stay
// Unimplemented for the entire experimental phase — scale-down is
// explicitly out of scope. Returning Unimplemented here is belt-
// and-suspenders on top of the sidecar's `--scale-down-enabled=false`
// flag so a Deployment that accidentally re-enables scale-down in
// its args fails loudly rather than silently triggering teardowns.

// toProtoNodeGroup is the translator between the internal NodeGroup
// struct (which carries parsed config + current size) and the sparse
// proto shape the sidecar consumes. Kept as a package-private helper
// rather than a method so tests can pin the translation in isolation
// from the gRPC surface.
func toProtoNodeGroup(g NodeGroup) *pb.NodeGroup {
	return &pb.NodeGroup{
		Id:      g.ID,
		MinSize: int32(g.Config.Min),
		MaxSize: int32(g.Config.Max),
		Debug:   fmt.Sprintf("currentSize=%d machineClass=%q capacityGate=%s", g.CurrentSize, g.MachineClassName, g.Config.CapacityGate),
	}
}
