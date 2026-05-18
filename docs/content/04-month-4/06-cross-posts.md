# M4 Cross-posts — Reddit + Lemmy

Milestone framing per `_shared/reddit-lemmy-post-patterns.md`.

---

## Piece 1: Upgrade playbook

Live URL: `https://dev.to/cliftonz/<upgrade-post-slug>`

### Reddit

#### r/kubernetes

**Title**:
After N cluster upgrades, here's the pre-flight ritual I always run — saved my weekend more than once

**Body**:

The "boring before the upgrade" part of any cluster bump matters more than the upgrade itself. The 10-minute checklist I run every time:

1. **Read the release notes**. Both Talos and Kubernetes. Out loud if it helps. Breaking changes hide here.
2. **Verify cluster health** — `kubectl get nodes`, `kubectl get pods -A | grep -v Running | grep -v Completed`. Pre-existing problems get worse during the roll.
3. **Audit single-replica workloads** — `kubectl get deployments -A` filtered by `replicas:1`. Anything single-replica has brief downtime when its node reboots. Fine if you accept it. Surprising if you didn't.
4. **etcd snapshot through Omni**. The only snapshot that matters if Kubernetes goes sideways.
5. **ZFS snapshot of VM zvols** (TrueNAS-specific). Cheap. Almost never used. Saves the weekend the one time you need it.
6. **Check etcd disk timing**. `talosctl logs etcd | grep -iE "took too long|slow"`. If you see slow-fsync warnings in the last week, *don't upgrade today*. Fix the storage first or apply the HDD timeout patch.

**The most important rule of a rolling upgrade**: when it's running, don't help. Manual interventions confuse the orchestrator. Wait for it.

Full playbook with the post-upgrade verification + "when Omni stalls" diagnostic flow: <link>

Repo: https://github.com/bearbinary/omni-infra-provider-truenas

#### r/devops

**Title**:
"When a rolling upgrade is running, don't help" — the principle that saves me every cluster bump

**Body**:

Upgrade days. Half the cluster has rolled. You're watching the dashboard. You see a node taking longer than the last one. The instinct is to do *something* — drain manually, force a reschedule, restart a pod.

Don't.

A rolling upgrade is an orchestrated state machine. Every manual intervention is a state-change it didn't expect. Two failure modes I've seen from "helping":

1. **Manually draining a node the upgrader was about to drain**. Orchestrator sees the node already drained, gets confused, moves on. The new image never installs.
2. **Force-deleting a pod that was about to be evicted gracefully**. PVC handlers don't get to finish. Volume replicas end up weird. You spend the next hour healing.

The right move during a rolling upgrade is: wait. If you've been waiting more than 10 minutes for a single node, check etcd logs first. 90% of "stuck" upgrades on infra clusters are etcd taking forever because the disk is slow.

Principle generalizes past Kubernetes — applies to any automated rollout (software deploys, schema migrations, config rollouts). The smartest thing you can do is sit on your hands until the orchestrator explicitly asks for help.

Full upgrade playbook with my pre-flight ritual: <link>

#### r/selfhosted

**Title**:
The 10-minute pre-flight checklist I run before any homelab Kubernetes upgrade

**Body**:

Stuff that's saved my weekend more than once. None of it is clever. All of it is consistent.

1. Read the release notes twice. Both Talos and Kubernetes. Breaking changes hide.
2. Verify every node is Ready, no pods stuck.
3. Note any single-replica workloads — they'll have brief downtime during node reboots.
4. etcd snapshot through Omni. Mandatory.
5. ZFS recursive snapshot of VM zvols: `zfs snapshot -r tank/omni-vms@pre-upgrade-$(date +%Y%m%d-%H%M)`. Cheap. Almost never used. Saves the weekend the one time you need it.
6. Check etcd for slow-fsync warnings in the last week. If you see them, *don't upgrade today*.

**During the upgrade**: don't touch anything. Don't drain manually, don't restart pods, don't help. The orchestrator knows what it's doing.

**Post-upgrade**: 24 hours of normal operation before deleting the snapshots. If something subtle broke, you want the rollback path.

