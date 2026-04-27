# Pre-Release Testing & Promotion

This document is the canonical process for validating a pre-release of
`omni-infra-provider-truenas` before promoting it to a stable tag and submitting
the corresponding chart bump to the **TrueNAS apps catalog (community train)**.

The provider is shipped to end users through `truenas/apps`
(`ix-dev/community/omni-infra-provider-truenas/`). A bad release does not just
inconvenience GHCR Docker pullers — it lands in the TrueNAS UI under
**Apps → Discover** and an "Update Available" prompt appears for every existing
install. Once a chart bump is merged into the catalog, retracting it requires
another PR and a forced `force_upgrade` on every affected user, plus whatever
recovery the regression itself needs. **Promotion is not free. Soak first.**

This process applies to every minor or major bump (`vX.Y.Z` → `vX.(Y+1).0` or
`v(X+1).0.0`). It also applies to patches that touch any of the surfaces in the
[Trigger Matrix](#trigger-matrix) — patches that only change tests, comments, or
internal refactors with unchanged behavior may skip it.

---

## Trigger Matrix

A pre-release validation cycle is **required** when the diff since the last
stable tag touches any of the following:

| Surface | Why it can break installs |
|---|---|
| `cmd/omni-infra-provider-truenas/data/schema.json` | MachineClass values that validated under N may be rejected under N+1 — every existing MachineSet stops reconciling until the operator edits |
| `internal/provisioner/data.go` (defaults, validators, floors) | Saved configs that omitted a field now hit a new default; floors raised mid-flight reject pre-existing classes |
| Env-var names, formats, or defaults | A user's deployment.yaml / docker-compose.yaml / TrueNAS app values stop being parsed correctly |
| `Dockerfile` (USER, COPY mode, ENTRYPOINT) | Container fails to start with `permission denied` or wrong-uid bind mounts on TrueNAS hosts |
| `internal/singleton/` lease behavior | Two processes race on lease acquisition or stale-takeover; users see flapping registrations in Omni |
| `internal/client/` JSON-RPC method names, params, or response handling | TrueNAS 25.04 / 25.10 / 26.x ship slightly different method shapes; we own the compatibility window |
| `internal/resources/`, COSI typed-resource fields | Existing Machine resources fail to decode after upgrade; provider crash-loops |
| Telemetry surface (metric names, labels, dashboard JSONs) | Downstream Grafana boards / Prometheus alert rules reference renamed series |
| Chart `templates/`, `ix_values.yaml`, `questions.yaml` (in `truenas/apps`) | Saved values from N stop being valid under N+1; Helm upgrade fails mid-rollout |
| Anything labeled "Breaking" in CHANGELOG | By definition |

If the diff is unambiguously confined to docs (`docs/**`, `README.md`,
`CHANGELOG.md` reordering, `llms.txt`), this process is **not** required.
A direct `cutting-release` patch is sufficient.

---

## Process Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│  1. Cut pre-release tag    (skill: cutting-pre-release)             │
│  2. Run pre-soak matrix    (this doc, §Pre-Soak Test Matrix)        │
│  3. Open soak window       (this doc, §Soak Window)                 │
│  4. Watch signals          (this doc, §Signals to Watch)            │
│  5. Go / no-go review      (this doc, §Promotion Criteria)          │
│  6. Cut stable tag         (skill: cutting-release)                 │
│  7. Bump catalog chart     (this doc, §Catalog Chart Bump)          │
└─────────────────────────────────────────────────────────────────────┘
```

If any step fails, the pre-release stays as-is on GitHub (immutable releases
are not deleted — they serve as evidence of the iteration), a new
`-rc.(N+1)` is cut with the fix, and the cycle restarts at step 2.

---

## Pre-Soak Test Matrix

These four paths must all pass on the **cohort environment** (`talos-default`
on the maintainer's TrueNAS SCALE 25.10.1 host) before the soak window opens.
Cohort environment because (a) we have control of the host and Omni tenant,
and (b) the workload exercises the real provision path with a real
`MachineClass`.

| # | Path | Setup | Pass Criteria |
|---|------|-------|---------------|
| 1 | **Fresh install** | Wipe provider state in Omni; install pre-release on a clean TrueNAS app slot | Provider reaches `Healthy=true` in Omni within 60s; first MachineRequest provisions a VM that boots into Talos `Ready` |
| 2 | **In-place upgrade from current stable** | Last stable already running with at least one provisioned MachineSet (1 CP + 1 worker minimum); upgrade to the pre-release image without changing values | Existing MachineRequests stay reconciled; no resource churn (`talosctl get machineconfigs --watch` shows no spurious config-patch loops); zero new error categories in `truenas.error_count` for 30m |
| 3 | **Skip-version upgrade** | Install N-2 (or earliest version still in the supported window per `docs/upgrading.md`); upgrade directly to the pre-release | Same as path 2; if the diff includes a documented migration step (e.g. v0.16.0→v0.16.1's `additional_nics.addresses` removal), the migration runs idempotently and the post-upgrade state matches `docs/upgrading.md`'s expected shape |
| 4 | **Rollback** | Install pre-release; provision one MachineRequest; downgrade back to last stable | Provider returns to N-1 behavior; the MachineRequest provisioned under the pre-release continues to reconcile under N-1 (no orphaned VMs, no zvols stuck in a half-deleted state) |

For **major version bumps**, also run:

| # | Path | Setup | Pass Criteria |
|---|------|-------|---------------|
| 5 | **Breaking-config negative test** | Apply a `MachineClass` that the previous major accepted but the new major rejects | Validation error names the offending field and points at `docs/upgrading.md`; the error is structured (categorized via `categorizeError`) and surfaces in the `config_invalid` metric bucket |
| 6 | **Documented migration replay** | Pick the most-disruptive migration step from `docs/upgrading.md` for this version; replay it from a snapshot of N-1 state | Migration completes without manual intervention beyond what the doc states; post-migration state is byte-identical to a clean v(N) install of the same MachineClass |

Record the outcome of each path in the soak-window tracking issue
(see [§Open the Soak](#open-the-soak)).

---

## Soak Window

The soak window is a fixed-duration period during which the cohort env runs
the pre-release under a normal workload. Duration scales with risk:

| Bump kind | Minimum soak |
|---|---|
| Patch (only triggered by the matrix above; otherwise N/A) | 24 hours |
| Minor (`vX.Y.Z` → `vX.(Y+1).0`) | 72 hours, including ≥1 full provision/deprovision cycle |
| Major (`vX.Y.Z` → `v(X+1).0.0`) | 7 days, including a deliberate fault-injection drill (see below) |

### Major-version fault injection (mandatory)

For majors, the soak must include at least one of each of these injected
faults during the 7-day window. Goal: prove the new code recovers the same
way (or better) than N-1, not worse.

1. **Kill the provider mid-provision.** While a MachineRequest is mid-flight,
   `kubectl delete pod -n omni-infra-provider <provider-pod>`. The lease's
   stale-heartbeat takeover (`PROVIDER_SINGLETON_STALE_AFTER`) should hand
   off cleanly; the new pod resumes provisioning without duplicating the VM.
2. **Restart Omni.** Use the Omni cloud control panel. Provider's reconnect
   loop should re-establish without manual intervention.
3. **Reboot TrueNAS.** Confirm the provider survives the long API outage,
   reconnects, and re-syncs zvol/VM state without spurious destroy calls.
4. **API-credential rotation.** Generate a new `TRUENAS_API_KEY`, swap it
   into the deployment, restart. Provider authenticates with new key; old
   sessions clean up.

### Open the soak

Open a tracking issue in the repo when the soak begins:

```
Title: Pre-release soak: vX.Y.Z-rc.N
Labels: release-soak

Body:
- Pre-release tag: vX.Y.Z-rc.N
- Target stable: vX.Y.Z
- Soak start: <UTC ISO8601>
- Soak end (earliest): <UTC ISO8601 = start + duration>
- Diff: https://github.com/bearbinary/omni-infra-provider-truenas/compare/<last_stable>...vX.Y.Z-rc.N
- Pre-soak matrix: [link to checklist or paste outcomes]
- Cohort env: talos-default (TrueNAS SCALE 25.10.1, Omni cloud tenant <name>)
- Known issues from previous soak: <link or "none">

Promotion blocked until:
- Soak window elapsed
- All four matrix paths pass on cohort env
- Zero sev-1 events
- All sev-2 events resolved or moved to known-issues
```

This issue is the single place where soak observations are pinned. Comment
on it for every signal worth recording: a new error category appearing,
a metric drift, a user report against the prerelease tag.

---

## Signals to Watch

For the duration of the soak, monitor these signals on the cohort env. Any
one of them tripping triggers a triage decision, not an automatic abort.

### Logs (provider container)

```bash
kubectl logs -n omni-infra-provider deploy/omni-infra-provider-truenas \
  -f --since=24h | grep -E '(level=error|panic|fatal)'
```

- Baseline: the error rate observed on the last stable for the same workload
- Trip: any new `panic` or `fatal`, OR error rate sustained at 2× baseline for >1h

### Metrics (Grafana dashboards, imported from `_out/grafana-dashboards.zip`)

| Series | Trip condition |
|---|---|
| `truenas_provision_duration_seconds` (p99) | > 2× the last stable's p99 over the same workload |
| `truenas_api_call_duration_seconds` (p99 by method) | > 2× last stable's p99 on any single method |
| `truenas_error_count_total` (by category) | A new category appears, or an existing category's rate triples |
| `truenas_config_patch_duration_seconds` (by patch_kind) | Any kind's p99 > 2× last stable |
| Provider pod restart count | Any non-zero value during steady-state |

### Omni surface

- MachineRequest reconcile errors (Omni UI → Resource Inspector → MachineRequest)
- Provider health flap (Omni UI shows `Healthy=false` even briefly)

### TrueNAS host

- `zfs list -t snapshot,filesystem,volume` — unexpected zvol growth or orphans
- TrueNAS Reporting → API session count (excessive session churn = client reconnect loop)

### GitHub

- Issues opened against the prerelease tag — search:
  `is:issue label:release-soak created:>=<soak-start>`

---

## Promotion Criteria

The pre-release is eligible for promotion to stable when **all** of the
following are simultaneously true:

- [ ] Soak window's minimum duration has elapsed (no early promotion)
- [ ] All pre-soak matrix paths passed on cohort env
- [ ] Zero sev-1 events recorded on the soak issue
- [ ] All sev-2 events are either fixed in a follow-up rc or explicitly
      documented as known-issues in the **stable** CHANGELOG entry
- [ ] No open issue labeled `regression` or `release-blocker` against the prerelease tag
- [ ] Cohort env has been on the prerelease for the full window without a forced rollback
- [ ] Maintainer (Zac) has explicitly authorized the stable cut for this specific version

If any criterion fails, the soak does **not** rollover. Either:
- **Fix-and-iterate:** cut `-rc.(N+1)` with the fix, restart soak from §Pre-Soak Test Matrix.
- **Abandon:** close the soak issue with a post-mortem comment, leave the
  prerelease tag in place (immutable history), and either drop the bump
  entirely or rescope it under a new target version.

### Severity definitions

- **Sev-1** — install break, data loss, secret leak, undetected silent corruption
- **Sev-2** — degraded performance, increased log noise, doc/UI drift, observability gap
- **Sev-3** — cosmetic, typos, micro-perf regressions within noise

---

## Stable Cut

When promotion criteria pass, cut stable from the **same commit** as the last
passing rc. Do not include any new commits — every commit between the
soaked rc and the stable tag is unsoaked code shipping under a stable label.

```bash
# Verify HEAD is the same commit as the soaked rc:
git log -1 --format=%H vX.Y.Z-rc.N
git log -1 --format=%H HEAD
# These must match. If not, either reset HEAD to the rc commit or
# cut another rc to soak the additional commits.
```

Then run the `cutting-release` skill, which writes a fresh CHANGELOG entry
for the stable version. Do **not** copy the rc's CHANGELOG entry verbatim —
the stable entry should describe final state (including any known-issues
inherited from the soak), not iteration history.

---

## Catalog Chart Bump

Only after the stable GitHub release is published does the catalog chart bump
go up. The catalog repo is `truenas/apps`, and our chart lives at
`ix-dev/community/omni-infra-provider-truenas/`.

### What to bump

In `app.yaml`:
- `app_version: vX.Y.Z` — match the new stable tag
- `version: A.B.C` — chart version, bumped per chart change
  - Patch bump (`A.B.(C+1)`) when only `app_version` changed
  - Minor bump (`A.(B+1).0`) when `templates/`, `ix_values.yaml`, or
    `questions.yaml` changed in a backward-compatible way
  - Major bump (`(A+1).0.0`) when chart-level changes are NOT backward
    compatible with users' saved values from prior versions

### Catalog-side validation

Before submitting the catalog PR, render the chart against a representative
saved-values snapshot from a real install:

```bash
cd ix-dev/community/omni-infra-provider-truenas
helm template . -f <saved-values-from-cohort-env>.yaml > /tmp/render.yaml
# Review for: removed env vars, missing required fields, image-tag drift
```

Then deploy the rendered chart side-by-side with the cohort env's running
install (separate namespace) and confirm:
- `helm upgrade --dry-run` against the cohort install succeeds with no
  warnings about deprecated/removed values
- `questions.yaml` schema accepts the cohort env's saved values without
  the user being prompted to "re-answer" any question
- The new chart's pod template diffs cleanly against the previous
  release's pod template (no unexpected env-var drops, no resource-limit
  surprises)

### Submit the bump

The catalog process is **issues-only on our side**, but PRs **on the
catalog side** (`truenas/apps`) — those are submissions to a third-party
repo we do not own, so they fall under the `public-pr-guard` skill's rules:

- **Always** invoke `public-pr-guard` before opening a PR against `truenas/apps`.
- **Never** auto-submit. Hand the maintainer (Zac) a step-by-step plan with
  the exact `app.yaml` diff, validation evidence (helm template / dry-run
  output), and a draft PR body. Let the maintainer push the trigger.
- Reference the soak issue from the catalog PR description so reviewers can
  see the validation evidence we gathered.

After the catalog PR merges, the iX catalog sync picks up the new chart
within a few hours. Users see "Update Available" in their TrueNAS Apps UI.

---

## Auto-Update Footnote (User-Facing)

TrueNAS apps support auto-update. Auto-updaters get the new chart as soon as
it lands in the catalog. They are effectively the **post-promotion canary**.
Watch the soak issue for 48h after catalog merge: any user report of a
broken auto-update is a sev-1 by default and triggers the rollback path
described next.

---

## Rollback Path (Post-Promotion)

If a regression is reported after the catalog chart merges:

1. **Triage immediately.** Open / reopen the soak issue. Tag `release-blocker`.
2. **Communicate.** Pin a banner-comment on the soak issue and a release-note
   amendment on the stable GitHub release: `WITHDRAWN — see #<issue>. Do not
   upgrade to vX.Y.Z. Use vX.Y.(Z-1).` Immutable releases mean the assets
   stay; the banner is the only honest signal we can ship.
3. **Catalog rollback PR.** Submit a follow-up to `truenas/apps` reverting
   the chart bump. The catalog will re-publish the previous chart version,
   and TrueNAS UI will show the prior version as the latest available.
   (`public-pr-guard` rules apply — same issues-only, hand-off-to-maintainer
   discipline as the original bump.)
4. **Fix forward.** Cut a new prerelease (`vX.Y.(Z+1)-rc.1`), restart this
   process from §Pre-Soak Test Matrix. Do **not** cut a stable hotfix without
   re-soaking — the entire reason this process exists is that we cannot
   safely ship to live marketplace users without one.

---

## Quick Reference

```
Trigger met?               → §Trigger Matrix
Cutting an rc?             → skill: cutting-pre-release
Running the matrix?        → §Pre-Soak Test Matrix
Soak issue not opened?     → §Open the soak (issue template inside)
Want to promote?           → §Promotion Criteria, then skill: cutting-release
Bumping the catalog?       → §Catalog Chart Bump (invoke public-pr-guard)
Regression after release?  → §Rollback Path
```
