# Node Rotation (DARK LAUNCH — NOT RELEASED)

> **Status: dark launch.** The reconciler code ships in the provider
> binary but the `node-rotation` subcommand refuses to start unless
> `NODE_ROTATION_DARK_LAUNCH=true` is set in its environment. This
> gate exists so the coupling with the autoscaler (rotation-state
> annotation → paused scaling) is exercised in CI while the feature
> is stabilized. Do not enable in production. The gate will be
> removed and the status downgraded to `experimental` when the
> feature is promoted; expect breaking changes on the annotation
> surface, lock TTL semantics, and observability contract until then.

Both `in-place` (worker) and `surge` (worker + control-plane)
strategies are supported.

## What it does

When an operator edits an opted-in MachineClass's `auto_provision`
block (CPU, memory, disk, kernel args, meta values, gRPC tunnel
toggle), the `node-rotation` subcommand notices and rotates members
of the matching MachineSets so they boot against the new spec — one
Machine at a time, never more, and only when min-healthy capacity
allows.

This is the same pattern as a Deployment rolling update in
Kubernetes, but for Talos VMs on TrueNAS.

## What it does NOT do

- **Talos version upgrades** — those flow through Omni's existing
  cluster-upgrade path. Talos version is on the Cluster spec, not on
  the MachineClass.
- **In-place rotation for control-plane** — refused at parse time
  because the boot-window gap on a CP member would drop the cluster
  below etcd quorum. CP classes must opt into `strategy: "surge"`.
- **Operator-managed MachineClasses** — classes without an
  `auto_provision` block aren't rotation candidates. The reconciler
  needs Omni to be the one issuing MachineRequests so that deleting a
  stale request reliably spawns a replacement.

## Opt-in

Add four annotations to the MachineClass:

```yaml
apiVersion: omni.sidero.dev/v1
kind: MachineClass
metadata:
  name: home-workers
  annotations:
    node-rotation.omni/enabled: "true"
    node-rotation.omni/role: "worker"
    node-rotation.omni/strategy: "in-place"
    # optional — defaults to 1 for worker, 2 for controlplane
    node-rotation.omni/min-healthy: "1"
```

The reconciler ignores any MachineClass without `enabled: "true"`.
Strict parse: typos like `yes` or `1` count as not opted in.

### Annotation reference

| Annotation | Required | Values | Notes |
|---|---|---|---|
| `node-rotation.omni/enabled` | yes | `true` / anything else | Master opt-in |
| `node-rotation.omni/role` | yes when enabled | `worker` / `controlplane` | User-declared. A `controlplane`-labeled MachineSet declared as `worker` is **refused** by default (in-place teardown would drop CP below quorum and brick etcd) — set `NODE_ROTATION_TRUST_DECLARED_ROLE=true` to override for hand-crafted clusters. The reverse mismatch (no CP label, declared `controlplane`) logs a warn and proceeds. |
| `node-rotation.omni/strategy` | yes when enabled | `surge` / `in-place` | `in-place` refused for `controlplane` |
| `node-rotation.omni/min-healthy` | no | integer ≥ 0 | Floor maintained during rotation. Defaults: cp=2, worker=1 |

### Reconciler-managed annotations

These are written by the reconciler — operators read them but should
not edit them by hand:

| Annotation | On | Purpose |
|---|---|---|
| `node-rotation.omni/class-generation` | MachineClass | Canonical hash of the current rotation-relevant inputs. Operators run `omnictl get machineclass -o yaml \| grep generation` to confirm rotation has caught up. |
| `node-rotation.omni/rotation-state` | MachineSet | Lock annotation. Format: `<generation>:<unix-ts>`. TTL: 5 min. The autoscaler reads this and pauses scaling for the affected node group while the lock is fresh. Refreshed every tick during a surge cycle so the lock stays alive across the multi-minute boot window. |
| `node-rotation.omni/surge-phase` | MachineSet | Surge state machine cursor. Format: `<phase>:<original-count>:<cycle-start-ts>`. Phases: `wait-up` (waiting for the +1 replacement to land) and `wait-down` (waiting for MRS to drain the oldest stale member after count drop). Cleared when a cycle completes. |

## Strategies

### `in-place` (v1)

Delete the oldest stale MachineRequest. The MachineRequestSet
controller observes the missing member and spawns a replacement at
the current MachineClass spec. Wait for the replacement to reach
`PROVISIONED`. Repeat.

Trade-off: **workload gap** on the rotated node from teardown until
the replacement schedules workloads. Talos boot + cluster join on
TrueNAS is typically 1–2 minutes. Kubernetes pods on the rotated
node are evicted; pod disruption budgets and replica counts on those
workloads determine whether users notice.