Full playbook + what to do when Omni stalls: <link>
Repo: https://github.com/bearbinary/omni-infra-provider-truenas

### Lemmy

#### `!selfhosted@lemmy.world`

**Title**: After N cluster upgrades, the pre-flight ritual that saves my weekend every time

**URL**: `https://dev.to/cliftonz/<upgrade-post-slug>?utm_source=lemmy&utm_medium=selfhosted&utm_campaign=upgrade-2026-08`

**Body**:
> 10-minute checklist run before every Talos + Kubernetes upgrade on my homelab. Same six steps, same order. The "boring before the upgrade" is what makes the upgrade itself boring.
>
> The load-bearing rule: when a rolling upgrade is running, don't help. Manual interventions confuse the orchestrator. Wait for it.
>
> Full playbook covers when Omni stalls (90% PDB violations or HDD etcd timing), what to do, and the post-upgrade verification + snapshot cleanup.

#### `!kubernetes@lemmy.world` + `!devops@lemmy.world`

Same body, audience-tailored intro.

---

## Piece 2: Singleton-lease pattern

Live URL: `https://dev.to/cliftonz/<singleton-lease-slug>`

### Reddit

#### r/golang

**Title**:
Built distributed leader election in 200 lines of Go because my SDK didn't ship one — sharing the pattern and the upstream bug I had to route around

**Body**:

Open-source Omni infrastructure provider, ~5kLOC Go. Omni SDK has no built-in leader election. Run two provider processes with the same provider ID and they both race on every machine request. State corruption follows.

**Constraints**: single-binary OSS project. No external infra (etcd, Redis, etc.) — every dependency would land in users' homelabs. Omni's existing COSI resource store is the only state I can rely on.

**Pattern that worked**:

1. Annotations on `infra.ProviderStatus` resource: `instance-id`, `heartbeat`, `epoch`
2. Read-then-write with COSI's optimistic concurrency for the compare-and-swap
3. 15-second heartbeat, 45-second stale-after threshold
4. Three consecutive refresh failures = loss signal, provider exits gracefully
5. Epoch counter bumps on every takeover (fencing token, fail-fast detection)

~200 lines including tests.

**Honest disclosure**: the fencing-token epoch is currently observability-only. I haven't threaded it through every state-mutating call yet. That's the bug I haven't filed against myself.

**The upstream-bug workaround**: Omni's gRPC-gateway occasionally returns `200 OK` with no `Content-Type` header on successful writes. gRPC client treats it as error. Substring-match on the error string to detect "really a success." Ugly. Correct. Documented in source so future-me doesn't try to clean it up.

Full write-up with the design tradeoffs, what I'd build differently, and the principle that generalizes (optimistic concurrency on existing state *is* leader election): <link>

Source: https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/internal/singleton/singleton.go

#### r/kubernetes

**Title**:
Leader election for an Omni infrastructure provider — building it from existing primitives because the SDK doesn't ship one

**Body**:

For anyone writing Omni infrastructure providers (or Cluster API providers, or similar) — the SDK doesn't ship leader election. The official mitigation is "make sure you only ever run one." That's a runbook entry, not a guarantee.

What I did instead: built one in ~200 lines on top of Omni's COSI resource store.

**Shape**:
- Annotations on `ProviderStatus` resource as the lease object
- COSI's optimistic-concurrency `version` field as the compare-and-swap primitive
- Heartbeat via timestamp annotation
- Epoch counter as fencing token

**Tuning that worked for my workload**:
- 15s refresh interval, 45s stale-after (3× headroom for transient network noise)
- 3 consecutive refresh errors = lease loss

**Pattern that generalizes**: when your framework doesn't ship a primitive you need, build it from the primitives you have. Optimistic concurrency on existing state is leader election if you squint right.

Full write-up: <link>
Source: https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/internal/singleton/singleton.go

#### r/programming

**Title**:
Distributed leader election in 200 lines of Go, with no external infrastructure — built on top of an existing resource store

**Body**:

