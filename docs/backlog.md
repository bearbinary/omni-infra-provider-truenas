# Production Backlog

Tracked improvements for future releases.

## Completed

- **ISO Cleanup** — Periodic cleanup of stale ISOs (v0.4.0)
- **Orphan Cleanup** — Removes orphan VMs/zvols not tracked by Omni (v0.4.0, rewritten in v0.12.0 to use TrueNAS state queries instead of in-memory tracking)
- **Error Reporting** — User-friendly error messages for Omni UI (v0.4.0)
- **Rate Limiting** — Semaphore-based API call limiter (v0.5.0)
- **Resource Pre-checks** — Pool free space check before zvol creation (v0.5.0)
- **Disk Resize** — Online zvol grows when MachineClass disk_size increases (v0.6.0)
- **Comprehensive QA** — 147 tests: e2e, contract, chaos, stress, telemetry integration (v0.7.0)
- **NVRAM Firmware Recovery** — Auto-detect ERROR state VMs, reset NVRAM, restart (v0.8.0)
- **Host Health Monitoring** — OTEL gauges for CPU, memory, pool space/health, disks, running VMs (v0.9.0)
- **Automatic Pool Selection** — Select a healthy pool with most free space when not explicit (v0.9.0)
- **Prometheus Alerting Rules** — 7 rules: VM errors, API latency, pool space/health, provision speed (v0.9.0)
- **Grafana Dashboard** — Pre-built dashboard autoloaded with all provider metrics (v0.9.0)
- **VM Resource Monitoring** — Per-VM runtime stats via host monitor (v0.9.0)
- **Docker Image Signing + SBOM** — Cosign keyless signing + SPDX SBOM on every release (v0.9.1)
- **ZFS Encryption at Rest** — AES-256-GCM encrypted zvols with auto-unlock on reboot (v0.10.0)
- **CNI Selection & Setup Guide** — Flannel, Cilium, Calico setup docs with Talos-specific config ([docs/cni.md](cni.md)) (v0.10.0)
- **Multiple Pool Support** — Per-machine pool selection via `pool` field in MachineClass config, with docs and tests (v0.10.0)
- **CSI Storage Guide** — NFS, iSCSI, democratic-csi, and node-local storage comparison ([docs/storage.md](storage.md)) (v0.10.0)
- **Zvol Tagging** — All provider-managed zvols tagged with `org.omni:managed`, `org.omni:provider`, `org.omni:request-id` (v0.10.0)
- **Pool Validation** — Clear error messages when pool doesn't exist or a dataset path used instead of pool name (v0.11.0)
- **MAC Address Logging** — VM NIC MAC logged for DHCP reservation setup (v0.11.0)
- **Networking Guide** — Complete docs for UniFi, pfSense, OPNsense, Mikrotik, MetalLB, VIP, DHCP reservations ([docs/networking.md](networking.md)) (v0.11.0)
- **Control Plane VIP** — Documented as an Omni config patch in [networking guide](networking.md) (v0.11.0)
- **Static IP / DHCP Reservations** — Documented router-side DHCP reservation workflow for all platforms (v0.11.0)
- **Multiple NIC Support** — Additional NICs via `additional_nics` in MachineClass config (v0.11.0, VLAN attr removed in v0.12.0 — TrueNAS 25.10 rejects VM-level tagging)
- **Memory Overcommit Pre-Check** — Blocks VMs requesting >80% of host RAM (v0.12.0)
- **Machine UUID / Infra ID** — Provider-generated SMBIOS UUID v7 passed to `vm.create` for Omni correlation. Fixes ghost "Provisioned/Waiting" entries (v0.12.0)
- **TrueNAS Version Check** — Fails at startup on SCALE < 25.04 with a clear error (v0.12.0)
- **Graceful VM Shutdown** — ACPI signal with configurable timeout before force-stop (v0.12.0)
- **Advertised Subnets Config Patch** — Generates Talos config patches for multi-NIC etcd/kubelet pinning. Auto-detects primary NIC subnet when not explicitly set (v0.12.0)
- **HTTP Health Endpoint** — `/healthz` and `/readyz` on port 8081 for proper K8s probes (v0.12.0)
- **Dataset Prefix** — Custom ZFS dataset path for cluster isolation (`dataset_prefix` in MachineClass) (v0.12.0)
- **Unknown Field Warnings** — Logs warning when MachineClass config has unrecognized fields (v0.12.0)
- **`nic_attach` → `network_interface` rename** — Clearer field naming throughout (v0.12.0)
- **Per-Zvol Encryption Passphrases** — Each encrypted zvol gets a unique passphrase stored as ZFS user property, replaces global `ENCRYPTION_PASSPHRASE` env var (v0.12.0)
- **Orphan Cleanup Rewrite** — Replaced in-memory VM tracking with TrueNAS state queries (`org.omni:managed` properties). Safe across restarts, handles dataset prefixes (v0.12.0)
- **Multi-Homing Guide** — Traefik with internal + DMZ subnets, MetalLB, firewall rules ([docs/multihoming.md](multihoming.md)) (v0.12.0)
- **Additional Disk Support** — Multi-disk VMs via `additional_disks` in MachineClass config. Per-disk pool and encryption. Prerequisite for Longhorn (v0.13.0)
- **Disk Resize for Additional Disks** — Additional disks resize on re-provision when config size increases, matching root disk behavior (v0.13.0)
- **Additional Disk Integration Tests** — 8 integration tests: multi-disk create/attach, deprovision cleanup, non-existent pool, encrypted lifecycle, dataset prefix hierarchy, resize grow/no-shrink, pool space check (v0.13.0)
- **MTU / Jumbo Frames** — Optional `mtu` field on `additional_nics`, passed to TrueNAS and applied as a Talos config patch via MAC-based matching (v0.13.0)
- **Deterministic MAC Addresses** — All NICs (primary and additional) get a deterministic MAC derived from the machine request ID. DHCP reservations survive reprovision on every interface (v0.13.0, per-NIC `deterministic_mac` opt-in removed in favor of always-on)
- **Node Auto-Replace Circuit Breaker** — VMs stuck in ERROR state auto-deprovisioned after `MAX_ERROR_RECOVERIES` (default 5) consecutive failures. Counter resets on RUNNING (v0.13.0)
- **Backup Guide** — Control plane backup via Omni, workload/PVC backup via Velero ([docs/backup.md](backup.md)) (v0.13.0)
- **NFS Auto-Storage (removed in v0.13.0)** — Was added then fully removed in v0.13.0. NFS had too many issues: networking complexity, app incompatibility, no Kubernetes-native snapshots, unmaintained provisioner. Replaced by Longhorn with `storage_disk_size`
- **Per-Disk Dataset Prefix** — `additional_disks[].dataset_prefix` overrides the MachineClass-level `dataset_prefix` per disk, enabling different dataset hierarchies per pool (e.g. `etcd-data` on SSD, `bulk` on HDD) (v0.13.0)
- **Helm Chart** — `deploy/helm/omni-infra-provider-truenas/` for deploying the provider as a Kubernetes workload (remote WebSocket). Supports `existingSecret` for pre-created credentials; configurable via `values.yaml` or `--set`
- **Singleton Enforcement** — Distributed lease on `infra.ProviderStatus` annotations prevents two instances with the same `PROVIDER_ID` from racing on VM/zvol/ISO operations. The Omni SDK has no built-in leader election, so two instances would both receive every `MachineRequest` and execute provisioning steps concurrently. The lease: fail-fast on conflict with clear error (names the conflicting instance-id + heartbeat age), stale-heartbeat takeover after `PROVIDER_SINGLETON_STALE_AFTER` (default 45s), SIGTERM release for fast successor handoff, refresh-loop abandonment after `maxConsecutiveRefreshErrors` consecutive failures (cancels root ctx). Opt-out via `PROVIDER_SINGLETON_ENABLED=false`. New package `internal/singleton/` with 16 unit tests including concurrent acquire, transient-error recovery, abandonment, stale boundary, and steal detection (90.9% statement coverage). Helm chart already uses `strategy.type=Recreate` so rolling-deploy overlap is a non-issue there. See `docs/architecture.md#singleton-enforcement`, `docs/troubleshooting.md`, `docs/upgrading.md` (unreleased)
- **Longhorn as Default Storage** — `storage_disk_size` convenience field adds a data disk for Longhorn. NFS auto-storage fully removed. Storage guide rewritten with Longhorn-first decision matrix (v0.13.0)
- **Longhorn Install Script** — `scripts/install-longhorn.sh <cluster>` one-command Longhorn setup (v0.13.0)
- **Velero CSI Snapshots & DR Runbook** — Extended [backup guide](backup.md) with Velero CSI snapshot integration and 5 disaster recovery scenarios (v0.13.0)
- **Grafana Cloud Observability** — OTEL exporters support `OTEL_EXPORTER_OTLP_HEADERS` for authenticated endpoints. Pyroscope supports `PYROSCOPE_BASIC_AUTH_USER` / `PYROSCOPE_BASIC_AUTH_PASSWORD` for Grafana Cloud Profiles (unreleased)
- **Cassette Coverage Backfill** — Recorded the 6 missing integration cassettes (`TestIntegration_{Ping,PoolExists,NetworkInterfaceValid,DatasetLifecycle,ZvolLifecycle,NIC_DeterministicMAC}`) and flipped `CI_REQUIRE_CASSETTES=1` from advisory → required in both `ci.yaml` and `release.yaml`. Missing cassettes now fail CI instead of skipping silently (unreleased)
- **WebSocket Transport Concurrency Hardening** — Four coupled fixes to `internal/client` shutdown/reconnect semantics, each surfaced by the 30× CI race-stress job: (1) `Close` closes a `closeCh` signal before taking `connMu` and all reconnect sleeps go through `sleepInterruptible`, so Close no longer waits behind a full cooldown+backoff cycle (~67s worst case); (2) `connGen` generation counter dedupes reconnects — N calls failing on one dead conn no longer queue N sequential 30s cooldown sleeps under the write lock; (3) conn-drop errors are consistently retryable — the fan-out's synthetic response used to decode as a non-retryable `*APIError` while the `readerDone` wake-up path retried, a select-race coin flip; (4) reconnect sleeps also select on the caller's `ctx.Done()`, so a reconnecting caller never holds the write lock past its own deadline (unreleased)
- **Crash-Loop Alert Actually Fires** — `TrueNASWSGoroutinePanicCrashLoop` matched `process_start_time_seconds{job="truenas-provider"}`, a series that never exists in the OTLP → collector → Prometheus pipeline (double no-op: wrong metric and wrong label). Provider now exports `truenas.provider.start_time_seconds` observable gauge; alert keys on `changes(...[5m]) >= 2`. Promtool tests cover firing + single-restart must-not-fire (unreleased)
- **Race-Stress CI De-Flake** — `TestWS_ConcurrentCloseAllWaitForDrain` compared Close-return timestamps against cross-goroutine post-`Call` timestamps recorded after `wg.Done` already fired (73–115µs skew on loaded runners); now anchored on the `close(release)` happens-before chain. `TestWS_ReaderFailsAllPendingOnConnDrop` now pins transparent recovery (all in-flight calls succeed via reconnect+retry). Race-stress job green for the first time since introduction (unreleased)
- **Release Gate Hardening — Semver Sort + Tag-Matrix Match** — Two latent bugs in the release-workflow gates, both exposed the moment the first was fixed: (1) `is_newest` used `sort -rV` for semver ordering, but GNU version-sort ranks `v0.16.1-rc.5` ABOVE `v0.16.1` (extra suffix = greater), the opposite of semver. A stable cut immediately following an rc of the same version would have marked `is_newest=false` and skipped the `:preview` Docker tag update — silently keeping `:preview` on the RC. Fixed with a `sed 's/-/~/'` transform before sort (`~` sorts LOWER than end-of-string in `-V`). (2) The dry-run's tag-matrix assertion used `grep -qF` (substring), so `:0.16` falsely matched `:0.16.2-rc.1` — masking the very "MAJOR_MINOR emitted for prerelease" regression the step exists to catch. Switched every tag check to `grep -qxF` (exact whole-line match) piped via `printf '%s\n'`. Verified with dry-run on both `v0.16.2-rc.1` (prerelease) and `v0.16.2` (stable) — both green (unreleased)
- **Declarative Dev-Env via jarvy** — `jarvy.toml` at repo root declares the 15-tool dev toolchain (Go 1.26+, golangci-lint v2+, betterleaks, delve, docker, gh, git, make, kubectl, helm, k9s, stern, ripgrep, fd, jq, yq, lazydocker) mirroring CLAUDE.md § Prerequisites plus the ops CLIs release-testing.md soak windows actually use. `make dev-setup` / `dev-diff` / `dev-doctor` wrap the jarvy CLI. Jarvy MCP server registered so the assistant can drive `jarvy_wizard_plan` / `jarvy_validate_config` from a session (unreleased)
- **CI Dependency Sweep** — Rebased and squash-merged 10 stale-base dependabot PRs (actions/checkout 6.0.2→7.0.1, actions/setup-go 6.4.0→7.0.0, actions/setup-python→7.0.0, docker/setup-buildx-action→4.2.0, golangci-lint-action→9.3.0, docker/metadata-action→6.2.0, docker/login-action→4.4.0, docker/build-push-action→7.3.0, docker/setup-qemu-action→4.2.0, sigstore/cosign-installer→4.1.2). Closed 3 superseded ones (omni client, zap, opentelemetry group — already covered by main's earlier sweep). All post-merge CI runs on `main` green (unreleased)

## Upstream Issues

- **Provision errors not visible in Omni UI** — SDK clears error on every retry, users only see "Provisioning" forever. Filed: [siderolabs/omni#2629](https://github.com/siderolabs/omni/issues/2629)
- **Teardown stuck when machine never joined Omni** — SDK's `reconcileTearingDown` never calls `Deprovision` if machine state was destroyed before the check. Filed: [siderolabs/omni#2642](https://github.com/siderolabs/omni/issues/2642)
- **Provision steps not re-run on Talos upgrade** — SDK returns early for `PROVISIONED` machines, so upgrade hooks (CDROM swap) never fire. Filed: [siderolabs/omni#2646](https://github.com/siderolabs/omni/issues/2646)
- **Pressure-based autoscaling patterns** — Discussion on how infra providers should handle autoscaling. [siderolabs/omni#2647](https://github.com/siderolabs/omni/discussions/2647)

---

## Upstream SDK Workarounds

### Error Visibility Workaround (siderolabs/omni#2629)
The Omni SDK clears the error on every retry, so users only see "Provisioning" forever when something fails. Build a workaround that persists the last error so it surfaces in the Omni UI. Options: write error to a sidecar annotation or condition on MachineRequestStatus, or expose via the health endpoint.

### Talos Upgrade Step Re-Run (siderolabs/omni#2646)
The SDK does not re-run provision steps after a machine reaches `PROVISIONED` stage, so CDROM swap for Talos upgrades never fires. Build a polling loop outside the step framework that detects when the requested Talos version differs from what's installed and triggers CDROM swap independently.

### Teardown Stuck Workaround (siderolabs/omni#2642)
The SDK's `reconcileTearingDown` never calls `Deprovision` if machine state was destroyed before the check. Build a periodic reconciler that finds machines stuck in tearing-down state and forces deprovision after a configurable timeout.

---

### Automatic Autoscaling Setup (shipped experimental in v0.16)
Pressure-based autoscaling of TrueNAS-provisioned clusters via the `omni-infra-provider-truenas autoscaler` subcommand — implements the Kubernetes Cluster Autoscaler external-gRPC cloud-provider interface. Per-MachineClass opt-in via `bearbinary.com/autoscale-min` / `bearbinary.com/autoscale-max` annotations. Helm chart ships in `deploy/helm/omni-autoscaler/`, operator guide in [`docs/autoscaler.md`](autoscaler.md).

**Shipped scope (v0.16 experimental):**
- ✅ MachineSet discovery from COSI state
- ✅ TrueNAS capacity gate (pool free bytes; host-mem interface-only for now)
- ✅ Read-side gRPC handlers (`NodeGroups`, `NodeGroupTargetSize`)
- ✅ Write path via `safe.StateUpdateWithConflicts[*omni.MachineSet]` — `NodeGroupIncreaseSize` wired with Max bound re-check inside the mutator
- ✅ Helm chart + operator docs

**Remaining (post-v0.16):**
- Scale-down path — currently disabled at multiple layers; needs `MachineSetNode`/`ClusterMachine` joins for node→group mapping
- `system.mem_info` wrapper in `internal/client` so the host-memory dimension of the capacity gate can actually gate (not just interface-only)
- Scale-from-zero
- Promote from experimental once the above land and a real cluster has run it through a few pressure cycles

---

## CI/CD & Release

### Nightly Cassette Drift Detection
Connect the TrueNAS box to GitHub Actions via Tailscale or Cloudflare Tunnel so the nightly E2E workflow can re-record cassettes from live TrueNAS and detect API drift automatically.
Opens an issue when cassette responses differ from what's committed.

---

## Might Implement

Items that are possible but niche — will implement if there's demand.

### Node Rotation — etcd Health Gate Between CP Steps

The v0.17.0 `node-rotation` reconciler supports both `in-place`
(worker) and `surge` (worker + control-plane) strategies. One
follow-up remains:

- **etcd health gate for CP rotation**: between surge cycles on a CP
  set, the reconciler currently relies on `min-healthy` + the surge
  state machine's `wait-up` PROVISIONED check. A stronger gate would
  query etcd directly (member list, leader stability) before
  approving the next cycle. Not load-bearing today — surge already
  keeps the cluster above quorum by adding before removing — but a
  belt-and-suspenders check would catch transient etcd partitions
  before they compound.

### Multi-Host Provider
Support multiple TrueNAS hosts behind a single provider instance. Enables HA and load distribution. Most users have a single NAS — this targets the rare multi-host setup.

**Workaround today:** Run a separate provider instance per TrueNAS host, each registered with Omni. Omni handles scheduling across providers natively. This covers most use cases without any provider changes.

**Testing requirement:** Needs a second TrueNAS instance (physical or VM) to develop and test against.

Implementation:
- Accept multiple `TRUENAS_HOST` entries (comma-separated or config file)
- Create a client pool with health checks per host
- Simple bin-packing placement: provision VMs on the host with the most available resources (free RAM + pool space)
- Anti-affinity: use `pctx.GetMachineRequestSetID()` to spread control plane nodes across hosts (Proxmox does this with `pickNode`)
- If a host goes down, new VMs are placed on healthy hosts
- Existing VMs on a failed host are reported as unavailable to Omni
- Add OTEL metrics per host: `truenas.host.vms_running`, `truenas.host.pool_free_bytes`

### Webhook / Event Notifications
Notify external systems when provisioning completes, fails, or a VM enters ERROR state.
Currently, the only way to observe these events is via logs or Prometheus alerts. 
A webhook callback would enable automation workflows (e.g., Slack notification on provision failure, auto-trigger config management on a new node).

Implementation:
- Add `WEBHOOK_URL` env var (optional)
- POST JSON payloads on key events: `vm.provisioned`, `vm.deprovisioned`, `vm.error`, `vm.upgrade`
- Include VM name, request ID, timestamp, and event-specific data
- Fire-and-forget with short timeout — webhook failures should not block provisioning

### GPU/PCIe Passthrough
TrueNAS supports PCI device passthrough to VMs. Useful for AI/ML workloads (Ollama, vLLM), video transcoding (Plex/Jellyfin), and hardware crypto acceleration.

**Practical reality:** Most homelab servers have 1-2 GPUs. PCI passthrough is exclusive — one device, one VM. A typical setup would be a dedicated `truenas-gpu-worker` MachineClass with `replicas: 1` targeting a specific PCI slot, alongside regular non-GPU workers. The GPU node is effectively a pet disguised as cattle (same PCI slot on reprovision).

**Risk:** Users setting `pci_devices` on a MachineClass with multiple replicas when only 1 GPU exists. The provider must return a clear error ("PCI device 0000:01:00.0 is already attached to another VM") rather than a cryptic API failure.

Implementation:
- Add `pci_devices` array to `Data` struct: `[{"pci_slot": "0000:01:00.0"}]`
- Query available PCI devices via `vm.device.passthrough_device_choices`
- Attach devices using `vm.device.create` with `dtype: "PCI"` during `stepCreateVM`
- Validate device is available (not already passed through to another VM)
- Add a pre-check: verify IOMMU is enabled on the host
- Update `schema.json` with passthrough config

---

## Not Implementing

Items considered and intentionally ruled out.

### Cross-Host ZFS Replication
Replicate VM zvols to a secondary TrueNAS host using `zfs send/recv` for DR failover. **Why not:** complexity is enormous for a homelab provider — replication lag tracking, split-brain handling, zvol promotion, and VM re-registration. Users needing DR should use TrueNAS's built-in replication tasks or a dedicated backup solution. Out of scope for this provider.

### UEFI Secure Boot
Enable Secure Boot on VMs via the provider. **Why not:** Secure Boot is configured through Omni's cluster config patches and Talos machine configuration, not at the provider level. Talos handles UKI generation, key enrollment, and the signed boot chain automatically. The provider just boots VMs with standard UEFI firmware — Secure Boot is orthogonal to VM provisioning.

### VM Migration Between Pools
Live-migrate a VM's zvol between pools using `zfs send/recv`. **Why not:** TrueNAS doesn't support this as an atomic operation — it requires stop, send/recv, path update, restart. Easier to deprovision and reprovision the VM on the new pool. Omni handles this gracefully since it treats VMs as cattle.

### Network Policy Enforcement at Hypervisor Level
Use TrueNAS bridge firewall rules (nftables) to enforce network isolation between clusters. **Why not:** Kubernetes CNIs (Cilium, Calico) already enforce NetworkPolicy natively — duplicating at the hypervisor is redundant. Managing nftables on TrueNAS is fragile (rules can reset on OS updates) and operates outside the JSON-RPC API. For physical segmentation, use the Multiple NIC + VLAN Tagging feature to put clusters on separate VLANs.
