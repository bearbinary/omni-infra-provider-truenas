# YouTube Scripts: M2 Comparison Videos (V3 + V4)

Two videos. Both companions to the M2 written comparison posts. Record in same session if possible — same shirt, same lighting, same audio profile as V1+V2. Ship V3 first (Talos+Omni vs k3s) since it directly answers the "what now?" question from the V2 walkthrough audience. V4 (TrueNAS vs Proxmox) goes ~10 days later.

**Channel conventions** (locked from V1/V2): cold open, lower-third name plate on first appearance, 20s end screen with repo + next-video card, pinned comment with repo + hero post + issues note.

**M2 placement** in monthly plan: V3 is the "month 2 flagship." It's the comparison video that puts you on the map as the honest reviewer. Treat thumbnail/title with care.

---

## V3 — "Talos + Omni vs k3s on TrueNAS — honest comparison"

**Working title**: `Talos + Omni vs k3s on TrueNAS: which Kubernetes path should you pick?`
**Length target**: 9:00–11:00.
**Format**: face-cam open/close, screencast comparisons in the middle, on-screen scoring overlay throughout.
**Thumbnail text**: "TALOS vs k3s" + your face + small Talos/k3s logos.

### Title options

1. `Talos + Omni vs k3s on TrueNAS: which Kubernetes path should you pick?` ← SEO favorite
2. `I ran both Talos and k3s on my TrueNAS for 6 months. Here's the honest take.` ← curiosity hook
3. `Which Kubernetes for your TrueNAS homelab? Talos + Omni vs k3s, named honestly.`

### Description

```
TrueNAS killed built-in Kubernetes in 25.04. So which path back do you take — manual k3s in a TrueNAS VM, or Talos + Omni via omni-infra-provider-truenas? I ran both for 6 months. Here's the honest comparison.

Chapters:
00:00 — The two paths
01:00 — What "k3s on TrueNAS" actually means in 2026
02:00 — Setup time comparison
03:30 — Day-2 operations (where the gap is real)
05:00 — Multi-node and upgrades
06:30 — Debuggability — k3s wins this one
07:30 — Storage and PVCs
08:30 — The decision: who should pick which
10:00 — What I actually run, and why

Links:
— Written companion (the full comparison post): https://dev.to/cliftonz/<m2-talos-vs-k3s-slug>
— Provider repo: https://github.com/bearbinary/omni-infra-provider-truenas
— Install walkthrough video: <V2 URL>
— Origin story: https://dev.to/cliftonz/truenas-killed-kubernetes-so-i-brought-it-back-4n7h

#Kubernetes #TrueNAS #Talos #k3s #Homelab #SelfHosted
```

### Script

**[0:00–0:30 — COLD OPEN, face-cam]**

> When TrueNAS killed built-in Kubernetes, two real paths emerged: roll your own k3s in a VM, or use Talos plus Omni. I ran both for six months. Both work. They are not the same thing. This video is the honest comparison — what each one is best at, and which one you should actually pick.

**[0:30–1:00 — Setup, face-cam]**

> [Title card: "Talos + Omni vs k3s on TrueNAS"]
> I'm Zac, I maintain the open-source Omni provider that makes the Talos path work. Don't worry — I'll call out where k3s is genuinely better. There are real ones.

**[1:00–2:00 — What k3s on TrueNAS means in 2026, face-cam with on-screen table]**

> [SCREEN: 3-row table from the written post]
> Quick clarifier. When someone says "k3s on TrueNAS" today, they could mean three things.
> One — the old built-in TrueNAS K3s. That's dead, removed in 25.04. Not an option.
> Two — the new TrueNAS Apps system with Docker Compose. Not Kubernetes. Different product.
> Three — k3s you install manually in a TrueNAS VM. That's the DIY path, and that's what I'm comparing against.
> If you just want to run Docker containers and you don't actually need Kubernetes, use the new TrueNAS Apps and stop watching. Save yourself an evening.

**[2:00–3:30 — Setup time, split screen]**

