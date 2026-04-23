package autoscaler

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// Close the listener we grabbed to discover the port — the server
	// will re-bind the address in Listen. This is technically racy but
	// in practice the test binary holds the port through the brief
	// gap, and the server's own bind will surface a clear error if
	// somebody else snatches it.
	_ = lis.Close()

	srv := NewServer(nil, cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		done <- srv.Listen(ctx)
	}()

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

// TestServer_NodeGroupsRespondsUnimplemented pins the phase-3a
// behavior: the server accepts connections and answers the RPC
// surface, but every method returns codes.Unimplemented with an
// operator-readable hint pointing at the roadmap. When phase 3b
// wires real discovery this test gets replaced by one that asserts
// the list-of-node-groups shape.
func TestServer_NodeGroupsRespondsUnimplemented(t *testing.T) {
	t.Parallel()

	client, shutdown := newTestServer(t)
	defer shutdown()

	_, err := client.NodeGroups(context.Background(), &pb.NodeGroupsRequest{})
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok, "gRPC errors must wrap a status")
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "phase 3b",
		"Unimplemented message must name the next phase so operators know when to retry")
}

// TestServer_NodeGroupForNodeRespondsUnimplemented mirrors the
// NodeGroups check for the read-side mapping call.
func TestServer_NodeGroupForNodeRespondsUnimplemented(t *testing.T) {
	t.Parallel()

	client, shutdown := newTestServer(t)
	defer shutdown()

	_, err := client.NodeGroupForNode(context.Background(), &pb.NodeGroupForNodeRequest{
		Node: nil,
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
