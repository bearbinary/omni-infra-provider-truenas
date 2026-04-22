package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

// --- omniVMDescription / isOmniManagedVM ---

func TestOmniVMDescription_ContainsPrefixAndRequestID(t *testing.T) {
	t.Parallel()

	desc := omniVMDescription("abc-123")
	assert.True(t, strings.HasPrefix(desc, omniVMDescriptionPrefix),
		"description must start with the ownership prefix so isOmniManagedVM can identify it")
	assert.Contains(t, desc, "abc-123",
		"description must embed the request id so deprovision-time ownership tracing is possible")
}

func TestIsOmniManagedVM_NilReturnsFalse(t *testing.T) {
	t.Parallel()

	// Guards against a nil-deref panic if GetVM ever returns a nil pointer alongside nil error.
	assert.False(t, isOmniManagedVM(nil))
}

func TestIsOmniManagedVM_EmptyDescriptionRejected(t *testing.T) {
	t.Parallel()

	vm := &client.VM{Description: ""}
	assert.False(t, isOmniManagedVM(vm))
}

func TestIsOmniManagedVM_PrefixAtStartAccepted(t *testing.T) {
	t.Parallel()

	vm := &client.VM{Description: omniVMDescription("req-xyz")}
	assert.True(t, isOmniManagedVM(vm))
}

func TestIsOmniManagedVM_LegacyPrefixOnlyStillAccepted(t *testing.T) {
	t.Parallel()

	// Pre-v0.15 VMs had exactly "Managed by Omni infra provider" with no
	// request-id suffix. The ownership check must continue to accept them
	// for the upgrade window documented in docs/upgrading.md.
	vm := &client.VM{Description: "Managed by Omni infra provider"}
	assert.True(t, isOmniManagedVM(vm),
		"legacy v0.14 VMs (no request-id suffix) must still pass the ownership check")
}

func TestIsOmniManagedVM_PrefixMidStringRejected(t *testing.T) {
	t.Parallel()

	// A malicious operator might prepend their own text to trick a startsWith check —
	// HasPrefix guards against that.
	vm := &client.VM{Description: "totally legit VM. Managed by Omni infra provider"}
	assert.False(t, isOmniManagedVM(vm),
		"management marker must appear at the start of the description, not mid-string")
}

// --- verifyZvolOwnership ---

func TestVerifyZvolOwnership_EmptyPathRejected(t *testing.T) {
	t.Parallel()

	err := verifyZvolOwnership(context.Background(), newMockClientForOwnership(nil), "", "req-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestVerifyZvolOwnership_ManagedTrueAndMatchingRequestIDAccepted(t *testing.T) {
	t.Parallel()

	c := newMockClientForOwnership(map[string]map[string]string{
		"tank/omni-vms/req-1": {
			"org.omni:managed":    "true",
			"org.omni:request-id": "req-1",
		},
	})

	err := verifyZvolOwnership(context.Background(), c, "tank/omni-vms/req-1", "req-1")
	assert.NoError(t, err)
}

func TestVerifyZvolOwnership_ManagedMissingRejected(t *testing.T) {
	t.Parallel()

	c := newMockClientForOwnership(map[string]map[string]string{
		"tank/omni-vms/unknown": {},
	})

	err := verifyZvolOwnership(context.Background(), c, "tank/omni-vms/unknown", "req-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not tagged")
}

func TestVerifyZvolOwnership_ManagedFalseRejected(t *testing.T) {
	t.Parallel()

	c := newMockClientForOwnership(map[string]map[string]string{
		"tank/omni-vms/req-1": {
			"org.omni:managed": "false",
		},
	})

	err := verifyZvolOwnership(context.Background(), c, "tank/omni-vms/req-1", "req-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not tagged")
}

func TestVerifyZvolOwnership_RequestIDMismatchRejected(t *testing.T) {
	t.Parallel()

	c := newMockClientForOwnership(map[string]map[string]string{
		"tank/omni-vms/somebody-elses": {
			"org.omni:managed":    "true",
			"org.omni:request-id": "not-our-request",
		},
	})

	err := verifyZvolOwnership(context.Background(), c, "tank/omni-vms/somebody-elses", "req-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has request-id")
	assert.Contains(t, err.Error(), "expected")
}

func TestVerifyZvolOwnership_EmptyExpectedRequestIDSkipsComparison(t *testing.T) {
	t.Parallel()

	// When requestID is "" (e.g., deprovision has no MachineRequest), the
	// managed=true tag alone is enough. This mirrors the fallback path in
	// Deprovision for very old MachineRequests.
	c := newMockClientForOwnership(map[string]map[string]string{
		"tank/omni-vms/anything": {
			"org.omni:managed":    "true",
			"org.omni:request-id": "some-other-request",
		},
	})

	err := verifyZvolOwnership(context.Background(), c, "tank/omni-vms/anything", "")
	assert.NoError(t, err,
		"empty expected request-id should accept any stored request-id provided managed=true")
}

func TestVerifyZvolOwnership_EmptyStoredRequestIDAccepted(t *testing.T) {
	t.Parallel()

	// Pre-v0.10 zvols may have managed=true but no request-id tag. The
	// check explicitly accepts empty stored values.
	c := newMockClientForOwnership(map[string]map[string]string{
		"tank/legacy-zvol": {
			"org.omni:managed": "true",
		},
	})

	err := verifyZvolOwnership(context.Background(), c, "tank/legacy-zvol", "req-1")
	assert.NoError(t, err,
		"pre-v0.10 zvols without request-id tag should still pass ownership check")
}

func TestVerifyZvolOwnership_ManagedReadErrorSurfaced(t *testing.T) {
	t.Parallel()

	// Inject an error on the managed-property read. The error must propagate
	// so cleanup refuses to delete rather than assuming non-ownership.
	c := client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.dataset.query" {
			return nil, errors.New("truenas unreachable")
		}

		return nil, nil
	})

	err := verifyZvolOwnership(context.Background(), c, "tank/something", "req-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read ownership propert")
}

// --- test helpers ---

// newMockClientForOwnership builds a Client whose pool.dataset.query returns
// user_properties matching the supplied map. Keyed by dataset path — in this
// test harness the filter contents are not inspected, so each path must be
// tested in isolation (one path per mock).
func newMockClientForOwnership(propsByPath map[string]map[string]string) *client.Client {
	return client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		if method != "pool.dataset.query" {
			return nil, nil
		}

		// Return a single aggregate response. Our helper only reads one
		// property at a time so we just encode every property the test
		// promised; GetDatasetUserProperty picks the one it asked for.
		// Merge all path entries into a single user_properties map — callers
		// pass a single path per test anyway.
		merged := map[string]any{}
		for _, props := range propsByPath {
			for k, v := range props {
				merged[k] = map[string]any{"value": v}
			}
		}

		return map[string]any{"user_properties": merged}, nil
	})
}
