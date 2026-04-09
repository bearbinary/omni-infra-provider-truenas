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
- **CNI Selection & Setup Guide** — Flannel, Cilium, Calico setup docs with Talos-specific config ([docs/cni.md](cni.md)) (v0.10.0)
- **Multiple Pool Support** — Per-machine pool selection via `pool` field in MachineClass config, with docs and tests (v0.10.0)
- **CSI Storage Guide** — NFS, iSCSI, democratic-csi, and node-local storage comparison ([docs/storage.md](storage.md)) (v0.10.0)
- **Zvol Tagging** — All provider-managed zvols tagged with `org.omni:managed`, `org.omni:provider`, `org.omni:request-id` (v0.10.0)
- **Pool Validation** — Clear error messages when pool doesn't exist or dataset path used instead of pool name (v0.11.0)
- **MAC Address Logging** — VM NIC MAC logged for DHCP reservation setup (v0.11.0)
- **Networking Guide** — Complete docs for UniFi, pfSense, OPNsense, Mikrotik, MetalLB, VIP, DHCP reservations ([docs/networking.md](networking.md)) (v0.11.0)
- **Control Plane VIP** — Documented as Omni config patch in [networking guide](networking.md) (v0.11.0)
- **Static IP / DHCP Reservations** — Documented router-side DHCP reservation workflow for all platforms (v0.11.0)
- **Multiple NIC Support + VLAN Tagging** — Additional NICs with per-NIC VLAN tagging via `additional_nics` in MachineClass config (v0.11.0)
- **Memory Overcommit Pre-Check** — Blocks VMs requesting >80% of host RAM (v0.12.0)
- **Machine UUID / Infra ID** — Sets `MachineUUID` and `MachineInfraID` for Omni correlation (v0.12.0)
- **TrueNAS Version Check** — Fails at startup on SCALE < 25.04 with clear error (v0.12.0)
- **Graceful VM Shutdown** — ACPI signal with configurable timeout before force-stop (v0.12.0)
- **Advertised Subnets Config Patch** — Generates Talos config patches for multi-NIC etcd/kubelet pinning (v0.12.0)
- **HTTP Health Endpoint** — `/healthz` and `/readyz` on port 8081 for proper K8s probes (v0.12.0)
- **Dataset Prefix** — Custom ZFS dataset path for cluster isolation (`dataset_prefix` in MachineClass) (v0.12.0)
- **Unknown Field Warnings** — Logs warning when MachineClass config has unrecognized fields (v0.12.0)
- **`nic_attach` → `network_interface` rename** — Clearer field naming throughout (v0.12.0)

## Upstream Issues