> [SCREEN: split — left side a fresh Ubuntu VM SSHing in, right side Omni cluster create form]
> Setup time. Path A — k3s in a VM. Create the VM. Boot it. SSH in. Curl-pipe the install script. Hope your cert SAN config is right. Repeat for every node.
> 30 minutes a node, give or take. You can script it — and you will, differently than every other person who's also scripting it.
> Path B — Talos plus Omni. Provider's already installed. MachineClasses are already defined. Cluster create form: name, control plane class, worker class, replicas, click Create.
> [Time-lapse: cluster from request to Ready]
> Five minutes the first time. Less every time after.

**[3:30–5:00 — Day-2 ops, face-cam with cutaway to Omni UI]**

> [Face-cam]
> Here's where the gap is real. Day-2 operations.
> With k3s-in-a-VM, you own the whole stack. Ubuntu security updates. k3s version bumps. kubelet cert rotation. etcd backups. When something breaks at 11pm, you're SSHing in, journalctl-ing, googling.
> [SCREEN: Omni upgrade UI]
> With Talos plus Omni — Omni handles Talos OS upgrades and Kubernetes upgrades through the web UI. Rolling, one node at a time, with health checks between.
> [Face-cam, leaning in]
> And here's the mental shift that matters most: Talos is immutable. You don't fix nodes. You replace them. Two minutes after a node misbehaves, it's gone and a fresh one is up. Configuration drift isn't a phrase that exists in your life anymore.
> If you've been bitten by "snowflake node" debugging — and if you've run a cluster for a year, you have — this is genuinely better.

**[5:00–6:30 — Multi-node + upgrades, screencast]**

> [SCREEN: Omni MachineSet replica counter]
> Multi-node and HA. Path A: you wire it up yourself. Embedded etcd or external datastore, each control plane addition manual, worker scaling manual.
> Path B: bump the replica count from 1 to 3. The provider creates two VMs, Omni adds them, etcd rebalances. Done.
> [SCREEN: Omni Kubernetes upgrade flow]
> Upgrades — same story. With k3s-in-VM, re-run the install script per node, cross your fingers. With Talos plus Omni, click "Upgrade Kubernetes" in Omni and watch the rolling upgrade. If a node fails to come back, Omni stops and tells you.

**[6:30–7:30 — Debuggability — k3s wins, face-cam, honest]**

> [Face-cam, direct]
> Okay — here's where k3s genuinely wins.
> Debuggability. With k3s-in-a-VM you can SSH into the node. You can cat a config file. You can strace a process. Every Linux debugging tool you've ever learned still works.
> With Talos? No SSH. No shell. No package manager. You debug via talosctl, which is genuinely good, but it's a learning curve. The first week feels alien. Especially if you've spent your career SSH-ing into boxes.
> [Brief pause, then]
> You'll get over it. The replace-don't-fix model means you debug less. But week one is real, and I'm not going to pretend it isn't.

**[7:30–8:30 — Storage, screencast]**

> [SCREEN: Longhorn UI showing PVCs]
> Storage works on both. Same options: Longhorn in-cluster, democratic-csi for ZFS-native PVCs, NFS for the brave.
> The Talos provider attaches a data disk to each worker automatically — one config knob, storage_disk_size, and Longhorn picks it up. With k3s-in-VM you wire that up by hand.
> Both reach the same destination. The Talos path has less wiring.

**[8:30–10:00 — The decision, face-cam with on-screen overlay]**

> [Face-cam, direct]
> So who should pick which?
> [Overlay: "Pick k3s-in-VM if..."]
> Pick k3s-in-a-VM if you want full shell access for educational reasons. If you genuinely enjoy managing OS-level details — cert rotation, package upgrades, the works. If you're single-node and staying single-node forever. Or if you're already a confident Linux admin and you don't want to learn a new mental model.
> [Overlay: "Pick Talos + Omni if..."]
> Pick Talos plus Omni if you want a real multi-node cluster that feels like one. If you want upgrades to be a button. If you want immutable, reproducible nodes so "configuration drift" stops being a thing. If you'll spend a week learning talosctl to save a year of day-2 ops.
> For most people watching this — especially if you lost your built-in TrueNAS Kubernetes in 25.04 — the Talos path is the upgrade, not the alternative.

