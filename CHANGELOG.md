# Changelog

All notable changes to this project are documented here.

## [Unreleased]

## [v0.15.1] — Post-release stuck-teardown fixes from Grafana audit + CI protoc pin

### Fixes
- **`recordProvisionError` no longer treats the SDK's `RequeueError` as a failure** — v0.15.0 changed `recordProvisionError` to log every provision step error at Error level and bump `truenas_provision_errors_total`. That applied to `*controller.RequeueError` too, whose `Error()` string is just `"requeue in <duration>"` — a benign retry signal, not a failure. Live evidence from bearbinary.grafana.net after the v0.15.0 rollout showed 9 MachineRequests each producing a storm of Error-level "provision error" log lines with `error_category="unknown"` that were actually normal step waits, drowning out real failures and polluting the errors counter. Fixed in `internal/provisioner/steps.go`: if the error is a `RequeueError`, unwrap via `.Err()`; log + count only when the inner error is non-nil, otherwise return silently. `TestRecordProvisionError_RequeueUnwrap` pins the three cases (pure requeue, requeue wrapping a real error, non-requeue pass-through).
- **`client.IsNotFound` now recognises TrueNAS's `MatchNotFound()` response** — `vm.query`, `pool.dataset.query`, `disk.query` and the other `query` methods return `{code: 22, message: "MatchNotFound()"}` (NOT code 2 / ENOENT) when called with `{"get": true}` and the filter matches zero rows. `IsNotFound` only matched code 2, so the v0.15.0 ownership check in `cleanupVM` propagated `failed to read VM N for ownership check: MatchNotFound()` on every Deprovision call for a VM already deleted externally — the SDK then requeued the teardown forever, holding the finalizer and leaving Machines stuck in `tearing down`. Production impact observed post-v0.15.0 rollout: 7 machines from the talos-preview teardown cycle wedged with destroy-never-completed because their VM IDs had been removed on TrueNAS out-of-band. Fixed in `internal/client/truenas.go`: `IsNotFound` now also accepts code 22 when the message contains `MatchNotFound`. Keeps a genuine EINVAL (code 22 with any other message) as a real error. `TestIsNotFound` extended with both cases.
- **`DeleteVM` passes `{force: true, force_after_timeout: true}`** — `vm.delete` with no options internally stops the VM first and refuses with `EFAULT VM state is currently not 'RUNNING / SUSPENDED'` if the VM is in a transitional state (STOPPING, LOCKED, STARTING, …). That was the exact path orphan cleanup ran into during the stuck-teardown aftermath, producing `failed to delete orphan VM (id=638): truenas api error (code 14)` and making orphan cleanup pointless for the VMs it was most needed for. Forcing the delete skips the precondition, which is the correct behavior for a provider that owns the VMs and is tearing them down.

### CI
- **Correct the pinned SHA256 for `protoc-27.1-linux-x86_64.zip` in `.github/workflows/ci.yaml`** — v0.15.0 shipped with `6125d83c…`, which doesn't match the artifact published on the `v27.1` release (the correct SHA is `8970e3d8…`). Every post-v0.15.0 CI `make generate` job failed at the `sha256sum -c -` gate before it ever invoked protoc. Verified by downloading the actual artifact and confirming it's a real 9.4 MB Linux x86_64 `bin/protoc` + the standard `include/` tree.

## [v0.15.0] — Security hardening pass + observability corrections (validation, transport, secrets, ownership, extensions, TOFU ISO, fencing, WS mutex split, CI SHA-pinning, histogram buckets, singleton malformed-200 workaround)

### Observability
- **Histogram buckets now match the recorded unit** — All six `Float64Histogram` instruments in `internal/telemetry/metrics.go` (`truenas.api.duration`, `truenas.provision.duration`, `truenas.deprovision.duration`, `truenas.iso.download.duration`, `truenas.provision.step.duration`, `truenas.deprovision.step.duration`) now pass `metric.WithExplicitBucketBoundaries(...)` explicitly. Previously the OTel SDK defaults (`[0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000]`) were treated as milliseconds against a metric unit of seconds, pushing every call <5s into the first populated bucket and making `histogram_quantile()` return the bucket midpoint (~2.5s p50, ~4.95s p99) regardless of real latency. Real average for `pool.query` is ~19ms; dashboards were reading ~250× too high. New boundaries: API `[1ms…30s]`, provision `[5s…1h]`, deprovision `[1s…10m]`, ISO download `[1s…15m]`, step `[100ms…5m]`, deprovision step `[100ms…2m]`.
- **Provision errors are now logged at Error level with their category** — `recordProvisionError` in `internal/provisioner/steps.go` previously only incremented the `truenas_provision_errors_total` counter and attached the error to the active span. The counter was observable but the error text was not — leaving operators with a number but no way to find the root cause in Loki. Function now also emits `logger.Error("provision error", zap.String("error_category", …), zap.Error(err))`. Signature gained a `*zap.Logger` parameter; all four call sites (createSchematic, uploadISO, createVM, healthCheck) updated.

### Resilience
- **Singleton lease release tolerates upstream siderolabs/omni#2642** — `Lease.Release` previously surfaced the Omni gRPC-gateway's `"unexpected HTTP status code received from server: 200 (OK); malformed header: missing HTTP content-type"` response as a Warn, leaving operators to believe the heartbeat/instance-id annotations were stuck on the resource and that the successor would have to wait `staleAfter` to take over. The response body actually writes successfully on the server — the gRPC client rejects an otherwise-valid 200 because the gateway omitted `Content-Type`. `isMalformed200` in `internal/singleton/singleton.go` detects the specific signature (both `200` and the malformed-header substring) and the Release path now logs Info and returns. Narrow predicate: a 502 with the same malformed-header marker is still treated as a real failure. `TestIsMalformed200` pins six cases including wrapped errors and the non-200 negative.

