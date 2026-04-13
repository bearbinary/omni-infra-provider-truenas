# Changelog

All notable changes to this project are documented here.

## [v0.13.1] — Grafana Cloud Observability

### Features
- **Grafana Cloud observability support** — OTEL exporters now accept `OTEL_EXPORTER_OTLP_HEADERS` for authenticated endpoints (e.g., Grafana Cloud OTLP gateway). Pyroscope client supports `PYROSCOPE_BASIC_AUTH_USER` and `PYROSCOPE_BASIC_AUTH_PASSWORD` for Grafana Cloud Profiles. Both local dev stacks and Grafana Cloud work with the same provider binary — just different env vars.

### Housekeeping
- Reserve removed proto field `nfs_dataset_path` (field 10) to prevent accidental reuse
- Remove stale `configureStorage` and NFS panels from Grafana provisioning dashboard

## [v0.13.0] — Multi-Disk VMs, Singleton Lease, Deterministic MACs, Circuit Breaker & Storage

### Breaking / Behavior Changes
- **Longhorn is now the only supported storage path** — NFS auto-storage has been fully removed (see Removed section below). Add a dedicated data disk via `storage_disk_size` in your MachineClass, then install Longhorn via Helm. See [`docs/storage.md`](docs/storage.md) for setup steps.
- **Deterministic MAC addresses are now always on for additional NICs** — the per-NIC `deterministic_mac` opt-in field on `additional_nics` has been removed. All NICs (primary and additional) now unconditionally receive a stable MAC derived from the machine request ID so DHCP reservations survive reprovisioning on every interface, not just the primary. Existing `MachineClass` configs with `deterministic_mac: true` still work (the field is silently ignored via unknown-field warning); configs with `deterministic_mac: false` will start getting deterministic MACs on next reprovision.

### Bug Fixes
- **Drop `mtu` from NIC device create** — TrueNAS 25.10 rejects `mtu` on `vm.device.create` with `[EINVAL] vm_device_create.attributes.NIC.mtu: Extra inputs are not permitted`, which blocked provisioning of any additional NIC whose MachineClass set an `mtu` value (typical for jumbo-frame storage networks). `NICConfig.MTU` is now ignored on the hypervisor call — MTU is still applied inside the guest via the existing MAC-matched Talos config patch (`buildMTUPatch`), which is the correct layer for it. Same shape as the v0.12.0 `vlan` attribute removal.

### Features
- **Singleton enforcement via distributed lease** — The provider now claims an exclusive lease on startup via annotations on the `infra.ProviderStatus` resource, preventing two processes with the same `PROVIDER_ID` from racing on VM creation, zvol creation, and ISO upload. The Omni SDK has no built-in leader election, so two instances with the same ID would both receive every `MachineRequest` and execute provisioning steps concurrently against TrueNAS — typically resulting in duplicate VM names, failed zvol creates, and half-provisioned machines. The lease fails fast when a fresh heartbeat is observed from another instance (surfacing duplicate-provider misconfigurations loudly) and takes over automatically when the prior holder is ungracefully killed and its heartbeat goes stale (default: 45s). Opt-out via `PROVIDER_SINGLETON_ENABLED=false` for debugging or advanced sharding. Tunable via `PROVIDER_SINGLETON_REFRESH_INTERVAL` (default 15s) and `PROVIDER_SINGLETON_STALE_AFTER` (default 45s). See `docs/architecture.md#singleton-enforcement` and `docs/troubleshooting.md` for operational details. Kubernetes rolling deploys should use `strategy.type=Recreate` or `maxSurge=0` to avoid overlap windows.
- **Additional disk support (multi-disk VMs)** — Attach extra data disks beyond the root disk via `additional_disks` in MachineClass config. Each disk can target a different ZFS pool and independently toggle encryption. Enables dedicated etcd disks on fast SSD pools, bulk data disks on HDD pools, and is a prerequisite for node-local distributed storage (Longhorn). Max 16 additional disks per VM. Paths tracked in protobuf state for automatic cleanup on deprovision.
- **Additional disk resize** — Additional disks grow automatically when the `size` in `additional_disks` config increases, matching the root disk resize behavior. Shrinking is prevented (ZFS limitation).
- **`storage_disk_size` convenience field** — New MachineClass schema field that adds a dedicated data disk for persistent storage (Longhorn). Setting `storage_disk_size: 100` is equivalent to `additional_disks: [{size: 100}]` but simpler in the Omni UI.
- **MTU / jumbo frames for additional NICs** — Optional `mtu` field on `additional_nics` items. Applied as a Talos machine config patch using MAC-based interface matching. Set to 9000 for jumbo frames on storage networks.
- **Deterministic MAC addresses** — All NICs (primary and additional) get a stable MAC derived from the machine request ID, so DHCP reservations survive reprovision. Collision detection queries the same network segment before attaching.
- **Node auto-replace circuit breaker** — VMs stuck in ERROR state are automatically deprovisioned after exceeding `MAX_ERROR_RECOVERIES` (default: 5) consecutive failed recoveries. Omni's reconciliation loop then provisions a fresh replacement. Configurable via env var; set to `-1` to disable.
- **Longhorn install script** — `scripts/install-longhorn.sh <cluster>` one-command Longhorn setup: applies Talos config patch via omnictl, Helm installs Longhorn, sets default StorageClass, verifies with test PVC. Idempotent.

