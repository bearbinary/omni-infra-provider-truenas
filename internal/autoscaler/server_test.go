package autoscaler

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/omni/client/api/omni/specs"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/bearbinary/omni-infra-provider-truenas/internal/autoscaler/proto/externalgrpc"
)

// newTestServer starts a Server on a random port and returns a
// client + a shutdown func. Blocks until the server is accepting
// connections so tests can immediately issue RPCs without racing the
// listener.
func newTestServer(t *testing.T) (pb.CloudProviderClient, func()) {
	t.Helper()

	// :0 asks the kernel for an ephemeral port — avoids the "test ran
	// twice and the second run fails because the first hasn't released
	// port 8086" class of flake.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	cfg := &SubcommandConfig{
		ClusterName:     "test-cluster",
		ListenAddress:   lis.Addr().String(),
		RefreshInterval: time.Minute,
	}

	// Keep the listener and hand it directly to the server via
	// ServeOnListener so there's no "close + rebind" race under
	// parallel test load.
	srv := NewServer(nil, cfg, nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)

	go func() {
		done <- srv.ServeOnListener(ctx, lis)
	}()

	cfg.ListenAddress = lis.Addr().String()

	// Poll the address until it's accepting connections so tests can
	// dial without sleep-based retries.
	var conn *grpc.ClientConn

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := grpc.NewClient(cfg.ListenAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			conn = c

			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	require.NotNil(t, conn, "server did not come up within 2s")

	shutdown := func() {
		_ = conn.Close()

		cancel()

		select {
		case err := <-done:
			assert.NoError(t, err, "Listen must return nil on clean ctx-cancel shutdown")
		case <-time.After(2 * time.Second):
			t.Fatal("server did not shut down within 2s of ctx cancel")
		}
	}

	return pb.NewCloudProviderClient(conn), shutdown
}

// TestServer_NodeGroupsWithoutDiscovererReturnsUnimplemented pins
// the boot-incomplete fallback: a Server built without a Discoverer
// (phase 3a style) answers NodeGroups with Unimplemented + a clear
// message rather than silently returning an empty list. Silent-empty
// would be indistinguishable from "cluster has no opted-in
// MachineSets," which is a legitimate steady state.
func TestServer_NodeGroupsWithoutDiscovererReturnsUnimplemented(t *testing.T) {
	t.Parallel()

	client, shutdown := newTestServer(t)
	defer shutdown()

	_, err := client.NodeGroups(context.Background(), &pb.NodeGroupsRequest{})
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok, "gRPC errors must wrap a status")
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "discoverer missing",
		"Unimplemented message must name the missing dependency so operators know the boot sequence failed")
}

// TestServer_NodeGroupForNodeReturnsEmptyWhenConfigured pins the
// "not ours" return shape. Without a Discoverer we still get
// Unimplemented; with a Discoverer we always return an empty NodeGroup
// through phase 3c (the full mapping lives in phase 3d+).
func TestServer_NodeGroupForNodeWithoutDiscovererReturnsUnimplemented(t *testing.T) {
	t.Parallel()

	client, shutdown := newTestServer(t)
	defer shutdown()

	_, err := client.NodeGroupForNode(context.Background(), &pb.NodeGroupForNodeRequest{
		Node: &pb.ExternalGrpcNode{Name: "talos-home-worker-1"},
	})
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
}

// TestServer_GracefulStopOnCtxCancel verifies the Listen loop unwinds
// cleanly on ctx cancellation and does not leak the listening
// goroutine. The shutdown func inside newTestServer already asserts
// Listen returns nil; this test additionally verifies a second Stop
// after ctx cancel is a no-op (no panic).
func TestServer_GracefulStopOnCtxCancel(t *testing.T) {
	t.Parallel()

	_, shutdown := newTestServer(t)
	shutdown()
}

// newTestServerWithDiscoverer is the phase-3c variant: boots a Server
// wired to a real Discoverer over an inmem Omni state. Caller seeds
// the state before calling. Everything else mirrors newTestServer.
func newTestServerWithDiscoverer(t *testing.T, st state.State, cluster string) (pb.CloudProviderClient, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	cfg := &SubcommandConfig{
		ClusterName:     cluster,
		ListenAddress:   lis.Addr().String(),
		RefreshInterval: time.Minute,
	}

	d := NewDiscoverer(st, cluster, zaptest.NewLogger(t))
	w := NewScaleWriter(st)
	srv := NewServer(zaptest.NewLogger(t), cfg, nil, d, w)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)

	go func() { done <- srv.ServeOnListener(ctx, lis) }()

	cfg.ListenAddress = lis.Addr().String()

	var conn *grpc.ClientConn

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := grpc.NewClient(cfg.ListenAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			conn = c

			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	require.NotNil(t, conn)

	shutdown := func() {
		_ = conn.Close()

		cancel()

		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("server did not shut down within 2s of ctx cancel")
		}
	}

	return pb.NewCloudProviderClient(conn), shutdown
}