Cross-posting from the Go community because the design lesson generalizes.

**Constraint**: build leader election for a single-binary OSS infra tool that already talks to a resource store with optimistic-concurrency semantics. No etcd, no Redis, no Consul — those would land in users' homelabs and add operational burden.

**Solution**: use the existing resource store as the lease backend. Annotations on an existing status resource as the lease object. The store's compare-and-swap is the leader-election primitive.

**Three things that surprised me**:
1. Failure-mode design dominated correctness work. The lease should fail closed (instance exits), not fail open (split-brain).
2. Fencing tokens matter even when you can't fully enforce them. The epoch counter is observability-only currently but it's still useful because takeovers are visible in logs.
3. Routing around an upstream gRPC bug with substring-matching on error strings was unavoidable and is documented as such in source.

Lesson: narrow, well-understood extension points (a resource store with optimistic concurrency) compose into bigger primitives (leader election) with surprisingly little code.

Full write-up: <link>

### Lemmy

#### `!golang@programming.dev`

**Title**: Distributed leader election in 200 lines of Go, on top of an existing resource store — when your SDK doesn't ship the primitive

**URL**: `https://dev.to/cliftonz/<singleton-lease-slug>?utm_source=lemmy&utm_medium=golang&utm_campaign=singleton-lease-2026-08`

**Body**:
> Pattern post. Real code, real constraints, honest about the parts I haven't finished (fencing token currently observability-only). The upstream-bug workaround is its own subplot.
>
> The design lesson: optimistic concurrency on existing state is leader election if you squint right. You don't always need to add infrastructure to add primitives.
>
> Source linked above is ~200 lines of Go. MIT, lift it if it fits your problem.

#### `!programming@programming.dev`

Same body, lighter framing on the Go-specific syntax.

---

## Piece 3: Multi-host cluster (one Talos cluster across multiple TrueNAS boxes)

Live URL: `https://dev.to/cliftonz/<multi-host-slug>`

This piece is the deepest-end M4 content. Assumes the reader has done single-host first. Cross-post audiences need to be filtered — this is for operators, not beginners.

### Reddit

#### r/selfhosted

**Title**:
After scaling my homelab Kubernetes to 3 TrueNAS hosts, here's the architecture that actually works

**Body**:

Most homelab K8s-on-TrueNAS content assumes one NAS, one cluster. Outgrew that. Sharing the multi-host setup after several months running it.

**The mental model**: one provider instance per TrueNAS host (unique `PROVIDER_ID` each), per-host MachineClasses, one cluster composed from N pools via separate MachineSets in Omni. No provider code changes — the multi-tenancy was already supported, just under-documented.

**Three patterns for where to put control planes**:

1. **1 CP on the biggest host + workers everywhere**: capacity-constrained, no HA on cluster API.
2. **3 CPs spread across 3 hosts (HA)**: real failure independence. 1 host can fail without losing etcd quorum.
3. **Dedicated CP host**: concentrates risk, usually worse than #2.

**The thing that surprised me**: Longhorn's HA story gets dramatically better in multi-host. Replicas spread across hosts, so a whole NAS reboot doesn't lose data — workloads keep responding (degraded) on workers elsewhere. Same Longhorn config as single-host.

