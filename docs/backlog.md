# Production Backlog

Tracked improvements for future releases.

## Completed

- **ISO Cleanup** — Periodic cleanup of stale ISOs (v0.4.0)
- **Orphan Cleanup** — Removes orphan VMs/zvols not tracked by Omni (v0.4.0)
- **Error Reporting** — User-friendly error messages for Omni UI (v0.4.0)
- **Rate Limiting** — Semaphore-based API call limiter (v0.5.0)
- **Resource Pre-checks** — Pool free space check before zvol creation (v0.5.0)
- **Disk Resize** — Online zvol grow when MachineClass disk_size increases (v0.6.0)
- **Backup/Snapshot Support** — Auto-snapshot before Talos upgrades with retention policy (v0.6.0)
- **Comprehensive QA** — 147 tests: e2e, contract, chaos, stress, telemetry integration (v0.7.0)
- **Talos Upgrade Orchestration** — Version detection, pre-upgrade snapshot, CDROM swap to new ISO (v0.8.0)
- **NVRAM Firmware Recovery** — Auto-detect ERROR state VMs, reset NVRAM, restart (v0.8.0)
- **Host Health Monitoring** — OTEL gauges for CPU, memory, pool space/health, disks, running VMs (v0.9.0)
- **Automatic Pool Selection** — Select healthy pool with most free space when not explicit (v0.9.0)
- **Prometheus Alerting Rules** — 7 rules: VM errors, API latency, pool space/health, provision speed (v0.9.0)
- **Grafana Dashboard** — Pre-built dashboard auto-loaded with all provider metrics (v0.9.0)
- **VM Resource Monitoring** — Per-VM runtime stats via host monitor (v0.9.0)
- **Docker Image Signing + SBOM** — Cosign keyless signing + SPDX SBOM on every release (v0.9.1)
- **ZFS Encryption at Rest** — AES-256-GCM encrypted zvols with auto-unlock on reboot (v0.10.0)

---

## Storage Integration

### CSI Storage Auto-Configuration
Auto-configure persistent storage for provisioned clusters via Omni config patches. See [Storage Guide](storage.md) for the full comparison of CSI options.

The recommended starting point is **NFS with nfs-subdir-external-provisioner** (simplest, no Talos extensions needed). For better performance with stateful workloads, **democratic-csi with iSCSI** provides block-level storage with dynamic ZFS zvol provisioning.

> **Note:** democratic-csi's API-based drivers use the TrueNAS REST v2.0 API, while SCALE 25.04+ uses JSON-RPC internally. The SSH-based drivers (which execute ZFS commands directly) are unaffected. Verify REST API compatibility on your version before choosing API-based drivers.

Implementation:
- Detect available NFS/iSCSI services on TrueNAS via `sharing.nfs.query` / `iscsi.target.query`
- For NFS: generate Helm values for nfs-subdir-external-provisioner with server IP and share path
- Inject as a cluster-level config patch in Omni during provisioning
- For iSCSI: include `iscsi-tools` Talos system extension in machine config
- Configure a default StorageClass so PVCs work out of the box

