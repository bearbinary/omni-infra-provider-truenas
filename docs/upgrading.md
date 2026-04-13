# Upgrading

This guide covers upgrading the Omni TrueNAS provider between versions.

## General Upgrade Process

1. Check this page for your target version
2. Update the image tag or binary version
3. Restart the provider

For Docker/Kubernetes deployments, update the image tag:

```bash
# Docker Compose
docker compose -f deploy/docker-compose.yaml pull
docker compose -f deploy/docker-compose.yaml up -d

# Kubernetes
kubectl set image deployment/omni-infra-provider-truenas \
  provider=ghcr.io/bearbinary/omni-infra-provider-truenas:v0.7.0 \
  -n omni-infra-provider
```

For TrueNAS app deployments, update the image tag in the app configuration.

## Version Notes

### v0.13.0

**NFS auto-storage has been removed.** The `auto_storage` MachineClass field, `AUTO_STORAGE_ENABLED` and `NFS_HOST` environment variables, and all NFS provisioning code have been deleted.

**Action required:**

- **If you were using NFS auto-storage**: Migrate to Longhorn. Add `storage_disk_size` to your MachineClass to attach a data disk, then install Longhorn via Helm. See [Storage Guide](storage.md) for setup steps. Existing clusters with NFS already provisioned will continue working (the NFS provisioner and StorageClass are in-cluster resources, not managed by the provider after creation).
- **If you were not using NFS auto-storage**: No action needed.

**Behavior change: singleton enforcement is on by default.**

