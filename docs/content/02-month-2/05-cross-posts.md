# M2 Cross-posts — Reddit + Lemmy

**Framing rule (locked)**: every post is milestone-framed. See `_shared/reddit-lemmy-post-patterns.md` for the full playbook. Title leads with lesson/experience/decision. Body delivers value before any link. Repo and post URLs land at the end.

**Posting cadence per piece**: dev.to first → 24h later LinkedIn → 24h later X thread → Reddit (stagger by sub, 2–3 days apart) → Lemmy (cross-post pattern, see `_shared/lemmy-communities.md`).

Skip subs/communities where a piece isn't a strong fit. Better fewer high-quality drops than spam.

---

## Piece 1: Talos vs k3s comparison

Live URL: `https://dev.to/cliftonz/<talos-vs-k3s-slug>`

### Reddit

#### r/selfhosted

**Title**:
I ran k3s and Talos in parallel on my TrueNAS for 6 months. Here's the honest comparison.

**Body**:

When TrueNAS shipped 25.04 without K3s, I tried both paths back: manual k3s in a TrueNAS VM, and Talos + Sidero Omni. Ran them side by side for 6 months specifically to compare them. Here's where I land.

**Talos wins on day-2 ops.** Omni handles upgrades through a UI. No SSH-ing into nodes at 11pm. Replace-don't-fix changes how you operate clusters — configuration drift stops being a phrase you use.

**k3s wins on debuggability.** You can SSH in. You can cat config files. You can strace processes. Every Linux tool still works. The first week of Talos feels alien because none of that is available. By month three you're glad it isn't.

**Where they're identical**: resource footprint, cost, and outcome (a working Kubernetes cluster).

**Where Talos genuinely wins**: multi-node, upgrades, immutability.

**Where k3s genuinely wins**: single-node, first-week familiarity, the "I want full shell access for educational reasons" use case.

For most people: Talos + Omni is the upgrade, not the alternative. The mistake is picking k3s because it feels familiar without acknowledging you're trading a week of learning for a year of day-2 ops.

Wrote the full comparison: <link>
The Omni provider that makes the Talos path work: https://github.com/bearbinary/omni-infra-provider-truenas

#### r/homelab

**Title**:
After 6 months running both, here's what Talos + Omni gets right vs k3s on TrueNAS — and where k3s genuinely wins

**Body**:

Kept both setups alive specifically to write a fair comparison. Real homelab, real workloads.

**Talos + Omni wins**:
- Multi-node scale-out is a replica counter. k3s requires manual joins.
- Upgrades are a button. k3s upgrades are re-running the install script per node and crossing your fingers.
- Nodes are reproducible artifacts. You delete a misbehaving one, two minutes later there's a fresh one.

**k3s wins**:
- Full Linux shell access. Educational, debugging-friendly, familiar.
- Single-node simplicity. Talos + Omni overhead isn't worth it for 1 node.
- The first week of Talos has a real learning curve — `talosctl` instead of SSH, no shell, no package manager. You'll get over it. Week one is real.

**Where I'd pick which**:
- 1 node, doesn't matter how it operates → k3s
- ≥ 2 nodes, you'll run this for years → Talos + Omni
- Want to learn cluster ops on a familiar OS → k3s
- Have ambitions beyond a single static cluster → Talos + Omni

Full writeup with every tradeoff axis: <link>
Repo (provider): https://github.com/bearbinary/omni-infra-provider-truenas

#### r/kubernetes

**Title**:
Talos vs k3s on TrueNAS — honest comparison from 6 months running both

**Body**:

Comparison post focused on the operational and architectural differences, not the "which is better" trap. Both work. They optimize for different things.

**Operational differences that matter**:

1. **Upgrades**. Talos + Omni does rolling upgrades through a UI with health checks between nodes. k3s in a VM is per-node `sh install.sh` and prayer.
2. **Configuration model**. Talos config is declarative and immutable — apply once, replace nodes for updates. k3s is `/etc/rancher/k3s/` + flags + whatever drift accumulates.
3. **Multi-node**. Talos + Omni MachineSets scale by replica count. k3s multi-node requires manual joining (etcd or external datastore decisions, cert SANs, etc).
4. **etcd backups**. Omni handles them. k3s embedded etcd needs manual snapshot configuration.
5. **Debuggability**. k3s wins here. SSH works. Every standard Linux tool works. Talos forces you onto `talosctl`, which is genuinely good but has a learning curve.

**The honest answer**: for ≥ 2-node clusters that need to operate for more than a year, the day-2 ops gap is large. For single-node clusters, the overhead of Talos + Omni isn't earned.