Refused for `controlplane` — see the surge follow-up below.

### `surge`

Adds a fresh Machine **before** tearing down a stale one, so there's
no workload gap. Required for control-plane rotation; recommended for
worker pools whose workloads have tight pod-disruption budgets.

Per-rotation cycle (one cycle per stale member):

1. **idle → SurgeUp**: bump `MachineAllocation.MachineCount` by 1.
   The MachineRequestSet controller spawns one new MachineRequest at
   the current MachineClass spec. Annotation `surge-phase` is set to
   `wait-up`.
2. **wait-up**: poll until the new request reaches `PROVISIONED`.
   The reconciler refreshes the lock TTL on every tick so the
   autoscaler stays paused.
3. **wait-up complete → SurgeDown**: drop `MachineCount` by 1. The
   MRS controller's scale-down picker is **in-use first, non-CP
   first, oldest first** — by construction the oldest member is one
   of the stale ones, which is exactly what we want torn down.
   Annotation `surge-phase` flips to `wait-down`.
4. **wait-down**: poll until total request count returns to original.
5. **wait-down complete → SurgeCycleComplete**: clear both
   annotations. The next tick re-enters from idle; if more stale
   members remain (multi-machine class), a new cycle begins.

If the operator (or another writer) edits `MachineCount` mid-cycle,
the engine detects the drift on its next tick, emits
`SurgeAborted`, clears the surge annotations, and replans from
scratch on the following tick. Operator intent wins; no rotation
state machine corruption.

The MRS scale-down picker is documented at
`internal/backend/runtime/omni/controllers/omni/machine_request_set_status.go::scaleDown`
in the upstream Omni repo. The "in-use first, non-CP first, oldest
first" ordering is what makes surge converge without us having to
target specific MachineRequest IDs ourselves.

## Coupling with the autoscaler

Both `node-rotation` and `autoscaler` write to
`MachineSet.MachineAllocation.MachineCount` (the surge strategy
bumps and drops it; the in-place strategy never touches it). To
prevent races, the rotation engine takes a string-annotation lock
(`node-rotation.omni/rotation-state`) on the MachineSet for the
duration of a step (in-place) or the whole cycle (surge).

The autoscaler reads this annotation (see
`internal/autoscaler/rotation_lock.go` in the source tree) and, if the
lock is within TTL, returns the node group to Cluster
Autoscaler with `Min == Max == CurrentSize`. CAS treats the group as
at-cap-and-at-floor and skips it for that refresh cycle.

The two subcommands share no Go types. The coupling is purely the
annotation string contract, so either subcommand can be deployed
independently. The contract is pinned by tests on both sides:

- `internal/autoscaler/rotation_lock_test.go::TestDiscover_PausesOnRotationLock`
- `internal/noderotation/engine_test.go::TestRotationLockAnnotationRoundtrip`

## Deploy

The reconciler is a separate subcommand of the same binary, so the
container image is the same as the provisioner / autoscaler. Point
the Deployment at `node-rotation` as the argv[1] and set the
cluster:

```yaml
spec:
  template:
    spec:
      containers:
        - name: node-rotation
          image: ghcr.io/bearbinary/omni-infra-provider-truenas:v0.17.0
          args: ["node-rotation"]
          env:
            - name: OMNI_ENDPOINT
              value: "https://omni.example.com"
            - name: OMNI_SERVICE_ACCOUNT_KEY
              valueFrom:
                secretKeyRef:
                  name: omni-service-account
                  key: key
            - name: PROVIDER_ID
              value: "truenas"
            - name: NODE_ROTATION_CLUSTER
              value: "talos-home"
```

`PROVIDER_ID` must match the provisioner's `PROVIDER_ID` — the
reconciler filters MachineRequests by `LabelInfraProviderID` so it
never touches another provider's requests.

### Environment variables

