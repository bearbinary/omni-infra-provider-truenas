---
title: "I ran NFS, democratic-csi, and Longhorn in production for 6 months each. Here's what I picked and why."
published: false
description: "Three Kubernetes storage paths on TrueNAS, side by side. Real workloads, real failure modes, real opinions about which one wins for which job."
tags: kubernetes, truenas, longhorn, storage
cover_image: ""
series: "Self-hosted Kubernetes on TrueNAS"
---

**TL;DR — Three viable storage paths for Kubernetes on TrueNAS: NFS (simplest, worst), democratic-csi (TrueNAS-native ZFS, ZFS-snapshots-per-PVC), and Longhorn (in-cluster block storage). I've run all three in homelab production for at least six months each. This post is the honest comparison — what each gets right, where each falls apart, and what I run for which workload.**

I'm Zac Clifton. I maintain [`omni-infra-provider-truenas`](https://github.com/bearbinary/omni-infra-provider-truenas). After the storage video on the channel (V5), I got asked enough follow-up questions to justify a written companion with the long-form version. This is it.

For install context: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>). For everything else this post references (sizing, networking, upgrades), the [hub page](https://dev.to/cliftonz/<hub-page-slug>) links it all.

---

## The three paths in one paragraph each

**NFS**: TrueNAS exports an NFS share. Your Kubernetes nodes mount it. PersistentVolumes are files inside the share. Simplest possible setup. Five minutes of work.

**democratic-csi**: A CSI driver that talks to TrueNAS over its API or SSH and creates a ZFS dataset or zvol per PersistentVolumeClaim. Native ZFS snapshots, native ZFS quotas, native ZFS encryption. TrueNAS is the single source of truth for storage.

**Longhorn**: Block storage that runs *inside* the cluster, replicating volumes across worker-node data disks. No dependency on TrueNAS at runtime for storage I/O. CNCF-grade project with a polished UI.

---

## What each gets right

### NFS

The good things about NFS:

- **Universally understood.** Every Linux engineer has mounted an NFS share. Debugging is googleable.
- **Trivial backup.** The share is a directory tree. `rsync` it. Snapshot the underlying ZFS dataset. Done.
- **Cheap.** No additional services to run. No extra CPU/RAM on workers. TrueNAS already serves NFS.
- **Fits read-mostly workloads naturally.** Media libraries, document stores, any data that's mostly read and rarely written.

If your only Kubernetes storage need is "expose a directory my pods can read from," NFS is correct.

### democratic-csi

The good things about democratic-csi:

- **Every PVC is a ZFS dataset or zvol.** Real ZFS, with all its features. Snapshots, clones, encryption, quotas, compression — exposed as Kubernetes primitives.
- **ZFS-native backups.** `zfs send` to a backup server. Volume snapshot APIs in Kubernetes for crash-consistent point-in-time recovery.
- **TrueNAS is the source of truth.** If you want one system to be authoritative for all your data — files, VM disks, PVCs — democratic-csi delivers that.
- **iSCSI mode is fast.** For block-heavy workloads (databases especially), iSCSI from TrueNAS to the cluster is performant.

If your operational model is "ZFS first, Kubernetes second" — democratic-csi is the path.

### Longhorn

The good things about Longhorn:

- **Block storage performance.** Replicas are local to workers; reads are local-disk speed when the replica is co-located with the pod.
- **Independent failure domain.** Storage doesn't depend on TrueNAS at runtime. TrueNAS can reboot, your cluster's storage layer keeps responding.
- **CNCF-grade tooling.** The UI shows replica health, snapshot status, recurring jobs, disaster recovery flow. Easy to understand what's happening.
- **Backups to S3 or NFS** out of the box. Snapshot, backup, restore — all in the Longhorn UI.
- **Active development.** Real engineering effort from Rancher/SUSE behind it.

If you want Kubernetes-native block storage that doesn't care what's underneath it — Longhorn is the path.

---

## Where each falls apart

### NFS — the failure modes

- **File permissions.** Containers run as random UIDs. NFS shares expect specific UIDs. You spend a weekend on `subPath` and `fsGroup` magic, or you `chmod 777` your data, or you accept that some workloads just won't run.
- **Locking semantics.** NFSv4 locking works for one-pod-one-volume. For databases or multi-writer workloads, you'll see corruption or stalls under load.
- **No volume-snapshot integration.** You can snapshot the ZFS dataset under the share manually, but Kubernetes doesn't know. No `VolumeSnapshot` resource, no instant clone, no point-in-time restore through the standard Kubernetes APIs.
- **Operational sprawl.** Every workload's storage is its own NFS share (or its own subdirectory you have to coordinate). Ten workloads, ten shares, ten authorization rules, ten things to think about.
- **The database thing.** PostgreSQL on NFS will eventually corrupt or stall. MySQL too. SQLite too. Anything that fsyncs aggressively. Don't put a database on NFS. I cannot say this loudly enough.

NFS's job is read-mostly, single-writer, low-concurrency workloads. Press it past that and it fails in ways that look mysterious until you've been bitten enough times to recognize them.

### democratic-csi — the failure modes

- **Setup is the most involved of the three.** Helm chart, driver configuration, TrueNAS-side user permissions, SSH key auth or API tokens, plus a learning curve when something doesn't behave.
- **Performance bottlenecked by network.** Every read and every write goes from the worker to TrueNAS and back. Loopback bridge (in TrueNAS-only setups) helps. Separate boxes hurt. Either way, you're not getting local-disk speed.
- **The driver itself has corner cases.** I've had snapshot creation hang, dataset cleanup fail to find its target, and iSCSI sessions drop in ways that took hours to recover. Less mature than Longhorn or NFS, and the issue tracker shows it.
- **TrueNAS coupling cuts both ways.** If TrueNAS goes down or needs maintenance, your PVCs go down too. Your cluster's storage is hard-coupled to your NAS's uptime. With Longhorn you get a separation of failure domains; with democratic-csi you don't.

democratic-csi's job is "I want every PVC to be a first-class ZFS object." It's not the right path if you want clean Kubernetes-native storage that's independent of TrueNAS's state.

### Longhorn — the failure modes

- **Needs at least 3 workers for the default 3-replica setup.** Single-node or 2-node clusters can't use Longhorn well — you'd be running everything at replica-count-1, which defeats the point.
- **Cluster RAM and CPU overhead.** Longhorn isn't free. The manager, replicas, instance-managers, engine pods — all add up. On a 3-worker cluster you're spending maybe 1–2 GB of RAM total on Longhorn itself, which is fine on a 64 GB host and a problem on a 16 GB one.
- **Data disk sizing is its own decision.** When the worker data disks fill up, Longhorn rebalances if it can, fails if it can't. 100 GB per worker is my comfortable floor. 50 GB is too small for any non-trivial workload.
- **No ZFS-side snapshots of PVCs.** Longhorn snapshots are inside Longhorn, not visible to TrueNAS. If your backup story relies on `zfs send`, that doesn't reach Longhorn data.
- **Recovery from a wiped worker disk isn't automatic.** If you reprovision a worker through Omni without telling Longhorn, that node's replicas are lost. With 3 replicas you stay healthy but degraded — heal them before doing it again.
- **Performance is mediocre under heavy random I/O for some workloads.** It's good but not the best. If you're pegging IOPS on a database, you may notice.

Longhorn's job is "Kubernetes-native block storage, independent of underlying storage system." That's exactly what most workloads want. It's not what every workload wants.

---

## What I run for which workload

After running all three across the last 18 months in various combinations, here's my actual production layout. Use this as a starting point, not a prescription.

| Workload | Storage | Why |
|---|---|---|
| **Default StorageClass** | Longhorn | Block storage performance, independent failure domain, good UI. ~90% of my apps land here. |
| **Databases** (PostgreSQL, MySQL) | Longhorn | Block storage matters here. iSCSI via democratic-csi works too but Longhorn is simpler operationally. |
| **Plex / Jellyfin media library** | NFS off TrueNAS | Read-mostly, library already lives on TrueNAS, no point copying it into the cluster. |
| **Velero backup target** | NFS off TrueNAS | Backups are write-once, read-on-restore. Perfect NFS workload. |
| **Workloads needing per-PVC ZFS snapshots** | democratic-csi | Very rare in my setup. Velero + Restic to S3 gives me what I need without per-PVC ZFS coupling. |
| **CI scratch volumes / ephemeral data** | Longhorn | Default. Don't think about it. |

The high-level rule: **Longhorn is the default, NFS is for legitimate read-mostly workloads, democratic-csi is for the specific case where you really want ZFS as the single source of truth for PVC data**.

---

## What changed my mind over time

When I started, I assumed democratic-csi was obviously correct — "use the NAS as the storage, that's what it's for." That assumption didn't survive contact with real operational concerns:

- **Snapshot strategies are workload-specific.** Most of my apps don't benefit from per-PVC ZFS snapshots. They benefit from periodic application-level backups via Velero. ZFS snapshots are at the wrong layer.
- **Failure independence matters more than I expected.** When TrueNAS needs a reboot for upgrades or maintenance, Longhorn-backed apps keep responding (degraded, but up). democratic-csi-backed apps go down. For a homelab where I want maintenance windows that don't take everything down at once, that's huge.
- **The "TrueNAS as source of truth" pitch is real but narrower than I thought.** It matters for files (NAS data), it matters for VM disks (the provider already does this), and it doesn't really matter for ephemeral application state.

I migrated my default StorageClass to Longhorn around month 9 and never looked back. Longhorn isn't the right answer for everyone, but it's the right answer for more people than I initially believed.

---

## Honest constraints you should know

- **All three options are evolving.** Longhorn's release pace is fast (read changelogs before upgrading). democratic-csi's maintainership is smaller — the project is healthy but the bus factor is real. NFS doesn't evolve, which is both good and bad.
- **You can run more than one.** Nothing stops you from having multiple StorageClasses. I do. The "what I run" table above is exactly that — multiple classes for multiple workload types.
- **My setup is on a single TrueNAS-only host.** If you're running TrueNAS + Proxmox split, the storage path latency math is different — NFS over a real LAN is slower than my loopback-bridge case. Adjust accordingly.
- **None of this advice survives a 2-node cluster.** 2-node Kubernetes is a different beast. Longhorn won't work properly. democratic-csi works fine. NFS works fine. If you're on 2 nodes, plan around that constraint.

---

## What you'd do if starting from scratch today

If I were setting up a new TrueNAS-hosted Kubernetes cluster tomorrow with what I know now:

1. **Default StorageClass: Longhorn.** Attach a 100 GB data disk to each worker via the provider's `storage_disk_size`. Install Longhorn via Helm. Run 3 workers minimum. Don't think about it again until something breaks.
2. **Second StorageClass: NFS** for media-library-shaped workloads. Use it sparingly. Not for databases. Ever.
3. **Skip democratic-csi.** Unless you have a *specific* per-PVC-ZFS-snapshot requirement, the operational overhead doesn't pay off.
4. **Backup strategy: Velero + Restic + S3-compatible target.** Independent of which StorageClass each app uses. Application-level backups beat infrastructure-level backups for most homelab needs.

If you have the specific case democratic-csi is good at — sure, run it. But don't default to it just because it sounds neat. The defaults above will serve 90% of homelab Kubernetes clusters fine.

---

## Try it

- **Provider repo + install**: [github.com/bearbinary/omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas)
- **Companion video walkthrough** (V5 — same opinions, on-screen demos): [Storage for homelab Kubernetes: Longhorn vs democratic-csi vs NFS](#)
- **Hero install guide**: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>)
- **Storage guide on the repo** (configs + Helm values): [docs/storage.md](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/docs/storage.md)

If you run something I didn't cover — Rook/Ceph, OpenEBS, Portworx — and you'd argue it beats Longhorn on TrueNAS, I want to hear why. File an issue or find me on [LinkedIn](#).

---

**About the author**: Zac Clifton is an infrastructure engineer building tools for self-hosters and small teams. He maintains `omni-infra-provider-truenas` and writes about pragmatic homelab Kubernetes. Subscribe on [YouTube](#) for monthly deep-dives.
