# Cross-posts: Hero piece "Kubernetes on TrueNAS SCALE: the Talos + Omni Path"

**Canonical URL**: `https://dev.to/cliftonz/<hero-post-slug>` (publish hero on dev.to first, then drop the live URL into every link below)

**Rule of thumb for each platform**: deliver real value in the post itself + tease the deep-dive. Don't paste the hero. Each platform's audience is different — hooks below are tailored.

**Posting cadence**: dev.to first → wait ~24h → LinkedIn → wait 24h → X thread → wait 24–48h → Reddit (Reddit dislikes simultaneous cross-posts, and the 24h delay lets you mention "I wrote this up" without looking like fresh spam). Don't post to all four Reddit subs the same day — stagger by 2–3 days minimum.

---

## 1. LinkedIn (native long post — ~1500 chars, professional brand voice)

**Why this format**: LinkedIn rewards native content (no leaving the feed). First 2 lines are the truncated preview — load the hook there. End with a soft CTA to the article. Add 3–5 hashtags at the bottom.

---

When TrueNAS dropped built-in Kubernetes support, a lot of homelab clusters quietly died.

Mine was one of them — so I built the way back.

For the last several months I've been shipping an open-source Omni infrastructure provider that turns a TrueNAS SCALE 25.04+ box into a fleet of Talos Linux VMs. The result: a real, multi-node Kubernetes cluster running on the NAS you already own, managed by Sidero Omni, with no second hypervisor needed.

A few things I learned along the way:

→ Immutable OSes (Talos) change how you think about node lifecycle. You stop maintaining nodes. You replace them. Two minutes after a node misbehaves, it's gone and a new one is up.

→ TrueNAS 25.10 removed implicit Unix-socket auth on its API. That single change forced a transport rewrite — WebSocket + API key only. Worth knowing if you're integrating with TrueNAS programmatically.

→ Sizing matters more than people admit. A 2 GB control plane is fine for a raw cluster. The moment you add Crossplane, Argo with many ApplicationSets, or Prometheus Operator at full scrape, the apiserver swaps and the cluster looks intermittently broken with no obvious cause. Plan for 4 GB minimum on production-ish CPs.

→ HDD pools and etcd are not friends. etcd assumes <10 ms fsync. HDDs under load see 50–200 ms. You either add an NVMe SLOG or you patch the heartbeat/election timeouts. Most "intermittent flakes" I've debugged for people come back to this.

I wrote the canonical how-to today: from "TrueNAS in a closet" to "cluster running real workloads" in an evening. Sizing, storage opinions, the pitfalls that bite real users, the lot.

Link in comments (LinkedIn de-prioritizes external links in the body).

Building self-hosted infra is more accessible than it's ever been. If you've got a NAS sitting there, you've got most of the hardware.

#Kubernetes #TrueNAS #Homelab #Talos #SelfHosted

---

**First comment (post immediately after publishing)**:

The full guide: https://dev.to/cliftonz/<hero-post-slug>

And the repo if you want to skip straight to install: https://github.com/bearbinary/omni-infra-provider-truenas

---

## 2. X / Twitter thread (10 tweets, hook on T1)

**Why this format**: X rewards threads, hook decay is real, end with a clean payoff link. Don't pad. Numbers + concrete claims do better than vibes.

---

**T1 (hook):**

When TrueNAS dropped Kubernetes in 25.04, my homelab cluster died.

So I built the way back: an Omni provider that turns a TrueNAS box into a fleet of Talos VMs.

Result: a real multi-node K8s cluster on the NAS you already own. No second hypervisor.

🧵

---

**T2:**

The stack:

- TrueNAS SCALE 25.04+ hosts the VMs
- Sidero Omni manages the cluster (free tier covers homelab)
- Talos Linux runs inside each VM (immutable, no SSH, no shell)
- A small Go provider I maintain wires them together

MIT licensed.

---

**T3:**

Why Talos and not Ubuntu?

You can't apt-install anything. You can't shell in. You can't drift.

Cluster nodes become reproducible artifacts you replace, not maintain.

Sounds restrictive. It's the feature.

---

**T4:**

Hardware budget for a comfortable home cluster (1 CP + 2 workers):

- 8 cores
- 32 GB RAM
- 100 GB free on ZFS pool

Minus ~8 GB RAM for TrueNAS itself.

Most mid-tier NAS boxes from the last 3 years can do this.

---

**T5:**

Trap #1: HDD pools and etcd.

etcd assumes <10 ms fsync. HDDs under load see 50–200 ms.

No SLOG = intermittent leader changes and NodeNotReady flaps with no obvious cause.

