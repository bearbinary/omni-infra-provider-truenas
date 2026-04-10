package provisioner

import (
	"crypto/sha256"
	"fmt"
)

// DeterministicMAC derives a stable, locally-administered unicast MAC address
// from a request ID and a NIC index. The same inputs always produce the same MAC,
// so DHCP reservations survive VM reprovisioning.
//
// The NIC index differentiates NICs within a single VM:
//   - 0 = primary NIC
//   - 1, 2, ... = additional NICs (in order)
//
// The first octet is forced to 02 (locally-administered, unicast) per IEEE 802,
// leaving 40 bits of hash entropy — collision probability among N VMs is ~N²/2^41.
func DeterministicMAC(requestID string, nicIndex int) string {
	input := fmt.Sprintf("%s:%d", requestID, nicIndex)
	h := sha256.Sum256([]byte(input))

	// Force locally-administered unicast: first octet = 0x02
	return fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x",
		h[1], h[2], h[3], h[4], h[5])
}

// maxCollisionRetries is the number of rehash attempts before giving up.
// Each attempt shifts to different hash bytes, giving 5 independent tries.
const maxCollisionRetries = 5

// ResolveDeterministicMAC returns a deterministic MAC for the given request ID
// and NIC index, checking it against existingMACs (lowercase MAC -> VM ID).
// If a collision is found with a different VM, it rehashes with an incrementing
// salt. Returns the resolved MAC and whether a collision was encountered.
//
// Collisions are astronomically unlikely (~N²/2^41) but not impossible.
// When one occurs, the salt is appended to the hash input, producing a
// completely different MAC while remaining deterministic for the same inputs.
func ResolveDeterministicMAC(requestID string, nicIndex int, existingMACs map[string]int) (string, bool) {
	for attempt := range maxCollisionRetries {
		var mac string
		if attempt == 0 {
			mac = DeterministicMAC(requestID, nicIndex)
		} else {
			// Rehash with collision salt — deterministic for the same attempt number
			input := fmt.Sprintf("%s:%d:collision:%d", requestID, nicIndex, attempt)
			h := sha256.Sum256([]byte(input))
			mac = fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x",
				h[1], h[2], h[3], h[4], h[5])
		}

		if _, taken := existingMACs[mac]; !taken {
			return mac, attempt > 0
		}
	}

	// Exhausted retries — fall back to the base MAC and let TrueNAS reject
	// the duplicate. This should never happen in practice (5 independent
	// 40-bit hashes all colliding requires ~2^200 luck).
	return DeterministicMAC(requestID, nicIndex), true
}