### Observability
- Add `truenas.vms.auto_replaced` metric — counts VMs deprovisioned by the circuit breaker
- Add ”VMs Auto-Replaced” stat panel to provisioning Grafana dashboard
- Add `TrueNASVMAutoReplaced` Prometheus alert rule — fires when circuit breaker triggers, severity: warning

### Removed
- **Remove NFS auto-storage** — The `configureStorage` provision step, `auto_storage` MachineClass field, `AUTO_STORAGE_ENABLED` / `NFS_HOST` env vars, NFS client methods (`CreateNFSShare`, `GetNFSShareByPath`, `DeleteNFSShare`, `EnsureNFSService`, `SetDatasetPermissions`), NFS config patch builder, and all related tests have been fully removed. NFS had too many issues in Kubernetes: networking complexity (port 2049 reachability, firewall rules), broad application incompatibility (PostgreSQL, Redis, Elasticsearch, and any WAL/Raft-based system corrupt data on NFS), no support for Kubernetes-native VolumeSnapshots, and the underlying provisioner (nfs-subdir-external-provisioner) has been unmaintained since 2022. Use Longhorn with `storage_disk_size` instead — it's self-contained, supports snapshots, and works in any network topology.
- **Remove ZFS snapshot/rollback code** — Talos nodes are immutable; the correct recovery path is to replace a failed VM (Omni reprovisions automatically), not to roll back a zvol. Removed: `CreateSnapshot`, `ListSnapshots`, `DeleteSnapshot`, `RollbackSnapshot` client methods, `snapshotBeforeUpgrade` and `enforceSnapshotRetention` provisioner logic, `last_upgrade_snapshot` protobuf field, snapshot telemetry counters, and all related tests. The `Snapshot` type and pre-upgrade snapshot workflow introduced in v0.6.0–v0.8.0 are fully removed.

### Documentation
- Rewrite storage guide (`docs/storage.md`) — Longhorn as recommended default, NFS removed as provider-managed option, democratic-csi as advanced alternative
- Add Velero CSI snapshot integration to backup guide (`docs/backup.md`) — VolumeSnapshotClass setup for Longhorn and democratic-csi, CSI Snapshot Data Movement for off-site S3
- Add disaster recovery runbook to backup guide — 5 scenarios with step-by-step procedures and recovery time table
- Add backup & disaster recovery guide (`docs/backup.md`) — control plane backup via Omni, workload/PVC backup via Velero to remote S3
- Add jumbo frames / MTU guide to networking docs (`docs/networking.md`)
- Remove snapshot rollback documentation from upgrading guide

## [v0.12.0] — VM Identity Fix, Per-Zvol Encryption, Health Endpoint & Hardening

### Bug Fixes
- **Fix VM identity duplication** — VMs now get a provider-generated SMBIOS UUID passed to `vm.create`, ensuring the bhyve UUID matches what the provider reports to Omni. Previously, bhyve assigned a random UUID causing Talos to register as a separate machine, resulting in ghost "Provisioned/Waiting" entries alongside the real nodes.
- Fix pool free space reporting — now queries root dataset (`pool.dataset.query`) for usable space that matches TrueNAS UI, instead of raw pool stats that ignore ZFS overhead/parity/metadata.
- Fix ZFS encryption API compatibility — use `AES-256-GCM` (uppercase) and set `inherit_encryption: false` for TrueNAS 25.04+ compatibility.
- Fix `UserProperties` format — use list-of-objects (`[{key, value}]`) instead of map for TrueNAS 25.10+ compatibility.
- Fix pool validation errors — suggest `dataset_prefix` when user passes a dataset path as pool name.
- Fix `checkExistingVM` — reset `CdromDeviceId` alongside `VmId` when VM is deleted externally.
- Keep CDROM attached after provisioning — removing it required stopping the VM, which killed Talos mid-install. CDROM is now cleaned up only on deprovision.
- Remove `vlan` attribute from NIC device creation — TrueNAS 25.10 rejects VM-level VLAN tagging via `vm.device.create`. VLAN tagging is handled at the host level by attaching to VLAN interfaces (e.g., `vlan666`)
- Switch UUID generation from hand-rolled v4 to `google/uuid` v7
- **Fix orphan cleanup deleting all VMs after provider restart** — replaced in-memory VM tracking (lost on restart) with TrueNAS state queries. Orphan VMs are now detected by checking if their backing zvol (tagged with `org.omni:managed`) still exists. Orphan zvols are detected by checking if their corresponding VM still exists. No in-memory state needed — safe across restarts