Full comparison: <link>
The provider that makes the TrueNAS Talos path work: https://github.com/bearbinary/omni-infra-provider-truenas

#### r/truenas

**Title**:
For anyone who lost K3s in 25.04 and is choosing what to rebuild with — k3s-in-VM vs Talos + Omni, honest comparison after 6 months

**Body**:

Two real options on TrueNAS in 2026 (the built-in catalog Kubernetes is gone, the new Docker apps aren't Kubernetes):

1. Manual k3s in a TrueNAS VM (DIY path)
2. Talos + Sidero Omni via the [open-source provider I maintain](https://github.com/bearbinary/omni-infra-provider-truenas)

I ran both for 6 months. Honest comparison from a TrueNAS-user perspective:

**Both paths**:
- Run as VMs on TrueNAS — same hardware, same ZFS-backed disks.
- Use the bridge interface for networking.
- Need an NVMe SLOG (or etcd timeout patch) on HDD pools to behave.

**Talos + Omni wins on**: upgrades, multi-node, immutability, fail-loud at config boundaries.

**k3s wins on**: single-node simplicity, SSH-based debugging, no new mental model.

**TrueNAS-specific consideration**: the Talos path needs a dedicated API key user with `builtin_administrators` membership (TrueNAS requires `SYS_ADMIN` for the `/_upload` endpoint, only granted via that group). k3s-in-VM doesn't need any TrueNAS API access at all.

If you'll run more than one node and operate this for a year+ — Talos + Omni. If single-node and you already know Linux — k3s is fine.

Full writeup: <link>
Provider repo: https://github.com/bearbinary/omni-infra-provider-truenas

### Lemmy

#### `!selfhosted@lemmy.world`

**Title**: I ran k3s and Talos on my TrueNAS for 6 months to figure out which one to recommend. Here's the honest take.

**URL**: `https://dev.to/cliftonz/<talos-vs-k3s-slug>?utm_source=lemmy&utm_medium=selfhosted&utm_campaign=talos-vs-k3s-2026-06`

**Body**:
> Kept both setups alive specifically to write a fair comparison. Same hardware, same TrueNAS, same workloads where possible.
>
> Where Talos + Omni wins: day-2 ops, upgrades, multi-node, configuration immutability. Where k3s wins: full shell access for debugging, no new mental model to learn, single-node simplicity.
>
> The honest answer for most homelab users on TrueNAS: Talos + Omni is the upgrade, not the alternative. But "upgrade" comes with a real week-one learning curve. Worth naming that up front.
>
> Full writeup linked above. The Omni provider that makes the Talos path work on TrueNAS is MIT-licensed, issues-only contribution model.

Cross-post to `!homelab@lemmy.ml`, `!kubernetes@lemmy.world` after 24h.

---

## Piece 2: TrueNAS vs Proxmox comparison

Live URL: `https://dev.to/cliftonz/<truenas-vs-proxmox-slug>`

### Reddit

#### r/selfhosted

**Title**:
I switched my homelab Kubernetes from Proxmox+TrueNAS to TrueNAS-only after a year. Here's the honest tradeoff.

**Body**:

Ran the Proxmox + TrueNAS split for a year. Built a TrueNAS-only path (one box, no separate hypervisor). Several months in on the new setup, no regrets — but the case isn't as one-sided as either camp tends to argue.

**Where two boxes wins**:
- GPU passthrough and PCI device shenanigans (Plex transcoding with iGPU, ML inference) — Proxmox is mature here, TrueNAS isn't.
- Live migration. Proxmox does it. TrueNAS doesn't.
- Independent failure domains. Cluster host dies, file shares survive.
- Hypervisor flexibility in general — LXC, advanced storage configs, fancier networking.

**Where one box wins**:
- ZFS as the single source of truth for files, VM disks, *and* PVCs.
- Power, rack space, money. One PSU, one bill, one thing to maintain.
- Storage path latency — cluster and storage on the same bridge is essentially loopback.
- Simpler mental model. One thing to upgrade. One thing to back up.

**My actual reason for switching**: I wasn't using Proxmox's hypervisor flexibility. I was paying for it in hardware and power without consuming the benefits. Single-box was the right call for me. Wouldn't be the right call for someone running GPU workloads.

Full decision matrix: <link>
Repo (the provider that makes one-box work): https://github.com/bearbinary/omni-infra-provider-truenas

#### r/homelab

**Title**:
Proxmox + TrueNAS vs TrueNAS-only for homelab Kubernetes — decision matrix from someone who's run both

**Body**:

The DM I get most: "should I run Kubernetes on TrueNAS or do I need Proxmox?" Answer is "it depends," and here's the matrix.

**Run Proxmox + TrueNAS (two boxes) if**:
- You need GPU passthrough (Plex transcoding, ML, gaming VMs)
- You want live migration
- You want independent failure domains
- You have abundant hardware budget and rack space

**Run TrueNAS-only (one box) if**:
- You want ZFS as the single source of truth for everything
- Power/space/cost is a real constraint
- You don't need PCI passthrough
- You'd rather have one thing to manage

**The non-obvious part**: most homelab users aren't actually using Proxmox's flexibility. They're paying for it in hardware and power without consuming the benefits. If that's you, one box is the right answer.

**The case I'd reverse on**: GPU-heavy workloads. TrueNAS PCI passthrough exists but is less battle-tested. Don't fight it — use Proxmox.

Full decision matrix with 8 tradeoff axes: <link>
Repo for the one-box path: https://github.com/bearbinary/omni-infra-provider-truenas

#### r/truenas

**Title**:
TrueNAS-only Kubernetes vs Proxmox+TrueNAS — honest comparison for anyone deciding which way to go

**Body**:

If you've been weighing whether to add Proxmox to your homelab specifically to host Kubernetes VMs, here's the matrix.

**The setup that works on TrueNAS alone**:
- VMs hosted directly on TrueNAS via the Omni provider I maintain
- ZFS-backed VM disks via zvols
- Bridge networking with everything sharing the NAS's primary NIC
- PVCs via Longhorn (in-cluster) or democratic-csi (ZFS-native)

**What you give up vs Proxmox**:
- GPU passthrough quality. Possible on TrueNAS but more work.
- Live migration. Doesn't exist on TrueNAS.
- LXC containers alongside VMs. Not a TrueNAS feature.

**What you gain**:
- One box. One power bill. One thing to upgrade.
- ZFS as the single storage source for files, VM disks, and PVCs.
- Storage path between cluster and TrueNAS is essentially loopback — fast.

**Honest verdict**: most homelab users on TrueNAS who *think* they need Proxmox don't actually need its flexibility. They're going to bridge their existing TrueNAS to handle file serving anyway. Skipping the extra hypervisor saves real money and complexity. But if you need GPU passthrough, don't fight it — that's the legitimate Proxmox use case.

Full comparison: <link>
Repo: https://github.com/bearbinary/omni-infra-provider-truenas

### Lemmy

#### `!selfhosted@lemmy.world`

**Title**: I switched my homelab K8s from Proxmox+TrueNAS to TrueNAS-only after a year — honest tradeoff matrix

**URL**: `https://dev.to/cliftonz/<truenas-vs-proxmox-slug>?utm_source=lemmy&utm_medium=selfhosted&utm_campaign=truenas-vs-proxmox-2026-06`

**Body**:
> Most homelab K8s guides assume Proxmox + TrueNAS split. I ran that for a year, then built a TrueNAS-only path and migrated. Sharing what changed and why.
>
> Where two boxes legitimately wins: GPU passthrough, live migration, independent failure domains.
> Where one box wins: power/space/cost, ZFS as single source of truth, simpler mental model.
> My actual reason for switching: wasn't using Proxmox's flexibility, was paying for it anyway.
>
> Not a "Proxmox is bad" take — Proxmox is great at what it's for. This is "most people don't need what Proxmox is for, and the cost of the extra hypervisor is real."
>
> Full decision matrix in the canonical above. Provider that makes the one-box path work is MIT-licensed.

Cross-post to `!homelab@lemmy.ml`, `!truenas@<instance>` (find active) after 24h.

---

## Piece 3: Host-OOM war story

Live URL: `https://dev.to/cliftonz/<host-oom-war-story-slug>`

### Reddit

#### r/selfhosted

**Title**:
My TrueNAS host rebooted overnight three times before I tracked down a memory-config bug. Here's the post-mortem.

**Body**:

Got three independent reports over two weeks. They didn't look like the same bug at first.

- "TrueNAS rebooted overnight, logs don't show anything"
- "Cluster nodes disappeared, NAS came back but something's wrong"
- "QEMU OOM kills in dmesg, my VM never starts"

The third report made it clickable. All three were the same bug: my open-source provider accepted a memory config the host couldn't actually allocate. `min_memory` (soft floor) was being set higher than `memory` (hard cap) in some users' MachineClass YAML. TrueNAS dutifully tried to lock more RAM than the VM was allowed to use. Kernel killed QEMU, then thrashed the host.

**The fix**: three lines of schema validation in v0.16.1. If `min_memory > memory`, reject the spec at apply time with a clear error message naming both values and the rule.

**The lesson**: validate at the boundary the user touches (apply-time schema), not at the runtime where the failure surfaces. For infrastructure tools especially — the runtime cost of late failure isn't a 500 status, it's a host rebooting.

Full post-mortem with the three places this validation could have lived (and why I picked one): <link>
v0.16.1 release notes: https://github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.16.1

#### r/golang

**Title**:
Bug post-mortem: a missing two-field constraint in my Go infra tool's schema caused user hosts to OOM. Lesson on where validation belongs.

**Body**:

Open-source Omni infrastructure provider, ~5kLOC Go. MachineClass config had two memory fields — `memory` (hard cap) and `min_memory` (optional soft floor). Schema enforced minimums on each independently but not the relationship between them.

Users with the obvious typo (e.g., `memory: 4096; min_memory: 8192`) had their config accepted, applied, and reach runtime where TrueNAS tried to start a VM whose minimum memory exceeded its maximum. Kernel killed QEMU. Host thrashed. Multiple users reported overnight reboots.

**Three places this could have been validated**:

1. **At runtime** (inside the provision step). Too late — user has already applied and waited. Error surfaces minutes after the cause.
2. **In Omni's resource-write hook**. Wrong layer — Omni doesn't know the semantic constraint, just sees two ints.
3. **In the provider's JSON Schema** registered at startup. Right layer — Omni rejects the apply against the schema before anything tries to run.

I went with option 3. Three lines of conditional validation. v0.16.1 ships it.

The principle that generalizes for any declarative-config tool: validate cross-field constraints at the input boundary, not at the runtime. The runtime cost is asymmetrically larger — especially for infra code where "runtime failure" can mean someone's hardware rebooting.

Full write-up with the actual Go code: <link>
Source: https://github.com/bearbinary/omni-infra-provider-truenas

### Lemmy

#### `!selfhosted@lemmy.world`

**Title**: My TrueNAS host OOMed three times overnight before I tracked the bug back to my own schema validation gap

**URL**: `https://dev.to/cliftonz/<host-oom-war-story-slug>?utm_source=lemmy&utm_medium=selfhosted&utm_campaign=host-oom-2026-06`

**Body**:
> Post-mortem on a v0.16.1 fix. Three users had hosts rebooting overnight. None of the bug reports looked like the same bug at first — symptoms varied, root cause was identical.
>
> Bug: my provider's schema accepted memory configs where `min_memory > memory` (user typos, mostly). TrueNAS dutifully tried to lock more RAM than the VM was allowed. Kernel killed QEMU. Host thrashed.
>
> Fix: three lines of conditional schema validation. Fails at the boundary (user apply-time), not at the runtime (VM start). Clear error message naming both values.
>
> The principle that generalizes: validate cross-field constraints at the input edge, especially when the runtime cost is "someone's hardware rebooting."
>
> Full post-mortem above. Honest accountability paragraph at the end — three users had bad nights because of something I shipped.

#### `!programming@programming.dev`

**Title**: Bug post-mortem: where to validate a cross-field constraint in a Go infra tool (and why "at the runtime" is the wrong answer)

**URL**: `https://dev.to/cliftonz/<host-oom-war-story-slug>?utm_source=lemmy&utm_medium=programming&utm_campaign=host-oom-2026-06`

**Body**:
> Same content, different community angle. The Go/programming audience cares less about the host-OOM symptom and more about the validation-layering decision.
>
> The shape of the lesson: any two config fields with a semantic relationship (`min < max`, `start < end`, `quota < limit`) is a validation candidate. If you're not writing the constraint, you're telling users to trust you to never get it wrong. They won't.
>
> Three possible places to put the check — runtime, framework-side hook, schema layer. The schema layer wins for boundary-fail-fast reasons.
>
> Source includes the actual Go code for the validator + the JSON Schema conditional that does the apply-time enforcement.

---

## Cadence

| Day | Action |
|---|---|
| Day 0 (Mon) | Publish Talos-vs-k3s on dev.to |
| Day 1 | LinkedIn |
| Day 2 | X thread |
| Day 4 | r/truenas (smallest, tune hook) |
| Day 7 | r/selfhosted + `!selfhosted@lemmy.world` (same week, separate days) |
| Day 10 | r/homelab + `!homelab@lemmy.ml` cross-post |
| Day 13 | r/kubernetes + `!kubernetes@lemmy.world` cross-post |
| Day 14 | Publish TrueNAS-vs-Proxmox on dev.to |
| Day 15 | LinkedIn for piece 2 |
| Day 18 | r/truenas for piece 2 |
| ... | (continue pattern for piece 2 and piece 3, ~2 weeks per piece) |

Total M2 spans ~6 weeks if each piece gets a full distribution cycle. Compress where the audience overlap is high (don't post the same week to both subs with major audience overlap).
