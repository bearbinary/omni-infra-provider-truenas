# PROPOSED: Drain-aware Node Rotation

**Status:** PROPOSED — boundaries under discussion. No implementation. Do
not promote to `docs/` until Round 1, 2, and 3 decisions below are
recorded.

**Owner:** Zac Clifton

**Created:** 2026-06-03

**Supersedes:** none

**Depends on:** `internal/noderotation` (shipped EXPERIMENTAL in the
v0.17.0 line)

**Related:**
- `docs/node-rotation.md` — current rotation contract
- `docs/storage.md` — supported storage list (Longhorn + NFS only as of
  2026-06-03)

---

## Problem

The node-rotation reconciler talks only to Omni COSI state. It tears
down a MachineRequest and lets the MachineRequestSet controller spawn a
replacement, but does NOT cordon the corresponding Kubernetes node,
drain workloads, or wait for Longhorn replicas to rebuild elsewhere.

Failure modes this creates today on a real cluster:

- **Longhorn replica loss under rotation pressure.** A 3-replica volume
  with one replica on the doomed node loses redundancy the instant the
  VM disappears. Three rotations back-to-back, faster than Longhorn's
  per-volume rebuild completes, can take all three replicas offline →
  data loss.
- **Workload-gap longer than necessary.** Pods on the doomed node are
  not pre-evicted, so they crash-restart on the new node instead of
  draining cleanly to a peer first. PDBs are bypassed.
- **No graceful Talos shutdown.** kubelet on the doomed node stops
  heartbeating because the VM is gone, not because a graceful drain
  ran. StatefulSet workloads with quorum see an abrupt loss.

`MinHealthy` in the engine today only counts MachineRequest stage
(`PROVISIONED`). It is blind to k8s node readiness, PDB compliance, and
Longhorn replica health.

## Proposed shape (Option A, recommended)

In-binary drain. New annotation `node-rotation.omni/drain-mode` per
MachineClass:

| Mode | When to use | Pre-teardown work |
|---|---|---|
| `none` | Stateless workers, no shared storage worries | (current behavior — no drain) |
| `cordon-drain` | NFS-backed PVCs, stateless workloads needing graceful eviction | `kubectl cordon` + `kubectl drain` semantics, respect PDBs |
| `cordon-drain-longhorn` | Longhorn-backed PVCs | cordon + drain + wait for Longhorn replicas to rebuild on other nodes |

Default = `none` for back-compat. Operators opt in per class.

New package `internal/noderotation/drain/` with a `Drainer` interface
and two implementations. Reconciler instantiates a k8s clientset
(in-cluster config or `KUBECONFIG`). New action `ActionDrainNode`
inserted before `ActionTeardownStale` / `ActionSurgeDown`. New metric
`truenas.node_rotation.drain.duration` (histogram, labels `mode`,
`outcome`).

## Why not Option B (external drain hook)

Considered: reconciler emits `node-rotation.omni/drain-pending`
annotation on the Talos `Machine` resource, waits for an external
operator (or a Longhorn-aware sidecar) to clear it before proceeding.

Rejected because it shifts the same code into operator land. Operators
would write the same cordon-drain-wait logic with less testing and no
shared maintenance. Boundary is wrong — the rotation engine already
makes destructive decisions, drain belongs in the same scope.

## Why not "third-party CSI matrix"

We dropped democratic-csi on 2026-06-03. The supported set is now
Longhorn + NFS. Two drain modes cover both. Anything else (Rook-Ceph,
Portworx, …) the operator runs with `drain-mode: none` and a docs
warning. No third drain mode in v1.

---

## Boundaries to pin

15 open decisions, grouped into three rounds. Each decision needs a
recorded answer before code lands.

### Round 1 — Ownership boundaries

These define what the reconciler considers in-scope. Everything else
cascades from here.

#### D1. Pod eviction policy

**Question:** Does drain respect PodDisruptionBudgets? Skip system
namespaces? Force DaemonSet-managed pods or skip them?

**Lean:** Use stock `kubectl drain` semantics:
- Respect PDBs (refuse to evict if PDB would be violated)
- Skip DaemonSet-managed pods (they boot on the replacement automatically)
- Evict everything else including system namespaces unless explicit skip-list

**Open:** Should there be an annotation-level allow-list of skip
namespaces? `node-rotation.omni/drain-skip-namespaces: kube-system,longhorn-system`?

**Decision:** _TBD_

---

#### D2. Longhorn health signal

**Question:** What does "safe to proceed" mean for Longhorn?

**Lean:** For every Longhorn `Volume` that has a replica on the doomed
node:
- `volume.status.robustness == healthy`
- Other-node healthy replica count `>= volume.spec.numberOfReplicas - 1`
  (we tolerate losing the doomed replica; rebuild happens after teardown)
- No active snapshot or backup operation on the volume

**Open:** Strict "wait for full replica count restored to N elsewhere
before teardown" vs lenient "tolerate N-1 on other nodes." Strict is
safer; lenient is faster.

**Decision:** _TBD_

---

