# Production Backlog

Tracked improvements for future releases.

## Completed

- **ISO Cleanup** — Periodic cleanup of stale ISOs (v0.4.0)
- **Orphan Cleanup** — Removes orphan VMs/zvols not tracked by Omni (v0.4.0, rewritten in v0.12.0 to use TrueNAS state queries instead of in-memory tracking)
- **Error Reporting** — User-friendly error messages for Omni UI (v0.4.0)
- **Rate Limiting** — Semaphore-based API call limiter (v0.5.0)
- **Resource Pre-checks** — Pool free space check before zvol creation (v0.5.0)
- **Disk Resize** — Online zvol grow when MachineClass disk_size increases (v0.6.0)
- **Comprehensive QA** — 147 tests: e2e, contract, chaos, stress, telemetry integration (v0.7.0)
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
- **Multiple NIC Support** — Additional NICs via `additional_nics` in MachineClass config (v0.11.0, VLAN attr removed in v0.12.0 — TrueNAS 25.10 rejects VM-level tagging)
- **Memory Overcommit Pre-Check** — Blocks VMs requesting >80% of host RAM (v0.12.0)
- **Machine UUID / Infra ID** — Provider-generated SMBIOS UUID v7 passed to `vm.create` for Omni correlation. Fixes ghost "Provisioned/Waiting" entries (v0.12.0)
- **TrueNAS Version Check** — Fails at startup on SCALE < 25.04 with clear error (v0.12.0)
- **Graceful VM Shutdown** — ACPI signal with configurable timeout before force-stop (v0.12.0)
- **Advertised Subnets Config Patch** — Generates Talos config patches for multi-NIC etcd/kubelet pinning. Auto-detects primary NIC subnet when not explicitly set (v0.12.0)
- **HTTP Health Endpoint** — `/healthz` and `/readyz` on port 8081 for proper K8s probes (v0.12.0)
- **Dataset Prefix** — Custom ZFS dataset path for cluster isolation (`dataset_prefix` in MachineClass) (v0.12.0)
- **Unknown Field Warnings** — Logs warning when MachineClass config has unrecognized fields (v0.12.0)
- **`nic_attach` → `network_interface` rename** — Clearer field naming throughout (v0.12.0)
- **Per-Zvol Encryption Passphrases** — Each encrypted zvol gets a unique passphrase stored as ZFS user property, replaces global `ENCRYPTION_PASSPHRASE` env var (v0.12.0)
- **Orphan Cleanup Rewrite** — Replaced in-memory VM tracking with TrueNAS state queries (`org.omni:managed` properties). Safe across restarts, handles dataset prefixes (v0.12.0)
- **Multi-Homing Guide** — Traefik with internal + DMZ subnets, MetalLB, firewall rules ([docs/multihoming.md](multihoming.md)) (v0.12.0)
- **Additional Disk Support** — Multi-disk VMs via `additional_disks` in MachineClass config. Per-disk pool and encryption. Prerequisite for Longhorn (v0.13.0)
- **Disk Resize for Additional Disks** — Additional disks resize on re-provision when config size increases, matching root disk behavior (v0.13.0)
- **Additional Disk Integration Tests** — 8 integration tests: multi-disk create/attach, deprovision cleanup, non-existent pool, encrypted lifecycle, dataset prefix hierarchy, resize grow/no-shrink, pool space check (v0.13.0)
- **MTU / Jumbo Frames** — Optional `mtu` field on `additional_nics`, passed to TrueNAS and applied as Talos config patch via MAC-based matching (v0.13.0)
- **Deterministic MAC Addresses** — Primary NIC always gets a deterministic MAC derived from request ID. Additional NICs opt in via `deterministic_mac`. DHCP reservations survive reprovision (v0.13.0)
- **Node Auto-Replace Circuit Breaker** — VMs stuck in ERROR state auto-deprovisioned after `MAX_ERROR_RECOVERIES` (default 5) consecutive failures. Counter resets on RUNNING (v0.13.0)
- **Backup Guide** — Control plane backup via Omni, workload/PVC backup via Velero ([docs/backup.md](backup.md)) (v0.13.0)