Either add NVMe SLOG or patch the heartbeat timeouts.

---

**T6:**

Trap #2: undersizing the control plane.

2 GB RAM is fine for a raw cluster.

Add Crossplane, Argo with many ApplicationSets, or Prometheus Operator at full scrape — apiserver swaps, cluster looks broken.

Plan for 4 GB minimum if you have ambitions.

---

**T7:**

Storage opinions, ranked:

1. Longhorn — recommended default. Block storage, CNCF, runs in-cluster.
2. democratic-csi — ZFS-native PVCs with snapshots, battle-tested.
3. nfs-subdir-external-provisioner — please don't.

---

**T8:**

What the provider actually does:

- Listens for Omni `MachineRequest` resources
- Creates a zvol + VM + NIC on TrueNAS via JSON-RPC
- Boots Talos with a SideroLink tunnel back to Omni
- Tears it all down when Omni asks

2–5 min per node from request to Ready.

---

**T9:**

Recent v0.16.1 work:

- Host-OOM safety check (memory schema validation that fails loud)
- SAST sweep — 6 findings, all closed
- 42 cassette-based integration tests run in CI with zero hardware

I trust this as my daily driver. One-maintainer caveats apply.

---

**T10 (CTA):**

Full canonical how-to — sizing, storage, the pitfalls that bite real users:

https://dev.to/cliftonz/<hero-post-slug>

Repo:

https://github.com/bearbinary/omni-infra-provider-truenas

Issues > stars. Tell me what breaks.

---

## 3. Reddit — four subs, each milestone-framed

**Framing rule (locked)**: every Reddit post leads with a **milestone, lesson, or experience** — never with the project name. The project is the resource you mention near the end, not the headline. See `_shared/reddit-lemmy-post-patterns.md` for the full playbook.

**Why**: Reddit (and Lemmy) audiences downvote anything that smells like product marketing. They upvote stories, lessons, real specs, honest tradeoffs. The same content — re-framed as "what I learned" vs "what I built" — gets dramatically different engagement.

**General Reddit rules**:
- Title is the whole pitch. Subreddit-specific. Lead with TIME/EXPERIENCE/LESSON.
- Body delivers real value before any link. Concrete numbers, specific gotchas, honest tradeoffs.
- Link at the end, framed as "if you want to go down the same path, here's where I wrote it up."
- Stagger posts across multiple days, not the same day.
- Be in the comments answering questions for the first ~6 hours after posting.

---

### 3a) r/selfhosted

**Title**:
After 6+ months running Kubernetes on a single TrueNAS box, here's the writeup of what actually works

**Body**:

Six months ago, TrueNAS 25.04 shipped without built-in Kubernetes and my homelab cluster went dark. I didn't want to add a Proxmox box just to host K8s VMs — so I started running Talos Linux directly on TrueNAS, managed by Sidero Omni.

Six months in, here's what I learned the hard way:

**1. Immutability changes everything.** Talos has no SSH, no shell, no package manager. The first week feels alien. The second week feels like a relief. By month three you stop "fixing" nodes — you replace them. Configuration drift stops being a phrase you use.

**2. Sizing matters more than people admit.** A 2 GB control plane is fine for a raw cluster. The moment you add Argo CD with many ApplicationSets, or Prometheus Operator at full scrape, or Crossplane — the apiserver swaps and the cluster looks intermittently broken. Bump to 4 GB before you install those, not after.

**3. HDD pools and etcd don't get along.** etcd assumes sub-10 ms fsync. Spinning rust under load sees 50–200 ms. Without an NVMe SLOG you'll see intermittent NodeNotReady flaps and waste a weekend debugging. Either add the SLOG or apply the etcd timeout patch.

**4. Storage path latency is invisible until it isn't.** When the cluster and TrueNAS share a bridge, PVC I/O is essentially loopback. Move them to separate boxes and you're suddenly running every storage call over the LAN. Worth knowing before you architect.

**5. The smallest community first.** I posted in r/truenas before bigger subs every time. Smaller audience, tighter ICP, better feedback.

I wrote the full canonical install guide last week — every step, every sizing decision, every gotcha — so the next person doesn't have to learn it the slow way:

Guide: https://dev.to/cliftonz/<hero-post-slug>
Repo (MIT-licensed Omni provider, what makes this work): https://github.com/bearbinary/omni-infra-provider-truenas

Happy to answer questions. Issues > stars.

---

### 3b) r/homelab

**Title**:
My homelab Kubernetes cluster runs on a single TrueNAS box — setup, lessons, what I'd do differently after 6 months