### Features
- Add multiple NIC support via `additional_nics` in MachineClass config
- Add `advertised_subnets` config patch support — automatically generates and applies Talos machine config patches for etcd `advertisedSubnets` and kubelet `nodeIP.validSubnets` when set in MachineClass config
- Auto-detect primary NIC subnet when `advertised_subnets` is not set but additional NICs are configured — queries TrueNAS `interface.query` for the primary NIC's IPv4 CIDR and applies the config patch automatically
- Add per-zvol auto-generated encryption passphrases — replaces global `ENCRYPTION_PASSPHRASE` env var. Each encrypted zvol gets a unique cryptographically random passphrase stored as a ZFS user property (`org.omni:passphrase`), enabling auto-unlock after TrueNAS reboots without a shared secret.
- Add graceful VM shutdown on deprovision (ACPI signal with configurable timeout before force-stop)
- Add HTTP health endpoint (`/healthz`, `/readyz`) for Kubernetes liveness/readiness probes — verifies actual TrueNAS connectivity instead of just process liveness. Configurable via `HEALTH_LISTEN_ADDR` (default `:8081`)
- Add VM existence health check step — replaces `removeCDROM` step with `healthCheck` that verifies VMs still exist on TrueNAS and resets state for re-provision if deleted externally
- Add TrueNAS version check at startup — fails with clear error on versions below 25.04
- Add memory overcommit pre-check — blocks VMs requesting >80% of host RAM
- Add unknown field detection in MachineClass config — warns when unrecognized fields are present (typos, removed fields)
- Add `dataset_prefix` support for organizing VM storage under nested ZFS datasets
- Add `GetDatasetUserProperty()` client method for reading ZFS user properties
- Add CDROM swap logic for Talos version upgrades — **note: currently non-functional** because the Omni SDK does not re-run provision steps after a machine reaches `PROVISIONED` stage ([siderolabs/omni#2646](https://github.com/siderolabs/omni/issues/2646))

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
- Add betterleaks secret scanning: pre-push hook, CI job with pinned version + checksum

### Deployment
- Replace `pgrep` liveness probe with HTTP health checks in Kubernetes deployment manifest
- Add readiness probe to Kubernetes deployment
- Remove `ENCRYPTION_PASSPHRASE` from env config, secrets, and deployment manifests

### Quality
- 314 tests (up from 196)
- Replace `go vet + gofmt` in CI with golangci-lint v2.11.4 via official action
- Fix all golangci-lint v2 issues (errcheck, gocritic, gofmt, staticcheck, unused)
- Update `.golangci.yml` for v2 (`gofmt` moved to formatters, `gosimple` merged into `staticcheck`)
- Add protobuf compatibility test suite (`api/specs/compat_test.go`)
- Add config patch tests, unknown fields tests, VM lifecycle tests, step sequence tests, step integration tests
- Add WebSocket chaos tests (`internal/client/ws_chaos_test.go`)
- Add health endpoint tests (`internal/health/health_test.go`)
- Add E2E CI workflow (`.github/workflows/e2e.yaml`)
- Add UUID integration test verifying TrueNAS accepts and persists the `uuid` field on `vm.create`
- Add 27 cleanup tests including integration test with mixed active/orphan/non-omni resources and crash recovery scenarios
- Tune log levels (routine operations Info→Debug, NVRAM failures Warn→Error)
- Add `make scan` and `make setup-hooks` targets

### Upstream Discussions
- Opened discussion on pressure-based autoscaling patterns with infrastructure providers ([siderolabs/omni#2647](https://github.com/siderolabs/omni/discussions/2647))

### Documentation & SEO
- Add multi-homing guide (`docs/multihoming.md`): Traefik with internal + DMZ subnets, MetalLB DMZ pool, firewall rules, DHCP reservations, storage network variation
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
- 8 TrueNAS API contract tests
- Chaos, failure injection, and load/stress tests
- Fix: `filesystem.stat` returns `realpath` not `name`

## [v0.6.0] — Disk Resize

- Add disk resize support
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

[v0.13.1]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.13.1
[v0.13.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.13.0
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
