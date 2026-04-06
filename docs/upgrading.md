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