**Body**:

Sharing what's been working in my homelab for the last six months: a real multi-node Kubernetes cluster running directly on a TrueNAS SCALE 25.10 box. One machine. No Proxmox alongside. No second hypervisor.

**Hardware**: TrueNAS SCALE 25.10, 12 cores, 64 GB RAM, NVMe pool.
**Cluster shape**: 1 control plane, 3 workers. ~4 vCPU and 8 GB RAM per worker, plus a 100 GB data disk per worker for Longhorn.
**Workloads**: Argo CD, cert-manager, Prometheus, Velero, Home Assistant, Jellyfin, Nextcloud, Pi-hole.

**What works**:

- One box, one power bill, one rack slot, one thing to maintain.
- ZFS as the single source of truth for files, VM disks, and PVC snapshots.
- Storage path between cluster and TrueNAS shares is loopback — fast.
- Talos upgrades through Omni's UI = rolling, with health checks between nodes.

**What I'd do differently if starting over**:

- Add an NVMe SLOG day one, even on an NVMe pool — gives etcd extra headroom for free.
- Start with a 4 vCPU / 4 GB control plane, not 2/2. Saves a future resize when you install Argo/Prometheus/etc.
- Use Longhorn from the beginning. NFS-as-StorageClass was a detour.
- Set `recordsize=16K` on the dataset hosting VM zvols. Default 128 KiB hurts etcd write amplification.

**What surprised me**:

- 90% of the "intermittent" failure modes come back to ZFS write latency. Once I understood that, debugging got 10× faster.
- The provider that makes this work doesn't try to be clever. It does one thing — create Talos VMs — and stays out of the way.

Wrote the canonical install guide with sizing tables, storage opinions, and every gotcha I hit:

Guide: https://dev.to/cliftonz/<hero-post-slug>
Repo (MIT-licensed Omni provider I maintain): https://github.com/bearbinary/omni-infra-provider-truenas

---

### 3c) r/kubernetes

**Title**:
Lessons from shipping an Omni infrastructure provider for a non-cloud target (TrueNAS), 6 months in

**Body**:

I've been maintaining an open-source Omni infrastructure provider for TrueNAS SCALE for the last 6+ months. Wanted to share what I've learned about building Omni provider extensions — patterns that compounded, gaps in the SDK, and the unglamorous parts that ended up mattering most.

**Context**: Omni's `infra.NewProvider()` + `provision.Step` pattern is the framework. You give it a provider ID, register provision/deprovision steps, and Omni delivers MachineRequests for you to fulfill. For my target, that means creating Talos VMs on TrueNAS via JSON-RPC over WebSocket.

**Patterns that compounded**:

**1. Singleton-lease leader election.** The SDK ships none. Run two processes with the same provider ID and they both race on every MachineRequest. I built one in ~200 lines on top of Omni's COSI resource store, using annotations on `ProviderStatus` as the lease object and optimistic concurrency for the compare-and-swap. Fencing token via an epoch annotation. Fail-fast at startup.

**2. Cassette-based integration tests.** TrueNAS's API surface is wide and stateful. Hand-rolling mocks was painful and false-positive-prone. Recording real responses and replaying them gives dramatically better signal than mocks. 42 cassettes now. CI doesn't need TrueNAS hardware.

**3. Schema validation at the boundary, not the runtime.** A bad MachineClass that gets to runtime can OOMKill the host. The recent v0.16.1 release fails this loud at the schema, which is the right place. Lesson: for infra tools, validate aggressively at the input edge.

**4. Routing around upstream bugs honestly.** Omni has a gRPC-gateway quirk where successful writes occasionally return `200 OK` with no `Content-Type` header. The gRPC client treats it as an error. Substring-matching on error strings to detect "really a success" is ugly but correct — and the workaround is documented in source so future-me doesn't try to clean it up.

**What I'd build differently**: thread the fencing-token epoch through every state-mutating call from day one. I haven't fully wired that and it's the bug I haven't filed against myself yet.

Source (singleton lease, cassettes, the whole project): https://github.com/bearbinary/omni-infra-provider-truenas
Canonical install guide for context: https://dev.to/cliftonz/<hero-post-slug>

Curious what others have built on top of Omni. Patterns from other community providers?

---

### 3d) r/truenas

**Title**:
For anyone who lost K3s when 25.04 shipped — here's the path I rebuilt my homelab Kubernetes with after 6 months

**Body**:

When 25.04 dropped without K3s, my homelab cluster went dark. I didn't want to add a Proxmox box just to host VMs, so I went down a different path: Talos Linux VMs hosted directly on TrueNAS, managed by Sidero Omni.

