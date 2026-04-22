package provisioner

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

// --- classifyTOFU decision table ---

func TestClassifyTOFU_EmptyStoredIsFirstUse(t *testing.T) {
	t.Parallel()

	assert.Equal(t, tofuFirstUse, classifyTOFU("", "abc"))
}

func TestClassifyTOFU_ExactMatch(t *testing.T) {
	t.Parallel()

	assert.Equal(t, tofuMatch, classifyTOFU("abc", "abc"))
}

func TestClassifyTOFU_Mismatch(t *testing.T) {
	t.Parallel()

	assert.Equal(t, tofuMismatch, classifyTOFU("abc", "def"))
}

func TestClassifyTOFU_PoisonedPrefixShortCircuits(t *testing.T) {
	t.Parallel()

	// Poisoned outcome fires whether the downloaded hash matches the poison
	// marker's tail or not — poison is permanent until operator cleanup.
	cases := []struct {
		stored, downloaded string
	}{
		{poisonMarker("badhash"), "badhash"},
		{poisonMarker("badhash"), "anything"},
		{poisonMarker(""), "anything"},
	}

	for _, tc := range cases {
		assert.Equal(t, tofuPoisoned, classifyTOFU(tc.stored, tc.downloaded),
			"stored=%q downloaded=%q should classify as poisoned", tc.stored, tc.downloaded)
	}
}

func TestCachedISOPoisoned(t *testing.T) {
	t.Parallel()

	assert.True(t, cachedISOPoisoned("POISONED-abc"))
	assert.True(t, cachedISOPoisoned("POISONED-"))
	assert.False(t, cachedISOPoisoned(""))
	assert.False(t, cachedISOPoisoned("abc"))
	assert.False(t, cachedISOPoisoned("poisoned-abc"), "prefix is case-sensitive on purpose")
}

func TestPoisonMarker_Format(t *testing.T) {
	t.Parallel()

	m := poisonMarker("abc123")
	assert.True(t, strings.HasPrefix(m, poisonedPrefix))
	assert.True(t, strings.HasSuffix(m, "abc123"))
	// Round-trip: a constructed poison marker must classify as poisoned.
	assert.Equal(t, tofuPoisoned, classifyTOFU(m, "abc123"))
}

// --- Integration via MockClient: observable side effects ---

// tofuMockClient tracks which dataset-user-property calls were made. Mocks
// pool.dataset.query (read) and pool.dataset.update (write) for the ISO hash
// property, plus the minimal calls the step emits around them.
type tofuMockClient struct {
	mu        sync.Mutex
	props     map[string]string // key: property name, value: stored string
	setCalls  []struct{ Key, Value string }
	updateErr error
}

func (m *tofuMockClient) handler(method string, params json.RawMessage) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch method {
	case "pool.dataset.query":
		// Return all tracked properties; GetDatasetUserProperty picks the one it wants.
		userProps := map[string]any{}
		for k, v := range m.props {
			userProps[k] = map[string]any{"value": v}
		}

		return map[string]any{"user_properties": userProps}, nil

	case "pool.dataset.update":
		if m.updateErr != nil {
			return nil, m.updateErr
		}

		// Extract the user_properties_update entry.
		var args []any
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, err
		}

		if len(args) < 2 {
			return nil, nil
		}

		upd, _ := args[1].(map[string]any)
		ups, _ := upd["user_properties_update"].([]any)

		for _, e := range ups {
			entry, _ := e.(map[string]any)
			k, _ := entry["key"].(string)
			v, _ := entry["value"].(string)
			m.props[k] = v
			m.setCalls = append(m.setCalls, struct{ Key, Value string }{k, v})
		}

		return nil, nil
	}

	return nil, nil
}

// newTOFUClient wires a MockClient into a real *client.Client so our helper
// code under test hits the mock's handler through the normal RPC path.
func newTOFUClient(t *testing.T) (*client.Client, *tofuMockClient) {
	t.Helper()

	m := &tofuMockClient{props: map[string]string{}}
	return client.NewMockClient(m.handler), m
}

func TestSetDatasetUserProperty_WritesValue(t *testing.T) {
	t.Parallel()

	c, m := newTOFUClient(t)
	ctx := context.Background()

	require.NoError(t, c.SetDatasetUserProperty(ctx, "tank/talos-iso", "org.omni:iso-sha256-xyz", "deadbeef"))

	m.mu.Lock()
	defer m.mu.Unlock()
	require.Len(t, m.setCalls, 1)
	assert.Equal(t, "org.omni:iso-sha256-xyz", m.setCalls[0].Key)
	assert.Equal(t, "deadbeef", m.setCalls[0].Value)
}

func TestGetDatasetUserProperty_ReturnsStoredValue(t *testing.T) {
	t.Parallel()

	c, m := newTOFUClient(t)
	ctx := context.Background()

	// Seed the mock.
	m.mu.Lock()
	m.props["org.omni:iso-sha256-xyz"] = "deadbeef"
	m.mu.Unlock()

	got, err := c.GetDatasetUserProperty(ctx, "tank/talos-iso", "org.omni:iso-sha256-xyz")
	require.NoError(t, err)
	assert.Equal(t, "deadbeef", got)
}

func TestGetDatasetUserProperty_MissingReturnsEmpty(t *testing.T) {
	t.Parallel()

	c, _ := newTOFUClient(t)
	ctx := context.Background()

	got, err := c.GetDatasetUserProperty(ctx, "tank/talos-iso", "org.omni:iso-sha256-never-set")
	require.NoError(t, err)
	assert.Equal(t, "", got, "missing property returns empty string so callers can distinguish first-use")
}

// TestISOPoisonMarker_RoundTrip simulates the full mismatch path observable
// via the client: set a hash, detect mismatch on a new download, write a
// POISON marker. Verifies the marker is persisted and classified correctly
// on the next read.
func TestISOPoisonMarker_RoundTrip(t *testing.T) {
	t.Parallel()

	c, m := newTOFUClient(t)
	ctx := context.Background()
	key := "org.omni:iso-sha256-xyz"

	// Simulate a TOFU first-use recording.
	require.NoError(t, c.SetDatasetUserProperty(ctx, "tank/talos-iso", key, "expected-hash"))

	// Next provision: downloaded hash differs. Decision logic classifies mismatch.
	stored, err := c.GetDatasetUserProperty(ctx, "tank/talos-iso", key)
	require.NoError(t, err)
	assert.Equal(t, tofuMismatch, classifyTOFU(stored, "attacker-hash"))

	// The step then overwrites with a POISON marker.
	require.NoError(t, c.SetDatasetUserProperty(ctx, "tank/talos-iso", key, poisonMarker("attacker-hash")))

	// A subsequent provision reads the poisoned value and refuses.
	stored, err = c.GetDatasetUserProperty(ctx, "tank/talos-iso", key)
	require.NoError(t, err)
	assert.True(t, cachedISOPoisoned(stored),
		"POISON marker must survive round-trip through the dataset property store")
	assert.Equal(t, tofuPoisoned, classifyTOFU(stored, "any-new-hash"))

	// Verify the mock tracked both set calls.
	m.mu.Lock()
	defer m.mu.Unlock()
	assert.Len(t, m.setCalls, 2)
	assert.Equal(t, "expected-hash", m.setCalls[0].Value)
	assert.True(t, strings.HasPrefix(m.setCalls[1].Value, poisonedPrefix))
}