Starting in this release, the provider refuses to start if another process is
already running as the same `PROVIDER_ID`. It claims a lease via annotations
on the `infra.ProviderStatus` resource and heartbeats every 15s. If you start
a second copy, it will fail fast with an error naming the conflicting
instance. See [Architecture › Singleton Enforcement](architecture.md#singleton-enforcement)
and [Troubleshooting](troubleshooting.md#singleton-lease-acquire-failed-another-provider-instance-holds-the-singleton-lease).

**Action required:**

- **Kubernetes deployments**: if you are using a custom Deployment manifest
  (not the Helm chart), set `strategy.type=Recreate` — or
  `rollingUpdate: {maxSurge: 0, maxUnavailable: 1}` — so the old pod is fully
  terminated before the new one starts. The shipped Helm chart already does
  this. With the default `maxSurge=25%` rolling strategy, the new pod would
  briefly overlap with the old and crashloop on the preflight check.
- **Docker / systemd / TrueNAS app deployments**: no action needed. These run
  one instance by design.
- **Advanced sharding (rare)**: if you deliberately run multiple provider
  instances behind the same `PROVIDER_ID` (uncommon — Omni handles scheduling
  across distinct provider IDs natively), set
  `PROVIDER_SINGLETON_ENABLED=false` to bypass the check.

No MachineClass schema changes; no state migration required.

### v0.13.0

**MAC address change on first reprovision:**

The primary NIC now uses a deterministic MAC address derived from the machine request ID. This means DHCP reservations survive future reprovisions. However, existing VMs have TrueNAS-generated random MACs — the first reprovision after upgrading to v0.13.0 will change each VM's MAC address one final time. After that, the MAC is stable.

**Action required if you use DHCP reservations:**
1. Upgrade the provider
2. When a VM is reprovisioned (scale event, manual delete, pool change), note the new MAC from the provider logs: `VM NIC MAC address (deterministic) — stable across reprovision for DHCP reservations`
3. Update the DHCP reservation in your router (UniFi, pfSense, OPNsense, Mikrotik) to the new MAC
4. No further updates needed — the MAC is now permanent for that machine request ID

VMs that are not reprovisioned keep their existing MAC. The change only takes effect when a VM is created fresh.

No other breaking changes. Adds multi-disk VM support via `additional_disks` in MachineClass config. Removes ZFS snapshot/rollback code — Talos nodes are immutable, so the correct recovery for a failed VM is replacement (Omni reprovisions automatically), not zvol rollback. Adds a backup guide covering control plane backup via Omni and workload/PVC backup via Velero. See [Backup & Recovery](backup.md).

### v0.12.0

**Breaking changes:**

- **`nic_attach` renamed to `network_interface`** in MachineClass config and `additional_nics` items. Update all MachineClass configs in Omni.
- **`DEFAULT_NIC_ATTACH` renamed to `DEFAULT_NETWORK_INTERFACE`** — update your `.env`, ConfigMap, or Docker Compose.
- **`TRUENAS_INSECURE_SKIP_VERIFY` now defaults to `false`** — if your TrueNAS uses a self-signed certificate, you must explicitly set `TRUENAS_INSECURE_SKIP_VERIFY=true`. Previously this defaulted to `true`.
- **`pool`, `network_interface`, `boot_method`, `architecture` are now required** in MachineClass config. Previously they fell back to provider defaults — now the schema enforces them.

**New features:**

- `dataset_prefix` — optional ZFS dataset path under the pool for isolating clusters (e.g., `dataset_prefix: prod/k8s`)
- `advertised_subnets` — now generates Talos config patches (was previously just a warning). Required for multi-NIC clusters.
- HTTP `/healthz` and `/readyz` endpoints on port 8081 for proper Kubernetes health probes
- Unknown MachineClass field warnings — logs when config contains unrecognized fields
- Multiple NIC support with per-NIC VLAN tagging
- Graceful VM shutdown with configurable timeout (`GRACEFUL_SHUTDOWN_TIMEOUT`)
- 4 Grafana dashboards (overview, provisioning, API, cleanup) with Loki and Pyroscope integration
- Full LGTM+P observability stack (Loki, Grafana, Tempo, Prometheus, Pyroscope)

**Migration steps:**

1. Rename `nic_attach` → `network_interface` in all MachineClass configs
2. Rename `DEFAULT_NIC_ATTACH` → `DEFAULT_NETWORK_INTERFACE` in env vars
3. If using self-signed TrueNAS certs, add `TRUENAS_INSECURE_SKIP_VERIFY=true`
4. Add `pool`, `network_interface`, `boot_method`, `architecture` to any MachineClass configs that relied on provider defaults

### v0.11.0

No breaking changes. Adds pool validation, MAC address logging, networking guide. Safe to upgrade from v0.10.0.

### v0.10.0

New optional env var: `ENCRYPTION_PASSPHRASE` (required only when using `encrypted: true` in MachineClass). Existing deployments are unaffected.

### v0.9.0

No breaking changes. Adds host health monitoring, Grafana dashboard, Prometheus alerts, and automatic pool selection.

### v0.8.0

No breaking changes. Adds NVRAM firmware recovery for failed VM boots. Existing VMs are unaffected.

### v0.7.0

No breaking changes. QA overhaul with 147 tests and full E2E coverage. Safe to upgrade from any prior version.

### v0.6.0

No breaking changes. Adds disk resize support. Existing VMs are unaffected.

### v0.5.0

No breaking changes. Adds API rate limiting (`TRUENAS_MAX_CONCURRENT_CALLS`, default: 8) and resource pre-checks before provisioning. The new env var is optional and has a sensible default.

### v0.4.0

No breaking changes. Adds background cleanup for stale ISOs and orphan VMs/zvols. The cleanup runs automatically — no configuration needed.

### v0.3.0

No breaking changes. Adds WebSocket auto-reconnect and graceful shutdown. Recommended MachineClass sizes updated (control plane: 10 GiB disk, worker: 100 GiB disk).

### v0.2.0

New optional environment variables for observability:
- `OTEL_EXPORTER_OTLP_ENDPOINT` — OpenTelemetry collector endpoint
- `OTEL_EXPORTER_OTLP_INSECURE` — insecure gRPC for collector
- `OTEL_SERVICE_NAME` — override service name
- `PYROSCOPE_URL` — Pyroscope profiling endpoint

These are all optional. Existing deployments work without changes.

Also auto-removes CDROM device after Talos installs to disk — no action needed.

### v0.1.0

Initial release. No upgrade path — fresh install only.

## Rollback

If an upgrade causes issues, revert to the previous image tag:

```bash
# Docker Compose — pin to specific version in .env or compose file
image: ghcr.io/bearbinary/omni-infra-provider-truenas:v0.6.0

# Kubernetes
kubectl set image deployment/omni-infra-provider-truenas \
  provider=ghcr.io/bearbinary/omni-infra-provider-truenas:v0.6.0 \
  -n omni-infra-provider
```

The provider is stateless — rolling back the binary is sufficient. VM state lives on TrueNAS and Omni, not in the provider.

