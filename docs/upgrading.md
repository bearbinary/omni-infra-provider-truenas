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

No breaking changes. Adds Talos upgrade orchestration with automatic pre-upgrade snapshots and NVRAM firmware recovery. Existing VMs are unaffected.

### v0.7.0

No breaking changes. QA overhaul with 147 tests and full E2E coverage. Safe to upgrade from any prior version.

### v0.6.0

No breaking changes. Adds disk resize support and ZFS snapshot capabilities. These are new features — existing VMs are unaffected.

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