Six months in, it works. Sharing the writeup because I know I'm not the only one who hit this.

**TrueNAS-specific things I wish I'd known sooner**:

**1. Don't use the root user's API key.** Create a dedicated `omni-provider` user with no password (API-only), add it to `builtin_administrators`. The reason: TrueNAS requires `SYS_ADMIN` for the `/_upload` endpoint, and you only get `SYS_ADMIN` through `builtin_administrators` membership. Custom roles don't substitute. Found this out at 11pm one night.

**2. Bridge before anything else.** Bridge your primary NIC under Network > Interfaces. VMs share the bridge with TrueNAS itself. This briefly drops the NAS off the network while the IP migrates — that's normal but it surprises people the first time.

**3. WebSocket only.** TrueNAS 25.10 removed implicit Unix-socket auth. API key over WebSocket is the only path now. If you were on an older provider version using socket transport, it stopped working.

**4. ZFS recordsize matters for etcd.** Default 128 KiB is overkill for etcd's 4–16 KiB writes. Set `recordsize=16K` on whatever dataset hosts the VM zvols. Write amplification drops, etcd latency drops, your cluster gets quieter.

**5. Longhorn or democratic-csi for PVCs.** Both work. Longhorn is my default — block storage, in-cluster, no LAN traffic for PVC I/O. democratic-csi if you want native ZFS snapshots of every PVC.

**6. HDD pools need an NVMe SLOG.** etcd's fsync timing assumptions don't survive spinning rust. Without SLOG you'll see intermittent NodeNotReady flaps that look like network issues but aren't. Either add the SLOG or apply the etcd timeout patch.

Full writeup with every step + sizing decisions + storage opinions: https://dev.to/cliftonz/<hero-post-slug>
Source for the provider (MIT, listed on the TrueNAS apps community catalog): https://github.com/bearbinary/omni-infra-provider-truenas

Issues-only contribution model on the repo. Happy to answer questions here.

---

## 4. Posting checklist (run through before each)

- [ ] Hero post live on dev.to, canonical URL pasted everywhere placeholder `<hero-post-slug>` appears
- [ ] LinkedIn: hashtags trimmed to 3–5, first 2 lines are the hook, article link in first comment only
- [ ] X: thread numbering removed (it shows up as cruft), each tweet under 280 chars, links unwrapped (X auto-cards)
- [ ] Reddit: read the sub's pinned rules + recent top posts before submitting. Some subs require flair. r/kubernetes wants "Discussion" or "Article" flair. r/selfhosted is strict on self-promo cadence.
- [ ] Be in comments for the first 6 hours after each post — this is where retention happens.

---

## 5. Distribution cadence (suggested)

| Day | Action |
|---|---|
| Day 0 (Mon) | Publish hero on dev.to. Capture live URL. |
| Day 1 (Tue) | LinkedIn native post + first-comment link. |
| Day 2 (Wed) | X thread. |
| Day 4 (Fri) | **r/truenas post first** — smallest, exact-fit ICP, lowest risk. Use comments here to tune the hook before bigger subs. Active in comments. |
| Day 7 (Mon next week) | r/selfhosted post. Active in comments. Confirm you've contributed elsewhere recently — sub enforces a ~10% self-promo rule. |
| Day 10 (Thu) | r/homelab post. |
| Day 14 (Mon week 3) | r/kubernetes post. Reframe hook to the infrastructure-provider angle (architecture / SDK), not the install guide — that's what this sub rewards. |

Spreading by ~3 days per Reddit drop prevents the "this guy is everywhere" signal and lets each sub's discussion grow on its own. r/truenas first gives you free hook-tuning before the bigger subs see anything.

---

## 6. Tracking

In the hero post and each cross-post, append a UTM to the repo URL so you can attribute traffic:

```
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=devto&utm_medium=post&utm_campaign=hero-2026-05
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=linkedin&utm_medium=social&utm_campaign=hero-2026-05
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=x&utm_medium=thread&utm_campaign=hero-2026-05
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=reddit&utm_medium=selfhosted&utm_campaign=hero-2026-05
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=reddit&utm_medium=homelab&utm_campaign=hero-2026-05
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=reddit&utm_medium=kubernetes&utm_campaign=hero-2026-05
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=reddit&utm_medium=truenas&utm_campaign=hero-2026-05
```

GitHub strips most query params on display but they hit your analytics if you've got Plausible/PostHog wired to the repo's homepage redirect or the docs site. If repo doesn't track, point the UTM URLs at the docs site instead.