### Breaking
- **VM names now embed provider ID** — `omni_<requestID>` → `omni_<providerID>_<requestID>`. Prevents two providers sharing a TrueNAS host from racing on VM names. `BuildVMName` collapses any run of underscores produced by sanitization (unicode, punctuation) across both segments and trims trailing underscores, so the name is deterministic regardless of provider-id punctuation — a pre-release QA defect that produced `omni__req` / `omni___req` for empty or pure-punctuation provider IDs was fixed in this same version. See [`docs/upgrading.md#upgrading-to-v015`](docs/upgrading.md#upgrading-to-v015) for the migration path — existing v0.14 VMs will not be adopted; drain before upgrade or accept a cluster recreate.
- **`PROVIDER_ID` required for non-localhost `OMNI_ENDPOINT`** — fail-fast on startup otherwise. Prevents multi-tenant lease collision on the default `"truenas"` ID. `isLocalOmniEndpoint` uses boundary-aware prefix matching (next char must be `:`, `/`, `?`, `#`, end-of-string, or digit after `127.`) so a deceptive `https://localhost-attacker.example` endpoint cannot slip past the guard and suppress the PROVIDER_ID requirement — a pre-release QA defect fixed in this same version.

### Security — Critical / High
- **Deprovision ownership check** (Critical) — VM description embeds request ID; `cleanupVM` refuses VMs whose description doesn't carry the `Managed by Omni infra provider` marker. `cleanupZvol` verifies `org.omni:managed=true` and `org.omni:request-id` match before deletion. `handleExistingVM` refuses adoption of non-Omni VMs. Prevents accidental deletion after name collision or state corruption.
- **Talos extension allowlist** (High) — `extensions:` entries must appear on the built-in vetted list in [`internal/provisioner/extensions.go`](internal/provisioner/extensions.go) or be explicitly opted-in with `ALLOW_UNSIGNED_EXTENSIONS=true`. Structural checks (`..`, whitespace, empty string) always apply. Stops a semi-compromised MachineClass author from running arbitrary kernel modules inside Talos.
- **ISO TOFU supply-chain hash pinning** (High) — SHA-256 of every downloaded Talos ISO is recorded as a ZFS user property on the cache dataset. Subsequent downloads compare; mismatch marks the stored hash `POISONED-` and fails the provision. Protects against factory.talos.dev swap / MITM scenarios.
- **SecretString passphrase redaction in recorder** (High) — Cassettes written by `RecordingTransport` now scrub `passphrase`, `password`, `api_key`, `apikey`, `token`, `secret` anywhere a JSON field name *contains* any of those substrings. The substring form is load-bearing: a first cut used exact-match and missed the provider's own `org.omni:passphrase` property when it echoed back in a `pool.dataset.query` response — the pre-release QA pass caught it and the fix shipped in the same version. Methods whose first positional param IS the secret (`auth.login_with_api_key`, etc.) have every positional param blanked. Existing cassette `TestIntegration_AdditionalDisks_EncryptedLifecycle` scrubbed.
- **Singleton lease epoch fencing + server-time fallback** (High) — Each lease write includes a monotonically-increasing `bearbinary.com/singleton-epoch` annotation. Staleness computation falls back to COSI's server-observed `Metadata().Updated()` when the heartbeat annotation is missing, preparing for eventual client-clock-immune operation.
- **WebSocket mutex split** (Medium) — Replaces single call-mutex with a reader goroutine + per-request pending map + short-held write lock. Slow RPCs no longer cascade timeouts; `ctx` cancellation unblocks waiters immediately. New `TestWSChaos_ConcurrentCalls_DoNotSerialize` and `TestWSChaos_CtxCancelDoesNotWaitForMutex` pin the behavior.
- **`TRUENAS_HOST` validation + upload URL hardening** (Medium) — `validateHost` rejects schemes, paths, user-info, query, fragments before anything reaches the bearer-token upload path. `uploadClient.CheckRedirect` returns `http.ErrUseLastResponse` so a 3xx can't forward credentials. Upload URL built via `net/url` rather than `fmt.Sprintf`.
- **WebSocket read size cap (16 MiB)** — Malicious or compromised server frames cannot OOM the provider.
- **`filesystem.put` body via `json.Marshal`** — Hand-rolled `fmt.Sprintf %q` JSON replaced; no Unicode corner-case divergence between Go quoting and JSON.
- **`slog.Warn` on cleartext `ws://` fallback** — Loud warning when `TRUENAS_INSECURE_SKIP_VERIFY=true` downgrades to cleartext. Suppressed for loopback so dev/CI is quiet.
- **SO_LINGER + bounded `Close()` deadline** — Half-open TCPs no longer wedge provider shutdown.
- **Env secret scrubbing** (Medium) — `TRUENAS_API_KEY`, `OMNI_SERVICE_ACCOUNT_KEY`, `PYROSCOPE_BASIC_AUTH_PASSWORD`, `OTEL_EXPORTER_OTLP_HEADERS` captured into local vars and then `os.Unsetenv`'d immediately. `/proc/<pid>/environ` and core dumps can no longer recover them.
- **Auth error reason scrubbing** — Long alphanumeric substrings (key-shaped) in server-returned error reasons are redacted before wrapping into Go errors.
- **`/healthz` returns generic error** — Raw TrueNAS error text (pool names, IPs) stays in server-side logs only.

### Security — Validation hardening
- `Data.Validate` rejects negative / overflow `cpus`, `memory`, `disk_size`, `storage_disk_size`; caps `additional_disks[i].size` at `MaxDiskSizeGiB` (1 PiB). Defense-in-depth against callers that bypass schema validation.

### Security — Supply chain
- **All third-party GitHub Actions pinned by full commit SHA**. Dependabot `package-ecosystem: github-actions` added to keep pins fresh. Blocks tag-move attacks on actions with `id-token: write` / `contents: write` scope.
- **`govulncheck` pinned to `@v1.1.4`** (was `@latest`).
- **Tag signature verification** in release workflow (`git tag --verify`) — require signed tags before releasing.
- **Multi-arch image smoke test** after GHCR push — pulls both `linux/amd64` and `linux/arm64` digests and runs `--version` under QEMU.
- **`anchore/sbom-action` pinned by SHA** instead of the floating `@v0` preview channel.
- **`make generate` CI check** — regenerates protobuf-backed code in a container with pinned `protoc` + `protoc-gen-go` and fails on diff. Stops a maintainer (or compromised account) from smuggling divergent `specs.pb.go`.
- **Helm chart `image.digest` override** + cosign verification recipe documented in `docs/hardening.md`. Production deployments can pin to an immutable digest and gate rollouts on cosign-verify via Kyverno / connaisseur.
- **betterleaks allowlist tightened** — blanket `docs/**` allowlist removed; scoped to specific files only. Historical example API key entry in the baseline flagged for rotation verification.

### Docs
- **`docs/hardening.md` v0.15 security model section** — documents ISO TOFU recovery, extension allowlist override, singleton epoch, ZFS passphrase trust model (known weakness: passphrase stored on the zvol it protects, acknowledged and scheduled for KEK-wrapping in v0.16+).
- **`docs/upgrading.md#upgrading-to-v015`** — breaking-change migration guide.

### QA — Test coverage and bugs caught before release

Added ~60 new test functions across nine files during a dedicated QA pass. The new tests flushed out three real defects that were introduced earlier in this same release cycle; all three were fixed before merge.

**Defects caught and fixed:**
- **Recorder passphrase redaction was exact-match only** — fields under namespaced keys like `org.omni:passphrase` (the property the provider writes on encrypted zvols) did not match the exact-name allowlist, so passphrases echoed back in a `pool.dataset.query` response would have landed on disk in a cassette recording. Fixed by switching `sensitiveFieldNames` from `map[string]bool` to a substring list and wrapping matches in `isSensitiveFieldName`. `TestRecordingTransport_E2E_RedactsResultField` pins the repair.
- **`BuildVMName` failed to collapse underscores across the provider-id / request-id boundary** — an empty `providerID` produced `omni__req_1`; a `providerID` sanitized to pure punctuation produced `omni___req_1`. Fixed by post-concatenation `__` collapse and trailing-underscore trim in `internal/resources/meta/meta.go:27`. `TestBuildVMName_EdgeCases` covers unicode, empty, pure-punctuation, long, and legacy-prefix inputs.
- **`isLocalOmniEndpoint` bypassed by deceptive subdomain** — `https://localhost-hijacker.example` matched the `https://localhost` prefix and would have dropped the multi-tenant `PROVIDER_ID` requirement on startup. Fixed with boundary-aware prefix matching (next char must be `:`, `/`, `?`, `#`, end-of-string, or digit after `127.`). `TestIsLocalOmniEndpoint_CorrectlyRejectsDeceptiveSubdomain` pins the exact exploit vector; `TestIsLocalOmniEndpoint_TableDriven` covers the full lookup surface.

**New test files:**
- `internal/provisioner/ownership_test.go` — 13 cases: `isOmniManagedVM` nil/prefix/legacy/mid-string, `omniVMDescription` format, `verifyZvolOwnership` managed-missing / managed-false / request-id mismatch / empty-expected / legacy-zvol / read-error.
- `internal/provisioner/iso_tofu.go` + `iso_tofu_test.go` — extracted TOFU decision into `classifyTOFU` + `cachedISOPoisoned` + `poisonMarker` helpers, tested directly; plus MockClient integration test for the POISON-marker round-trip through `SetDatasetUserProperty` / `GetDatasetUserProperty`.
- `internal/provisioner/data_test.go` (additions) — `TestValidate_NumericBounds` table: negative / over-max CPUs, Memory, DiskSize, StorageDiskSize; upper-bound on `additional_disks[i].size`.
- `internal/provisioner/testhelpers_test.go` — shared `managedVM` / `managedVMWithName` / `managedVMPtr` / `managedZvolQueryResult` helpers. Existing scattered boilerplate across `chaos_test.go`, `steps_test.go`, `vm_lifecycle_test.go`, `deprovision_test.go`, `step_integration_test.go`, `upgrade_test.go` refactored to use them.
- `internal/singleton/epoch_test.go` — 11 cases: epoch starts at 1, bumps on takeover (stale / unclaimed / malformed heartbeat), preserved on re-entrant acquire, detected-under-us as stolen during Run refresh, cleared by Release, and the `leaseAge` fallback to `Metadata().Updated()` for legacy pre-v0.15 resources.
- `internal/client/ws_lifecycle_test.go` — 6 cases for the v0.15 reader goroutine + pending map: pending-entry cleanup on ctx-cancel (50 concurrent calls, assert map empty), reader exits on Close, all pending fail on conn drop, orphan response dropped silently, ctx deadline beats the 30s default, and a race-enabled concurrent-calls stress test.
- `internal/client/ws_transport_edges_test.go` — 4 cases: Close bounded on half-open TCP, `SetReadLimit` rejects oversized frames, upload `CheckRedirect` refuses 3xx + bearer not forwarded, upload body JSON-valid for Unicode paths.
- `internal/client/recorder_replay_e2e_test.go` — 8 cases: recorder end-to-end redaction of request params / response result / sensitive-method positional args; `ReplayTransport.SetStrictParams` off-by-default preserves existing cassette behavior, on-catches-mismatch, on-structural-order-insensitive; `isLoopbackHost` table covering localhost / 127.x / `[::1]` + guard against substring-match regression.
- `internal/resources/meta/meta_test.go` (additions) — 5 sub-cases: unicode, empty, invalid-only, very-long, legacy-prefix shape.
- `internal/health/health_test.go` (additions) — 4 sub-cases: pool name, internal IP, request-id UUID, VM name all scrubbed from `/healthz` response body.
- `cmd/omni-infra-provider-truenas/secret_env_test.go` — `consumeSecretEnv` unsets after read, missing-var returns empty; `isLocalOmniEndpoint` table + dedicated deceptive-subdomain guard.

**Production refactors for testability:**
- `ReplayTransport.t` changed from `*testing.T` to a narrow `testReporter` interface. Lets tests substitute a recording fake for the strict-params mismatch path without hijacking the enclosing test's failure state.
- TOFU decision logic extracted from `stepUploadISO` into package-level `classifyTOFU` / `cachedISOPoisoned` / `poisonMarker` so the decision table is unit-testable without a real HTTP server + TrueNAS mock.

Full suite (including `-race`) passes. `make lint` is clean.



## [v0.14.7] — Empirically-verified API key setup, hardening guide, metrics-server docs, regression guards

### Security / Documentation
- **Rewrite API key setup after empirical verification against TrueNAS 25.10.1** — Prior docs told users to create the API key under **Credentials > Local Users > root > API Keys**, which ties the provider's audit trail to interactive root activity and can't be revoked without affecting root login. An earlier attempt (also in Unreleased) recommended a scoped-roles custom privilege with 13 roles instead; that recommendation was based on partial information and **does not actually work** because the Talos ISO upload endpoint (`/_upload`) enforces the `SYS_ADMIN` account attribute on top of the role system, and `SYS_ADMIN` is granted only via `builtin_administrators` group membership. Replaced with the verified-working recipe: dedicated non-root user + `builtin_administrators` group membership. All doc surfaces updated (`README.md`, `AGENT.md`, `docs/truenas-setup.md`, `docs/quickstart.md`, `docs/getting-started.md`, `docs/index.md`, `docs/troubleshooting.md`, `llms-full.txt`, `.env.example`, `deploy/docker-compose.yaml`). The new `docs/truenas-setup.md#5-api-key` documents the empirical findings including why scoped privileges alone don't work, with cross-links to the two upstream bug reports.

### Documentation
- **New `docs/hardening.md`** — Practical security hardening guide for the provider, organized as eight rungs from highest-feasibility-today to aspirational. Covers: dedicated non-root TrueNAS user (with the `builtin_administrators` requirement explained), API key rotation flow, scoped privilege caveats with cross-link to the upstream bug, network-level controls (management VLAN, firewall allow-list), secret storage for Kubernetes / Docker Compose / standalone, TLS hygiene, audit log + Prometheus alert ingestion, and per-zvol ZFS encryption. Includes a Mermaid threat model diagram, a `SecurityContext` snippet for Kubernetes, a `security_opt` snippet for Compose, a cosign verification one-liner, and a printable hardening checklist. Linked from `docs/truenas-setup.md` and added to mkdocs nav under Operations.
- **Metrics Server guide for Talos clusters** — New `docs/getting-started.md` Step 7 (plus pointer from Step 4) and matching blocks in `AGENT.md`, `llms-full.txt`, and `llms.txt` document the Talos-specific install recipe: cluster config patch `machine.kubelet.extraArgs.rotate-server-certificates: true` plus the `kubelet-serving-cert-approver` and `metrics-server` manifests delivered via Omni Extra Manifests. Covers both bootstrap-at-cluster-creation (preferred) and patch-existing-cluster paths. Follows the upstream Sidero guide.
- **Grafana dashboard marketplace descriptions** — New `deploy/observability/dashboards/README.md` with ready-to-submit entries (Name, Summary, Description, Panels, Tags, Required data sources) for the four bundled dashboards: Overview, VM Provisioning, TrueNAS API Performance, and Cleanup & Maintenance. Intended as the Description field when uploading to grafana.com/grafana/dashboards.

### Tools
- **`scripts/verify-api-key-roles`** — New Go probe that exercises every JSON-RPC method and the `/_upload` endpoint the provider calls, using an API key you supply, and prints a pass/fail matrix for the 13 recommended roles (or `FULL_ADMIN`). The probe creates and tears down a throw-away dataset, 1 MB test zvol, and a stopped test VM — no persistent state on success, no VMs are started, no existing data is touched. Lets operators verify a scoped privilege *before* assigning it to the provider. Cross-referenced from `docs/truenas-setup.md` and `docs/hardening.md`.

### Upstream bug reports
- **`docs/upstream-bugs/truenas-role-recursion.md` (NEW)** — TrueNAS 25.10.1 `middlewared/role.py:362-363` has no cycle detection in `RoleManager.roles_for_role()`. Saving a custom privilege with a meta-role (e.g. `FULL_ADMIN`, `READONLY_ADMIN`, `FILESYSTEM_FULL_CONTROL`) alongside its transitively-included child roles triggers `RecursionError: maximum recursion depth exceeded` on every subsequent `auth.login_*` call for any user bound to that privilege. Middleware restart doesn't fix it because the bad privilege is persisted in the config DB. Recovery requires editing the privilege via `midclt` from another admin account. Report includes full stack trace, minimal reproduction, proposed fix (visited-set guard), and user-side workarounds. File this upstream at iXsystems.
- **`docs/upstream-bugs/truenas-upload-role-gap.md` (NEW)** — TrueNAS 25.10.1 `/_upload` HTTP endpoint ignores `FILESYSTEM_DATA_WRITE` role and returns HTTP 403 unless the user is in `builtin_administrators`. Inconsistent with the JSON-RPC `filesystem.put` method which the role is documented to cover. Report includes a pass/fail matrix showing every other filesystem operation authorized by the same roles succeeding for the same user, `auth.me` diff between working (admin) and failing (scoped) users isolating `SYS_ADMIN` as the only differing attribute, reproduction script path, and proposed fix options. File this upstream at iXsystems.

### Tests
- **Regression guards** — New tests pinning invariants that were found missing during the v0.14.3–v0.14.6 investigation. `TestCreateConfigPatch_AlwaysUsesPatchNameHelper` (AST walk failing any bare-string-literal `CreateConfigPatch` call), `TestStepCreateVM_WiresAllExpectedPatches` (4 patch kinds present in `stepCreateVM`), `TestDefaultExtensions_RequiredEntries` (`iscsi-tools`/`util-linux-tools`/`qemu-guest-agent`), `TestBuildOTLPExporters_ProtocolSelection` (4 cases covering gRPC/HTTP selection), `TestBuildOTLPExporters_UnsupportedProtocolFailsFast`, `TestBuildHTTPExporters_UsesSignalEndpointWiring` (source-grep that `signalEndpoint` is actually called), `TestChangelog_VersionEntriesUseBracketFormat` (release-workflow awk extractor compat), `TestChangelog_EveryVersionHasReferenceLink`, `TestEnvDefaults_SafetyCriticalSettings` (6 sub-cases: `PROVIDER_SINGLETON_ENABLED=true`, `TRUENAS_INSECURE_SKIP_VERIFY=false`, `OMNI_INSECURE_SKIP_VERIFY=false`, `OTEL_EXPORTER_OTLP_PROTOCOL=grpc`, `GRACEFUL_SHUTDOWN_TIMEOUT=30`, `MAX_ERROR_RECOVERIES=5`).

### CI
- **Release workflow asserts Dockerfile + image invariants** — New "Verify image and Dockerfile invariants" step between smoke test and multi-arch push: asserts `Config.User == 65534:65534` on the built image (catches silent base-image default drift), and greps `Dockerfile` for `COPY --chmod=0755` and `^USER 65534:65534` (catches refactor drift that the runtime smoke test alone wouldn't detect in isolation). Any drift fails the build before anything reaches GHCR.

## [v0.14.6] — Fix every storage-side gap that made Longhorn silently broken

> **Storage-side hardening release.** Three independent bugs in v0.13.0–v0.14.5 left users with non-functional or silently-broken Longhorn deployments. This release fixes all three plus adds the Talos-side operational config the `install-longhorn.sh` script used to apply, so `storage_disk_size: 100` in a MachineClass is now sufficient for a Longhorn-ready worker. **Migration required for any existing cluster** — see end of entry.

### Fixes
- **Drop `maxSize: 0` from emitted `UserVolumeConfig`** — The patch builder added in v0.14.3 emitted `maxSize: 0` intending "unbounded", but Talos parses 0 as a literal byte count and rejects the document with `UserVolumeConfig/longhorn: min size is greater than max size`. Any worker that received the patch was stuck at Talos `stage: 3` with `configuptodate: false` and never finished joining the cluster. Per Talos v1.12 docs, the correct way to express "fill the disk" is to **omit** `maxSize` and rely on `grow: true`. Fixed in `buildUserVolumePatch`. Pinned by `TestBuildUserVolumePatch_SingleDisk_Longhorn` (now also asserts `maxSize` is absent from the YAML).
- **Fix `CreateConfigPatch` name collision across `MachineRequests`** — The Omni SDK's `provision.Context.CreateConfigPatch(ctx, name, data)` uses the literal `name` as the resource ID and upserts on every call. Every MachineRequest reconciling with the same unqualified name (e.g. `"data-volumes"`) wrote to the SAME `ConfigPatchRequest` resource — last writer wins, the other 5 of 6 machines silently went without their patch. Verified on a real cluster: 6 MachineRequests, 1 surviving `data-volumes` ConfigPatchRequest labeled for whichever request reconciled last. Same bug applied to `nic-mtu` and `advertised-subnets` patches. Fixed by introducing `patchName(kind, requestID)` helper and threading the request ID into all 4 call sites in `stepCreateVM`. Pinned by `TestPatchName_IncludesRequestID`, `TestPatchName_DistinctAcrossRequests`, `TestPatchName_DistinctAcrossKinds`.
- **Auto-emit Longhorn operational patch when a disk is named `longhorn`** — From v0.13.0 to v0.14.5 the provider attached the Longhorn data disk and (from v0.14.3) mounted it at `/var/mnt/longhorn`, but the **Talos-side bits** that make the node Longhorn-ready had to be applied by `scripts/install-longhorn.sh` — which most users either forgot to run or ran with the broken self-bind from v0.13.0–v0.14.2. The provider now emits a `longhorn-ops-<requestID>` patch alongside the `UserVolumeConfig` whenever any disk is named `longhorn` (set implicitly by `storage_disk_size`, explicitly by `additional_disks: [{name: longhorn, ...}]`). The patch loads the `iscsi_tcp` kernel module (without it, Longhorn iSCSI replica attachment fails and PVCs stay Pending forever), binds `/var/mnt/longhorn` → `/var/lib/longhorn` with `bind,rshared,rw` (without it, Longhorn writes replicas to Talos's ephemeral root partition — silent data loss on node replace), and sets `vm.overcommit_memory: "1"` (recommended for replica process stability). After v0.14.6, `helm install longhorn` is the only remaining user step. Pinned by 5 new test cases asserting source≠destination on the bind mount, `rshared` option present, `iscsi_tcp` module loaded, `vm.overcommit_memory=1` set, and `LonghornVolumeName` constant equals `"longhorn"`.

### Migration

Existing clusters provisioned on v0.13.0–v0.14.5 need cleanup before v0.14.6 starts emitting the new patches:

```bash
# 1. Delete the stuck data-volumes ConfigPatchRequest (collision artifact, has bad maxSize:0)
omnictl delete configpatchrequest data-volumes --namespace=infra-provider

# 2. If you have a manual Longhorn patch (e.g. longhorn-data-disk), delete it —
#    the provider now emits an equivalent patch automatically.
#    Two UserVolumeConfigs both named "longhorn" applied to the same machine
#    will be rejected by Talos.
omnictl delete configpatch longhorn-data-disk

# 3. Reprovision worker VMs so they pick up the per-request patches and the
#    operational patch on first boot. Easiest path: scale the worker
#    MachineRequestSet down to 0 then back up to N.

# 4. After workers come back up, install/upgrade Longhorn via Helm.
#    scripts/install-longhorn.sh is now optional — it still works (the Talos
#    patch it applies is a superset of what the provider emits, so it's a
#    no-op merge), but the only step that matters going forward is the Helm
#    install itself.
helm install longhorn longhorn/longhorn -n longhorn-system --create-namespace \
  --set defaultSettings.defaultDataPath=/var/lib/longhorn
```

## [v0.14.5] — Fix Grafana Cloud OTLP 404s (for real this time) + run as uid 65534

### Fixes
- **Fix OTLP 404s on Grafana Cloud (the v0.14.1 fix was wrong)** — v0.14.1 claimed to honor `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf` by forwarding `OTEL_EXPORTER_OTLP_ENDPOINT` through `otlptracehttp.WithEndpointURL(url)`, under the (incorrect) assumption that the SDK would append `/v1/traces`, `/v1/metrics`, `/v1/logs` to the path. It doesn't: `WithEndpointURL` in the Go OTEL SDK uses the URL path **verbatim** — it implements the `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` per-signal-URL semantic, not the `OTEL_EXPORTER_OTLP_ENDPOINT` base-URL semantic. So when a user set `OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp-gateway-prod-us-east-3.grafana.net/otlp`, every OTLP request went to `.../otlp` (no signal suffix) and Grafana Cloud returned `404 Not Found`. Observed as repeating `failed to send logs to https://.../otlp: 404 Not Found` / `traces export: ... 404` lines with no telemetry reaching the gateway. Fixed by introducing `signalEndpoint(base, "/v1/<signal>")` that appends the per-signal path before calling `WithEndpointURL`. Covered by `TestSignalEndpoint_AppendsPath` (6 cases including Grafana Cloud base URL, trailing slash, host-only, root path, and invalid-URL fallback) and `TestSignalEndpoint_InvalidURL_PassesThrough`.

### Behavior Changes
- **Container runs as uid/gid 65534 (`nobody`) instead of 65532** — The Dockerfile now sets `USER 65534:65534` explicitly, overriding the distroless `:nonroot` tag's default uid 65532. On TrueNAS hosts, `nobody` is uid 65534 by default, so bind-mounted volumes from the host now align with the container user without needing a `chown`. Container-only installs (pure Docker Compose, Kubernetes PVCs) are unaffected as long as volume ownership matches 65534 (most default volume-plugins create volumes owned by the container's uid). **Manual migration may be required** for existing deployments where volumes were pre-created and chown'd to 65532 (the old default): either `chown -R 65534:65534 <volume-path>` on the host, or override with `docker run --user 65532:65532` to keep the old behavior. The binary is statically linked Go — no username lookups — so the fact that uid 65534 has no `/etc/passwd` entry in the distroless image is harmless.

## [v0.14.4] — Fix container permission denied + add image smoke test; yank v0.14.3

> **v0.14.4 = v0.14.3 + permission-denied fix + pipeline smoke test.** All of v0.14.3's fixes ship here (UserVolumeConfig auto-emission for additional disks, `install-longhorn.sh` bind-mount correction) — the v0.14.3 release was yanked because its Docker image failed to start. Upgrading from v0.14.2 to v0.14.4 gives you every v0.14.3 fix plus a working binary. See the v0.14.3 entry below for the full storage fix details.

### Fixes
- **Fix `exec: permission denied` on container startup (v0.14.1–v0.14.3 images are broken)** — The parallelize-builds refactor in v0.14.1 introduced a silent regression: `actions/upload-artifact@v4` packages files as ZIP and strips the execute bit on upload; `actions/download-artifact@v4` restores them without `+x`. The Dockerfile's `COPY` then preserved the zero-permission file, so every Docker image published for v0.14.1, v0.14.2, and v0.14.3 fails immediately on startup with `OCI runtime create failed: exec: "/usr/local/bin/omni-infra-provider-truenas": permission denied`. Two-part fix: **(1)** Dockerfile now uses `COPY --chmod=0755` to set the execute bit at build time regardless of source file mode, and **(2)** the release workflow runs `chmod +x _out/omni-infra-provider-truenas-*` right after `download-artifact` so the signed binaries uploaded to the release page are also directly executable for users downloading them outside the container. **Users on v0.14.1–v0.14.3 must upgrade to v0.14.4.** Pinning to `v0.13.x` also works as a fallback (pre-regression), but v0.13.x is missing the v0.14.x fixes (WebSocket-only transport, Longhorn iscsi-tools extension, OTEL protocol honoring, boot-order fix, UserVolumeConfig auto-emission).

### CI
- **Add image smoke test to the release pipeline** — New step in the release workflow builds the image for `linux/amd64` into the local Docker daemon *before* the multi-arch push, then runs `docker run --rm smoke-test:<tag> --version` and asserts the output matches the tag. A broken binary (missing execute bit, corrupted cross-compile, failed `ldflags`) fails the workflow before anything reaches GHCR, cosign, or the GitHub release page. The multi-arch push only runs if the smoke test passes, so users cannot pull a broken image even transiently. Also adds a `--version` / `-v` / `version` flag to the CLI itself (prints version and exits 0) — separate from `run()` so no Omni/TrueNAS config is required, which makes it safe to invoke from CI with no env.

## [v0.14.3] — Fix additional disks never reaching Talos (Longhorn was running on the root disk) — YANKED

> ⚠️ **Yanked — do not use v0.14.3.** The published Docker image fails at startup with `OCI runtime create failed: exec: "/usr/local/bin/omni-infra-provider-truenas": permission denied` because `actions/upload-artifact` stripped the execute bit from the compiled binary. The same regression affects v0.14.1 and v0.14.2 images. The GitHub Release page for v0.14.3 has been removed; users on v0.14.3 (or v0.14.1/v0.14.2) should upgrade to **v0.14.4**, which carries all v0.14.3 fixes plus the permission-denied repair and a pipeline smoke test that prevents this class of regression.

### Fixes
- **Auto-emit Talos `UserVolumeConfig` for additional disks** — Setting `additional_disks` (or the `storage_disk_size` shorthand) attached the disk as a VM device on TrueNAS but never emitted the Talos config patch needed to format and mount it inside the guest. The disk showed up as a raw unformatted block device (`/dev/vdb`, `/dev/vdc`, ...) invisible to Longhorn, local-path-provisioner, and every other Kubernetes storage driver. Users had to apply a custom `UserVolumeConfig` patch manually for every MachineClass. Fixed by emitting a `UserVolumeConfig` patch per additional disk in `stepCreateVM` — filesystem `xfs` (default) or `ext4`, mounted at `/var/mnt/<name>`, with a CEL selector keyed to each zvol's exact byte-size (±1 MiB tolerance for block-alignment) so multiple same-sized disks assign 1:1 to volumes in discovery order. Two new `AdditionalDisk` fields: `name` (defaults to `data-N`, 1-indexed) and `filesystem` (defaults to `xfs`). `storage_disk_size` expansion now auto-sets `name: longhorn` so the volume mounts at `/var/mnt/longhorn` to match Longhorn's `defaultDataPath`. Validation rejects duplicate volume names (two disks can't mount at the same path) and unknown filesystems. Added `TestBuildUserVolumePatch_*`, `TestStorageDiskSize_ExpandsWithLonghornVolumeName`, `TestAdditionalDisks_DefaultsFillNameAndFilesystem`, and three new validation tests.
- **Fix `install-longhorn.sh` bind mount — Longhorn was silently running on the ephemeral root disk** — The Talos config patch in `scripts/install-longhorn.sh` declared `source: /var/lib/longhorn` and `destination: /var/lib/longhorn`: a self-bind that was effectively a no-op. It exposed the path under Talos's read-only `/var` overlay without mounting the attached data disk, so Longhorn has been writing replica data to Talos's ephemeral root partition instead of the `storage_disk_size` zvol since v0.13.0. Every `storage_disk_size` zvol on every existing Longhorn cluster has been attached, unformatted, and unused for two releases. Fixed to `source: /var/mnt/longhorn` to bind the provider's now-auto-emitted `UserVolumeConfig` mount into the path Longhorn's pods expect. Combined with the `UserVolumeConfig` auto-emission above, new clusters provisioned on this release get Longhorn running on the intended data disk out of the box. **Existing clusters need to re-run the script (idempotent — the config patch gets replaced) _after_ reprovisioning their worker VMs on this release** so the UserVolumeConfig mount exists before the bind references it. Migrating data off the ephemeral root is Longhorn's problem: drain replicas to new nodes, remove old nodes, rebalance.

## [v0.14.2] — Fix UEFI boot order trapping Talos in halt_if_installed

### Fixes
- **Boot order: root disk before CDROM** — Provisioned VMs set CDROM `order=1000` and root disk `order=1001`, which in bhyve's UEFI boot manager means "CDROM first, disk second". The initial install worked because Talos installs from the ISO, reboots, and the disk then has a bootloader — but any subsequent reboot where UEFI re-entered the CDROM caused the VM to halt with `task haltIfInstalled: Talos is already installed to disk but booted from another media and talos.halt_if_installed kernel parameter is set`. Re-ordered to root disk `1000`, additional disks `1001+`, CDROM `1500`, NIC `2001`. Now UEFI tries the disk first and only falls through to the CDROM on a fresh VM where the disk is empty. Added `TestBootOrder_DiskBeforeCDROM` to pin the invariant. **Migration required for VMs provisioned on v0.14.1 or earlier** — bump each CDROM's `order` from `1000` to `1500` (TrueNAS UI: VM → Devices → CDROM → Device Order; or `midclt call vm.device.update <id> '{"order": 1500}'`). New VMs provisioned on v0.14.2 and later are unaffected. See [Troubleshooting](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/docs/troubleshooting.md#vm-halts-on-reboot-with-talos-is-already-installed-to-disk-but-booted-from-another-media) and [Upgrading](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/docs/upgrading.md#upgrading-to-the-boot-order-fix-v0142).

### Removed
- **TrueNAS app catalog packaging** — Deleted the `truenas-app/` directory (app.yaml, questions.yaml, ix_values.yaml, docker-compose template, migrations stub). The provider is no longer being submitted to the TrueNAS community apps catalog. Installation on TrueNAS is still supported via **Apps > Discover > Install via YAML** with the compose YAML documented in `README.md` and `docs/quickstart.md` — the removed files were only used for catalog-format submission. Affected doc language was updated from "TrueNAS App (Recommended)" to "Docker Compose on TrueNAS (Recommended)" in `README.md`, `docs/index.md`, `docs/quickstart.md`, `AGENT.md`, `llms.txt`, and `llms-full.txt`. Bug report template's deployment-method field updated accordingly.

### Documentation
- **New control plane sizing guide** (`docs/sizing.md`) — When to bump CP VM resources, with concrete observable triggers (apiserver p99 > 1s, etcd `apply request took too long` warnings, kube-apiserver OOMKilled, `kubectl top` CPU/mem > 70% sustained, etcd DB > 2 GiB, heavy operator installs like ArgoCD / Crossplane / service meshes). Includes a sizing table from homelab (2 vCPU / 2 GiB) up to 50+ node clusters, an HA rolling-replace procedure (drain → delete → scale up → repeat) with a Mermaid sequence diagram, single-CP in-place resize via `midclt`, and a note that etcd fsync latency is a ZFS/SLOG problem — bumping CPU/RAM won't fix it. Linked from `index.md`, `getting-started.md`, `quickstart.md` MachineClass config table, and mkdocs nav under Operations.

### CI
- **Restore Grafana dashboards + alert rules as release assets** — The parallelize-builds refactor in v0.14.1 inadvertently dropped the dashboard bundling step added for v0.14.0 discoverability. Re-added: the release workflow now uploads `overview.json`, `provisioning.json`, `api-performance.json`, `cleanup.json`, a combined `grafana-dashboards.zip`, and `truenas-provider.rules.yml` as release assets on every tag. Users can grab them directly from the GitHub release page for import into Grafana Cloud / self-hosted.

## [v0.14.1] — Fix OTEL_EXPORTER_OTLP_PROTOCOL for Grafana Cloud

### Fixes
- **Honor `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`** — The `OTELProtocol` config field was declared but `initOTEL` only wired up the gRPC exporters, so setting `http/protobuf` silently fell back to gRPC. When users pointed `OTEL_EXPORTER_OTLP_ENDPOINT` at a Grafana Cloud OTLP gateway URL (`https://otlp-gateway-...grafana.net/otlp`), the gRPC name resolver rejected the `https://` scheme and logged `failed to upload metrics: exporter export timeout: rpc error: code = Unavailable desc = name resolver error: produced zero addresses` on repeat. Fixed by branching on `OTEL_EXPORTER_OTLP_PROTOCOL`: `grpc` (default) uses the existing gRPC exporters; `http/protobuf` (or `http`) uses the OTLP/HTTP exporters via `WithEndpointURL`, which accepts full URLs and appends `/v1/traces`, `/v1/metrics`, `/v1/logs` to the base path as the spec requires. Unknown protocol values now fail fast with a clear error instead of silently defaulting.

### Internal
- Update Grafana dashboard title assertions in `TestGrafanaDashboards_ValidJSON` to match the grafana.com-ready names shipped in v0.14.0.
- Add multi-size logo assets (128/256/512) for grafana.com plugin catalog upload.

## [v0.14.0] — WebSocket-Only Transport, Longhorn Default

### Breaking / Behavior Changes
- **Drop Unix socket transport — WebSocket + API key required** — TrueNAS 25.10 removed implicit authentication on the `middlewared.sock` Unix socket. Every JSON-RPC call now returns `ENOTAUTHENTICATED` unless the client has authenticated first, which means the "zero-auth Unix socket" path is no longer possible. The transport auto-detection logic, the `socketTransport`, `TRUENAS_SOCKET_PATH` env var, and the socket mount have all been removed. `TRUENAS_HOST` and `TRUENAS_API_KEY` are now required in all deployments. When running as a TrueNAS app, set `TRUENAS_HOST=localhost` and `TRUENAS_INSECURE_SKIP_VERIFY=true`.

### Features
- **Console OTEL exporters (opt-in)** — Set `OTEL_CONSOLE_EXPORT=true` to emit traces, metrics, and logs to stdout in addition to the configured gRPC endpoint. Off by default to avoid log spam in production. Traces and logs use pretty-printed JSON; metrics print every 60s. Useful for local debugging without wiring up a collector.
- **Startup log includes TrueNAS host and TLS verify status** — The `TrueNAS client connected` log line now shows `host=<truenas-host>` and `tls_verify=<bool>` to make misconfiguration easier to spot.
- **Add `siderolabs/iscsi-tools` to default extensions** — Longhorn (the default storage) uses iSCSI internally to attach replicas to pods. Previously users had to manually add `iscsi-tools` to their MachineClass `extensions` list or PVCs would sit in Pending forever. Now it's baked in alongside `qemu-guest-agent` and `util-linux-tools`.
- **Longhorn install script loads `iscsi_tcp` kernel module** — `scripts/install-longhorn.sh` now includes `machine.kernel.modules: [iscsi_tcp]` in the Talos config patch. Required for Longhorn to establish iSCSI sessions between replicas and pods.

### Removed
- `socketTransport` implementation and all Unix-socket-specific code paths
- `TRUENAS_SOCKET_PATH` environment variable
- `SocketPath` field on `client.Config`
- Unix socket host mount from the TrueNAS app definition
- **`siderolabs/nfs-utils` from default Talos extensions** — the provider no longer manages NFS storage, so the NFS client is no longer needed in every VM. Users who want democratic-csi NFS mode or manual NFS mounts can add `siderolabs/nfs-utils` to their MachineClass `extensions` field.

### CI
- **Parallelize release binary builds via matrix strategy** — Release workflow now cross-compiles the four target platforms (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`) on separate runners in parallel via GitHub Actions `strategy.matrix`, instead of sequentially on a single runner. Each matrix job uploads its binary as an artifact; the release job downloads all four before signing and publishing. Cuts wall-clock time on the build stage roughly 4x.
- **Drop duplicate compile in release gate** — The `test` job in `release.yaml` no longer runs `make build`. `go test` already compiles the packages, so the separate build step was pure duplication. Saves ~30s per release.

## [v0.13.2] — Fix Unix Socket Transport for TrueNAS 25.10 (SUPERSEDED — use v0.14.0+)

> ⚠️ **KNOWN BROKEN.** The Unix socket fix in v0.13.2 was incomplete. TrueNAS 25.10's middleware requires authentication on every JSON-RPC call, so the "zero-auth Unix socket" path is no longer viable. Upgrade to v0.14.0, which uses WebSocket with mandatory API key authentication.

### Bug Fixes
- **Fix Unix socket transport for TrueNAS 25.10+** — TrueNAS 25.10 (Goldeye) changed the middleware Unix socket from raw JSON-RPC to JSON-RPC 2.0 over WebSocket. The provider now uses WebSocket-over-Unix with pure JSON-RPC 2.0 framing (no DDP handshake), matching `midclt`'s `JSONRPCClient`. Without this fix, the provider crash-loops with `invalid character 'H' looking for beginning of value` or `i/o timeout` when deployed as a TrueNAS app.

### CI
- **Eliminate QEMU from Docker builds** — The Dockerfile no longer compiles Go inside the container. Pre-built binaries from Go's native cross-compilation are `COPY`ed directly into distroless, removing the QEMU emulation bottleneck for arm64. Release builds that took 10+ minutes now complete in under 30 seconds.

### Housekeeping
- Remove unused raw JSON-RPC request/response types (superseded by WebSocket protocol)
- Add reconnect with exponential backoff to Unix socket transport (matches WebSocket transport behavior)

## [v0.13.1] — Grafana Cloud Observability

> ⚠️ **Incompatible with TrueNAS SCALE 25.10 (Goldeye).** Upgrade to [v0.14.0](#v0140--websocket-only-transport-longhorn-default) if you're on 25.10+.

### Features
- **Grafana Cloud observability support** — OTEL exporters now accept `OTEL_EXPORTER_OTLP_HEADERS` for authenticated endpoints (e.g., Grafana Cloud OTLP gateway). Pyroscope client supports `PYROSCOPE_BASIC_AUTH_USER` and `PYROSCOPE_BASIC_AUTH_PASSWORD` for Grafana Cloud Profiles. Both local dev stacks and Grafana Cloud work with the same provider binary — just different env vars.

### Housekeeping
- Reserve removed proto field `nfs_dataset_path` (field 10) to prevent accidental reuse
- Remove stale `configureStorage` and NFS panels from Grafana provisioning dashboard

## [v0.13.0] — Multi-Disk VMs, Singleton Lease, Deterministic MACs, Circuit Breaker & Storage

> ⚠️ **Incompatible with TrueNAS SCALE 25.10 (Goldeye).** Upgrade to [v0.14.0](#v0140--websocket-only-transport-longhorn-default) if you're on 25.10+.

### Breaking / Behavior Changes
- **Longhorn is now the only supported storage path** — NFS auto-storage has been fully removed (see Removed section below). Add a dedicated data disk via `storage_disk_size` in your MachineClass, then install Longhorn via Helm. See [`docs/storage.md`](storage.md) for setup steps.
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

[v0.15.1]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.15.1
[v0.15.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.15.0
[v0.14.7]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.14.7
[v0.14.6]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.14.6
[v0.14.5]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.14.5
[v0.14.4]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.14.4
[v0.14.3]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.14.3
[v0.14.2]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.14.2
[v0.14.1]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.14.1
[v0.14.0]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.14.0
[v0.13.2]: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.13.2
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