#### D3. Scope of "volumes on this node"

**Question:** When draining a node, which Longhorn volumes do we care
about — only PVCs bound to pods currently scheduled on it, or all
volumes that have a replica on the node's local disk?

**Lean:** All volumes with a replica on the node. Longhorn replicas do
NOT follow pods — a pod can move while its replicas stay put. The
node-local data is what matters for redundancy.

**Decision:** _TBD_

---

#### D11. Annotation scope

**Question:** Is `drain-mode` declared per MachineClass (current
rotation annotation pattern) or per MachineSet?

**Lean:** Per MachineClass for v1. Simpler. Operators who need
per-workload drain modes can run two MachineClasses backed by
different node pools.

**Decision:** _TBD_

---

#### D12. Interaction with `min-healthy`

**Question:** Does a node that's been cordoned but not yet destroyed
count toward `min-healthy`?

**Lean:** NO. Once cordon lands, the node is committed to teardown.
Counting it as healthy hides the actual redundancy state from the
engine. `min-healthy` should reflect "actually serving workload."

**Decision:** _TBD_

---

### Round 2 — Failure modes & escape valves

What happens when reality breaks. Drives escape-valve design.

#### D4. Drain hang policy

**Question:** Drain stops making progress (stuck finalizer, ungraceful
pod, PDB blocks last replica). What does the engine do?

**Options:**
- **(a) Abort rotation.** Leave node cordoned, emit `ActionDrainStuck`,
  metric fires, operator paged via alert. Operator decides next step.
- **(b) Force-evict.** Pass `--force --grace-period=0` style. Risky.
- **(c) Proceed anyway.** Teardown despite drain incomplete. Defeats the point.

**Lean:** (a) by default. Annotation
`node-rotation.omni/drain-on-stuck: force | skip | abort` lets
operators override per class.

**Decision:** _TBD_

---

#### D5. Longhorn rebuild stuck

**Question:** Longhorn cannot reach `numberOfReplicas - 1` healthy
elsewhere (other nodes out of disk, all replicas degraded). Same three
options as D4.

**Lean:** (a) abort. Operator must add capacity or change min-healthy
expectations. Don't silently proceed.

**Decision:** _TBD_

---

#### D6. Operator kill-switch

**Question:** How does someone yank a stuck rotation cycle?

**Options:**
- **Class-wide halt:** `node-rotation.omni/halt: true` on the
  MachineClass pauses all candidates from that class. Engine emits
  `ActionHalted` while annotation present.
- **Per-MachineSet halt:** Same key on the MachineSet only.
- **Lease drop:** Operator deletes the singleton lease annotation,
  reconciler crashes, k8s restarts it, lease re-acquires. Heavy hammer.
- **Manual annotation surgery:** `omnictl annotate ms/X -d
  node-rotation.omni/surge-phase` clears the cycle.

**Lean:** Class-wide halt + per-MachineSet halt, both honored. The
manual annotation surgery already works today (engine re-plans on next
tick).

**Decision:** _TBD_

---

#### D7. Concurrent multi-node drain

**Question:** Two MachineSets in the same cluster each have a stale
member. Can both drain concurrently, or must one finish before the
other starts?

**Lean:** Serial per cluster. Longhorn rebuild bandwidth is shared
across the cluster; two concurrent drains pulling replicas across the
network compound the redundancy window. The autoscaler-pause coupling
already gives us a natural serialization point.

**Open:** Worth a class-level annotation
`node-rotation.omni/parallel: 1 | unlimited` for operators who want
faster rotation on stateless pools?

**Decision:** _TBD_

---

#### D8. Lock TTL vs drain duration

**Question:** A fat node draining a database StatefulSet can take >5
minutes. The existing rotation lock TTL is 5m. Does the drain step
refresh the lock?

**Lean:** YES. Same pattern as the existing surge wait-up/wait-down
refresh. Extend the in-progress refresh to cover the drain phase.
Trivial.

**Decision:** _TBD_

---

### Round 3 — Meta (RBAC, processes, docs)

#### D9. Minimum RBAC

**Question:** What kubeconfig privilege does the reconciler need?

**Lean:**
- Nodes: get, list, patch (for cordon)
- Pods: list, delete (for drain)
- Pods/eviction: create
- Leases (coordination.k8s.io): get, list, watch (PDB check side)
- PodDisruptionBudgets (policy): get, list
- volumes.longhorn.io / replicas.longhorn.io: get, list (Longhorn mode only)

ClusterRole scope. Pin in `templates/rbac-noderotation.yaml`.

**Open:** Should we ship two ClusterRoles (one Longhorn-aware, one
not) so non-Longhorn operators don't grant Longhorn read?

**Decision:** _TBD_

---

#### D10. kubeconfig source

**Question:** In-cluster only, or allow `KUBECONFIG` for dev?

**Lean:** Both. In-cluster preferred via downward API + ServiceAccount;
`KUBECONFIG` env honored when set for local dev / one-off runs. Same
pattern Omni's other tooling uses.

**Decision:** _TBD_

