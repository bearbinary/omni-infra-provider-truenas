# Production Backlog

Tracked improvements for future releases. Items are roughly prioritized.

## High Priority

### Error Reporting
Provision failures show raw Go error strings in Omni UI. Map common TrueNAS errors to user-friendly messages on MachineRequestStatus:
- "Pool 'tank' is full" instead of `pool.dataset.create failed: [ENOSPC]...`
- "NIC attach 'br999' not found" instead of raw EINVAL
- "TrueNAS unreachable" instead of WebSocket connection errors

## Medium Priority

### Rate Limiting
Large scale-ups (e.g., 20 VMs) fire concurrent API calls that can overwhelm TrueNAS. Add a semaphore or token bucket to limit concurrent TrueNAS API calls (separate from Omni's `CONCURRENCY` setting which controls parallel reconciliations).

### Resource Pre-checks
Before creating a zvol, check if the pool has enough free space. Before creating a VM, check available host memory. Fail fast with a clear error instead of letting TrueNAS return cryptic errors mid-provision.

### Disk Resize
Currently, changing `disk_size` in a MachineClass only affects new VMs. Support resizing existing zvols for running machines (ZFS supports online zvol resize).

### Multiple Pool Support
Allow different MachineClasses to use different ZFS pools (e.g., NVMe pool for workers, HDD pool for archival). Already supported via the `pool` field in MachineClass config — just needs documentation and testing.

## Low Priority

### Docker Image Signing + SBOM
Sign container images with cosign and generate SBOM for supply chain security. Add to release pipeline.

### Prometheus Alerting Rules
Ship default alerting rules for the OTEL metrics:
- `truenas_vms_errored_total` increasing
- `truenas_api_duration_seconds` p99 > 30s
- Provider health check failing

### Backup/Snapshot Support
Leverage ZFS snapshots for VM state backup. Snapshot zvols before Talos upgrades as a rollback mechanism.