**What doesn't work yet**:
- No live migration across hosts (TrueNAS doesn't do live migration period)
- No cross-host autoscaling
- No hot failover for the provider instance itself

For a 3-host setup running real workloads, this is the architecture that's been stable for me.

Full writeup with the per-host MachineClass setup, storage decisions across hosts, and failure-mode walkthrough: <link>

Repo: https://github.com/bearbinary/omni-infra-provider-truenas

#### r/homelab

**Title**:
I run a single Talos Kubernetes cluster across 3 TrueNAS hosts — architecture, failure modes, and what I'd skip

**Body**:

Sharing what's been working for the last several months: one Kubernetes cluster, three physical TrueNAS boxes, six worker VMs spread across them, three control planes (one per host for real HA).

**Hardware per host**: 16 cores, 64 GB RAM, NVMe pool.

**Cluster shape**:
- 3 control planes — one per host. Talos VIP floats between them.
- 6 workers — 2 per host. Each with a 100 GB data disk for Longhorn.
- Longhorn replicates 3x across hosts so a whole-host reboot doesn't lose data.

**The trick that made it click**: each TrueNAS runs its own `omni-infra-provider-truenas` instance with a unique `PROVIDER_ID`. MachineClasses bind to specific provider IDs. So `worker-rack-a` only ever creates VMs on Host A, `worker-rack-b` only on Host B. Omni sees the providers as separate pools, the cluster composes from all of them.

**Failure tolerance**:
- One host reboots → cluster degraded, not down. 2 of 3 CPs keep etcd quorum. Workloads with Longhorn replicas elsewhere keep responding.
- One host hard-fails → same story. Recovery = bring the host back.

**Where I'd reverse**: 2-host setup, not 3. With only 2 hosts you can't get real CP HA (you'd need a tiebreaker), and the operational complexity of multi-host doesn't pay off. Either stay single-host or commit to 3+.

Full architecture writeup with the per-host config, storage tradeoffs, and the patterns I considered but didn't pick: <link>

Repo: https://github.com/bearbinary/omni-infra-provider-truenas

#### r/kubernetes

**Title**:
Multi-host infrastructure-provider pattern: composing one Kubernetes cluster from N pools via per-host PROVIDER_ID

**Body**:

For anyone writing or operating Omni infrastructure providers (or Cluster API providers, or similar) — a pattern for multi-host topology that works today on the TrueNAS provider I maintain.

**Architecture**: each physical host runs its own provider instance with a unique `PROVIDER_ID`. Each MachineClass binds to a specific provider ID. A cluster composes from N MachineSets, each pointing at a different provider's MachineClass.

**Why this works without provider-side changes**:
- Singleton lease (annotation-keyed on `infra.ProviderStatus`) is per-`PROVIDER_ID`, so different IDs = independent leases = no race.
- MachineClass `spec.autoprovision.providerid` field already targets a specific provider — Omni's existing routing handles per-host placement.
- Talos cluster doesn't care which provider created which node; it's just a flat node pool from K8s's perspective.

**Three CP-placement patterns + failure tolerance**:

1. **1 CP on the biggest host** → zero CP HA, cluster API offline during that host's maintenance.
2. **3 CPs spread across 3 hosts** → 1-host fault tolerance for etcd quorum. The right answer for HA.
3. **Dedicated CP host** → concentrates risk, almost always worse than #2.

**The honest limits**:
- No cross-host live migration (TrueNAS limitation, not provider).
- No quorum-aware MachineSet operations — Omni doesn't currently know that scaling a CP MachineSet to 0 would lose quorum. You think about it yourself.
- The autoscaler is per-host, not cluster-wide. Each host autoscales independently.

Full writeup including the per-host MachineClass YAML, storage placement decisions, and inter-host etcd timing considerations: <link>

Source: https://github.com/bearbinary/omni-infra-provider-truenas

#### r/truenas

**Title**:
If you have 2+ TrueNAS boxes and want one Kubernetes cluster spanning them — here's the architecture

**Body**:

Direct multi-host setup question I get from r/truenas a lot. The answer: yes, today, with no provider code changes. Each TrueNAS host runs its own provider instance with a unique `PROVIDER_ID`.

**TrueNAS-specific considerations for multi-host**:

1. **One dedicated API key per host**. Each TrueNAS needs its own `omni-provider` user (in `builtin_administrators`) and its own API key. The provider container on each host uses that host's key.

2. **Same Omni service account key across all hosts**. The hosts share one Omni service account — they're distinguished by `PROVIDER_ID`, not by credentials.

3. **Bridge networking on each host**. Same setup as single-host. All hosts ideally on the same Layer 2 subnet so cluster nodes can reach each other directly.

4. **Per-host pool name**. `DEFAULT_POOL=tank` on Host A, whatever the local pool name is on Host B (could be different — pool names are local to each TrueNAS).

5. **ZFS recordsize tuning per host**. If you're hosting etcd disks, set `recordsize=16K` on each host's VM zvol dataset.

**Where the multi-host story shines for TrueNAS specifically**:
- Longhorn replication across hosts gives you real PVC HA — workloads survive a whole-NAS reboot if replicas exist on other hosts' worker data disks.
- You can dedicate a "file-server" TrueNAS (NFS serving media, Velero backup target) and a "compute" TrueNAS (workers + heavy I/O) — workloads stay on the right host for their access pattern.

**Where it's not worth it**:
- 2 hosts where neither has spare capacity individually. You don't get HA from 2 hosts (need quorum tiebreaker). Stay single-host or commit to 3.

Full writeup: <link>
Repo: https://github.com/bearbinary/omni-infra-provider-truenas

### Lemmy

#### `!selfhosted@lemmy.world`

**Title**: After scaling my homelab K8s from 1 to 3 TrueNAS hosts, here's the architecture that actually worked

**URL**: `https://dev.to/cliftonz/<multi-host-slug>?utm_source=lemmy&utm_medium=selfhosted&utm_campaign=multi-host-2026-08`

**Body**:
> Multi-host Talos cluster across 3 TrueNAS boxes. Real HA control plane (one CP per host), Longhorn replicas spread across hosts, single cluster spans the lot.
>
> The trick: one provider instance per host with a unique `PROVIDER_ID`. Per-host MachineClasses. Multiple MachineSets in Omni. No provider code changes needed — multi-tenancy was already supported, just under-documented.
>
> Honest about the limits — no live migration across hosts, no cross-host autoscaling, no quorum-aware MachineSet ops. None of those are blockers, they just mean multi-host operations require more manual care.
>
> Full architecture writeup linked above. The provider that makes this work is MIT-licensed and runs as a container on each TrueNAS.

Cross-post to `!homelab@lemmy.ml`, `!kubernetes@lemmy.world` after 24h.

#### `!homelab@lemmy.ml`

**Title**: 3-host Talos Kubernetes setup on TrueNAS — architecture, hardware, failure modes

**URL**: same as above with `utm_medium=homelab`

**Body**:
> Hardware: 3 × 16-core / 64 GB NVMe TrueNAS boxes. Cluster: 3 control planes (one per host), 6 workers (two per host), Longhorn 3-way replication.
>
> Same Layer 2 subnet across all hosts. Talos VIP for the API endpoint — floats across CPs, kubeconfig points at the VIP not at individual CP IPs.
>
> What I'd skip: 2-host setups. You don't get real HA from 2 hosts. Stay single-host or commit to 3+.
>
> Full writeup above. Provider is MIT, container per host, unique PROVIDER_ID each.

#### `!kubernetes@lemmy.world`

**Title**: Multi-host infrastructure-provider pattern — composing one cluster from N pools via per-host PROVIDER_ID

**URL**: same as above with `utm_medium=kubernetes`

**Body**: Same content as r/kubernetes above, technical framing on the per-host provider isolation + MachineClass routing.

---

### X / Twitter thread (multi-host)

10 tweets. Lead with the architecture, end with the post link.

**T1 (hook)**:

> My homelab Kubernetes cluster runs across 3 TrueNAS boxes.
>
> One cluster. Three physical NAS hosts. Real HA control plane. Longhorn replicas spread across hosts.
>
> Sharing the architecture and the limits. 🧵

**T2**:

> The mental model: one provider instance per TrueNAS host. Unique `PROVIDER_ID` each. Per-host MachineClasses bind to specific provider IDs.
>
> Omni sees them as separate infrastructure pools. The cluster composes from all of them via multiple MachineSets.

**T3**:

> Why each host gets its own provider:
>
> Singleton lease (in Omni state) is keyed by `PROVIDER_ID`.
>
> Different IDs = independent leases = no race between instances.
>
> Multi-tenancy was already supported. Just under-documented.

**T4**:

> Three CP-placement patterns:
>
> 1. One CP on the biggest host — zero HA, simplest.
> 2. Three CPs spread across 3 hosts — real fault tolerance.
> 3. Dedicated CP host — concentrates risk.
>
> #2 is the right answer once you have 3 hosts.

**T5**:

> The CP HA math:
>
> 3 CPs across 3 hosts = etcd quorum survives 1 host failure.
>
> One NAS reboots for maintenance? Cluster API stays up via the other 2 CPs.
>
> Use Talos VIP so kubeconfig doesn't point at a specific CP IP.

**T6**:

> Storage HA gets free in multi-host with Longhorn.
>
> Replicas spread across worker data disks on different hosts. A whole-host reboot doesn't lose data.
>
> Same Longhorn config as single-host. The HA story just gets dramatically better.

**T7**:

> Network shape:
>
> Same Layer 2 subnet across all hosts. Cluster nodes can reach each other directly.
>
> MetalLB works as in single-host (one /24, one MetalLB range).
>
> Talos VIP for the API endpoint. Floats across CPs.

**T8**:

> Honest limits:
>
> - No cross-host live migration (TrueNAS doesn't do it)
> - No cross-host autoscaling
> - No hot failover for the provider instance
> - No quorum-aware MachineSet operations
>
> None block multi-host. They just need more manual care.

**T9**:

> When NOT to go multi-host:
>
> - One NAS has the capacity. Single-host operational simplicity is real.
> - You only have 2 hosts. Can't get real HA from 2.
> - Hosts are on different subnets. etcd hates that.
>
> Stay single-host or commit to 3+.

**T10 (CTA)**:

> Full writeup — per-host MachineClass YAML, storage placement decisions, failure-mode walkthrough, what doesn't work yet:
>
> [dev.to URL]
>
> Repo (MIT, run one instance per TrueNAS host):
>
> https://github.com/bearbinary/omni-infra-provider-truenas

---

### LinkedIn post (multi-host)

Native long post. ~1300 chars in body, link in first comment.

**Hook**:

> My homelab Kubernetes cluster runs across three physical TrueNAS boxes. One cluster. Three hosts. Real HA on the control plane.

**Body**:

> The architecture in one sentence: each TrueNAS host runs its own infrastructure-provider instance with a unique `PROVIDER_ID`, per-host MachineClasses bind to specific provider IDs, and a single cluster composes from N pools via separate MachineSets in Omni.
>
> No provider code changes needed. The multi-tenancy was already supported in the open-source provider I maintain — it was just under-documented.
>
> Three things I learned scaling from one host to three:
>
> 1. The HA math changes meaningfully at 3 hosts. With one CP per host, etcd quorum survives a single-host failure. A whole NAS can reboot for maintenance without taking the cluster API offline. Two hosts won't give you this — you need quorum tiebreakers.
>
> 2. Longhorn's storage HA story gets dramatically better in multi-host. Replicas spread across worker data disks on different physical hosts. A whole-host reboot doesn't lose data because the workload's storage lives elsewhere. Same Longhorn config as single-host — the topology gain is automatic.
>
> 3. The honest limits matter. No cross-host live migration. No cross-host autoscaling. No quorum-aware MachineSet operations. None of these block multi-host from working — they just require more manual care than single-host.
>
> The right order of investment: one host done well first. Add the second host when you've genuinely outgrown the first. Add the third when you specifically want HA you'll exercise.
>
> Full architecture writeup with the per-host config, storage tradeoffs, and failure-mode walkthrough in the comments.

**First comment**:

> Full writeup: [dev.to URL]
>
> Provider repo (open source, MIT): https://github.com/bearbinary/omni-infra-provider-truenas

**Hashtags**: `#Kubernetes #TrueNAS #Homelab #Talos #SelfHosted`

---

## Cadence

Pieces stagger 2 weeks apart. Singleton-lease has highest external-share potential (Go Weekly, HN /r/programming) — give it the freshest content slot in the month.

Multi-host is the deepest-end piece. Ship it last in M4 — assumes the reader has done single-host, has read at least the install guide and probably the sizing post. Posting too early in the campaign means most readers haven't reached the "I've outgrown one host" point yet.
