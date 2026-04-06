# Production Backlog

Tracked improvements for future releases.

## Completed

- **ISO Cleanup** — Periodic cleanup of stale ISOs (v0.4.0)
- **Orphan Cleanup** — Removes orphan VMs/zvols not tracked by Omni (v0.4.0)
- **Error Reporting** — User-friendly error messages for Omni UI (v0.4.0)
- **Rate Limiting** — Semaphore-based API call limiter (v0.5.0)
- **Resource Pre-checks** — Pool free space check before zvol creation (v0.5.0)

---

## Storage Integration

### TrueNAS CSI Driver Auto-Configuration
Auto-configure the [democratic-csi](https://github.com/democratic-csi/democratic-csi) or TrueNAS CSI driver via Omni config patches so the Kubernetes cluster can provision PersistentVolumes backed by TrueNAS NFS/iSCSI shares — zero manual setup. This is the #1 feature for stateful workloads.

Implementation:
- Detect available NFS/iSCSI services on TrueNAS via `sharing.nfs.query` / `iscsi.target.query`
- Generate a Helm values patch or raw manifests for democratic-csi
- Inject as a cluster-level config patch in Omni during provisioning
- Configure StorageClass with TrueNAS pool, NFS share path, and iSCSI portal
- Support both NFS (simple, good for most workloads) and iSCSI (block storage, better performance)

### Dedicated Storage Dataset Per Cluster
Auto-create a ZFS dataset per cluster (e.g., `tank/k8s/<cluster-name>/`) for PersistentVolumes. Separate from VM zvols. Apply ZFS quotas based on MachineClass config. Clean up on cluster teardown.

Implementation:
- Create `<pool>/k8s-pvs/<cluster-name>` dataset during first provision for a cluster
- Set `quota` and `reservation` properties based on a new `pv_quota_gib` MachineClass field
- Configure the CSI driver to use this dataset as its parent
- Delete the dataset (with confirmation) on full cluster teardown
- Track cluster dataset in MachineSpec protobuf state

### Disk Resize
Currently, changing `disk_size` in a MachineClass only affects new VMs. Support resizing existing zvols for running machines (ZFS supports online zvol resize).

Implementation:
- Add `ResizeZvol(ctx, path, newSizeGiB)` to the TrueNAS client using `pool.dataset.update` with `volsize`
- In `stepCreateVM`, compare the existing zvol size with the requested `disk_size`
- If the requested size is larger, resize the zvol online (grow only — shrinking is destructive and should be rejected)
- Talos will detect the larger disk and expand the filesystem automatically
- Add OTEL metric: `truenas.zvol.resized_total`

### Backup/Snapshot Support
Leverage ZFS snapshots for VM state backup. Snapshot zvols before Talos upgrades as a rollback mechanism.

Implementation:
- Add `CreateSnapshot(ctx, dataset, name)` and `RollbackSnapshot(ctx, dataset, name)` to the TrueNAS client using `zfs.snapshot.create` and `zfs.snapshot.rollback`
- Add `ListSnapshots(ctx, dataset)` and `DeleteSnapshot(ctx, dataset, name)` for management
- Auto-snapshot zvols before Talos version upgrades (detect version change in `stepCreateSchematic`)
- Snapshot naming convention: `omni-pre-upgrade-<talosVersion>-<timestamp>`
- Add a configurable retention policy: keep last N snapshots per VM (default: 3)
- Expose snapshot operations via provider status for Omni UI visibility
- Add OTEL metrics: `truenas.snapshots.created_total`, `truenas.snapshots.rolled_back_total`

### VM Migration Between Pools
Move a running VM's zvol between pools (e.g., HDD → NVMe) using `zfs send/recv` without downtime. Useful when rebalancing storage or upgrading hardware.

Implementation:
- Add `MigrateDataset(ctx, source, destPool)` using `zfs.send` + `zfs.recv` pipeline
- Gracefully stop VM, migrate zvol, update VM disk path, restart
- Add a `migrate_pool` field to MachineClass or expose as an Omni operation
- Track migration state in MachineSpec to resume if interrupted

---

## Networking

### Static IP Assignment
Control plane nodes should have predictable IPs for stable API server endpoints. Add `ip_address`, `gateway`, `dns` fields to MachineClass config.

Implementation:
- Add `ip_address`, `gateway`, `dns_servers` fields to `Data` struct and `schema.json`
- Generate a Talos machine config patch with static network config when these fields are set
- Inject via `pctx.ConnectionParams` or as a meta value in the schematic
- Fall back to DHCP when fields are empty (current behavior)
- Validate IP is in the same subnet as the NIC attach target's network

### Multiple NIC Support
Attach multiple NICs to a VM — one for cluster traffic, one for storage traffic (e.g., dedicated iSCSI VLAN). Enables network segmentation.

Implementation:
- Add `additional_nics` array to `Data` struct: `[{"nic_attach": "vlan100", "type": "VIRTIO"}]`
- Call `AddNIC()` for each additional NIC during `stepCreateVM`
- Generate Talos network config patches to assign roles to each interface (e.g., eth0 = cluster, eth1 = storage)
- Update `schema.json` with the array field

### VLAN Tagging on VM NICs
Instead of requiring pre-configured bridge/VLAN interfaces on TrueNAS, allow VMs to do VLAN tagging at the VM level. Pass a trunk port and let the VM tag traffic.

Implementation:
- Add `vlan_id` field to NIC config
- Use TrueNAS `vm.device.create` with `trust_guest_rx_filters` enabled
- Generate Talos config patch with VLAN interface definition

---

## Hardware Passthrough

### GPU/PCIe Passthrough
TrueNAS supports PCI device passthrough to VMs. Critical for AI/ML workloads (Ollama, vLLM), video transcoding (Plex/Jellyfin), and hardware crypto acceleration.

Implementation:
- Add `pci_devices` array to `Data` struct: `[{"pci_slot": "0000:01:00.0"}]`
- Query available PCI devices via `vm.device.passthrough_device_choices`
- Attach devices using `vm.device.create` with `dtype: "PCI"` during `stepCreateVM`
- Validate device is available (not already passed through to another VM)
- Add a pre-check: verify IOMMU is enabled on the host
- Update `schema.json` with passthrough config

### USB Device Passthrough
Pass through USB devices (Zigbee/Z-Wave sticks, Coral USB TPU, security keys) to specific VMs.

Implementation:
- Add `usb_devices` array to `Data` struct
- Query available USB devices via `vm.device.usb_passthrough_choices`
- Attach during `stepCreateVM` with `dtype: "USB"`

---

## Multi-Node

### Multi-Host Provider
Support multiple TrueNAS hosts behind a single provider instance. Enables HA and load distribution.

Implementation:
- Accept multiple `TRUENAS_HOST` entries (comma-separated or config file)
- Create a client pool with health checks per host
- Simple bin-packing placement: provision VMs on the host with the most available resources (free RAM + pool space)
- If a host goes down, new VMs are placed on healthy hosts
- Existing VMs on a failed host are reported as unavailable to Omni
- Add OTEL metrics per host: `truenas.host.vms_running`, `truenas.host.pool_free_bytes`

### Cross-Host ZFS Replication
Replicate VM zvols to a secondary TrueNAS host using `zfs send/recv` for disaster recovery. If primary fails, provider can boot VMs from replica.

Implementation:
- Configure primary/secondary host pairs
- Periodic incremental `zfs send -i` to secondary
- On primary failure, promote secondary zvols and boot VMs there
- Track replication state and lag in provider status

---

## Lifecycle & Upgrades

### Talos Upgrade Orchestration
When Omni triggers a Talos version upgrade, the provider should orchestrate the process safely.

Implementation:
- Detect Talos version change in `stepCreateSchematic` (compare with stored `talos_version`)
- Auto-snapshot the zvol before upgrade
- Download the new ISO (new schematic with new version)
- Swap the CDROM device to point to the new ISO (or just let Omni handle the upgrade via its normal flow)
- Monitor upgrade success; rollback to snapshot on failure
- Add OTEL span: `provision.talosUpgrade`

### Automatic BIOS/UEFI Firmware Updates
When TrueNAS updates OVMF firmware, VMs may need NVRAM resets. Detect firmware changes and handle gracefully.

---

## Observability & Operations

### TrueNAS Host Health in Omni
Report TrueNAS host metrics back to Omni as provider health status. Operators see host health in the Omni UI without logging into TrueNAS.

Implementation:
- Extend health check to collect: CPU usage, RAM usage, pool free space, disk SMART status, network throughput
- Query via `system.info`, `pool.query`, `disk.query`, `reporting.get_data`
- Report as structured status on the provider's health endpoint
- Add OTEL gauges: `truenas.host.cpu_percent`, `truenas.host.memory_used_bytes`, `truenas.host.pool_free_bytes`, `truenas.host.disks_healthy`

### Automatic Pool Selection
When multiple pools exist, auto-select the best pool for new VMs instead of using a fixed default.

Implementation:
- Query all pools via `pool.query`, filter by health and free space
- Selection strategy: most free space, or fastest pool (NVMe > SSD > HDD) based on pool properties
- Override with explicit `pool` in MachineClass config
- Log which pool was selected and why

### VM Resource Monitoring
Track per-VM resource usage (CPU, memory, disk I/O) from TrueNAS and expose as OTEL metrics.

Implementation:
- Query `vm.get_instance` periodically for VM stats
- Publish as OTEL gauges with VM name/ID labels
- Enable alerting on high resource usage

---

## Security

### ZFS Encryption at Rest
Create VM zvols with ZFS native encryption for data-at-rest protection.

Implementation:
- Add `encrypted` boolean to MachineClass config
- Generate or retrieve encryption key from env var or TrueNAS key store
- Pass `encryption=on`, `encryption_options` to `pool.dataset.create`
- Handle key loading on TrueNAS reboot (zvols need unlock before VMs start)

### UEFI Secure Boot
Enable Secure Boot on VMs using Talos's signed boot chain.

Implementation:
- Set `bootloader: "UEFI_CSM"` or configure OVMF with Secure Boot firmware
- Talos provides signed EFI binaries — verify chain works with TrueNAS OVMF version
- Add `secure_boot` boolean to MachineClass config

### Network Policy Enforcement
Leverage TrueNAS bridge firewall rules to enforce network isolation between clusters.

Implementation:
- Create per-cluster bridges or use nftables rules on the TrueNAS host
- Prevent cross-cluster VM traffic at the hypervisor level
- Managed via provider config, not Kubernetes NetworkPolicy

---

## Developer Experience

### Multiple Pool Support
Already supported via the `pool` field in MachineClass config — just needs documentation and testing with multiple pools.

---

## CI/CD & Release

### Docker Image Signing + SBOM
Sign container images with cosign and generate SBOM for supply chain security. Add to release pipeline.

### Prometheus Alerting Rules
Ship default alerting rules for the OTEL metrics:
- `truenas_vms_errored_total` increasing
- `truenas_api_duration_seconds` p99 > 30s
- Provider health check failing

### Grafana Dashboard Template
Ship a pre-built Grafana dashboard JSON for the provider's OTEL metrics. Import into Grafana for instant visibility.

### Integration Test CI
Run integration tests against a real TrueNAS instance in CI (GitHub Actions self-hosted runner or cloud instance).
