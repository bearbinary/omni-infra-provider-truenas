---
title: "Talos + Omni on TrueNAS vs k3s in a TrueNAS VM: the honest comparison"
published: false
description: "TrueNAS killed built-in Kubernetes. Here are the two real paths back — and the tradeoffs nobody names."
tags: kubernetes, truenas, k3s, talos
cover_image: ""
series: "Self-hosted Kubernetes on TrueNAS"
---

**TL;DR — If you want Kubernetes on TrueNAS in 2026, you have two real paths: roll your own k3s inside a TrueNAS VM, or use the [Talos + Omni provider I maintain](https://github.com/bearbinary/omni-infra-provider-truenas) and let it manage VMs for you. Both work. They optimize for different things. This post names the tradeoffs honestly so you can pick the right one for your homelab.**

I'm Zac Clifton. I daily-drive the Talos + Omni path. I also kept a k3s-in-a-VM cluster running for six months alongside it specifically to write this comparison. This is what I actually learned.

For the canonical install guide for the Talos + Omni path, see [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>). This post is about *which* to pick, not *how* to install either one.

---

## First: what "k3s on TrueNAS" means in 2026

TrueNAS SCALE used to ship Kubernetes built-in. The "Apps" catalog through the Cobia and Dragonfish releases was a curated k3s with TrueChart-style apps on top. **That's gone.** In SCALE 25.04 ("Fangtooth"), TrueNAS replaced it with a Docker-based apps system. Single-node, container-orchestration-lite, not Kubernetes.

So when somebody says "k3s on TrueNAS" today, they mean one of three things:

| What they mean | What it actually is | Status |
|---|---|---|
| The old built-in TrueNAS K3s | Catalog-curated k3s tied to TrueNAS's old apps system | **Removed in 25.04** |
| The new TrueNAS Apps (Docker Compose) | Single-host Docker orchestration with a UI | Available, but **not Kubernetes** |
| Manual k3s installed inside a TrueNAS VM | k3s you install yourself in a VM on TrueNAS | The DIY path I'm comparing against |

This post compares **manual k3s in a TrueNAS VM** against **Talos + Omni + this provider**. The new Docker-based Apps system is a different tool entirely — if you just need to run a handful of containers and you don't want Kubernetes, use that and stop reading.

---

## The two paths in one paragraph each

### Path A: Manual k3s in a TrueNAS VM

You create a VM (or several) in TrueNAS's Virtualization UI. You boot Ubuntu or Debian. You SSH in and run `curl -sfL https://get.k3s.io | sh -`. You manage that cluster yourself — installs, upgrades, certs, OS patches, the whole stack. If you want HA, you stand up extra control planes manually. If you want to add a worker, you provision the VM, install k3s with the join token, and hope you got the cert SAN config right.

### Path B: Talos + Omni via [omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas)

You install the provider as an app on TrueNAS. You define a couple of "MachineClasses" in Omni — templates for VM size. You click Create Cluster in the Omni UI. The provider creates the VMs, boots them into Talos Linux (immutable, no SSH), and they auto-register with Omni. Upgrades, scaling, and config patches happen through Omni's web UI. You never SSH into a node — you can't, Talos doesn't have SSH.

---

## Where they actually differ

### Setup time

**k3s-in-VM**: 30 minutes for one node. Multiply by N for an N-node cluster. Each VM is a manual ritual — provision, boot, SSH, install, join. You can script it. You'll script it differently than the person next to you.

**Talos + Omni**: 5 minutes the first time, less every time after. The provider templates and Omni's cluster-create form mean defining a new cluster is "click click click" once the MachineClasses exist.

**Winner**: Talos + Omni, by an order of magnitude once you're past your first cluster.

### Day-2 operations

This is where the gap is real.

**k3s-in-VM**: You're responsible for the whole stack. Ubuntu security updates. k3s version bumps. kubelet certificate rotation. etcd backups. When something breaks at 11pm, you're SSHing into nodes and `journalctl`-ing.

**Talos + Omni**: Omni handles Talos OS upgrades and Kubernetes version bumps through the web UI. Talos is immutable — there's no security update treadmill, you replace nodes instead. etcd snapshots are a checkbox. When something breaks, you delete the node, the provider creates a fresh one, and 2 minutes later the cluster is healthy. You never debug a "snowflake" node because there are no snowflake nodes.

**Winner**: Talos + Omni. The mental model shift (replace, don't fix) is genuinely better.

### Resource footprint

**k3s-in-VM**: k3s is small. ~512 MB RAM for a single-node, more for multi-node. The OS underneath (Ubuntu/Debian) is the heavy part — call it 1–2 GB per VM.

**Talos + Omni**: Talos is minimal — no SSH daemon, no package manager, no shell. Memory footprint is 1–2 GB per node for the OS + Kubernetes components. Roughly comparable to Ubuntu + k3s.

**Winner**: Tie. Don't pick on this axis.

### Multi-node and HA

**k3s-in-VM**: Possible, but you're wiring it yourself. Embedded etcd vs external datastore is a real decision. Each control plane addition is manual. Worker scaling is manual.

**Talos + Omni**: Define replica count in the MachineSet. Bump from 1 to 3. The provider creates two new VMs, Omni adds them to the control plane, etcd rebalances. Same flow for workers.

**Winner**: Talos + Omni, decisively. This is what the stack is designed for.

### Storage integration

**k3s-in-VM**: You pick a CSI driver and wire it up by hand. Common picks: Longhorn (in-cluster), democratic-csi (TrueNAS-native via ZFS), nfs-subdir.

**Talos + Omni**: Same options, same tradeoffs — but the provider can attach a data disk to each worker automatically (`storage_disk_size`) for Longhorn. The Talos `UserVolumeConfig` plumbing happens for you. One config knob away from working PVCs.

**Winner**: Talos + Omni, mildly. Same drivers underneath, less wiring.

### Debuggability

This is the cleanest argument *for* k3s-in-VM.

**k3s-in-VM**: You can SSH in. You can `cat` a config file. You can `strace` a process. When something is weird, you have every tool a Linux box gives you.

**Talos + Omni**: No SSH. No shell. You debug via `talosctl` (which is genuinely good, but it's a learning curve) and via Omni's UI logs. If you've spent your career SSH-ing into boxes, this feels alien. You'll get over it in a week, but the first week is real.

**Winner**: k3s-in-VM, for the first week of your journey. After that, Talos.

### Upgrade story

**k3s-in-VM**: `apt upgrade` for the OS. Re-run the k3s install script for the runtime. Cross your fingers. Repeat per node. You will eventually break something.

**Talos + Omni**: Click "Upgrade Talos" in Omni → it does a rolling upgrade, one node at a time, with health checks between. Click "Upgrade Kubernetes" separately. Same flow. If a node fails to come back, Omni stops and tells you. If you want to revert, you don't — you replace the node with the previous Talos version (immutable, reproducible).

**Winner**: Talos + Omni. The replace-not-upgrade philosophy ends an entire category of bugs.

### Cost

Both paths are free at the homelab scale. Omni has a free tier for personal use. The provider is MIT licensed.

**Winner**: Tie.

### Community + support

**k3s-in-VM**: Rancher's k3s community is large. Documentation is plentiful. Stack Overflow has answers.

**Talos + Omni**: Sidero Labs is smaller, but the community is technical and responsive. Talos's docs are excellent. Omni's docs are good. The community Slack is one of the best I've been in. The provider has an issues-only model — file an issue, I respond.

**Winner**: k3s-in-VM has more answers on Google. Talos + Omni has better-quality answers in its community channels.

---

## The decision

Pick **manual k3s in a TrueNAS VM** if:

- You want full Linux shell access to your nodes for educational reasons.
- You explicitly enjoy managing OS-level details (cert rotation, package upgrades, kernel pinning).
- You're running a single-node cluster and don't see yourself going multi-node ever.
- You're already a confident Ubuntu/Debian admin and don't want to learn a new mental model.

Pick **Talos + Omni + this provider** if:

- You want a real multi-node cluster and you want it to feel that way.
- You want upgrades to be a button, not a weekend.
- You want immutable, reproducible nodes so "configuration drift" is not a phrase that exists in your life.
- You're willing to spend a week learning `talosctl` and the Omni UI to save a year of day-2 ops.

For most people reading this — and especially anyone who lost their built-in TrueNAS Kubernetes in 25.04 — the Talos + Omni path is the upgrade, not the alternative.

---

## What I actually run

For full disclosure: I run Talos + Omni as my homelab daily driver on a 12-core TrueNAS SCALE 25.10 box. The k3s-in-VM cluster I kept alongside for this comparison is now turned off. Not because k3s did anything wrong — it's a fine piece of software — but because every upgrade and every new workload reminded me that I'd just rebuilt, by hand, the things Omni already does.

If you're still running the manual path because that's where you started, no judgment. If you're picking from scratch in 2026, this is the recommendation.

---

## Try it

- **Provider repo + install**: [github.com/bearbinary/omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas)
- **Canonical install guide**: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>)
- **Origin story** (why this provider exists): [TrueNAS killed Kubernetes — so I brought it back](https://dev.to/cliftonz/truenas-killed-kubernetes-so-i-brought-it-back-4n7h)
- **Companion YouTube walkthrough**: [Talos + Omni vs k3s on TrueNAS — honest comparison](#)

If you've run both and disagree with any of this, I want to hear it. Open an issue on the repo or find me on [LinkedIn](#).

---

**About the author**: Zac Clifton is an infrastructure engineer building tools for self-hosters and small teams. He maintains `omni-infra-provider-truenas` and writes about pragmatic homelab Kubernetes. Subscribe on [YouTube](#) for monthly deep-dives on Talos, Omni, TrueNAS, and the parts of self-hosted infra nobody else is writing about.
