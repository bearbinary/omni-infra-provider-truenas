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