**[10:00–10:30 — What I run, face-cam]**

> What I actually run? Talos plus Omni on a single TrueNAS SCALE 25.10 box. I kept the k3s VM cluster alive for six months specifically to write this comparison. It's off now. Not because k3s did anything wrong — it's a fine piece of software — but because every upgrade reminded me that I'd just rebuilt, by hand, the things Omni already does.

**[10:30–11:00 — CTA, face-cam]**

> Written comparison with the full table is linked in the description. Repo is linked. Install walkthrough video is linked.
> Next video on this channel is the TrueNAS versus Proxmox comparison — the harder decision. Subscribe so it shows up.
> Tell me what I got wrong in the comments. Issues over stars.

**[10:55–11:15 — End screen: V4 thumbnail + repo card]**

### Production notes
- This is the channel's first opinionated comparison. **Energy stays calm-confident.** No hype, no dunking. The audience can smell it.
- On-screen scoring overlays should appear at the moment you name a winner per category. Don't drop them all in a single chart at the end — viewer needs the per-section payoff.
- The "k3s wins debuggability" segment (6:30–7:30) is non-negotiable. It's what makes the rest of the video feel honest.
- Pre-cut a 30-second "best moments" reel for the YouTube Shorts companion drop. Cold open + "k3s wins debuggability" + "what I actually run" = good shorts montage.

---

## V4 — "Running Kubernetes on TrueNAS vs Proxmox: when each makes sense"

**Working title**: `TrueNAS vs Proxmox for homelab Kubernetes: when each one wins`
**Length target**: 10:00–12:00.
**Format**: face-cam open/close, architecture diagram overlays in middle, screencast cutaways to each environment.
**Thumbnail text**: "TRUENAS vs PROXMOX" + your face + small product logos.

### Title options

1. `TrueNAS vs Proxmox for homelab Kubernetes: when each one wins` ← SEO favorite
2. `Should you run Kubernetes on TrueNAS or Proxmox? Honest take.`
3. `One machine or two? The TrueNAS vs Proxmox decision for homelab K8s.`

### Description

```
Proxmox + TrueNAS is the well-trodden path for homelab Kubernetes. TrueNAS-only (via omni-infra-provider-truenas) is the path that didn't exist until recently. Both are valid. Here's how to pick.

Chapters:
00:00 — The two architectures, one diagram each
01:30 — Hardware count and power cost
02:30 — Hypervisor flexibility (Proxmox wins this one)
04:00 — Storage architecture — one stack vs two
05:30 — Failure blast radius
06:30 — Resource contention and sizing reality
07:30 — Network topology and storage latency
08:30 — The decision matrix
10:00 — What I actually run, and why

Links:
— Written companion: https://dev.to/cliftonz/<m2-truenas-vs-proxmox-slug>
— Provider repo: https://github.com/bearbinary/omni-infra-provider-truenas
— Install walkthrough video: <V2 URL>
— Talos + Omni vs k3s comparison: <V3 URL>

#Kubernetes #TrueNAS #Proxmox #Homelab #SelfHosted
```

### Script

**[0:00–0:30 — COLD OPEN, face-cam]**

> If you want Kubernetes in your homelab, there are two well-thought-out architectures: Proxmox plus TrueNAS as a storage neighbor, or TrueNAS-only with the provider I maintain hosting the VMs directly. Both are valid. I've run both. This video tells you which one is right for *your* setup.

**[0:30–1:00 — Setup, face-cam]**

> [Title card: "TrueNAS vs Proxmox for homelab Kubernetes"]
> I'm Zac. I built the TrueNAS-only path. That should tell you my bias up front. I'll still call out where Proxmox genuinely wins — and there are real categories.

**[1:00–1:30 — The two architectures, on-screen diagrams]**

> [SCREEN: Mermaid diagram, Proxmox + TrueNAS]
> Architecture one: Proxmox hosts the VMs, TrueNAS is a storage neighbor. K8s persistent volumes come off TrueNAS via NFS or iSCSI. Two boxes or two clearly separated VMs on one host.
> [SCREEN: Mermaid diagram, TrueNAS-only]
> Architecture two: TrueNAS hosts the VMs directly via the provider. VM disks are ZFS zvols on the same pool as your files. PVCs come from inside the cluster (Longhorn) or from TrueNAS-native ZFS (democratic-csi). One box.
> The core difference: in the Proxmox setup, TrueNAS is a neighbor. In the TrueNAS-only setup, TrueNAS *is* the hypervisor.

