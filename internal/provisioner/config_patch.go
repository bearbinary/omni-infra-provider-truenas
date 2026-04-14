package provisioner

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// patchName returns a ConfigPatchRequest name unique per MachineRequest.
//
// The Omni SDK's `provision.Context.CreateConfigPatch(ctx, name, data)` uses
// the literal `name` as the resource ID for the underlying ConfigPatchRequest
// and upserts on every call. If multiple MachineRequests reconcile and call
// CreateConfigPatch with the same unqualified name (e.g. "data-volumes"),
// they all write to the SAME resource — only the last writer's patch survives,
// labeled with the last writer's request ID.
//
// Observed in v0.14.3–v0.14.5: 6 MachineRequests, only 1 ConfigPatchRequest
// named "data-volumes" existed (labeled for whichever request reconciled last),
// the other 5 machines silently went without their UserVolumeConfig patch.
//
// Format: "<kind>-<requestID>". Both components are required to ensure
// uniqueness across both the kind axis (a request can have many patch kinds)
// and the request axis (each request needs its own copy of each kind).
func patchName(kind, requestID string) string {
	return kind + "-" + requestID
}

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

// LonghornVolumeName is the reserved AdditionalDisk.Name that signals
// "this disk is for Longhorn, emit the Longhorn operational patch alongside
// the UserVolumeConfig". Set implicitly by the storage_disk_size shorthand.
// Users can also set this explicitly in additional_disks entries.
const LonghornVolumeName = "longhorn"

// buildLonghornOperationalPatch returns the Talos machine config patch that
// makes a worker Longhorn-ready. Emitted per-worker alongside the
// UserVolumeConfig whenever an additional disk is named "longhorn".
//
// Without this patch Longhorn silently fails in three distinct ways:
//
//  1. iSCSI replica attachment errors — the iscsi_tcp kernel module is NOT
//     loaded by default on Talos. Longhorn's manager can't open iSCSI
//     sessions between replicas and pods. PVCs stay Pending forever.
//
//  2. Data written to ephemeral root — Longhorn's container expects its
//     data path at /var/lib/longhorn. Without the bind mount, that path
//     resolves to Talos's EPHEMERAL partition (the system disk), NOT the
//     dedicated data zvol mounted at /var/mnt/longhorn. Replicas write to
//     the wrong disk, exhaust ephemeral storage, and are lost on node
//     replace. This bug went undetected from v0.13.0 through v0.14.2
//     because scripts/install-longhorn.sh had source==destination
//     (a no-op self-bind) — the bug was fixed in v0.14.3 but only if
//     the script actually ran.
//
//  3. Replica process OOM — vm.overcommit_memory=1 is a Longhorn
//     requirement for replica process stability under memory pressure.
//     Not setting it triggers sporadic OOM kills of replica processes.
//
// This patch eliminates all three silent failure modes at VM creation
// time, so the node is Longhorn-ready as soon as it joins the cluster.
// Users still need to install Longhorn via Helm — the Helm install is
// the only remaining step after v0.14.6+.
func buildLonghornOperationalPatch() ([]byte, error) {
	patch := map[string]any{
		"machine": map[string]any{
			"kernel": map[string]any{
				"modules": []map[string]any{
					{"name": "iscsi_tcp"},
				},
			},
			"kubelet": map[string]any{
				"extraMounts": []map[string]any{
					{
						"destination": "/var/lib/longhorn",
						"type":        "bind",
						"source":      "/var/mnt/longhorn",
						"options":     []string{"bind", "rshared", "rw"},
					},
				},
			},
			"sysctls": map[string]any{
				"vm.overcommit_memory": "1",
			},
		},
	}

	return json.Marshal(patch)
}

// hasLonghornDisk reports whether any additional disk is named "longhorn",
// signaling that the Longhorn operational patch should be emitted for this
// machine. Case-sensitive match — the storage_disk_size shorthand sets
// this name in lowercase via ApplyDefaults.
func hasLonghornDisk(disks []AdditionalDisk) bool {
	for _, d := range disks {
		if d.Name == LonghornVolumeName {
			return true
		}
	}

	return false
}