- **Provision errors not visible in Omni UI** — SDK clears error on every retry, users only see "Provisioning" forever. Filed: [siderolabs/omni#2629](https://github.com/siderolabs/omni/issues/2629)

---

## Security

### Kubernetes NetworkPolicy
Add a NetworkPolicy to the K8s deployment manifests restricting egress to only TrueNAS and Omni endpoints. All other egress should be denied by default.

### seccompProfile in K8s Deployment
Add `seccompProfile: RuntimeDefault` to the pod security context. Best practice for K8s 1.27+ and required by some Pod Security Standards.

---

## Reliability

---

## Storage Integration

### Additional Disk Support (Multi-Disk VMs)
Attach additional zvols to a VM beyond the root disk. Enables dedicated etcd disks (fast SSD pool), bulk data disks (HDD pool), and is a prerequisite for node-local distributed storage (Longhorn, Ceph).

Implementation:
- Add `additional_disks` array to `Data` struct and `schema.json`: `[{"size": 100, "pool": "ssd-pool", "encrypted": false}]`
- Create additional zvols during `stepCreateVM`, tag with same Omni metadata
- Attach via `AddDisk()` for each additional zvol
- Clean up all additional zvols during `Deprovision()`
- Store additional zvol paths in protobuf state for cleanup tracking

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

### MAC Address Stability Across Reprovision
When a VM is deprovisioned and reprovisioned (scale down/up, pool change, manual delete), it gets a new random MAC address, which breaks DHCP reservations. Since the networking guide documents DHCP reservation workflows for UniFi, pfSense, OPNsense, and Mikrotik, this is a real pain point — users set up a reservation, then lose it on reprovision.

Implementation:
- Derive a deterministic MAC from the machine request ID (e.g., hash request ID into the locally-administered MAC range `02:xx:xx:xx:xx:xx`)
- Pass the MAC to `AddNIC()` / `AddNICWithConfig()` via the TrueNAS `vm.device.create` attributes
- Same request ID always gets the same MAC, so DHCP reservations survive reprovision
- Primary NIC only — additional NICs can stay random (they're typically on isolated storage/management networks)

### MTU / Jumbo Frames for Storage NICs
For iSCSI or NFS storage networks on a dedicated NIC, jumbo frames (MTU 9000) significantly improve throughput. The `additional_nics` config has no MTU field — NIC type (VIRTIO/E1000) is set but MTU isn't passed through to TrueNAS or the Talos network config.

Implementation:
- Add optional `mtu` field to `AdditionalNIC` struct and schema.json (default: 1500)
- Pass MTU to TrueNAS `vm.device.create` NIC attributes
- Generate a Talos machine config patch for the corresponding interface to set the MTU at the OS level
- Validate: MTU must match the physical network (bridge/VLAN must also be configured for jumbo frames)

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

---

---

## CI/CD & Release

### Integration Test CI
Run integration tests against a real TrueNAS instance in CI (GitHub Actions self-hosted runner or cloud instance). See [feasibility & cost analysis](integration-test-ci.md).

### Dry-Run / Validate Mode
A `--dry-run` or `--validate` flag that connects to TrueNAS, runs all startup checks (pool, NIC, version, connectivity), and exits without connecting to Omni. Useful for verifying configuration during initial setup and in CI pipelines.

Implementation:
- Add `--validate` flag or `VALIDATE_ONLY=true` env var
- Run `runStartupChecks()` plus the new version check
- Print a summary of detected config (transport, pool free space, NIC choices, TrueNAS version)
- Exit 0 on success, exit 1 with clear error on failure

### Snapshot Rollback Documentation
The client has `RollbackSnapshot()` and pre-upgrade snapshots are created automatically, but there's no documented workflow for users to trigger a rollback after a failed Talos upgrade. Document the manual process (TrueNAS UI or `midclt`) and consider exposing an automated rollback path.

### Helm Chart
Helm chart for deploying the provider as a Kubernetes workload (connecting to TrueNAS remotely via WebSocket). Most homelab users run the provider directly on TrueNAS via Docker, but multi-cluster or enterprise setups may want to manage it as a K8s deployment.

---

### Node Auto-Replace Circuit Breaker
If a VM enters ERROR state and NVRAM reset doesn't fix it, the provider retries forever. There's no circuit breaker that says "this VM is permanently broken — deprovision and request a fresh one." Omni may handle this at the orchestration layer, but a provider-side max-retry with deprovision-and-recreate would be more responsive.

Implementation:
- Track consecutive ERROR recoveries per VM in memory
- After N failures (configurable, default: 3), deprovision the broken VM and let Omni's reconciliation loop create a replacement
- Log clearly: `"VM {id} failed {N} recovery attempts — deprovisioning for replacement"`
- Reset counter on successful RUNNING state

## Might Implement

Items that are feasible but niche — will implement if there's demand.

### Multi-Host Provider
Support multiple TrueNAS hosts behind a single provider instance. Enables HA and load distribution. Most users have a single NAS — this targets the rare multi-host setup.

**Workaround today:** Run a separate provider instance per TrueNAS host, each registered with Omni. Omni handles scheduling across providers natively. This covers most use cases without any provider changes.

**Testing requirement:** Needs a second TrueNAS instance (physical or VM) to develop and test against.

Implementation:
- Accept multiple `TRUENAS_HOST` entries (comma-separated or config file)
- Create a client pool with health checks per host
- Simple bin-packing placement: provision VMs on the host with the most available resources (free RAM + pool space)
- Anti-affinity: use `pctx.GetMachineRequestSetID()` to spread control plane nodes across hosts (Proxmox does this with `pickNode`)
- If a host goes down, new VMs are placed on healthy hosts
- Existing VMs on a failed host are reported as unavailable to Omni
- Add OTEL metrics per host: `truenas.host.vms_running`, `truenas.host.pool_free_bytes`

### Webhook / Event Notifications
Notify external systems when provisioning completes, fails, or a VM enters ERROR state. Currently the only way to observe these events is via logs or Prometheus alerts. A webhook callback would enable automation workflows (e.g., Slack notification on provision failure, auto-trigger config management on new node).

Implementation:
- Add `WEBHOOK_URL` env var (optional)
- POST JSON payloads on key events: `vm.provisioned`, `vm.deprovisioned`, `vm.error`, `vm.upgrade`
- Include VM name, request ID, timestamp, and event-specific data
- Fire-and-forget with short timeout — webhook failures should not block provisioning

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

### UEFI Secure Boot
Enable Secure Boot on VMs via the provider. **Why not:** Secure Boot is configured through Omni's cluster config patches and Talos machine configuration, not at the provider level. Talos handles UKI generation, key enrollment, and the signed boot chain automatically. The provider just boots VMs with standard UEFI firmware — Secure Boot is orthogonal to VM provisioning.

### VM Migration Between Pools
Live-migrate a VM's zvol between pools using `zfs send/recv`. **Why not:** TrueNAS doesn't support this as an atomic operation — it requires stop, send/recv, path update, restart. Easier to deprovision and reprovision the VM on the new pool. Omni handles this gracefully since it treats VMs as cattle.

### Network Policy Enforcement at Hypervisor Level
Use TrueNAS bridge firewall rules (nftables) to enforce network isolation between clusters. **Why not:** Kubernetes CNIs (Cilium, Calico) already enforce NetworkPolicy natively — duplicating at the hypervisor is redundant. Managing nftables on TrueNAS is fragile (rules can reset on OS updates) and operates outside the JSON-RPC API. For physical segmentation, use the Multiple NIC + VLAN Tagging feature to put clusters on separate VLANs.