## Upstream Issues

- **Provision errors not visible in Omni UI** — SDK clears error on every retry, users only see "Provisioning" forever. Filed: [siderolabs/omni#2629](https://github.com/siderolabs/omni/issues/2629)
- **Teardown stuck when machine never joined Omni** — SDK's `reconcileTearingDown` never calls `Deprovision` if machine state was destroyed before the check. Filed: [siderolabs/omni#2642](https://github.com/siderolabs/omni/issues/2642)
- **Provision steps not re-run on Talos upgrade** — SDK returns early for `PROVISIONED` machines, so upgrade hooks (CDROM swap) never fire. Filed: [siderolabs/omni#2646](https://github.com/siderolabs/omni/issues/2646)
- **Pressure-based autoscaling patterns** — Discussion on how infra providers should handle autoscaling. [siderolabs/omni#2647](https://github.com/siderolabs/omni/discussions/2647)

---

## Storage Integration

### CSI Storage Auto-Configuration
Auto-configure persistent storage for provisioned clusters via Omni config patches. The recommended driver is **democratic-csi** (actively maintained, single-maintainer). See [Storage Guide](storage.md) for driver comparison and maintenance status.

> **Note:** democratic-csi's API-based drivers use the TrueNAS REST v2.0 API, while SCALE 25.04+ uses JSON-RPC internally. The SSH-based drivers (which execute ZFS commands directly) are unaffected. Verify REST API compatibility on your version before choosing API-based drivers.

Implementation:
- For democratic-csi NFS mode: auto-create NFS share on TrueNAS, generate Helm values, inject as cluster config patch
- For democratic-csi iSCSI mode: auto-enable iSCSI service, include `iscsi-tools` Talos extension in machine config
- Configure a default StorageClass so PVCs work out of the box
- Fallback: manual NFS PVs (documented in [Storage Guide](storage.md#manual-nfs-pvs-fallback)) require no auto-configuration

### Longhorn Support
Now that multi-disk VM support has landed (v0.13.0), document and test Longhorn deployment on TrueNAS-hosted clusters. Longhorn requires additional virtual disks attached to worker nodes for its storage pool. See [Storage Guide](storage.md) for trade-offs vs democratic-csi.

---

---

## CI/CD & Release

### Velero CSI Snapshots
Extend the [backup guide](backup.md) with Velero CSI snapshot integration. Any CSI driver that implements the Kubernetes `VolumeSnapshot` API can use this — for TrueNAS-backed storage, that means democratic-csi. This would allow Velero to take ZFS-native snapshots of PVs via the CSI snapshot API instead of file-system-level copies, improving backup speed and consistency for large volumes.

### Helm Chart
Helm chart for deploying the provider as a Kubernetes workload (connecting to TrueNAS remotely via WebSocket). Most homelab users run the provider directly on TrueNAS via Docker, but multi-cluster or enterprise setups may want to manage it as a K8s deployment.

---

### Automatic Autoscaling Setup
Guide and tooling for pressure-based autoscaling of TrueNAS-provisioned clusters. Uses the [Kubernetes Cluster Autoscaler](https://github.com/kubernetes/autoscaler) with a generic gRPC autoscaler that adds/removes nodes from an Omni MachineSet based on cluster pressure and resource usage. See proof-of-concept: [rothgar/omni-node-autoscaler](https://github.com/rothgar/omni-node-autoscaler). Upstream discussion: [siderolabs/omni#2647 (comment)](https://github.com/siderolabs/omni/discussions/2647#discussioncomment-16508705).

Implementation:
- Document how to deploy the Cluster Autoscaler + gRPC autoscaler alongside TrueNAS-provisioned clusters
- Provide example Omni MachineSet and autoscaler config tuned for homelab scale (conservative scale-down delays, min/max node counts)
- Test with this provider to validate the full loop: pressure → gRPC autoscaler → Omni MachineSet resize → provider provisions/deprovisions VM
- Consider shipping a Helm values template or config patch that wires up the autoscaler with sensible defaults

---

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
