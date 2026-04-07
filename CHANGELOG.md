# Changelog

All notable changes to this project are documented here.

## [v0.10.0] — ZFS Encryption, Zvol Tagging & Supply Chain Hardening

- Add ZFS native AES-256-GCM encryption at rest for VM disks (`encrypted: true` in MachineClass)
- Add automatic unlock of encrypted zvols on provider restart
- Tag all provider-managed zvols with ZFS user properties (`org.omni:managed`, `org.omni:provider`, `org.omni:request-id`)
- Release pipeline now triggers only on manual tag push
- SBOM cryptographically attested to Docker image digest
- Release binaries signed with cosign (`.sig` + `.cert`)
- SLSA provenance in Docker images
- 191 tests

## [v0.9.4] — Supply Chain Signing Fix

- Fix release pipeline to include SBOM attestation, binary signing, and SLSA provenance in a single workflow run

## [v0.9.3] — Supply Chain Hardening

- Attest SBOM to Docker image digest via `cosign attest`
- Sign all release binaries with cosign (`.sig` + `.cert` per binary)
- Add SLSA provenance metadata to Docker images via buildx

## [v0.9.2] — Docker Tag Fix

- Add `v`-prefixed Docker image tags alongside bare version tags (`v0.9.2` and `0.9.2`)

## [v0.9.1] — Container Image Signing & SBOM

- Sign all Docker images with cosign via Sigstore keyless signing (GitHub OIDC)
- Generate SPDX SBOM for every release, attached as release asset

## [v0.9.0] — Observability & Operations

- Add host health monitoring: CPU cores, memory, pool free/used space, pool health, disk count, running VMs (OTEL gauges every 30s)
- Add automatic pool selection — picks the healthy pool with the most free space when MachineClass doesn't specify one
- Add 7 Prometheus alerting rules (VM errors, API latency, pool space, pool health, no VMs, ISO slow, provision slow)
- Add 12-panel Grafana dashboard with auto-provisioning
- 179 tests (up from 147)

## [v0.8.0] — Talos Upgrade Orchestration & Documentation

- Add Talos upgrade orchestration and NVRAM recovery
- Add beginner getting-started tutorial (NAS to running cluster, no prior experience)
- Add upgrade guide, CNI selection guide, storage guide, networking guide
- Add comprehensive documentation, AI discoverability files (llms.txt, AGENT.md), and community health files

## [v0.7.0] — Production-Grade Test Suite

- Comprehensive QA overhaul with 147 tests and full E2E coverage
- Full provision/deprovision E2E against real TrueNAS hardware
- WebSocket auto-reconnect verified against real connection
- ZFS snapshot rollback verified end-to-end
- 8 TrueNAS API contract tests
- Chaos, failure injection, and load/stress tests
- Fix: `zfs.snapshot.rollback` required positional params, not a dict
- Fix: snapshot `Name` field contains full path, not just snap name
- Fix: `filesystem.stat` returns `realpath` not `name`

## [v0.6.0] — Disk Resize & Snapshots

- Add disk resize support
- Add ZFS snapshot creation and snapshot retention policies
- Add tests for extension merge (defaults only, custom additions, duplicates)

## [v0.5.0] — Rate Limiting & Pre-checks

- Add API rate limiting to prevent TrueNAS overload (default: 8 concurrent calls, configurable via `TRUENAS_MAX_CONCURRENT_CALLS`)
- Add resource pre-checks before provisioning (pool space validation)
- Add `SystemMemoryAvailable()` for future host memory checks
- 72 tests (up from 63)

## [v0.4.0] — Cleanup & Reliability

- Add background cleanup for stale ISOs and orphan VMs/zvols
- Add human-readable error mapping for TrueNAS API errors in Omni UI
- Wire cleanup loop into main with active resource tracking
- Add exported `MockClient` for cross-package testing
- 63 tests (up from 36)

## [v0.3.0] — WebSocket Reconnect & Graceful Shutdown

- Add WebSocket auto-reconnect on connection loss (exponential backoff, max 30s, 3 attempts)
- Add graceful shutdown on SIGTERM/SIGINT (10s drain timeout for in-flight API calls)
- Reduce cognitive complexity across main.go, ws.go, steps.go, deprovision.go
- Extract JSON-RPC method string literals into constants
- Add `Data.ApplyDefaults()` to centralize default value logic
- Update recommended MachineClass sizes (10 GiB control plane, 100 GiB worker)

## [v0.2.0] — Observability & Auto CDROM Removal

- Add OpenTelemetry tracing for every provision step and TrueNAS API call
- Add OpenTelemetry metrics (`truenas.vms.provisioned`, `truenas.provision.duration`, etc.)
- Add Pyroscope continuous profiling (CPU, memory, goroutine flame graphs)
- Add local dev observability stack (Grafana, Tempo, Prometheus, Pyroscope, OTEL Collector)
- Automatically detach ISO CDROM after Talos installs to disk (eliminates 7s GRUB delay)
- Add default storage extensions (`nfs-utils`, `util-linux-tools`) alongside `qemu-guest-agent`

## [v0.1.0] — Initial Release

- TrueNAS SCALE JSON-RPC 2.0 client with Unix socket and WebSocket transports
- 3-step provision flow: schematic generation, ISO upload, VM creation
- Deprovision with full cleanup (stop VM, delete VM, delete zvol)
- MachineClass config with per-class overrides (pool, NIC, boot method, arch)
- Default Talos extensions (qemu-guest-agent, nfs-utils, util-linux-tools)
- TrueNAS app packaging with custom questions.yaml
- CI/CD pipeline with GitHub Actions (test, lint, multi-arch Docker build, GitHub Release)
- Kubernetes and Docker Compose deployment manifests
- HOST-PASSTHROUGH CPU mode for full host CPU features
- ISO caching with SHA-256 deduplication
- 36 unit tests + 10 integration tests

[v0.10.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.10.0
[v0.9.4]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.9.4
[v0.9.3]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.9.3
[v0.9.2]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.9.2
[v0.9.1]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.9.1
[v0.9.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.9.0
[v0.8.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.8.0
[v0.7.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.7.0
[v0.6.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.6.0
[v0.5.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.5.0
[v0.4.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.4.0
[v0.3.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.3.0
[v0.2.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.2.0
[v0.1.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.1.0
