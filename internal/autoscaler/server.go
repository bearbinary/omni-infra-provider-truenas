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

	mu  sync.Mutex
	grp *grpc.Server
}

// NewServer constructs a Server bound to the provided logger and
// config. CapacityQuery and Discoverer may be nil during early-phase
// testing: handlers that need them return Unimplemented with a
// message naming the missing dependency, so a test that exercises
// only the Unimplemented surface doesn't have to wire Omni state.
//
// Real deploys pass both: a *TrueNASCapacityAdapter for the gate, and
// a *Discoverer built from the Omni state client.
func NewServer(logger *zap.Logger, cfg *SubcommandConfig, gate CapacityQuery, discoverer *Discoverer) *Server {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Server{
		logger:     logger,
		config:     cfg,
		gate:       gate,
		discoverer: discoverer,
	}
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

	grp := grpc.NewServer()
	pb.RegisterCloudProviderServer(grp, s)

	s.mu.Lock()
	s.grp = grp
	s.mu.Unlock()

	s.logger.Info("autoscaler gRPC server listening",
		zap.String("address", s.config.ListenAddress),
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

// NodeGroupIncreaseSize / NodeGroupDecreaseTargetSize / NodeGroupDeleteNodes
// / NodeGroupNodes / NodeGroupTemplateNodeInfo — the mutation/shape
// surface. Increase stays Unimplemented through phase 3c (write path
// lands in phase 3d behind the singleton lease). DeleteNodes +
// DecreaseTargetSize stay Unimplemented through the entire
// experimental phase — scale-down is explicitly out of scope, and
// returning Unimplemented here is belt-and-suspenders on top of the
// sidecar's `--scale-down-enabled=false` flag.
//
// Leaving NodeGroupIncreaseSize / NodeGroupNodes /
// NodeGroupTemplateNodeInfo to the generated
// UnimplementedCloudProviderServer defaults is fine — the
// default-via-embed returns Unimplemented with a minimal message.
// Once phase 3d wires the write path we'll override NodeGroupIncreaseSize
// explicitly to run the capacity gate and MachineAllocation update.

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
