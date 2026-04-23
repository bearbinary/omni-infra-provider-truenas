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

For TrueNAS Docker-Compose-on-host deployments (installed via Apps > Discover > Install via YAML), update the image tag in the app's YAML and redeploy from the TrueNAS UI.

## Version Notes

### Upgrading to v0.16.1

v0.16.1 is a **bugfix** on top of v0.16.0 that removes the `addresses` and `gateway` fields from `additional_nics[]`. **Do not use v0.16.0 in a multi-worker MachineSet** — it accepts static addresses/gateways per NIC, but a MachineClass is shared across every worker, so any static IP in the class would be claimed by N workers and collide. v0.16.1 removes the fields entirely and defaults `dhcp: true` on every additional NIC. Static pinning is only supported via **DHCP reservations on the upstream router**, keyed off the deterministic MAC the provider logs at VM creation.

**Migration from v0.16.0:**
- If a MachineClass has `additional_nics[*].addresses` or `additional_nics[*].gateway` set: remove those fields. The v0.16.1 provider rejects them at schema-validation time; MachineRequests will never reconcile until the class is edited.
- If you need to pin a specific worker to a specific IP on the secondary segment: add a DHCP reservation on your router for the NIC's deterministic MAC (shown in the provider's `attached additional NIC` log line at provision time).
- If you deployed v0.16.0 and are now upgrading to v0.16.1: any running VMs whose config patches include `addresses`/`routes` from v0.16.0 keep running — the patches sit in Omni state but no reconcile re-applies them against a v0.16.1 MachineClass. To fully roll off, replace the VMs against the v0.16.1 class.

### Upgrading to v0.16 (v0.16.0 — superseded by v0.16.1)

> **Skip v0.16.0.** The `additional_nics[*].addresses` / `.gateway` fields it introduced are unsafe on shared MachineClasses. Upgrade straight from v0.15.5 to v0.16.1.

v0.16 lands the multi-homing fix (additional NICs now actually usable on any segment), the experimental `autoscaler` subcommand, and one breaking change to `disk_size` validation.

#### Breaking: `disk_size` minimum raised from 5 GiB to 20 GiB on the root disk

The primary / OS disk now fails validation below 20 GiB. The additional-disk floor (`additional_disks[].size`) stays at 5 GiB — this change applies only to the MachineClass-level `disk_size`.

**Why:** a Talos control-plane node pulls kube-apiserver + kube-controller-manager + kube-scheduler + etcd + kube-proxy + CNI + CoreDNS during bootstrap, plus the Talos squashfs image and kubelet's 10% GC headroom. A 5–10 GiB root disk fills up mid-install, the kubelet evicts images mid-pull, and etcd never comes up. See [`docs/sizing.md#why-the-root-disk-has-a-20-gib-minimum`](sizing.md#why-the-root-disk-has-a-20-gib-minimum).

**Migration:**
1. Audit MachineClasses: `omnictl get machineclass -o yaml | grep -B2 "disk_size:"` — anything below 20 will fail to reconcile after the upgrade.
2. Edit the values to ≥ 20 (default `40` recommended for production).
3. Upgrade the provider image.

