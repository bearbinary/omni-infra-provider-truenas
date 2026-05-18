---
title: "Upgrading a Talos cluster on TrueNAS via Omni — without breaking ZFS"
published: false
description: "The end-to-end upgrade playbook: pre-flight checks, etcd snapshots, ZFS-aware rollout, what to do when Omni stalls, and the replace-don't-mutate rule."
tags: kubernetes, talos, truenas, homelab
cover_image: ""
series: "Self-hosted Kubernetes on TrueNAS"
---

**TL;DR — Upgrading a Talos cluster managed by Omni on a TrueNAS host is usually a one-button operation. The cases where it isn't all come down to two things: ZFS state you forgot about, and the replace-don't-mutate rule that Talos enforces. This post is the playbook I run on my own cluster, including the pre-flight ritual, the snapshot strategy that's actually useful, and what to do when Omni's rolling upgrade stalls.**

I'm Zac Clifton. I maintain [`omni-infra-provider-truenas`](https://github.com/bearbinary/omni-infra-provider-truenas) and I've done this upgrade enough times to have made every mistake worth making. This is what I do now.

For setup, see the [hero install guide](https://dev.to/cliftonz/<hero-post-slug>). For sizing decisions before an upgrade, see the [sizing post](https://dev.to/cliftonz/<sizing-post-slug>). This post assumes you have a running cluster and are about to bump Talos, Kubernetes, or the provider itself.

---

## What's actually getting upgraded

Three layers, each with its own cadence and its own failure mode. Know which one you're touching before you start.

| Layer | What it is | How you upgrade | Blast radius if it fails |
|---|---|---|---|
| **Talos OS** | The Linux image each VM boots into | Omni UI → cluster → Upgrade Talos | One node at a time, rolling. Cluster stays up if you have HA CP. |
| **Kubernetes** | apiserver, etcd, controller-manager, scheduler, kubelet versions | Omni UI → cluster → Upgrade Kubernetes | One node at a time, rolling. Same story as above. |
| **The provider** | The Go binary that creates VMs on TrueNAS | New image tag → restart provider container | No running VM is touched. New `MachineRequest`s use the new code. |

You can upgrade these independently. You usually want to — bundling Talos + Kubernetes in the same maintenance window is fine, but bundling the provider with either is just stacking failure modes.

---

## Pre-flight ritual (do this every time)

This is the 10-minute checklist I run before any upgrade. Skipping it is how you turn a 15-minute upgrade into a Saturday.

### 1. Read the release notes — actually read them

Talos has breaking changes in some minor releases. Kubernetes has API deprecations in every minor release. The provider has shipped breaking changes at v0.15 (VM naming) and v0.16 (root disk floor) — assume there's one in your target version too.

- Talos: [github.com/siderolabs/talos/releases](https://github.com/siderolabs/talos/releases)
- Kubernetes: [kubernetes.io/releases](https://kubernetes.io/releases/)
- Provider: [docs/upgrading.md](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/docs/upgrading.md)

### 2. Snapshot etcd through Omni

This is the only snapshot that matters if Kubernetes goes sideways. Take it via Omni's UI → Cluster → Backups. Verify the snapshot timestamp matches "right now."

### 3. Snapshot the VM zvols on TrueNAS

ZFS snapshots are cheap. Take them anyway.

```bash
# On the TrueNAS host
zfs list -t volume -r tank | grep omni-vms
# Then for each control-plane VM:
zfs snapshot tank/omni-vms/<vm-name>@pre-upgrade-$(date +%Y%m%d-%H%M)
```

These give you a "the host knew the disk in this state" rollback, separate from etcd. You'll almost never use them — but if a Talos upgrade hard-bricks a node, you can `zfs rollback` instead of re-bootstrapping.

For workers running Longhorn, the worker zvol snapshot is interesting *but* the actual storage volumes are inside Longhorn replicas. The Longhorn data on each worker survives a worker rebuild as long as you have ≥3 replicas. If you're on 1 replica, snapshot the worker zvols.

### 4. Check etcd disk timing on the control plane

If you're on an HDD pool without an SLOG, etcd might be running at the edge. A rolling upgrade adds load. Verify timing first:

```bash
omnictl --cluster <name> talosctl logs etcd | grep -iE "took too long|slow" | tail -20
```

If you see slow-fsync warnings in the last week, **don't upgrade today**. Fix the storage first or apply the HDD timeout patch (see [sizing post](https://dev.to/cliftonz/<sizing-post-slug>)).

### 5. Verify cluster health

```bash
kubectl get nodes
kubectl get pods -A --field-selector=status.phase!=Running | grep -v Completed
kubectl get events --sort-by=.lastTimestamp -A | tail -20
```

Pods stuck in CrashLoopBackOff or pending are pre-existing problems, and they'll get worse during an upgrade roll. Fix them first.

### 6. Confirm replica count for HA workloads

```bash
kubectl get deployments -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,REPLICAS:.spec.replicas | sort -k3
```

Anything with `replicas: 1` will have brief downtime when its node reboots during the upgrade. That's fine if you accept it. It's not fine if you didn't realize it.

### 7. Note the current versions

```bash
kubectl version
omnictl --cluster <name> talosctl version --nodes <CP-node-IP>
```

You'll want these numbers when you write "what changed" in your post-upgrade notes.

---

## The actual upgrade

### Talos OS upgrade — rolling

1. **Omni UI** → Cluster → Upgrades → Upgrade Talos.
2. Pick the target version. Confirm.
3. Watch the rolling upgrade in the Cluster view. One node at a time:
   - Node drains (workloads move).
   - Node reboots into the new Talos version.
   - Node rejoins the cluster.
   - Health check passes.
   - Omni moves to the next node.

A 5-node cluster takes ~25 minutes end to end. Don't touch anything during the roll.

**If a node fails to come back**: Omni stops the roll and shows you which node. Click into the node, look at the logs in Omni. 90% of the time it's a Talos bug in the new version (recently rare) or a config patch incompatibility. Roll back that one node:

- Omni UI → Cluster → Machines → [stuck node] → Reset → re-provision against the *previous* Talos version's MachineClass.
- Then file an issue on Talos with the logs.

### Kubernetes upgrade — separate, similar flow

1. **Omni UI** → Cluster → Upgrades → Upgrade Kubernetes.
2. Pick the target Kubernetes version. Omni only lets you skip one minor at a time (this is correct — don't fight it).
3. Confirm. Watch the rolling upgrade.

Same drill as Talos. Same recovery path if a node stalls.

**One Kubernetes-specific gotcha**: deprecated API versions. If you have manifests using a version that was removed in the target Kubernetes, they break *after* the upgrade rather than during. Run `kubectl deprecations` (or the official deprecation check) before pulling the trigger.

### Provider upgrade — careful with breaking changes

The provider has shipped breaking changes. Always read the version notes in [`docs/upgrading.md`](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/docs/upgrading.md) before bumping. Recent examples:

- **v0.15 namespaced VM names** — `omni_<requestID>` → `omni_<providerID>_<requestID>`. Existing VMs from v0.14 are not adopted by v0.15. Drain the MachineSet to zero before upgrading.
- **v0.16 raised the root disk floor to 20 GiB**. Any MachineClass with `disk_size < 20` won't reconcile. Fix the class before upgrading.
- **v0.16.1 removed `addresses`/`gateway` from `additional_nics[]`**. Static-pin via DHCP reservation on your router, keyed off the deterministic MAC the provider logs.

The pattern for any provider breaking change:

1. Audit your MachineClasses against the new schema.
2. Drain the affected MachineSets (scale to zero, wait for VMs to deprovision).
3. Upgrade the provider image.
4. Scale MachineSets back up.

The provider does not retroactively mutate running VMs. That's a feature, not a bug — see the next section.

---

## The replace-don't-mutate rule

Talos is immutable by design. That has a specific consequence for upgrades that's worth naming explicitly.

**You do not edit a running Talos node's configuration to "upgrade" it.** You replace it.

What this means in practice:

- A MachineClass change does *not* roll out to existing VMs. The new VM you provision against that class will use the new config. Existing VMs are unchanged.
- To "apply" a MachineClass change to a running cluster, you reprovision: scale the MachineSet down by 1, scale it back up by 1, repeat. Omni handles the rolling part.
- Config patches in Omni *do* roll out to existing nodes — they're additive and Talos applies them in-place. MachineClass changes are *structural* (different disk, different CPU) and require fresh VMs.

The rule of thumb: **anything that changes the VM's hardware shape is a replace operation. Anything that changes the cluster's software config is a roll operation.**

---

## When Omni's upgrade stalls

The two common cases I've hit.

### Case 1: A control-plane node is taking forever to come back

Symptom: Omni says "upgrading CP-1," it's been 15 minutes, the node hasn't rejoined.

Diagnostic:

```bash
omnictl --cluster <name> talosctl logs --nodes <CP-node-IP> kubelet
omnictl --cluster <name> talosctl logs --nodes <CP-node-IP> etcd
```

90% of the time it's etcd taking forever because the ZFS pool is slow. Look for `apply request took too long` or `slow fdatasync`. If you see them, the CP eventually comes up — wait 5 more minutes. If it doesn't, you've hit etcd's heartbeat timeout and the node will never converge — apply the HDD timeout patch and reboot the node.

### Case 2: Omni paused mid-roll with no obvious reason

Symptom: the upgrade UI shows a pause icon. No error message.

Diagnostic: look at the cluster status in `omnictl get cluster <name> -o yaml`. The conditions block usually names the actual stop reason — most often a node taint or an unsatisfiable PDB (PodDisruptionBudget). Find the offending workload (`kubectl get pdb -A`) and either delete the PDB temporarily or scale the workload to satisfy it.

This is a Kubernetes problem, not a Talos or Omni problem — and it'll bite you on any rolling-upgrade flow on any cluster.

---

## ZFS during the upgrade

A few ZFS-specific things to think about.

### Snapshot retention

Don't accumulate pre-upgrade snapshots forever. After a week of clean operation post-upgrade, delete them:

```bash
zfs list -t snapshot -r tank/omni-vms | grep pre-upgrade
zfs destroy -r tank/omni-vms/<vm-name>@pre-upgrade-<date>
```

Snapshots block ZFS from freeing space when zvols are deleted, which matters more than people think during reprovision cycles.

### zvol size persistence

If the upgrade involves a MachineClass change that bumps `disk_size`, the new VMs get the new size. The old VMs keep their old size. Don't try to resize zvols manually under a running Talos node — Talos doesn't grow the root partition on boot, and you'll be doing manual partition surgery for no reward. Just reprovision.

### Pool free space during reprovision

A worker reprovision creates a new VM (and its zvol) before destroying the old one — there's a brief window where both exist. If your pool is tight on free space, this will fail. Check before you start:

```bash
zpool list tank
```

Keep at least 20% free at all times. ZFS performance degrades sharply above 80% usage, and you don't want to find out during an upgrade.

---

## What I actually do, in order

For reference, here's the literal sequence I run on my own cluster for a Talos + Kubernetes minor bump:

1. Read release notes for both, twice.
2. `kubectl get nodes && kubectl get pods -A | grep -v Running | grep -v Completed`. Fix anything weird.
3. Take an etcd snapshot via Omni UI.
4. `zfs snapshot -r tank/omni-vms@pre-upgrade-$(date +%Y%m%d-%H%M)`.
5. `omnictl get cluster homelab -o yaml | grep -A2 version` — note current versions.
6. Omni UI → Upgrade Talos → target version → confirm.
7. Wait. (~25 minutes for 5 nodes.)
8. Verify all nodes ready, all pods running.
9. Omni UI → Upgrade Kubernetes → target version → confirm.
10. Wait again.
11. Verify again.
12. One full day of running before deleting the pre-upgrade ZFS snapshots.
13. Update my homelab notes with the new versions and any quirks I hit.

End to end: ~1 hour of active attention plus a day of waiting.

---

## Try it

- **Provider repo + install**: [github.com/bearbinary/omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas)
- **Hero install guide**: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>)
- **Companion video**: [Upgrading Talos on TrueNAS live](#) (V6 — drops within the week of this post)
- **Sizing companion**: [Sizing Talos control planes on TrueNAS](https://dev.to/cliftonz/<sizing-post-slug>)

Hit something this post doesn't cover? File an issue on the repo — that's how the next version of this doc gets written.

---

**About the author**: Zac Clifton is an infrastructure engineer building tools for self-hosters and small teams. He maintains `omni-infra-provider-truenas` and writes about pragmatic homelab Kubernetes. Subscribe on [YouTube](#) for monthly deep-dives.