### Node-Local Distributed Storage (Not Recommended)
Options like [Longhorn](https://longhorn.io/), [Rook/Ceph](https://rook.io/), and [Mayastor](https://openebs.io/docs/concepts/mayastor) can run inside the cluster using extra virtual disks attached to VMs. These are documented in the [Storage Guide](storage.md) for completeness, but **not recommended** for TrueNAS-hosted VMs — they treat virtual disks as if they were real physical drives, adding a redundant replication/management layer on top of storage that TrueNAS is already managing via ZFS. This means double the write amplification and no benefit from ZFS features like snapshots, replication, or scrubbing.

If you need replicated storage independent of the NAS, these options work — they just aren't efficient in a virtualized-on-ZFS environment. Requires multi-disk VM support (see below).

---

## Networking

### Control Plane VIP
Provide a shared virtual IP for the Kubernetes API server across control plane nodes, eliminating the need for an external load balancer. Talos uses etcd leader election to assign the VIP to one control plane node at a time (~1 min failover on unexpected failure). See [Talos VIP docs](https://docs.siderolabs.com/talos/v1.12/networking/advanced/vip).

Implementation:
- Add `vip` field to MachineClass config (or Omni cluster-level config patch)
- Generate a Talos machine config patch with `Layer2VIPConfig` specifying the VIP address and network interface
- Apply only to control plane nodes
- Requirements: all control plane nodes must share a Layer 2 network, VIP must be outside DHCP range
- Note: VIP is unavailable until etcd is bootstrapped; not recommended for Talos API access, only Kubernetes API

### Static IP Assignment
Assign predictable IPs to nodes for stable API server endpoints and reliable cluster operation. DHCP remains the default when no network config is set. When a user defines a network block in the MachineClass, the provider generates Talos machine config patches with static networking for each provisioned node.

A typical homelab /24 layout: `.50-.200` for nodes, `.201-.250` for load balancer / MetalLB IPs — but the user controls the range.

Implementation:
- Add network config fields to `Data` struct and `schema.json`: `network_cidr` (e.g., `192.168.1.0/24`), `gateway`, `dns_servers`, `ip_range_start`, `ip_range_end`
- Provider assigns the next available IP from the range when provisioning a node
- Track assigned IPs in MachineSpec protobuf state to prevent conflicts
- Generate a Talos machine config patch with static IP, gateway, and DNS for each node
- Fall back to DHCP when network fields are empty (current behavior)
- Validate that the range falls within the CIDR and doesn't overlap with the gateway

### Multiple NIC Support + VLAN Tagging
Attach multiple NICs to a VM for network segmentation (e.g., cluster traffic on one NIC, storage/iSCSI on another). Each NIC can optionally tag traffic with a VLAN ID, removing the need for pre-configured bridge/VLAN interfaces on TrueNAS.

Implementation:
- Add `additional_nics` array to `Data` struct and `schema.json`: `[{"nic_attach": "enp5s0", "type": "VIRTIO", "vlan_id": 100}]`
- `vlan_id` is optional per NIC — when set, use `trust_guest_rx_filters` on the TrueNAS device and generate a Talos VLAN interface config patch
- Call `AddNIC()` for each additional NIC during `stepCreateVM`
- Generate Talos network config patches to assign roles to each interface (e.g., eth0 = cluster, eth1 = storage VLAN)
- Update `schema.json` with the array field

### Multihoming / Dual-Stack Support
When VMs have multiple NICs or both IPv4 and IPv6 addresses, Talos needs explicit configuration to control which addresses etcd and kubelet use for communication. Without this, services may select different addresses across restarts, destabilizing the cluster. See [Talos Multihoming docs](https://docs.siderolabs.com/talos/v1.12/networking/multihoming).

This pairs with Multiple NIC Support (above) — once a VM has multiple interfaces, the provider needs to generate the right Talos config patches so cluster communication stays on the correct network.

Implementation:
- Add `advertised_subnets` (list of CIDRs) to `Data` struct and `schema.json` for controlling etcd and kubelet address selection
- Generate Talos machine config patches setting `cluster.etcd.advertisedSubnets` and `machine.kubelet.nodeIP.validSubnets` to the specified subnets
- Default: when a single NIC is used, no patch is needed (current behavior)
- When `additional_nics` is configured, require or strongly recommend setting `advertised_subnets` to pin cluster traffic to the primary NIC's subnet
- For dual-stack (IPv4 + IPv6): accept both IPv4 and IPv6 CIDRs in the list, Talos handles the rest
- Validate that the advertised subnets match at least one NIC's network

### CNI Selection & Setup Guide
Document how to swap from the default Flannel CNI to Cilium or Calico via Omni config patches. This is a user-side change (Omni cluster config patch), not a provider-side change — but users need guidance since CNI must be chosen before cluster bootstrap.

Options to cover:
- **Flannel** (default) — zero config, works out of the box, sufficient for most clusters
- **Cilium** (recommended for advanced use) — eBPF dataplane, Hubble observability, network policy enforcement. See [Siderolabs Cilium guide](https://docs.siderolabs.com/kubernetes-guides/cni/deploying-cilium)
- **Calico** — NFTables or eBPF dataplane, Tigera operator, network policy. See [Siderolabs Calico guide](https://docs.siderolabs.com/kubernetes-guides/cni/deploy-calico)

Documentation should include:
- Applying the `cluster.network.cni.name: none` machine config patch as an Omni cluster-level config patch before bootstrap
- Helm install or manifest deployment steps for each CNI
- When to choose each option (Flannel for simplicity, Cilium/Calico for network policy and observability)

---

## Multi-Node

### Multi-Host Provider (Low Priority)
Support multiple TrueNAS hosts behind a single provider instance. Enables HA and load distribution. Most users have a single NAS — this targets the rare multi-host setup.

**Workaround today:** Run a separate provider instance per TrueNAS host, each registered with Omni. Omni handles scheduling across providers natively. This covers most use cases without any provider changes.

**Testing requirement:** Needs a second TrueNAS instance (physical or VM) to develop and test against.

Implementation:
- Accept multiple `TRUENAS_HOST` entries (comma-separated or config file)
- Create a client pool with health checks per host
- Simple bin-packing placement: provision VMs on the host with the most available resources (free RAM + pool space)
- If a host goes down, new VMs are placed on healthy hosts
- Existing VMs on a failed host are reported as unavailable to Omni
- Add OTEL metrics per host: `truenas.host.vms_running`, `truenas.host.pool_free_bytes`

---

## Security

### UEFI Secure Boot
Enable Secure Boot on VMs using Talos's signed boot chain. Talos handles all signing, UKI generation, and key enrollment automatically — the provider just needs to use the right ISO and configure the VM firmware. See [Talos SecureBoot docs](https://docs.siderolabs.com/talos/v1.12/platform-specific-installations/bare-metal-platforms/secureboot).

Implementation:
- Add `secure_boot` boolean to `Data` struct and `schema.json`
- When enabled, download the SecureBoot-specific ISO from Image Factory instead of the standard nocloud ISO
- Create the VM with OVMF firmware in Secure Boot + setup mode (first boot auto-enrolls keys)
- Verify TrueNAS OVMF variant supports Secure Boot setup mode
- Fall back to standard UEFI when `secure_boot` is false (current behavior)

---

## CI/CD & Release

### Integration Test CI
Run integration tests against a real TrueNAS instance in CI (GitHub Actions self-hosted runner or cloud instance).

---

## Might Implement

Items that are feasible but niche — will implement if there's demand.

### GPU/PCIe Passthrough
TrueNAS supports PCI device passthrough to VMs. Useful for AI/ML workloads (Ollama, vLLM), video transcoding (Plex/Jellyfin), and hardware crypto acceleration.

**Practical reality:** Most homelab servers have 1-2 GPUs. PCI passthrough is exclusive — one device, one VM. A typical setup would be a dedicated `truenas-gpu-worker` MachineClass with `replicas: 1` targeting a specific PCI slot, alongside regular non-GPU workers. The GPU node is effectively a pet disguised as cattle (same PCI slot on reprovision).

**Risk:** Users setting `pci_devices` on a MachineClass with multiple replicas when only 1 GPU exists. The provider must return a clear error ("PCI device 0000:01:00.0 is already attached to another VM") rather than a cryptic API failure.

Implementation:
- Add `pci_devices` array to `Data` struct: `[{"pci_slot": "0000:01:00.0"}]`
- Query available PCI devices via `vm.device.passthrough_device_choices`
- Attach devices using `vm.device.create` with `dtype: "PCI"` during `stepCreateVM`
- Validate device is available (not already passed through to another VM)
- Add a pre-check: verify IOMMU is enabled on the host
- Update `schema.json` with passthrough config

---

## Not Implementing

Items considered and intentionally ruled out.

### Cross-Host ZFS Replication
Replicate VM zvols to a secondary TrueNAS host using `zfs send/recv` for DR failover. **Why not:** complexity is enormous for a homelab provider — replication lag tracking, split-brain handling, zvol promotion, and VM re-registration. Users needing DR should use TrueNAS's built-in replication tasks or a dedicated backup solution. Out of scope for this provider.

### ZFS Encryption at Rest
Create VM zvols with ZFS native encryption. **Why not:** requires key management (loading keys on reboot before VMs can start), and TrueNAS already supports pool-level encryption configured through its own UI. The provider shouldn't reimplement what TrueNAS handles natively. Users who want encryption should enable it at the pool or dataset level in TrueNAS.

### VM Migration Between Pools
Live-migrate a VM's zvol between pools using `zfs send/recv`. **Why not:** TrueNAS doesn't support this as an atomic operation — it requires stop, send/recv, path update, restart. Easier to deprovision and reprovision the VM on the new pool. Omni handles this gracefully since it treats VMs as cattle.

### Network Policy Enforcement at Hypervisor Level
Use TrueNAS bridge firewall rules (nftables) to enforce network isolation between clusters. **Why not:** Kubernetes CNIs (Cilium, Calico) already enforce NetworkPolicy natively — duplicating at the hypervisor is redundant. Managing nftables on TrueNAS is fragile (rules can reset on OS updates) and operates outside the JSON-RPC API. For physical segmentation, use the Multiple NIC + VLAN Tagging feature to put clusters on separate VLANs.
