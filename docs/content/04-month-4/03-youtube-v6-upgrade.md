# YouTube Script: V6 — Upgrading Talos on TrueNAS Live (M4)

The upgrade video. This is the one viewers come back to whenever they're about to bump versions. Treat it as evergreen reference content with a real-time live-walkthrough framing.

**Channel conventions**: same as V1–V5 (cold open, lower-third on first appearance, 20s end screen, pinned comment).

**Recording window**: align with a real upgrade you were planning to do anyway. The video is more credible if the cluster on screen is your actual homelab, not a throwaway demo cluster.

---

## V6 — "Upgrading Talos on TrueNAS live — what I do, what I check, what I don't do"

**Working title**: `Upgrading Talos Kubernetes on TrueNAS — live, real cluster, no edits`
**Length target**: 13:00–16:00.
**Format**: face-cam open/close, full screencast through the upgrade flow, occasional face-cam reaction inserts during waits. Real cluster on screen.
**Thumbnail text**: "UPGRADING TALOS LIVE" + face + Omni logo + Talos logo.

### Title options

1. `Upgrading Talos Kubernetes on TrueNAS — live, real cluster, no edits` ← SEO favorite
2. `Watch me upgrade my homelab Kubernetes cluster in real time`
3. `Zero-downtime Talos + Omni upgrade on TrueNAS — the actual playbook`

### Description

```
Upgrading a Talos cluster managed by Omni on TrueNAS, live on my actual homelab cluster. Pre-flight ritual, the upgrade itself, and what to do when something stalls. No edits to hide a "happy path" — what you see is what happens.

Chapters:
00:00 — What we're upgrading and why
01:00 — The three layers (Talos, Kubernetes, the provider)
02:00 — Pre-flight ritual — the 10-minute checklist
05:30 — Taking the etcd + ZFS snapshots
07:30 — The Talos rolling upgrade
10:30 — The Kubernetes upgrade
12:30 — What happens when a node stalls
14:00 — Post-upgrade verification + snapshot cleanup

Links:
— Upgrade playbook (the written companion): https://dev.to/cliftonz/<upgrade-post-slug>
— Provider repo: https://github.com/bearbinary/omni-infra-provider-truenas
— Hero install guide: https://dev.to/cliftonz/<hero-post-slug>
— Sizing post: https://dev.to/cliftonz/<sizing-post-slug>

#Kubernetes #Talos #TrueNAS #Omni #Homelab #SelfHosted
```

### Script

**[0:00–0:30 — COLD OPEN, face-cam]**

> I'm about to upgrade my homelab Kubernetes cluster. Live. Real cluster, real workloads, no edits. If something breaks, you'll see me debug it. If it doesn't, you'll see the boring version — which is what you want. Either way, by the end of this video you'll know exactly what I do every time I do this.

**[0:30–1:00 — Title card + intro tag]**

> [Title card: "Upgrading Talos on TrueNAS — live"]
> I'm Zac. I maintain the open-source Omni provider that runs this whole stack. Today I'm bumping Talos from one minor version to the next and Kubernetes alongside. If you're new to the channel, watch the install walkthrough first — link below. This video assumes you already have a cluster.

**[1:00–2:00 — Three layers, face-cam with on-screen table]**

> [SCREEN: 3-row table]
> Three layers we can upgrade independently. Know which one you're touching.
> Talos OS — the Linux image each VM boots into. Upgraded through Omni's UI. Rolling, one node at a time.
> Kubernetes itself — apiserver, etcd, kubelet versions. Also through Omni's UI. Also rolling.
> The provider — the Go binary that creates VMs on TrueNAS. Restart the container with a new image tag. Doesn't touch running VMs.
> Today I'm doing Talos plus Kubernetes. I'm not bumping the provider in the same window. Bundling those is just stacking failure modes.

**[2:00–5:30 — Pre-flight checklist, screencast through each step]**

> [Face-cam, leaning in]
> Pre-flight ritual. This is the part most tutorials skip. Don't skip it.
> [Terminal: kubectl get nodes]
> Step one — verify every node is ready. Mine are. If you've got a NotReady node, fix it before you upgrade.
> [Terminal: kubectl get pods -A | grep -v Running | grep -v Completed]
> Step two — verify no pods are stuck. Anything in CrashLoopBackOff or Pending is a pre-existing problem and it's going to get worse during the upgrade roll.
> [Browser: Talos release notes]
> Step three — read the release notes. Both Talos and Kubernetes. Out loud if it helps. Breaking changes hide here.
> [Browser: Kubernetes release notes, deprecation section]
> [Terminal: kubectl get deployments -A and grep replicas: 1]
> Step four — check what's running on a single replica. Anything single-replica has brief downtime when its node reboots. Fine if you accept it. Surprising if you didn't realize it.
> [Omni UI: cluster page]
> Step five — write down the current Talos and Kubernetes versions. We'll want them later when we write "what changed."

**[5:30–7:30 — Snapshots, screencast]**

> [Omni UI: backups section]
> Etcd snapshot through Omni. This is the only snapshot that matters if Kubernetes goes sideways.
> [Click snapshot, watch timestamp]
> Done. Note the timestamp — we want to confirm this is from right now, not a stale auto-snapshot.
> [Terminal: TrueNAS host]
> Now the ZFS side. I'm SSHed into the TrueNAS host.
> [Terminal: zfs list -t volume -r tank | grep omni-vms]
> All my VM zvols.
> [Terminal: zfs snapshot -r tank/omni-vms@pre-upgrade-$(date +%Y%m%d-%H%M)]
> Recursive snapshot of every VM disk with a timestamp tag.
> [Face-cam reaction]
> ZFS snapshots are cheap. I take them every time. I almost never use them. The one time I do, they save my weekend.

