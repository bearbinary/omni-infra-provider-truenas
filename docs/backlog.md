# Production Backlog

Tracked improvements for future releases.

## Completed

- **ISO Cleanup** — Periodic cleanup of stale ISOs (v0.4.0)
- **Orphan Cleanup** — Removes orphan VMs/zvols not tracked by Omni (v0.4.0)
- **Error Reporting** — User-friendly error messages for Omni UI (v0.4.0)
- **Rate Limiting** — Semaphore-based API call limiter (v0.5.0)
- **Resource Pre-checks** — Pool free space check before zvol creation (v0.5.0)

## Remaining

### Disk Resize
Currently, changing `disk_size` in a MachineClass only affects new VMs. Support resizing existing zvols for running machines (ZFS supports online zvol resize).

### Multiple Pool Support
Allow different MachineClasses to use different ZFS pools (e.g., NVMe pool for workers, HDD pool for archival). Already supported via the `pool` field in MachineClass config — just needs documentation and testing.

### Docker Image Signing + SBOM
Sign container images with cosign and generate SBOM for supply chain security. Add to release pipeline.

### Prometheus Alerting Rules
Ship default alerting rules for the OTEL metrics:
- `truenas_vms_errored_total` increasing
- `truenas_api_duration_seconds` p99 > 30s
- Provider health check failing

### Backup/Snapshot Support
Leverage ZFS snapshots for VM state backup. Snapshot zvols before Talos upgrades as a rollback mechanism.
