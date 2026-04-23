package client

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWireShape_* are call-site shape-pinning tests. They don't assert
// *correctness* of TrueNAS — they assert we send exactly the wire payload
// we believe TrueNAS accepts, with no extra keys and no missing required
// keys.
//
// Rationale: a test using `assert.Contains(params, "\"force\":true")` will
// keep passing after someone adds `force_after_timeout` alongside, because
// the substring still matches. That's how v0.15.1 shipped with
// `{"force": true, "force_after_timeout": true}` on `vm.delete`, which
// TrueNAS rejects with `[EINVAL] options.force_after_timeout: Extra inputs
// are not permitted`. Every v0.15.1 deprovision cycle stopped a VM and
// then failed at delete — VMs ended up stopped-but-not-removed, Omni
// finalizers stuck, and live cluster members got powered off.
//
// These tests pin the payload with `assert.JSONEq` so adding OR removing a
// key trips the assertion. When TrueNAS legitimately adds a new required
// field, update both the call site and the test in the same commit.
//
// Schema-drift workflow: when `make test-integration` fails with a TrueNAS
// EINVAL on a method, update this file to match the new accepted shape,
// then re-run unit tests. Missing coverage here means the offending call
// ships undetected.

func TestWireShape_VMDelete(t *testing.T) {
	captured := captureParams(t, "vm.delete", func(c *Client) {
		require.NoError(t, c.DeleteVM(context.Background(), 42))
	})

	require.Lenf(t, captured, 2, "vm.delete expects positional [id, opts]")

	var id int
	require.NoError(t, json.Unmarshal(captured[0], &id))
	assert.Equal(t, 42, id)

	// vm.delete options: TrueNAS 25.10 accepts only `force` (bool) and
	// `zvols` (bool). Do NOT add `force_after_timeout` — that's a vm.stop
	// option and vm.delete rejects it.
	assert.JSONEq(t, `{"force":true}`, string(captured[1]))
}

func TestWireShape_VMStop_Force(t *testing.T) {
	captured := captureParams(t, "vm.stop", func(c *Client) {
		require.NoError(t, c.StopVM(context.Background(), 42, true))
	})

	require.Lenf(t, captured, 2, "vm.stop expects positional [id, opts]")

	var id int
	require.NoError(t, json.Unmarshal(captured[0], &id))
	assert.Equal(t, 42, id)

	// vm.stop accepts {force, force_after_timeout}. Force-only is what our
	// cleanup path wants — force_after_timeout would keep the API call open
	// for the VM's stop-timeout which defaults to 90s. That's fine on
	// Deprovision but breaks the caller's ctx-cancel semantics.
	assert.JSONEq(t, `{"force":true}`, string(captured[1]))
}

func TestWireShape_VMStop_Graceful(t *testing.T) {
	captured := captureParams(t, "vm.stop", func(c *Client) {
		require.NoError(t, c.StopVM(context.Background(), 42, false))
	})

	require.Lenf(t, captured, 2, "vm.stop expects positional [id, opts]")
	assert.JSONEq(t, `{"force":false}`, string(captured[1]),
		"graceful stop must emit force:false explicitly — omitting the key makes TrueNAS pick its default, which can drift between versions")
}

func TestWireShape_PoolDatasetDelete(t *testing.T) {
	captured := captureParams(t, "pool.dataset.delete", func(c *Client) {
		require.NoError(t, c.DeleteDataset(context.Background(), "default/omni-vms/test"))
	})

	// pool.dataset.delete: [path]. No options object.
	require.Lenf(t, captured, 1, "pool.dataset.delete expects [path]")
	assert.JSONEq(t, `"default/omni-vms/test"`, string(captured[0]))
}

// --- helpers ---

// captureParams runs `fn` against a mock client that fails the test unless
// exactly one RPC with the expected method name is issued, captures its
// positional `params` array, and returns the array as raw JSON elements.
func captureParams(t *testing.T, expectedMethod string, fn func(*Client)) []json.RawMessage {
	t.Helper()

	var captured []json.RawMessage

	calls := 0

	c := newMockClient(t, func(method string, params json.RawMessage) (any, *jsonRPCError) {
		calls++

		if method != expectedMethod {
			t.Fatalf("unexpected RPC method %q (wanted %q)", method, expectedMethod)
		}

		var arr []json.RawMessage
		require.NoError(t, json.Unmarshal(params, &arr))

		captured = arr

		return true, nil
	})

	fn(c)

	require.Equalf(t, 1, calls, "expected exactly one %q RPC; got %d", expectedMethod, calls)

	return captured
}