**[7:30–10:30 — Talos rolling upgrade, screencast]**

> [Omni UI: Cluster > Upgrades]
> Omni UI. Cluster. Upgrades. Upgrade Talos.
> [Select target version]
> Target version selected. Confirm.
> [Watch the rolling upgrade view]
> Now we watch. One node at a time — drain, reboot, rejoin, health check, next.
> [Time-lapse, ~45 seconds compressing real time]
> Five-node cluster, roughly 25 minutes end to end. I don't touch anything during the roll.
> [Face-cam reaction insert during the wait]
> The most important rule of an upgrade: when it's running, don't help. Don't kubectl drain. Don't restart pods. Don't poke the cluster. Omni knows what it's doing. Let it.
> [Back to screencast: upgrade complete]
> Done. All nodes on the new Talos version. Let's verify.
> [Terminal: omnictl talosctl version, kubectl get nodes]
> Version numbers match. Nodes all Ready.

**[10:30–12:30 — Kubernetes upgrade, screencast]**

> [Omni UI: Upgrade Kubernetes]
> Same pattern. Upgrades. Upgrade Kubernetes. Target version.
> [Face-cam, brief]
> One Kubernetes-specific note. Omni only lets you skip one minor at a time. That's correct. Don't fight it. Two-version jumps on Kubernetes are not supported and will break you.
> [Confirm, watch the roll]
> Same rolling pattern. Same waiting game.
> [Time-lapse]
> [Verify]
> [Terminal: kubectl version]
> Done.

**[12:30–14:00 — When a node stalls, face-cam with screencast cutaway]**

> [Face-cam, direct]
> The case I haven't shown — what to do when something stalls.
> [SCREEN: Omni UI showing a paused upgrade — pre-recorded from a previous incident, labeled as such]
> When Omni pauses a rolling upgrade with no obvious reason, 90% of the time it's a PodDisruptionBudget on one of your workloads. Omni asks Kubernetes to drain a node. Kubernetes refuses because draining would violate a PDB.
> [Terminal: kubectl get pdb -A]
> Find the PDB. Either delete it temporarily or scale the workload to satisfy it.
> [Face-cam]
> The other common stall: a node taking forever to come back. That's almost always etcd taking forever because the ZFS pool is slow. Look at etcd logs through talosctl. If you see slow-fsync warnings, the node will come up — give it five more minutes. If it doesn't, you've hit the HDD-pool-no-SLOG trap. Reset the node, fix the pool, try again.

**[14:00–15:30 — Post-upgrade verification + cleanup, screencast]**

> [Terminal: kubectl get nodes -o wide]
> Post-upgrade. All nodes on the new versions, all Ready.
> [Terminal: kubectl get pods -A | grep -v Running | grep -v Completed]
> Nothing weird in the pod list.
> [Terminal: omnictl get cluster homelab -o yaml | grep version]
> Versions match. Conditions block is clean.
> [Face-cam]
> Now I let it bake. One full day of normal operation before I delete the pre-upgrade snapshots. If something subtle broke during the upgrade, I want a path back.
> [Terminal: zfs list -t snapshot ... after 24h]
> Tomorrow I'll come back and run zfs destroy on the snapshots.

**[15:30–16:00 — CTA, face-cam]**

> Full playbook is the written companion — linked in description. Repo is linked. Install guide is linked.
> Next video on this channel — the user case study. How somebody else is running this stack in their homelab. Subscribe so it shows up.
> Tell me how your last upgrade went in the comments. I read them.

**[15:55–16:15 — End screen: M5 case-study video + repo card]**

### Production notes
- **Real cluster only.** The credibility of this video is "Zac is doing this on his actual setup right now." A demo cluster loses that.
- **Don't fake a stall.** The 12:30–14:00 segment showing a paused upgrade should be clearly labeled as pre-recorded from a previous incident. The audience will see through a staged fake.
- **Time-lapse the waits.** A 25-minute real-time wait is the wrong pacing. Two 30-second time-lapses with clear "X minutes have passed" overlays work.
- **The "don't help during a roll" beat is critical.** It's the single most useful piece of advice in the video. Make sure it lands.

---

## Cross-promotion plumbing

- **Upgrade post** (`04-month-4/01-upgrade-post.md`): replace `[Upgrading Talos on TrueNAS live](#)` with V6 URL.
- **Hero post**: in the Day-2 ops section, add "Video walkthrough of an actual upgrade: <V6 URL>".
- **V2 description**: add "When you're ready to upgrade: <V6 URL>" to the "What's next" section.
- **Reddit / Talos Slack**: when upgrade questions come up on V1/V2 cross-posts or on M4 upgrade-post threads, link V6 in replies.
- **LinkedIn drumbeat M4**: weekly post about the "don't help during a roll" rule, the "wait 24 hours before deleting snapshots" rule, V6 clip drop, "what compounded after upgrading X times" reflection.

---

## Open placeholders

- Live URLs for upgrade post, hero post, sizing post (replace slugs).
- The pre-recorded stall footage for 12:30–14:00 — if you don't have one, omit that segment and the video drops to ~14 min total.
- Confirm your actual upgrade window aligns with the recording session. Don't fake the workflow.
