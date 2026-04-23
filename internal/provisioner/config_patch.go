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

// parseAdvertisedSubnets splits a comma-separated CIDR list and validates each
// entry. Returns nil, nil when the input is empty or only whitespace, matching
// the "no patch needed" signal the callers rely on.
func parseAdvertisedSubnets(advertisedSubnets string) ([]string, error) {
	if advertisedSubnets == "" {
		return nil, nil
	}

	var subnets []string
	for _, s := range strings.Split(advertisedSubnets, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}

		if _, _, err := net.ParseCIDR(s); err != nil {
			return nil, fmt.Errorf("advertised_subnets: %q is not a valid CIDR — %w", s, err)
		}

		subnets = append(subnets, s)
	}

	if len(subnets) == 0 {
		return nil, nil
	}

	return subnets, nil
}

// buildAdvertisedSubnetsPatch generates the full CP-role machine config patch
// (etcd + kubelet). Workers MUST use buildKubeletSubnetsPatch instead —
// `cluster.etcd.advertisedSubnets` applied to a worker fails Talos config
// validation with `etcd config is only allowed on control plane machines`,
// the machine never boots, the MachineRequest never completes.
//
// Observed in production v0.15.0–v0.15.3 with multi-homed talos-home cluster:
// the provider applied this full patch to every MachineRequest regardless of
// role, so every worker was DOA. Fixed by splitting the patch and gating the
// etcd section on CP role detection in the caller (see stepCreateVM).
func buildAdvertisedSubnetsPatch(advertisedSubnets string) ([]byte, error) {
	subnets, err := parseAdvertisedSubnets(advertisedSubnets)
	if err != nil || len(subnets) == 0 {
		return nil, err
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

// buildKubeletSubnetsPatch generates the worker-safe subset of the
// advertised-subnets patch — pins `machine.kubelet.nodeIP.validSubnets` only.
// Safe on any machine role; used on workers and also as a fallback on CPs if
// role detection is ambiguous (we'd rather lose the etcd pinning than fail
// Talos validation).
func buildKubeletSubnetsPatch(advertisedSubnets string) ([]byte, error) {
	subnets, err := parseAdvertisedSubnets(advertisedSubnets)
	if err != nil || len(subnets) == 0 {
		return nil, err
	}

	patch := map[string]any{
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

// resolveNICDHCP applies the DHCP-default policy for AdditionalNIC:
//   - explicit DHCP value wins (true or false)
//   - nil pointer + no static addresses → default true (DHCP on)
//   - nil pointer + static addresses     → default false (static-only)
//
// The pointer-based tri-state lets users distinguish "not set" (accept
// the default) from "explicitly disabled" — e.g., a NIC with static
// addresses AND DHCP on is reachable via both, set DHCP: true explicitly.
func resolveNICDHCP(nic AdditionalNIC) bool {
	if nic.DHCP != nil {
		return *nic.DHCP
	}

	return len(nic.Addresses) == 0
}

// nicInterfaceAggregate counts configured NIC dimensions for operator-facing
// rollout verification. Emitted at Info level alongside the patch so SRE can
// confirm (without turning on Debug) which codepath the operator actually
// reached: DHCP-only, static-addressed, or gateway-bearing.
type nicInterfaceAggregate struct {
	DHCPNICs    int
	StaticNICs  int
	GatewayNICs int
}

// collectNICInterfaceConfigs translates the per-NIC MachineClass spec +
// post-attach MAC addresses into the patch-builder input. Pure function —
// unit-testable without a live ProvisionContext, TrueNAS client, or VM.
//
// Inputs:
//   - nics:         MachineClass AdditionalNICs in declared order
//   - attachedMACs: one entry per NIC (same length/index) — the MAC returned
//     by client.AddNICWithConfig, or "" if the attach returned
//     no MAC. Empty-MAC entries are silently dropped (the
//     patch's deviceSelector would otherwise open-match and
//     apply to the primary NIC).
//
// Returns the per-NIC patch input plus an aggregate count of how many NICs
// fall into each Talos-config bucket. Aggregate is used by the Info log at
// the call site; the []nicInterfaceConfig is passed to
// buildAdditionalNICInterfacesPatch.
//
// Panics if len(nics) != len(attachedMACs). That mismatch means the caller
// skipped a NIC in the attach loop without recording a placeholder — a
// programming error, not recoverable runtime state.
func collectNICInterfaceConfigs(nics []AdditionalNIC, attachedMACs []string) ([]nicInterfaceConfig, nicInterfaceAggregate) {
	if len(nics) != len(attachedMACs) {
		panic(fmt.Sprintf("collectNICInterfaceConfigs: len(nics)=%d != len(attachedMACs)=%d — caller must record one MAC per NIC", len(nics), len(attachedMACs)))
	}

	var (
		configs []nicInterfaceConfig
		agg     nicInterfaceAggregate
	)

	for i, nic := range nics {
		mac := attachedMACs[i]
		if mac == "" {
			continue
		}

		dhcp := resolveNICDHCP(nic)
		configs = append(configs, nicInterfaceConfig{
			mac:       mac,
			dhcp:      dhcp,
			addresses: nic.Addresses,
			gateway:   nic.Gateway,
		})

		if dhcp {
			agg.DHCPNICs++
		}

		if len(nic.Addresses) > 0 {
			agg.StaticNICs++
		}

		if nic.Gateway != "" {
			agg.GatewayNICs++
		}
	}

	return configs, agg
}

// nicInterfaceConfig describes one additional NIC's desired Talos config
// (DHCP on/off, static addresses, optional gateway). MAC identifies the
// target link via deviceSelector so the config survives Talos interface-
// name shifts across reprovision.
type nicInterfaceConfig struct {
	mac       string
	dhcp      bool
	addresses []string
	gateway   string
}

// buildAdditionalNICInterfacesPatch returns a Talos machine config patch
// that configures every supplied additional NIC (DHCP, static IPs, and/or
// default route) via deviceSelector matching on MAC.
//
// Why: Talos's default platform config (nocloud, metal, …) only DHCPs the
// primary link. Additional NICs attached by the provider come up at the
// link layer but have no DHCP client and no static config, so they only
// ever acquire a link-local IPv6 address — the VM is effectively
// single-homed until the operator writes a manual per-machine config
// patch. Emitting this patch at provision time makes additional NICs
// usable out of the box, whether the segment runs DHCPv4 or needs
// explicit static addressing.
//
// Observed on talos-home (multi-homed, 2 NICs per worker) running
// v0.15.5: `talosctl get addresses` showed eth1 UP with only
// link-local IPv6, never a DHCPv4 lease. `talosctl get links` confirmed
// the second virtio_net was detected. No config patch configured the
// link, so it stayed unused.
//
// Orthogonal to advertised_subnets — that pins kubelet/etcd to a
// specific subnet but does not bring additional links up.
//
// MAC matching (not interface-name matching) is required because Talos's
// predictable NIC enumeration can shift between boots (virtio order,
// PCI discovery). The deterministic MACs the provider assigns already
// survive reprovision, so the patch keeps working across VM recreates.
//
// Output (each NIC becomes one interfaces[] entry):
//
//	{
//	  "machine": {
//	    "network": {
//	      "interfaces": [
//	        {
//	          "deviceSelector": {"hardwareAddr": "02:..."},
//	          "dhcp": true,
//	          "addresses": ["10.20.0.5/24"],
//	          "routes": [{"network": "0.0.0.0/0", "gateway": "10.20.0.1"}]
//	        },
//	        ...
//	      ]
//	    }
//	  }
//	}
func buildAdditionalNICInterfacesPatch(nics []nicInterfaceConfig) ([]byte, error) {
	if len(nics) == 0 {
		return nil, nil
	}

	// Defensive reject: duplicate MACs mean collision detection upstream
	// failed (or the caller wired the pre-collision MAC twice). Emitting
	// two interfaces[] entries with the same deviceSelector.hardwareAddr
	// leaves Talos last-write-wins — one config silently loses, probably
	// not the one the operator wanted. Fail closed.
	seen := make(map[string]int, len(nics))

	for i, nic := range nics {
		if nic.mac == "" {
			continue
		}

		if prev, dup := seen[nic.mac]; dup {
			return nil, fmt.Errorf("buildAdditionalNICInterfacesPatch: duplicate MAC %q at indices %d and %d — MAC collision resolution upstream must produce unique MACs per NIC", nic.mac, prev, i)
		}

		seen[nic.mac] = i
	}

	var interfaces []map[string]any

	for _, nic := range nics {
		if nic.mac == "" {
			continue
		}

		iface := map[string]any{
			"deviceSelector": map[string]any{
				"hardwareAddr": nic.mac,
			},
			"dhcp": nic.dhcp,
		}

		if len(nic.addresses) > 0 {
			// encoding/json handles []string inside a map[string]any — the
			// outer map's value type is `any`, and []string assigns cleanly.
			// No need to box into []any first.
			iface["addresses"] = nic.addresses
		}

		if nic.gateway != "" {
			// Pick the default-route network to match the gateway's address
			// family. An IPv6 gateway with network "0.0.0.0/0" is rejected by
			// Talos/Linux at apply time — emit "::/0" instead. Validation at
			// the MachineClass level already enforces family-match between
			// gateway and addresses, so we can pick confidently here.
			network := "0.0.0.0/0"
			if gwIP := net.ParseIP(nic.gateway); gwIP != nil && gwIP.To4() == nil {
				network = "::/0"
			}

			iface["routes"] = []map[string]any{
				{
					"network": network,
					"gateway": nic.gateway,
				},
			}
		}

		interfaces = append(interfaces, iface)
	}

	if len(interfaces) == 0 {
		return nil, nil
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