// TestServer_NodeGroups_ReturnsDiscoveredGroups verifies the happy
// path: NodeGroups forwards the Discoverer's output into the proto
// response, converting `Config.Min`/`Config.Max` to the `minSize`/
// `maxSize` fields CAS expects.
func TestServer_NodeGroups_ReturnsDiscoveredGroups(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "home-workers", map[string]string{
		AnnotationAutoscaleMin: "2",
		AnnotationAutoscaleMax: "10",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "home-workers", 3, false,
		specs.MachineSetSpec_MachineAllocation_Static)

	client, shutdown := newTestServerWithDiscoverer(t, st, "talos-home")
	defer shutdown()

	resp, err := client.NodeGroups(context.Background(), &pb.NodeGroupsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.NodeGroups, 1)

	g := resp.NodeGroups[0]
	assert.Equal(t, "talos-home-workers", g.Id)
	assert.Equal(t, int32(2), g.MinSize)
	assert.Equal(t, int32(10), g.MaxSize)
	assert.Contains(t, g.Debug, "currentSize=3")
	assert.Contains(t, g.Debug, "home-workers")
}

// TestServer_NodeGroups_EmptyClusterReturnsEmptyList — legitimate
// non-error steady state. A cluster with no opted-in MachineSets
// should get an empty list, not an error, so CAS keeps polling
// without marking us unhealthy.
func TestServer_NodeGroups_EmptyClusterReturnsEmptyList(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	client, shutdown := newTestServerWithDiscoverer(t, st, "talos-home")
	defer shutdown()

	resp, err := client.NodeGroups(context.Background(), &pb.NodeGroupsRequest{})
	require.NoError(t, err)
	assert.Empty(t, resp.NodeGroups)
}

// TestServer_NodeGroupTargetSize_Found verifies the current-count
// read path answers with MachineAllocation.MachineCount. Matches
// CAS's expectation that TargetSize and NodeGroups report the same
// current number on the same refresh tick.
func TestServer_NodeGroupTargetSize_Found(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "home-workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "5",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "home-workers", 4, false,
		specs.MachineSetSpec_MachineAllocation_Static)

	client, shutdown := newTestServerWithDiscoverer(t, st, "talos-home")
	defer shutdown()

	resp, err := client.NodeGroupTargetSize(context.Background(), &pb.NodeGroupTargetSizeRequest{
		Id: "talos-home-workers",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(4), resp.TargetSize)
}

// TestServer_NodeGroupTargetSize_NotFound verifies a structured
// NotFound on an unknown node-group ID. CAS uses this status to
// prune its internal cache — silently returning 0 would make CAS
// keep scaling requests against a deleted MachineSet.
func TestServer_NodeGroupTargetSize_NotFound(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	client, shutdown := newTestServerWithDiscoverer(t, st, "talos-home")
	defer shutdown()

	_, err := client.NodeGroupTargetSize(context.Background(), &pb.NodeGroupTargetSizeRequest{
		Id: "talos-home-ghost",
	})
	require.Error(t, err)

	st2, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st2.Code())
}

// TestServer_NodeGroupForNode_ConfiguredReturnsEmpty pins the phase
// 3c scope: with a Discoverer present, NodeGroupForNode answers nil-
// NodeGroup (a.k.a. "not ours"). Scale-down is disabled at multiple
// layers; this response shape is the sidecar's signal to leave the
// node alone.
func TestServer_NodeGroupForNode_ConfiguredReturnsEmpty(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	client, shutdown := newTestServerWithDiscoverer(t, st, "talos-home")
	defer shutdown()

	resp, err := client.NodeGroupForNode(context.Background(), &pb.NodeGroupForNodeRequest{
		Node: &pb.ExternalGrpcNode{Name: "talos-home-worker-1", ProviderID: "omni://machine/xxx"},
	})
	require.NoError(t, err)
	assert.Nil(t, resp.NodeGroup, "phase 3c: always return 'not ours' for NodeGroupForNode")
}