Existing VMs built against an older class are **not retroactively resized**. If a running node is hitting DiskPressure, reprovision the machine against the updated class — Talos is immutable by design (replace, don't mutate).

#### New: Additional NICs are now configured automatically

Prior to v0.16 the provider attached extra NICs at the hypervisor but emitted no Talos config patch to configure them — the links came up with only link-local IPv6 and the VM was effectively single-homed. v0.16 auto-enables DHCPv4 on every `additional_nics[]` entry and exposes optional static-addressing fields.

No action required for existing MachineClasses that only use DHCP on additional NICs — they will start working correctly on the next provision.

**Shipped-but-unsafe `additional_nics[]` fields (removed in v0.16.1):**
- `dhcp`, `addresses`, `gateway` — intended to support static addressing on
  secondary NICs. Unsafe on shared MachineClasses because every worker in a
  MachineSet renders the same class. v0.16.1 removes `addresses` and
  `gateway` entirely; `dhcp` stays as a simple tri-state (`true` default,
  `false` to attach without autoconfig).

**Cap:** `MaxAdditionalNICs = 16`, enforced in both Go validation and `schema.json` as `maxItems`.

See [`docs/multihoming.md`](multihoming.md) for supported examples.

#### New: Experimental `autoscaler` subcommand

`omni-infra-provider-truenas autoscaler` is a new opt-in subcommand implementing the Kubernetes cluster-autoscaler external-gRPC cloud-provider interface. Scale-up only (no scale-down yet), gated per-MachineClass via the `bearbinary.com/autoscale-min` and `bearbinary.com/autoscale-max` annotations. Without the annotations, a MachineClass is not discovered — zero impact on existing deployments.

See [`docs/autoscaler.md`](autoscaler.md) for deploy recipe and the full annotation reference. A Helm chart ships in `deploy/helm/omni-autoscaler/`.

The provisioner subcommand (no argv) is unchanged. Existing Deployments that bump the image tag without also adding `autoscaler` to the args will not start the autoscaler.

#### New: `truenas.config_patch.duration` histogram

Operators with the provider's OTel metrics wired into Prometheus will see a new histogram labeled by `patch_kind` (`data-volumes`, `longhorn-ops`, `nic-mtu`, `nic-interfaces`, `advertised-subnets`). Drop into the existing provider dashboard as a latency panel — no config change needed.

#### New: `config_invalid` and `config_patch` error categories

`truenas.provision.errors` now distinguishes MachineClass validation failures (new `error_category="config_invalid"`) and ConfigPatchRequest emission failures (`error_category="config_patch"`) from real TrueNAS-side NIC/pool errors. Alert-routing rules that match on `error_category=nic_invalid` should be reviewed — if the previous intent was "any NIC-related error", widen the match to `{config_invalid,nic_invalid}`.

### Upgrading to v0.15

v0.15 is a security-hardening release with **one breaking change** and several new invariants that may surface pre-existing state issues.

#### Breaking: VM names now namespaced by provider ID

Prior to v0.15 VM names were `omni_<requestID>`. From v0.15 onward they are `omni_<providerID>_<requestID>`. This prevents two providers sharing a TrueNAS host from racing on VM names.

**Impact on upgrade:**

- Existing v0.14 VMs **will not be adopted** by a v0.15 provider. The provider will try to create a new VM with the namespaced name, find nothing, and attempt to create — which then collides with the zvol already in use.
- Existing v0.14 VMs **will not be automatically cleaned up**. The cleanup scanner matches both shapes (legacy `omni_<reqID>` and new `omni_<providerID>_<reqID>`) so orphan detection still works, but the VM will be orphaned in Omni until you drain or delete it.

**Recommended upgrade path:**

1. Drain MachineRequests from the current provider (scale the MachineSet to zero, or delete nodes one by one from Omni) before upgrading.
2. Wait for all v0.14 VMs to be deprovisioned normally.
3. Upgrade the provider image to v0.15.
4. Scale the MachineSet back up. New VMs get the namespaced naming.

If you can tolerate a full cluster recreate (small homelab, preview environment), skip straight to the image bump and let your state reconverge.

#### New: PROVIDER_ID is required for remote Omni

If `OMNI_ENDPOINT` is not localhost, `PROVIDER_ID` is now required — the provider fails fast on startup otherwise. This prevents two tenants that both default to the hardcoded `"truenas"` ID from sharing the same singleton lease annotation keyspace.

If you were running the default provider ID before, pick a unique value and set it in your deployment:

```yaml
# values.yaml (Helm)
provider:
  id: "truenas-prod-dc1"
```

#### New: Talos extension allowlist

MachineClass `extensions:` entries must now appear on the [built-in allowlist](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/internal/provisioner/extensions.go) or the provider refuses to provision. If you legitimately use a custom schematic ID, set `ALLOW_UNSIGNED_EXTENSIONS=true` on the provider deployment and understand that no supply-chain review has been done for bypassed extensions.

#### New: Deprovision ownership check

v0.15 refuses to deprovision a VM whose description doesn't contain `Managed by Omni infra provider` or whose zvol isn't tagged `org.omni:managed=true`. If you see this error, the provider's stored state is pointing at a resource it didn't create — investigate before retrying. Most commonly this happens when a VM was manually renamed on TrueNAS, or a second provider instance wrote conflicting state.

#### New: ISO TOFU hash pinning

The provider records the SHA-256 of each Talos ISO on first download. Subsequent downloads are compared against the recorded hash; a mismatch fails the provision and marks the ISO `POISONED`. If you see a `POISONED` error, see [`docs/hardening.md`](hardening.md#iso-supply-chain-tofu) for the recovery recipe.

### Upgrading to the boot-order fix (v0.14.2)

**Existing VMs provisioned on v0.14.1 or earlier need a one-time boot-order
correction on TrueNAS.** Upgrading the provider image alone is not sufficient —
the provider only sets boot `order` at VM creation time, so pre-existing VMs
keep their old (incorrect) ordering until you change it manually.

**Symptom if left unfixed.** The VM boots fine today, but the next time it
reboots — host reboot, TrueNAS update, manual stop/start — it halts with
`task haltIfInstalled: Talos is already installed to disk but booted from
another media and talos.halt_if_installed kernel parameter is set`. See
[Troubleshooting](troubleshooting.md#vm-halts-on-reboot-with-talos-is-already-installed-to-disk-but-booted-from-another-media)
for the full explanation.

**Action required for each existing VM:**

TrueNAS UI:

1. **Virtualization > Virtual Machines > _your VM_ > Devices**
2. Edit the CDROM device → change **Device Order** from `1000` to `1500`
3. Save. Next reboot will boot from disk correctly.

TrueNAS shell (scriptable for many VMs):

```bash
# For each running Talos VM:
midclt call vm.device.query '[["vm","=",<VM_ID>]]' | jq '.[] | select(.attributes.dtype=="CDROM") | .id'
midclt call vm.device.update <CDROM_DEVICE_ID> '{"order": 1500}'
```

You do **not** need to reboot the VM immediately — the fix applies the next
time the VM starts. No data migration, no Talos reinstall, no config change.

**New VMs provisioned after the upgrade** are unaffected: they get the correct
boot order (root disk `1000`, additional disks `1001+`, CDROM `1500`,
NIC `2001`) at creation time.

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
- **Docker Compose / systemd deployments (including Docker-Compose-on-TrueNAS)**:
  no action needed. These run one instance by design.
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

