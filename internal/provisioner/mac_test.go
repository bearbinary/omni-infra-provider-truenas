package provisioner

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var macRe = regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)

func TestDeterministicMAC_Format(t *testing.T) {
	mac := DeterministicMAC("abc-123", 0)
	assert.Regexp(t, macRe, mac, "MAC should be colon-separated hex pairs")
}

func TestDeterministicMAC_LocallyAdministered(t *testing.T) {
	mac := DeterministicMAC("test-request-id", 0)
	assert.Equal(t, "02", mac[:2], "first octet must be 02 (locally-administered unicast)")
}

func TestDeterministicMAC_Deterministic(t *testing.T) {
	mac1 := DeterministicMAC("request-abc-123", 0)
	mac2 := DeterministicMAC("request-abc-123", 0)
	assert.Equal(t, mac1, mac2, "same inputs must produce same MAC")
}

func TestDeterministicMAC_DifferentRequestIDs(t *testing.T) {
	mac1 := DeterministicMAC("request-aaa", 0)
	mac2 := DeterministicMAC("request-bbb", 0)
	assert.NotEqual(t, mac1, mac2, "different request IDs must produce different MACs")
}

func TestDeterministicMAC_DifferentNICIndexes(t *testing.T) {
	mac0 := DeterministicMAC("request-abc", 0)
	mac1 := DeterministicMAC("request-abc", 1)
	mac2 := DeterministicMAC("request-abc", 2)

	assert.NotEqual(t, mac0, mac1, "primary and first additional NIC must differ")
	assert.NotEqual(t, mac0, mac2, "primary and second additional NIC must differ")
	assert.NotEqual(t, mac1, mac2, "additional NICs must differ from each other")
}

func TestDeterministicMAC_AllLocallyAdministered(t *testing.T) {
	// Verify multiple NIC indexes all have the locally-administered prefix
	for i := range 5 {
		mac := DeterministicMAC("request-xyz", i)
		assert.Equal(t, "02", mac[:2], "NIC index %d: first octet must be 02", i)
	}
}

func TestDeterministicMAC_UniqueAcrossMany(t *testing.T) {
	// Generate MACs for many request IDs across multiple NIC indexes and verify no collisions.
	// This catches both same-index and cross-index collisions.
	seen := make(map[string]string) // mac -> "requestID:nicIndex"

	for i := range 1000 {
		requestID := fmt.Sprintf("abcd1234-5678-9abc-def0-%012d", i)

		for nicIdx := range 3 { // primary + 2 additional
			mac := DeterministicMAC(requestID, nicIdx)
			key := fmt.Sprintf("%s:%d", requestID, nicIdx)

			require.Regexp(t, macRe, mac, "invalid MAC for %s", key)

			if prev, exists := seen[mac]; exists {
				t.Fatalf("collision: %s and %s both produced MAC %s", key, prev, mac)
			}

			seen[mac] = key
		}
	}

	// 3000 unique MACs across 1000 request IDs x 3 NIC indexes
	require.Len(t, seen, 3000)
}

// --- ResolveDeterministicMAC tests ---

func TestResolveDeterministicMAC_NoCollision(t *testing.T) {
	existing := map[string]int{"aa:bb:cc:dd:ee:ff": 99}

	mac, collided := ResolveDeterministicMAC("request-abc", 0, existing)
	assert.False(t, collided, "should not report collision when MAC is free")
	assert.Regexp(t, macRe, mac)
	assert.Equal(t, DeterministicMAC("request-abc", 0), mac, "should return base MAC when no collision")
}

func TestResolveDeterministicMAC_NilExistingMACs(t *testing.T) {
	// When ListAllNICMACs fails, existingMACs is nil — should not panic
	mac, collided := ResolveDeterministicMAC("request-abc", 0, nil)
	assert.False(t, collided)
	assert.Equal(t, DeterministicMAC("request-abc", 0), mac)
}

func TestResolveDeterministicMAC_CollisionResolved(t *testing.T) {
	baseMAC := DeterministicMAC("request-abc", 0)
	existing := map[string]int{baseMAC: 42} // simulate another VM already has this MAC

	mac, collided := ResolveDeterministicMAC("request-abc", 0, existing)
	assert.True(t, collided, "should report collision")
	assert.Regexp(t, macRe, mac)
	assert.NotEqual(t, baseMAC, mac, "resolved MAC must differ from the colliding one")
	assert.Equal(t, "02", mac[:2], "resolved MAC must still be locally-administered")
}

func TestResolveDeterministicMAC_CollisionResolvedIsDeterministic(t *testing.T) {
	baseMAC := DeterministicMAC("request-abc", 0)
	existing := map[string]int{baseMAC: 42}

	mac1, _ := ResolveDeterministicMAC("request-abc", 0, existing)
	mac2, _ := ResolveDeterministicMAC("request-abc", 0, existing)
	assert.Equal(t, mac1, mac2, "collision resolution must be deterministic")
}

func TestResolveDeterministicMAC_MultipleCollisions(t *testing.T) {
	// Simulate the unlikely case where both the base MAC AND the first
	// collision-salted MAC are taken.
	existing := make(map[string]int)

	// Block attempt 0 (base)
	existing[DeterministicMAC("request-abc", 0)] = 10

	// Block attempt 1 (first collision salt) — compute it manually
	mac1, _ := ResolveDeterministicMAC("request-abc", 0, map[string]int{
		DeterministicMAC("request-abc", 0): 10,
	})
	existing[mac1] = 20

	// Attempt 2 should find a different MAC
	mac2, collided := ResolveDeterministicMAC("request-abc", 0, existing)
	assert.True(t, collided)
	assert.Regexp(t, macRe, mac2)
	assert.NotEqual(t, DeterministicMAC("request-abc", 0), mac2)
	assert.NotEqual(t, mac1, mac2, "must skip past both blocked MACs")
}