// --- Phase 3d: NodeGroupIncreaseSize wire-path tests ---------------------
//
// These exercise the full happy path + the reject-with-status matrix so a
// future CAS-sidecar integration test can rely on the exact status codes
// cluster-autoscaler uses to make scheduling decisions:
//   - ResourceExhausted → stop retrying this node group for a while
//   - InvalidArgument → don't retry at all
//   - NotFound → prune from CAS's internal cache
//   - Unavailable → retry later

// TestServer_NodeGroupIncreaseSize_HappyPath pins the write:
// CurrentSize + delta lands in MachineAllocation.MachineCount.
func TestServer_NodeGroupIncreaseSize_HappyPath(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "10",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 3, false,
		specs.MachineSetSpec_MachineAllocation_Static)

	client, shutdown := newTestServerWithDiscoverer(t, st, "talos-home")
	defer shutdown()

	_, err := client.NodeGroupIncreaseSize(context.Background(), &pb.NodeGroupIncreaseSizeRequest{
		Id:    "talos-home-workers",
		Delta: 2,
	})
	require.NoError(t, err)

	got, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "talos-home-workers")
	require.NoError(t, err)
	assert.Equal(t, uint32(5), got.TypedSpec().Value.MachineAllocation.MachineCount,
		"MachineAllocation.MachineCount must reflect the scale-up")
}

// TestServer_NodeGroupIncreaseSize_RejectsNonPositiveDelta maps
// invalid input to InvalidArgument. CAS knows not to retry.
func TestServer_NodeGroupIncreaseSize_RejectsNonPositiveDelta(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "10",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 3, false,
		specs.MachineSetSpec_MachineAllocation_Static)

	client, shutdown := newTestServerWithDiscoverer(t, st, "talos-home")
	defer shutdown()

	_, err := client.NodeGroupIncreaseSize(context.Background(), &pb.NodeGroupIncreaseSizeRequest{
		Id:    "talos-home-workers",
		Delta: 0,
	})
	require.Error(t, err)

	st2, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st2.Code())
}

// TestServer_NodeGroupIncreaseSize_AboveMaxReturnsResourceExhausted
// maps Max breach to ResourceExhausted so CAS stops retrying.
func TestServer_NodeGroupIncreaseSize_AboveMaxReturnsResourceExhausted(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin: "1",
		AnnotationAutoscaleMax: "4",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 3, false,
		specs.MachineSetSpec_MachineAllocation_Static)

	client, shutdown := newTestServerWithDiscoverer(t, st, "talos-home")
	defer shutdown()

	_, err := client.NodeGroupIncreaseSize(context.Background(), &pb.NodeGroupIncreaseSizeRequest{
		Id:    "talos-home-workers",
		Delta: 3, // 3 + 3 = 6 > 4
	})
	require.Error(t, err)

	st2, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.ResourceExhausted, st2.Code())
	assert.Contains(t, st2.Message(), "max")
}

// TestServer_NodeGroupIncreaseSize_UnknownGroupReturnsNotFound
// covers the case where CAS asks to scale a group that's been
// deleted or never existed.
func TestServer_NodeGroupIncreaseSize_UnknownGroupReturnsNotFound(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)

	client, shutdown := newTestServerWithDiscoverer(t, st, "talos-home")
	defer shutdown()

	_, err := client.NodeGroupIncreaseSize(context.Background(), &pb.NodeGroupIncreaseSizeRequest{
		Id:    "talos-home-ghost",
		Delta: 1,
	})
	require.Error(t, err)

	st2, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st2.Code())
}

// TestMapWriterError pins the ScaleWriter-error → gRPC-status
// mapping. The rotation-lock arm is the load-bearing case: without
// the explicit errors.Is(ErrRotationLockHeld) branch, the writer's
// wrapped defense-in-depth error falls into the default arm and CAS
// gets codes.Unavailable — which it interprets as "transient, retry
// immediately", defeating the surge-cycle lock. FailedPrecondition
// tells CAS to back off and re-poll capacity later.
//
// Tested at the helper level (not through gRPC) because the
// Discoverer already pauses locked groups by reporting Min==Max, so
// the writer-level check is a TOCTOU-window guard that only fires
// when a lock lands between Discover and the writer's live CAS read
// — unreachable from a synchronous RPC test without race injection.
func TestMapWriterError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantResult string
		wantCode   codes.Code
	}{
		{
			name:       "AtOrAboveMax → ResourceExhausted (bounds)",
			err:        fmt.Errorf("wrapped: %w", ErrAtOrAboveMax),
			wantResult: ResultRejectedBounds,
			wantCode:   codes.ResourceExhausted,
		},
		{
			name:       "RotationLockHeld → FailedPrecondition (deferred)",
			err:        fmt.Errorf("wrapped: %w", ErrRotationLockHeld),
			wantResult: ResultDeferredRotation,
			wantCode:   codes.FailedPrecondition,
		},
		{
			name:       "unknown error → Unavailable (transient)",
			err:        errors.New("state client broke"),
			wantResult: ResultErroredInternal,
			wantCode:   codes.Unavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotResult, gotErr := mapWriterError(tc.err)
			assert.Equal(t, tc.wantResult, gotResult)

			st, ok := status.FromError(gotErr)
			require.True(t, ok, "returned error must carry a gRPC status")
			assert.Equal(t, tc.wantCode, st.Code())
		})
	}
}

