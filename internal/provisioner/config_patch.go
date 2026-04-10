package provisioner

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// buildAdvertisedSubnetsPatch generates a Talos machine config patch that pins
// etcd and kubelet to specific subnets. This is required for multi-NIC setups
// to prevent cluster traffic from using the wrong interface.
//
// Input: comma-separated CIDRs, e.g., "192.168.100.0/24" or "192.168.100.0/24,fd00::/64"
//
// Output: JSON config patch for Talos machine config:
//
//	{
//	  "cluster": {
//	    "etcd": {
//	      "advertisedSubnets": ["192.168.100.0/24"]
//	    }
//	  },
//	  "machine": {
//	    "kubelet": {
//	      "nodeIP": {
//	        "validSubnets": ["192.168.100.0/24"]
//	      }
//	    }
//	  }
//	}

// nicMTUConfig pairs a MAC address with a desired MTU for Talos config patching.
type nicMTUConfig struct {
	mac string
	mtu int
}

// buildMTUPatch generates a Talos machine config patch that sets MTU on
// network interfaces identified by their hardware (MAC) address.
//
// Using MAC address matching ensures the correct interface is configured
// regardless of Talos interface naming (eth0, enp0s4, etc.).
//
// Output: JSON config patch for Talos machine config:
//
//	{
//	  "machine": {
//	    "network": {
//	      "interfaces": [
//	        {
//	          "deviceSelector": {"hardwareAddr": "00:a0:98:..."},
//	          "mtu": 9000
//	        }
//	      ]
//	    }
//	  }
//	}
func buildMTUPatch(nics []nicMTUConfig) ([]byte, error) {
	var interfaces []map[string]any

	for _, nic := range nics {
		interfaces = append(interfaces, map[string]any{
			"deviceSelector": map[string]any{
				"hardwareAddr": nic.mac,
			},
			"mtu": nic.mtu,
		})
	}

	patch := map[string]any{
		"machine": map[string]any{
			"network": map[string]any{
				"interfaces": interfaces,
			},
		},
	}

	return json.Marshal(patch)
}

func buildAdvertisedSubnetsPatch(advertisedSubnets string) ([]byte, error) {
	if advertisedSubnets == "" {
		return nil, nil
	}

	// Parse comma-separated CIDRs
	var subnets []string
	for _, s := range strings.Split(advertisedSubnets, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}

		// Validate CIDR notation
		_, _, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("advertised_subnets: %q is not a valid CIDR — %w", s, err)
		}

		subnets = append(subnets, s)
	}

	if len(subnets) == 0 {
		return nil, nil
	}

	patch := map[string]any{
		"cluster": map[string]any{
			"etcd": map[string]any{
				"advertisedSubnets": subnets,
			},
		},
		"machine": map[string]any{
			"kubelet": map[string]any{
				"nodeIP": map[string]any{
					"validSubnets": subnets,
				},
			},
		},
	}

	return json.Marshal(patch)
}