**[1:30–2:30 — Hardware count + power, face-cam with cost overlay]**

> [Face-cam]
> Hardware count.
> [Overlay: cost comparison table]
> Proxmox plus TrueNAS is two boxes — or, if you're crafty, one machine running both as nested setups, which I do not recommend. Real-world deployments are usually two boxes.
> Two boxes means roughly 150 to 300 watts at idle, two power bricks, two PSUs to fail, two BIOSes to patch, two boot orders to debug.
> TrueNAS-only is one machine. 50 to 120 watts idle. One thing to maintain. Your power bill drops. Your rack space drops. Your partner's patience holds out longer.
> If hardware sprawl is a real cost for you — power, space, money — TrueNAS-only wins this category outright.

**[2:30–4:00 — Hypervisor flexibility, Proxmox wins, screencast cutaway]**

> [SCREEN: Proxmox VM creation UI with PCI passthrough options visible]
> Here's where Proxmox wins decisively.
> Hypervisor flexibility. Proxmox is best-in-class at this. PCI passthrough that works first try. LXC containers alongside VMs. Live migration between Proxmox nodes. GPU passthrough that's actually documented.
> [SCREEN: TrueNAS Virtualization UI]
> TrueNAS does VMs, but VM hosting is a feature, not its purpose. The UI is functional but less flexible. No live migration. PCI passthrough exists but is less battle-tested. GPU passthrough is possible but you'll spend an evening on it.
> [Face-cam, honest]
> If you need any of that — Plex transcoding with iGPU passthrough, ML inference, dedicated GPU per VM — Proxmox is the answer. Don't fight TrueNAS for it. The hardware does the job. Use the tool designed for it.

**[4:00–5:30 — Storage architecture, face-cam with diagram]**

> [SCREEN: side-by-side storage diagrams]
> Storage.
> Proxmox plus TrueNAS is two storage stacks. Proxmox has its own VM disks — local LVM, ZFS-on-Proxmox, or Ceph. TrueNAS has its own ZFS for files and shares. K8s PVCs come from TrueNAS via NFS or iSCSI.
> Two systems to manage. Two snapshot policies. Two backup stories. Two places to check when something's wrong.
> TrueNAS-only is one storage stack. ZFS for VM disks, files, *and* PVCs. One snapshot policy. One backup story. One pool health to monitor.
> [Face-cam]
> This is the philosophical hook of the TrueNAS-only path. If you want ZFS as the single source of truth for everything in your homelab — files, VM disks, persistent volumes — TrueNAS-only is the pitch. If you want strict isolation between hypervisor storage and bulk storage, Proxmox plus TrueNAS gives you that.

**[5:30–6:30 — Failure blast radius, face-cam]**

> [Face-cam]
> Failure blast radius.
> Two boxes means independent failure domains. Proxmox dies, your cluster degrades but TrueNAS still serves your files. TrueNAS dies, the cluster keeps spinning but PVCs go offline.
> One box means one PSU, one reboot, one drive failure takes everything down at once.
> [Honest beat]
> For most homelabs, neither setup is genuinely HA. You're going to lose stuff during a power outage either way. Pick the failure model that matches the scenarios you actually care about. If "I can still stream movies while my cluster is down" matters to you, two boxes. If you'd just bring it all back up together anyway, one box is simpler.

**[6:30–7:30 — Resource contention, face-cam with sizing overlay]**

> [Overlay: sizing table]
> Resource contention.
> Proxmox plus TrueNAS: each box does one job. No contention.
> TrueNAS-only: your NAS is file serving *and* hosting K8s nodes. Over-provision the VMs and your file shares slow down during heavy cluster workloads.
> This sounds scary in theory. In practice, most homelab K8s workloads are bursty, not pegged. As long as you size honestly — leave 8 gigs of RAM for TrueNAS, watch your CPU steal time — it's fine.
> The trap is allocating like Proxmox would let you. Don't.

