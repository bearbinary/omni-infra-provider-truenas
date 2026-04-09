# Changelog

All notable changes to this project are documented here.

## [Unreleased]

### Bug Fixes
- **Fix VM identity duplication** — VMs now get a provider-generated SMBIOS UUID passed to `vm.create`, ensuring the bhyve UUID matches what the provider reports to Omni. Previously, bhyve assigned a random UUID causing Talos to register as a separate machine, resulting in ghost "Provisioned/Waiting" entries alongside the real nodes.

### Features
- Add per-zvol auto-generated encryption passphrases — replaces global `ENCRYPTION_PASSPHRASE` env var. Each encrypted zvol gets a unique cryptographically random passphrase stored as a ZFS user property (`org.omni:passphrase`), enabling auto-unlock after TrueNAS reboots without a shared secret.
- Add `advertised_subnets` config patch support — automatically generates and applies Talos machine config patches for etcd `advertisedSubnets` and kubelet `nodeIP.validSubnets` when set in MachineClass config.
- Add HTTP health endpoint (`/healthz`, `/readyz`) for Kubernetes liveness/readiness probes — verifies actual TrueNAS connectivity instead of just process liveness. Configurable via `HEALTH_LISTEN_ADDR` (default `:8081`).
- Add VM existence health check step — replaces `removeCDROM` step with `healthCheck` that verifies VMs still exist on TrueNAS and resets state for re-provision if deleted externally.
- Add unknown field detection in MachineClass config — warns when unrecognized fields are present (typos, removed fields).
- Add `dataset_prefix` support for organizing VM storage under nested ZFS datasets.
- Add `GetDatasetUserProperty()` client method for reading ZFS user properties.

### Fixes
- Fix pool free space reporting — now queries root dataset (`pool.dataset.query`) for usable space that matches TrueNAS UI, instead of raw pool stats that ignore ZFS overhead/parity/metadata.
- Fix ZFS encryption API compatibility — use `AES-256-GCM` (uppercase) and set `inherit_encryption: false` for TrueNAS 25.04+ compatibility.
- Fix `UserProperties` format — use list-of-objects (`[{key, value}]`) instead of map for TrueNAS 25.10+ compatibility.
- Fix pool validation errors — suggest `dataset_prefix` when user passes a dataset path as pool name.
- Fix `checkExistingVM` — reset `CdromDeviceId` alongside `VmId` when VM is deleted externally.
- Keep CDROM attached after provisioning — removing it required stopping the VM, which killed Talos mid-install. CDROM is now cleaned up only on deprovision.

### Deployment
- Replace `pgrep` liveness probe with HTTP health checks in Kubernetes deployment manifest.
- Add readiness probe to Kubernetes deployment.
- Remove `ENCRYPTION_PASSPHRASE` from env config, secrets, and deployment manifests.

### Quality
- 313 tests (up from 196)
- Add protobuf compatibility test suite (`api/specs/compat_test.go`)
- Add config patch tests, unknown fields tests, VM lifecycle tests, step sequence tests, step integration tests
- Add WebSocket chaos tests (`internal/client/ws_chaos_test.go`)
- Add health endpoint tests (`internal/health/health_test.go`)
- Add E2E CI workflow (`.github/workflows/e2e.yaml`)

## [v0.12.0] — Multi-NIC, Graceful Shutdown, Observability & Hardening

### Features
- Add multiple NIC support with per-NIC VLAN tagging via `additional_nics` in MachineClass config
- Add `advertised_subnets` field for pinning etcd/kubelet to specific subnets with multi-NIC setups
- Add graceful VM shutdown on deprovision (ACPI signal with configurable timeout before force-stop)
- Add TrueNAS version check at startup — fails with clear error on versions below 25.04
- Add memory overcommit pre-check — blocks VMs requesting >80% of host RAM
- Set `MachineInfraID` and `MachineUUID` for Omni node-to-infrastructure correlation

### Observability
- Add 17 new OTEL metrics: per-step provision/deprovision durations, error categorization, ISO cache hits/misses, cleanup counters, WebSocket reconnects, rate limit queue depth, graceful shutdown outcomes
- Add OTEL log-trace correlation via otelzap bridge (trace_id/span_id in structured logs)
- Split monolithic Grafana dashboard into 4 focused dashboards (overview, provisioning, API performance, cleanup)
- Add 4 new Prometheus alerting rules (health check failures, WebSocket reconnects, forced shutdowns, orphan VMs)
- Add Loki log aggregation config to observability stack

### Security & Hardening
- Pin Docker base images to SHA256 digest to prevent supply chain tag mutation
- Switch Docker runtime from Alpine to distroless/static-debian12 (no shell, smaller attack surface)
- Inject version into Docker image via build arg (was always "dev")
- Add OCI LABEL metadata (title, vendor, source, license)
- Add `SecretString` type that redacts API keys from logs and fmt output
- Default `TRUENAS_INSECURE_SKIP_VERIFY` to `false` (was `true`)
- Add security comments to TrueNAS app template and Kubernetes secret manifest
- Replace placeholder API key in `.env.test.example` with non-secret value
- Add betterleaks secret scanning: pre-push git hook, CI job with pinned version + checksum

### Quality
- Replace `go vet + gofmt` in CI with golangci-lint v2.11.4 via official action
- Fix all golangci-lint v2 issues (errcheck, gocritic, gofmt, staticcheck, unused)
- Update `.golangci.yml` for v2 (`gofmt` moved to formatters, `gosimple` merged into `staticcheck`)
- Tune log levels (routine operations Info→Debug, NVRAM failures Warn→Error)
- Add `make scan` and `make setup-hooks` targets

### Documentation & SEO
- Add MkDocs Material docs site with GitHub Pages deployment
- Add CITATION.cff, FAQ page, FUNDING.yml
- Expand llms.txt and llms-full.txt with Q&A pairs for AI/answer engine optimization
- Add 7 GitHub topics (homelab, self-hosted, bare-metal, etc.)
- Backfill CHANGELOG.md with all releases from v0.1.0 through v0.10.0
- Restructure release workflow for immutable releases (single atomic upload, CHANGELOG.md-sourced notes)

## [v0.11.1] — Pool Validation, MAC Address Logging, Networking Guide

- Add `validatePool()` with clear errors for missing pools and dataset-path-as-pool mistakes
- Log VM NIC MAC address after creation for DHCP reservation setup
- Add comprehensive networking guide (`docs/networking.md`): bridge setup, DHCP reservations (UniFi, pfSense, OPNsense, Mikrotik), MetalLB, VIP, VLAN isolation
- Add CNI selection guide (`docs/cni.md`): Flannel, Cilium, Calico with Talos-specific setup
- Add integration test CI feasibility analysis (`docs/integration-test-ci.md`)
- Update troubleshooting guide with "stuck on Provisioning" debug steps
- 196 tests

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

[v0.12.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.12.0
[v0.11.1]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.11.1
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
