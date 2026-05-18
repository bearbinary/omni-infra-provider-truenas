# M6 Cross-posts — Reddit + Lemmy

Milestone framing per `_shared/reddit-lemmy-post-patterns.md`.

M6 has two pieces (the hub page + the Omni explainer). The hub page isn't really a Reddit announcement piece — it's a destination. But it's worth announcing as "6 months of work, now in one place." The Omni explainer is the broadest-audience piece in the whole 6-month run and the best HN candidate.

---

## Piece 1: The hub page (complete guide)

Live URL: `https://dev.to/cliftonz/<hub-page-slug>`

### Reddit

#### r/selfhosted

**Title**:
6 months of writing about Kubernetes on TrueNAS, now interlinked in one place — the canonical guide I wish had existed when I started

**Body**:

Just published the hub page that links every piece I've written this year about running Kubernetes on TrueNAS via Talos + Omni. Install, comparisons, sizing, storage, networking, upgrades, build-in-public retros, user case study, failure modes — all of it cross-referenced.

**Highest-utility section if you're already running this stack**: the failure-modes table near the bottom. Symptom → root cause → fix. I update it every release. Bookmark that specifically.

**If you're new to the stack**: start with the install guide linked at the top. Bookmark the hub for when you start operating the cluster.

**The work that compounds in OSS isn't the splashy releases.** It's the patient process of writing down every lesson so the next person doesn't have to learn it the slow way. Six months ago this hub didn't exist. Today it does, and it'll keep growing.

Hub: <link>
Repo (the open-source provider that makes this stack work): https://github.com/bearbinary/omni-infra-provider-truenas

#### r/truenas

**Title**:
The complete guide to running Kubernetes on TrueNAS via Talos + Omni — every piece I've written this year, in one place

**Body**:

Direct cousin to the announcement post when the provider first dropped, but now there's substance underneath it. The complete guide links:

- Install walkthrough
- Storage decision matrix (Longhorn / democratic-csi / NFS)
- Sizing the control plane
- Networking (bridges, DHCP, MetalLB, VIP)
- Upgrade playbook with the ZFS-aware pre-flight ritual
- Comparison vs k3s and vs Proxmox-based setups
- Failure-modes table with every gotcha I've hit, diagnostics included
- Build-in-public retros (SAST findings, leader-election pattern, host-OOM war story)

For TrueNAS users specifically, the failure-modes table covers the issues you'll hit most: SLOG-vs-no-SLOG etcd timing, `pool` field interpretation, dedicated API key user setup, the WebSocket-only auth change in 25.10.

If you've been thinking about migrating off the dead built-in K3s and want one bookmark for the whole path forward — this is it.

Hub: <link>
Repo: https://github.com/bearbinary/omni-infra-provider-truenas

### Lemmy

#### `!selfhosted@lemmy.world`

**Title**: Complete guide to Kubernetes on TrueNAS — 6 months of writing, now in one interlinked hub

**URL**: `https://dev.to/cliftonz/<hub-page-slug>?utm_source=lemmy&utm_medium=selfhosted&utm_campaign=hub-2026-11`

**Body**:
> Six months of pieces — install, comparisons, sizing, storage, networking, upgrades, build-in-public retros — finally linked in one canonical place.
>
> The most-useful section for returning visitors is the failure-modes table at the bottom. Symptom → root cause → fix, updated every release. Bookmark that specifically.
>
> Posting because some of you have been following the individual pieces as they dropped, and this is the consolidated entry point.

---

## Piece 2: Omni explainer (broadest reach)

Live URL: `https://dev.to/cliftonz/<omni-explainer-slug>`

This is the M6 piece with the widest audience. Lemmy + Reddit + HN + LinkedIn all get their version. Reddit subs especially want the technical-extension-point framing, not the install-this-tool framing.

### Reddit

#### r/kubernetes

**Title**:
If you've heard of Kubernetes but not Sidero Omni — Omni's killer feature isn't the UI, it's an extension point most people never see

**Body**:

Brief explainer for anyone who's worked with managed Kubernetes (EKS, GKE, AKS) but hasn't encountered the on-prem-friendly options.

**Omni in one paragraph**: a Kubernetes management platform that's environment-agnostic. Bring nodes from anywhere — cloud VMs, baremetal, hypervisor VMs, Raspberry Pis — and Omni handles the operating part. Talos Linux as the immutable node OS. SideroLink (outbound WireGuard) for connectivity. Free tier for personal use, paid for production scale.

**The killer feature**: infrastructure providers. A small extension point that lets anyone teach Omni how to create cluster nodes on a new kind of hardware. The Sidero team can't build a provider for every target — so the contract is open, and the long-tail providers are community-built.

**What an infrastructure provider does**:
1. Registers with Omni using a provider ID
2. Listens for `MachineRequest` resources
3. Creates the corresponding Talos VM on whatever hardware you support
4. Tears it down when asked

That's the whole contract. ~200 lines of boilerplate to get going. The interesting work is the steps inside.