**[7:30–8:30 — Network topology, diagrams]**

> [SCREEN: network diagrams]
> Network topology.
> Proxmox plus TrueNAS: K8s nodes and storage cross your LAN to talk to each other. NFS or iSCSI traffic on your home network. Often a dedicated storage VLAN if you've planned for it.
> TrueNAS-only: K8s nodes and storage share a bridge on the same physical NIC. Storage traffic is essentially loopback — faster, no LAN congestion.
> [Face-cam]
> If you have a dedicated 10G storage network already, the Proxmox model is fine. If you don't, the TrueNAS-only model gives you better storage latency for free.

**[8:30–10:00 — Decision matrix, face-cam with on-screen table]**

> [Overlay: decision matrix from written post, line by line]
> So — who picks what.
> If you already own a TrueNAS and you don't have Proxmox, this is the path that didn't exist before. TrueNAS-only.
> If you already own a Proxmox and a separate TrueNAS for storage — stick with Proxmox. Use Sidero's PVE provider, treat TrueNAS as a storage neighbor.
> Building from scratch with budget? Proxmox plus TrueNAS gives you separation and capability.
> Building from scratch with tight budget? TrueNAS-only. One box, one bill, one thing to fix.
> Need GPU passthrough or PCI shenanigans? Proxmox. Don't fight it.
> Want ZFS as the single source of truth for everything? TrueNAS-only. That's the whole pitch.

**[10:00–10:45 — What I actually run, face-cam]**

> I ran the Proxmox plus TrueNAS split for about a year. I built the TrueNAS provider because the split felt like overkill for my actual workload — I wasn't using Proxmox's flexibility, I was just paying for it in hardware and complexity.
> Several months later, I'm TrueNAS-only and not looking back. The one case I'd reverse on: GPU-heavy workloads where PCI passthrough quality matters. For everything else, one box is the right answer for me.

**[10:45–11:30 — CTA, face-cam]**

> Written comparison is linked. Repo is linked. Install walkthrough video is linked.
> Next video on this channel: storage deep-dive — Longhorn versus democratic-csi versus NFS, what I actually pick and why. Subscribe so it shows up.
> Tell me what you'd do differently in the comments.

**[11:25–11:45 — End screen: M3 storage video + repo card]**

### Production notes
- Architecture diagrams (Mermaid → exported PNG → overlay) carry a lot of this video. **Get those clean before recording.**
- The "Proxmox wins" segments (2:30–4:00 + 5:30–6:30) need real conviction. If you sound dismissive there, the rest of the video stops being credible.
- Don't try to dunk on Proxmox. The audience overlaps. The honest take is "different optimizations, both valid."
- Decision matrix overlay at 8:30 should appear one row at a time as you read it, not all at once. Pacing matters.

---

## Cross-promotion plumbing (M2 specific)

After V3 + V4 are up:

- **Both M2 written posts** (`scratch_m2_talos_vs_k3s.md` + `scratch_m2_truenas_vs_proxmox.md`): replace each post's `<companion-video-URL>` placeholder with the live YouTube URL.
- **V2 video description**: add "Comparison videos: <V3 URL> | <V4 URL>" to the description.
- **V3 description**: link to V4 in the chapter description.
- **Reddit comment templates**: when "but what about Proxmox" or "but what about k3s" comes up on any thread for V1/V2, drop the relevant V3 or V4 link. Don't preemptively post these in r/Proxmox — that sub is touchy about competitor content.
- **LinkedIn drumbeat for M2**: weekly post #1 = "k3s vs Talos decision matrix," weekly post #2 = "Proxmox vs TrueNAS decision matrix," weekly post #3 = V3 clip, weekly post #4 = V4 clip.

---

## Open placeholders to fill before record

- Live V2 URL once published.
- Final dev.to slugs for both M2 written posts (replace `<m2-talos-vs-k3s-slug>` and `<m2-truenas-vs-proxmox-slug>` throughout).
- Mermaid diagrams exported as PNGs at 1920×1080 with high-contrast colors for screen-readable overlays.