| Variable | Default | Notes |
|---|---|---|
| `OMNI_ENDPOINT` | required | Same as the provisioner |
| `OMNI_SERVICE_ACCOUNT_KEY` | required | Needs Admin scope to write MachineSet + delete MachineRequest |
| `PROVIDER_ID` | `truenas` | Must match provisioner. Required when `OMNI_ENDPOINT` is not localhost (SaaS Omni) to prevent cross-tenant lease collisions |
| `NODE_ROTATION_CLUSTER` | required | Cluster name to scope rotation to |
| `NODE_ROTATION_REFRESH_INTERVAL` | `30s` | Poll cadence |
| `NODE_ROTATION_LOCK_TTL` | `5m` | Step-lock TTL. Operator values below 30s are clamped to the floor (a sub-30s TTL defeats the autoscaler-pause coupling) |
| `NODE_ROTATION_HEALTH_LISTEN_ADDR` | `:8082` | HTTP /healthz + /readyz endpoint for k8s probes. Reports unhealthy if no tick has completed in 3× the refresh interval |
| `NODE_ROTATION_TRUST_DECLARED_ROLE` | `false` | When `true`, honor the operator-declared role even when it disagrees with `LabelControlPlaneRole`. Use only for hand-crafted clusters where the Omni labels are wrong |
| `NODE_ROTATION_SINGLETON_ENABLED` | `true` | Per-cluster singleton lease |
| `NODE_ROTATION_SINGLETON_FORCE_DISABLE` | `false` | Required additional opt-out: setting `NODE_ROTATION_SINGLETON_ENABLED=false` alone refuses to start. Set both consciously when you know two reconcilers will race on destructive writes |
| `NODE_ROTATION_SINGLETON_REFRESH_INTERVAL` | `15s` | Lease heartbeat |
| `NODE_ROTATION_SINGLETON_STALE_AFTER` | `45s` | Stale-takeover window |

`replicas: 1` is required. The singleton lease enforces this at
runtime; running multiple replicas would only waste API calls (the
lease loser sits idle), but the Deployment manifest should still pin
the replica count.

## Observability

OTel metrics emitted under `truenas.node_rotation.*`:

| Metric | Type | Labels | Description |
|---|---|---|---|
| `truenas.node_rotation.candidates` | histogram | — | Rotation candidates per tick |
| `truenas.node_rotation.decisions` | counter | `action`, `strategy`, `role` | Plan decisions by action |
| `truenas.node_rotation.progress` | counter | `strategy`, `role` | Successful rotation steps (teardown-stale, surge-down) |
| `truenas.node_rotation.errors` | counter | `action`, `role`, `strategy`, `abort_kind` | Step execution errors. `abort_kind` distinguishes operator-edit drift causes when `action=surge-aborted` |
| `truenas.node_rotation.role_mismatch_refused` | counter | `machineset`, `role` | Candidates refused because the MachineSet's CP label disagreed with the declared role |
| `truenas.node_rotation.tick.duration` | histogram | — | Wall-clock duration of one Discover→Plan→Execute pass |
| `truenas.node_rotation.surge.cycle.duration` | histogram | `strategy`, `role` | End-to-end duration of one completed surge cycle |
| `truenas.autoscaler.paused.for.rotation` | counter | `machineset`, `cluster` | Autoscaler observations clamped because the rotation lock was held |

Every log line within one tick shares a `tick_id` field so on-call can
filter `tick_id=<X>` to see the full Discover/Plan/Execute fan-out for
one reconciler pass. Generation-changing decisions carry the prior
hash as `previous_generation` for easy `git log -S <hash>` lookups.

The reconciler also logs one structured entry per candidate per
tick. Decisions at `info` level for actions that mutate state,
`debug` for steady-state / waiting, `warn` for refusals and
`min-healthy` floors.

## Failure modes

- **Reconciler crashes mid-step (in-place)**: the lock annotation
  has a 5-minute TTL. The autoscaler resumes scaling once TTL
  elapses. The next reconciler tick replans from current state.
- **Reconciler crashes mid-cycle (surge)**: the `surge-phase`
  annotation survives the crash. When a new reconciler starts up
  it reads the phase + original-count, validates against live
  MachineCount, and resumes from where the previous one left off
  (or emits `SurgeAborted` if state has drifted).
- **Operator hand-edits MachineCount during a surge cycle**: the
  engine detects the drift on its next tick and aborts the cycle
  cleanly. Operator intent wins; the cycle replans on the
  following tick.
- **Hash drift between two parties**: pinned by the `TestGenerationStability`
  test in `internal/noderotation/generation_test.go`. Both sides
  compute the same canonical SHA-256 over the same JSON envelope.
- **MachineRequest delete denied**: the engine logs the error,
  leaves the lock in place, and retries on the next tick. The TTL
  releases the lock if the failure is persistent so the autoscaler
  resumes scaling.
- **Operator hand-edits class-generation annotation**: harmless and
  self-correcting. The reconciler re-stamps the canonical value on
  the next tick and proceeds.

## See also

- [docs/autoscaler.md](autoscaler.md) — the experimental autoscaler subcommand this feature coordinates with.
- [docs/sizing.md](sizing.md) — when to rotate vs in-place resize a single CP.
- [docs/backlog.md](backlog.md) — surge + CP rotation follow-ups.