**The unreasonable freedom**: the contract is so narrow that as long as you produce running Talos nodes, you can do whatever you want underneath. My TrueNAS provider uses ZFS-backed zvols, cassette-based tests, custom leader election — none of which Omni knows or cares about.

If you have hardware nobody else has and an API to control it, you could write an Omni provider for it. The cost is a few thousand lines of Go.

Full explainer: <link>
My TrueNAS provider (5kLOC reference): https://github.com/bearbinary/omni-infra-provider-truenas

#### r/devops

**Title**:
Building extensions for managed Kubernetes platforms — what Sidero Omni's infrastructure-provider contract gets right

**Body**:

For folks who've worked with cloud-managed Kubernetes (EKS, GKE) and wondered what the on-prem equivalents look like — Sidero Omni is one of them, and its extension model is worth knowing about.

**The pattern**: Omni manages cluster lifecycle (upgrades, scale, config) without owning node creation. Node creation lives in "infrastructure providers" — small processes that fulfill `MachineRequest` resources by creating Talos VMs (or baremetal nodes) on whatever hardware the provider targets.

**Why this is interesting from a platform-engineering perspective**:
1. **Narrow contract**. Omni only cares that nodes appear and disappear on demand. Everything else is the provider's business.
2. **Long-tail enablement**. Sidero builds providers for AWS, GCP, Hetzner, Proxmox, baremetal. Community builds the rest — TrueNAS, Hyper-V, weird ARM clusters, specialized hardware.
3. **Composable failure domains**. A misbehaving provider can only affect node-creation for *its* target. Other providers and the broader cluster keep working.
4. **Solo-maintainability**. The contract is narrow enough that a single engineer can build, test, and maintain a provider for a target they care about.

Lesson for platform designers: well-defined, narrow extension points enable ecosystem growth in a way that monolithic platforms can't.

Full explainer with what an Omni provider actually does code-wise: <link>

#### r/selfhosted

**Title**:
Sidero Omni explained for self-hosters — what it actually does, why you'd use it, and how to extend it for unusual hardware

**Body**:

If you've heard about Omni in the context of Kubernetes and weren't sure whether it's for you — short version: yes, probably.

**Omni is**: a Kubernetes management platform that doesn't require you to be in any particular cloud. Free tier for homelab use. Self-hostable if you'd rather not depend on Sidero's cloud.

**Why it's interesting for self-hosters specifically**:
- Talos Linux nodes (immutable, no SSH, no shell). Sounds restrictive. Actually a feature.
- SideroLink — outbound WireGuard tunnel from each node back to Omni. No inbound ports. Works behind any NAT.
- Cluster management via web UI: upgrades, scaling, backups, kubeconfig generation.

**The thing nobody tells you**: Omni has an extension point called "infrastructure providers" that lets anyone teach it how to create nodes on new hardware. I used that extension point to write a provider for TrueNAS SCALE — so my homelab cluster is just Talos VMs running on my NAS, managed by Omni.

If you have unusual hardware nobody else has built a provider for — you could be the one. The cost is a few thousand lines of Go and a working knowledge of the platform.

Full explainer: <link>
TrueNAS provider as a reference: https://github.com/bearbinary/omni-infra-provider-truenas

### Lemmy

#### `!kubernetes@lemmy.world`

**Title**: What an Omni infrastructure provider actually is — and the unreasonable freedom they give you

**URL**: `https://dev.to/cliftonz/<omni-explainer-slug>?utm_source=lemmy&utm_medium=kubernetes&utm_campaign=omni-explainer-2026-11`

**Body**:
> Explainer for the Kubernetes audience that's heard of Omni but hasn't engaged with the extension model.
>
> Omni's `infra.NewProvider()` contract is one of the most well-designed extension points I've worked with recently. Narrow (produce a Talos node), well-documented, free to do whatever you want underneath.
>
> My TrueNAS provider is ~5kLOC of Go. The provider does ZFS-backed zvols, deterministic MAC addresses, cassette-based tests, custom leader election — none of which Omni knows about or cares about. That's the point.
>
> Lesson that generalizes past Omni: when platforms ship narrow extension points, ecosystems grow around them. Monolithic platforms don't get the same effect.

#### `!opensource@lemmy.ml`

Same body, intro emphasizing the OSS/community-providers angle.

#### `!selfhosted@lemmy.world`

Same body as the r/selfhosted post above.

---

## HN submission

The Omni explainer is the HN candidate. Submit on a Tuesday morning ET. Title for HN:

> What an Omni infrastructure provider actually is (and why I built one for a NAS)

HN audience appreciates the "narrow extension points enable ecosystem growth" lesson framing more than they appreciate the install guide framing. Lead with that.

Don't submit hub page to HN. It's a destination, not a story.

---

## Cadence

Hub page goes live first (Week 21 of the campaign). Omni explainer ~10 days later (Week 22), giving the hub time to settle.

After both are live, the M6 retro video (V8) ships ~Week 23. The forward-look LinkedIn post (Week 24) closes the 6-month run.