---

#### D13. Promotion gate

**Question:** What's the soak + test matrix that promotes drain-aware
rotation from EXPERIMENTAL to stable?

**Lean (minimum):**
- 1× Longhorn cluster rotation under real workload (existing
  `talos-default` cluster qualifies)
- 1× NFS-only cluster rotation
- 1× operator kill-switch test (halt annotation mid-cycle)
- 1× drain-hang test (synthetic PDB-blocking pod) with operator override
- 1× Longhorn rebuild-stuck test (synthetic out-of-disk scenario)

Lift the EXPERIMENTAL banner after all five plus a 14-day soak in
homelab with no incident.

**Decision:** _TBD_

---

#### D14. Documentation contract

**Question:** Where do the operator-facing docs live?

**Lean:**
- `docs/node-rotation.md` gets a "Drain modes" section pointing at the
  next item.
- New `docs/node-rotation-drain.md` carries the full reference: mode
  picker decision tree, RBAC manifest, runbook for "drain stuck" and
  "Longhorn rebuild stuck," kill-switch procedure.
- This proposed doc (`docs/proposed/node-rotation-drain.md`) gets
  archived under `docs/proposed/_decided/` once all 15 decisions are
  recorded.

**Decision:** _TBD_

---

#### D15. Annotation schema versioning

**Question:** If `drain-mode` grammar changes pre-stable (e.g., we add a
new mode, or rename `cordon-drain` → `evict-drain`), what's the
operator migration story?

**Lean:** Pre-stable = EXPERIMENTAL banner says schema may break. No
migration tooling. Each new mode is additive; renames get a one-line
note in CHANGELOG under `### Breaking`. Once stable, schema bumps
follow semver and breaking renames are forbidden.

**Decision:** _TBD_

---

## Non-goals

- **Multi-CSI drain matrix.** Two modes only: `cordon-drain` and
  `cordon-drain-longhorn`. Anything else uses `drain-mode: none`.
- **Pod-level retry of stuck evictions.** If `kubectl drain`-style
  eviction can't make progress, we abort the rotation and let the
  operator intervene. We do not invent our own eviction policy.
- **Cluster-wide drain coordination across multiple node-rotation
  reconcilers.** The singleton lease already enforces one reconciler
  per cluster. Multi-cluster fleets get one reconciler each; no global
  scheduler.
- **Replacing Cluster Autoscaler's drain behavior.** This is rotation,
  not scale-down. CAS-driven scale-downs go through CAS's own drain
  machinery and the autoscaler-pause-on-rotation coupling keeps the
  two engines out of each other's way.
- **Backup-before-teardown.** Not in scope. If you want a snapshot
  before rotation, take it via Velero / Longhorn UI before bumping the
  MachineClass spec. The reconciler does not pause for backup.

## Out-of-scope items captured for future docs

- Cross-cluster rotation coordination (multi-cluster fleets sharing
  one Omni). Maybe relevant when someone runs 10+ clusters; not now.
- Rolling upgrade of Talos version via this engine. Talos upgrades
  flow through Omni's existing cluster-upgrade path. Out of scope per
  `docs/node-rotation.md`.

## Decision log

Once a decision is pinned, move it from the "Decision: _TBD_" line
above into this log with a date and one-line rationale.

| ID | Date | Decision | Rationale |
|---|---|---|---|
| _none yet_ | | | |

---

## Implementation checklist (DO NOT START until all decisions recorded)

When the 15 decisions above are pinned, the implementation breaks
down as follows. Listed here for scoping, not as a green light to
code.

- [ ] New package `internal/noderotation/drain/` with `Drainer`
      interface, `cordonDrainStrategy`, `cordonDrainLonghornStrategy`
- [ ] k8s clientset construction in `cmd/.../noderotation.go`
      (in-cluster + KUBECONFIG fallback)
- [ ] New annotation parser for `node-rotation.omni/drain-mode` and
      related per-D-decision keys
- [ ] New `ActionDrainNode` + insertion into the plan switch before
      `ActionTeardownStale` / `ActionSurgeDown`
- [ ] Lock-refresh during drain phase (D8)
- [ ] New metric `truenas.node_rotation.drain.duration` (histogram,
      labels `mode`, `outcome`)
- [ ] Halt annotation honored (D6)
- [ ] Drain-hang escape-valve (D4)
- [ ] Longhorn rebuild-stuck escape-valve (D5)
- [ ] Helm chart additions: ServiceAccount + ClusterRole + binding
      gated by `drain.enabled=true`
- [ ] Unit tests for both Drainer implementations (k8s fake + Longhorn
      CRD fake)
- [ ] Integration tests against a real cluster (1× Longhorn, 1× NFS)
- [ ] `docs/node-rotation-drain.md` (new) + section in
      `docs/node-rotation.md`
- [ ] CHANGELOG entry under Unreleased

Rough sizing: ~700 lines production code + ~400 lines tests + ~200
lines docs + ~100 lines Helm chart YAML. One PR, lands behind the
existing EXPERIMENTAL banner.