// --- evaluateCapacityGate switch-arm coverage ----------------------------
//
// The four Decision.Outcome arms (DeniedHard, Errored, WarnedSoft, Allowed)
// each map to a different gRPC status / metric / log shape. These tests
// pin the mapping so a future edit that swaps codes.ResourceExhausted ↔
// codes.Unavailable, or drops a recordScaleUpResult metric increment,
// fails CI immediately.

// newTestServerWithGate boots a Server with a real Discoverer + a
// caller-supplied CapacityQuery so tests can drive the capacity-gate
// arms by configuring the fake's response.
func newTestServerWithGate(t *testing.T, st state.State, cluster string, gate CapacityQuery, defaultPool string) (pb.CloudProviderClient, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	cfg := &SubcommandConfig{
		ClusterName:     cluster,
		ListenAddress:   lis.Addr().String(),
		RefreshInterval: time.Minute,
	}

	d := NewDiscoverer(st, cluster, zaptest.NewLogger(t))
	w := NewScaleWriter(st)
	srv := NewServer(zaptest.NewLogger(t), cfg, gate, d, w).WithDefaultPool(defaultPool)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)

	go func() { done <- srv.ServeOnListener(ctx, lis) }()

	cfg.ListenAddress = lis.Addr().String()

	var conn *grpc.ClientConn

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := grpc.NewClient(cfg.ListenAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			conn = c
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NotNil(t, conn)

	shutdown := func() {
		_ = conn.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not shut down within 2s of ctx cancel")
		}
	}

	return pb.NewCloudProviderClient(conn), shutdown
}

// TestNodeGroupIncreaseSize_GateDeniedHard_ReturnsResourceExhausted —
// OutcomeDeniedHard arm: pool free below floor with capacity-gate=hard
// must surface as codes.ResourceExhausted and increment
// recordScaleUpResult with ResultDeniedCapacity.
func TestNodeGroupIncreaseSize_GateDeniedHard_ReturnsResourceExhausted(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)
	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin:            "1",
		AnnotationAutoscaleMax:            "10",
		AnnotationAutoscaleCapacityGate:   string(CapacityGateHard),
		AnnotationAutoscaleMinPoolFreeGiB: "100",
		AnnotationAutoscaleMinHostMemGiB:  "0",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 3, false,
		specs.MachineSetSpec_MachineAllocation_Static)

	// Pool reports 10 GiB free, threshold 100 GiB → DeniedHard.
	gate := fakeCapacityQuery{
		pool: func(_ string) (int64, error) { return 10 * 1024 * 1024 * 1024, nil },
	}

	client, shutdown := newTestServerWithGate(t, st, "talos-home", gate, "default")
	defer shutdown()

	_, err := client.NodeGroupIncreaseSize(context.Background(), &pb.NodeGroupIncreaseSizeRequest{
		Id:    "talos-home-workers",
		Delta: 1,
	})
	require.Error(t, err)

	got, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.ResourceExhausted, got.Code(),
		"hard-deny must surface as ResourceExhausted so CAS backs off this group")
	assert.Contains(t, got.Message(), "capacity gate denied")
}

