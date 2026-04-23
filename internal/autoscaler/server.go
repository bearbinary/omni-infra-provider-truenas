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

	logger *zap.Logger
	config *SubcommandConfig
	gate   CapacityQuery

	mu  sync.Mutex
	grp *grpc.Server
}

// NewServer constructs a Server bound to the provided logger and
// config. The CapacityQuery may be nil during early-phase testing;
// real deploys pass a *TrueNASCapacityAdapter.
func NewServer(logger *zap.Logger, cfg *SubcommandConfig, gate CapacityQuery) *Server {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Server{
		logger: logger,
		config: cfg,
		gate:   gate,
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
// enumerate the node groups this autoscaler manages. Real
// implementation lands in phase 3b: enumerate MachineSets for
// `OMNI_CLUSTER_NAME`, filter to ones whose MachineClass has the
// `bearbinary.com/autoscale-*` annotations.
func (s *Server) NodeGroups(_ context.Context, _ *pb.NodeGroupsRequest) (*pb.NodeGroupsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "phase 3b: node-group enumeration not yet wired — see internal/autoscaler/discovery.go (pending)")
}

// NodeGroupForNode is called to map a Kubernetes node back to a node
// group. Phase 3b wires this to the Omni MachineSetNode label.
func (s *Server) NodeGroupForNode(_ context.Context, _ *pb.NodeGroupForNodeRequest) (*pb.NodeGroupForNodeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "phase 3b: node → node-group mapping not yet wired")
}

// NodeGroupTargetSize / NodeGroupIncreaseSize / NodeGroupDecreaseTargetSize
// / NodeGroupDeleteNodes / NodeGroupNodes / NodeGroupTemplateNodeInfo are
// the mutation surface. They stay Unimplemented through phase 3b and land
// in phase 3c (read paths) and phase 3d (write paths behind the
// singleton lease).
//
// DeleteNodes/DecreaseTargetSize stay Unimplemented through the entire
// experimental phase — scale-down is explicitly out of scope. Both
// handlers will return Unimplemented with an operator-readable message
// pointing at docs/autoscaler.md so deployments that accidentally
// re-enable scale-down in the sidecar args fail loudly rather than
// silently triggering teardowns.
