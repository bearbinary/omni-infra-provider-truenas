# Changelog

All notable changes to this project are documented here.

## [v0.7.0] — QA Overhaul

- Comprehensive QA overhaul with 147 tests and full E2E coverage

## [v0.6.0] — Disk Resize & Snapshots

- Add disk resize support
- Add ZFS snapshot creation and snapshot retention policies
- Add tests for extension merge (defaults only, custom additions, duplicates)

## [v0.5.0] — Rate Limiting & Pre-checks

- Add API rate limiting to prevent TrueNAS overload
- Add resource pre-checks before provisioning (pool space, NIC validity)
- Full test coverage for rate limiter and pre-check logic

## [v0.4.0] — Cleanup & Reliability

- Background cleanup for stale ISOs and orphan VMs/zvols
- Wire cleanup loop into main with active resource tracking
- E2E integration tests for all cleanup features
- Reduce cognitive complexity in telemetry and provisioner
- Add tests for VM helpers, telemetry init, and mock client

## [v0.3.0] — WebSocket Reconnect & Graceful Shutdown

- WebSocket auto-reconnect on connection loss
- Graceful shutdown on SIGTERM/SIGINT
- Updated recommended MachineClass sizes (10 GiB control plane, 100 GiB worker)

## [v0.2.0] — Observability & Polish

- OpenTelemetry tracing and metrics with OTLP export
- Pyroscope continuous profiling support
- Auto-remove CDROM after Talos installs to disk
- Recommended MachineClass documentation
- Code quality improvements (complexity reduction, constants extraction)

## [v0.1.0] — Initial Release

- TrueNAS SCALE JSON-RPC 2.0 client with Unix socket and WebSocket transports
- 3-step provision flow: schematic generation, ISO upload, VM creation
- Deprovision with full cleanup (stop VM, delete VM, delete zvol)
- MachineClass config with per-class overrides (pool, NIC, boot method, arch)
- Default Talos extensions (qemu-guest-agent, nfs-utils, util-linux-tools)
- TrueNAS app packaging with custom questions.yaml
- CI/CD pipeline with GitHub Actions (test, lint, multi-arch Docker build, GitHub Release)
- Kubernetes and Docker Compose deployment manifests

[v0.7.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.7.0
[v0.6.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.6.0
[v0.5.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.5.0
[v0.4.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.4.0
[v0.3.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.3.0
[v0.2.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.2.0
[v0.1.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.1.0