// TestNodeGroupIncreaseSize_GateErrored_ReturnsUnavailable —
// OutcomeErrored arm: a TrueNAS query failure must surface as
// codes.Unavailable (CAS retries later) rather than ResourceExhausted
// (CAS stops retrying).
func TestNodeGroupIncreaseSize_GateErrored_ReturnsUnavailable(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)
	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin:            "1",
		AnnotationAutoscaleMax:            "10",
		AnnotationAutoscaleCapacityGate:   string(CapacityGateHard),
		AnnotationAutoscaleMinPoolFreeGiB: "100",
		AnnotationAutoscaleMinHostMemGiB:  "0",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 3, false,
		specs.MachineSetSpec_MachineAllocation_Static)

	gate := fakeCapacityQuery{
		pool: func(_ string) (int64, error) {
			return 0, fmtError("transient TrueNAS API failure")
		},
	}

	client, shutdown := newTestServerWithGate(t, st, "talos-home", gate, "default")
	defer shutdown()

	_, err := client.NodeGroupIncreaseSize(context.Background(), &pb.NodeGroupIncreaseSizeRequest{
		Id:    "talos-home-workers",
		Delta: 1,
	})
	require.Error(t, err)

	got, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, got.Code(),
		"errored gate must surface as Unavailable so CAS retries (not ResourceExhausted, which stops retries)")
	assert.Contains(t, got.Message(), "capacity gate query failed")
}

// TestNodeGroupIncreaseSize_GateSoftWarn_ProceedsToWriter —
// OutcomeWarnedSoft arm: capacity-gate=soft + below-threshold must
// log a warn but still proceed to the writer (the scale-up completes).
func TestNodeGroupIncreaseSize_GateSoftWarn_ProceedsToWriter(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)
	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin:            "1",
		AnnotationAutoscaleMax:            "10",
		AnnotationAutoscaleCapacityGate:   string(CapacityGateSoft),
		AnnotationAutoscaleMinPoolFreeGiB: "100",
		AnnotationAutoscaleMinHostMemGiB:  "0",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 3, false,
		specs.MachineSetSpec_MachineAllocation_Static)

	// 10 GiB free under a 100 GiB soft threshold → WarnedSoft.
	gate := fakeCapacityQuery{
		pool: func(_ string) (int64, error) { return 10 * 1024 * 1024 * 1024, nil },
	}

	client, shutdown := newTestServerWithGate(t, st, "talos-home", gate, "default")
	defer shutdown()

	_, err := client.NodeGroupIncreaseSize(context.Background(), &pb.NodeGroupIncreaseSizeRequest{
		Id:    "talos-home-workers",
		Delta: 2,
	})
	require.NoError(t, err, "soft-warn must NOT block scale-up")

	got, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "talos-home-workers")
	require.NoError(t, err)
	assert.Equal(t, uint32(5), got.TypedSpec().Value.MachineAllocation.MachineCount,
		"writer must have applied the delta even on soft-warn")
}

// TestNodeGroupIncreaseSize_GateAllowed_ProceedsToWriter — OutcomeAllowed
// arm: pool above threshold + gate=hard must allow and write the delta.
// Pins the "no metric on the success arm beyond the writer's path" claim.
func TestNodeGroupIncreaseSize_GateAllowed_ProceedsToWriter(t *testing.T) {
	t.Parallel()

	st := newInMemOmniState(t)
	seedMachineClass(t, st, "workers", map[string]string{
		AnnotationAutoscaleMin:            "1",
		AnnotationAutoscaleMax:            "10",
		AnnotationAutoscaleCapacityGate:   string(CapacityGateHard),
		AnnotationAutoscaleMinPoolFreeGiB: "10",
		AnnotationAutoscaleMinHostMemGiB:  "0",
	})
	seedMachineSet(t, st, "talos-home", "talos-home-workers", "workers", 3, false,
		specs.MachineSetSpec_MachineAllocation_Static)

	// 100 GiB free above 10 GiB hard threshold → Allowed.
	gate := fakeCapacityQuery{
		pool: func(_ string) (int64, error) { return 100 * 1024 * 1024 * 1024, nil },
	}

	client, shutdown := newTestServerWithGate(t, st, "talos-home", gate, "default")
	defer shutdown()

	_, err := client.NodeGroupIncreaseSize(context.Background(), &pb.NodeGroupIncreaseSizeRequest{
		Id:    "talos-home-workers",
		Delta: 1,
	})
	require.NoError(t, err)

	got, err := safe.StateGetByID[*omni.MachineSet](context.Background(), st, "talos-home-workers")
	require.NoError(t, err)
	assert.Equal(t, uint32(4), got.TypedSpec().Value.MachineAllocation.MachineCount)
}

// fmtError is a tiny helper so the table tests above don't pull in errors.
// It's a one-line stand-in for fmt.Errorf where the test only needs a
// throwaway error value.
func fmtError(s string) error {
	return &simpleErr{msg: s}
}

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }
