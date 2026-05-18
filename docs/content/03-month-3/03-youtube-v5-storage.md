# YouTube Script: V5 — Storage Deep-Dive (M3)

The opinionated storage video. This is the one viewers will pre-search for ("kubernetes truenas storage") and the one I'll get DM'd about most. Get the take crisp, don't hedge.

**Channel conventions**: same as V1–V4 (cold open, lower-third on first appearance, 20s end screen, pinned comment).

**Recording order in M3**: V5 ships ~mid-month, after the sizing post drops on dev.to.

---

## V5 — "Storage for homelab Kubernetes: Longhorn vs democratic-csi vs NFS"

**Working title**: `Kubernetes storage on TrueNAS: Longhorn vs democratic-csi vs NFS — what I pick and why`
**Length target**: 11:00–13:00.
**Format**: face-cam open/close, screencast through each storage option in TrueNAS + cluster, on-screen pros/cons overlays per option.
**Thumbnail text**: "K8s STORAGE on TRUENAS" + face + small Longhorn/democratic-csi/NFS logos.

### Title options

1. `Kubernetes storage on TrueNAS: Longhorn vs democratic-csi vs NFS — what I pick and why` ← SEO favorite
2. `Stop using NFS for your homelab Kubernetes. Here's what to use instead.` ← curiosity
3. `Three ways to do storage on TrueNAS Kubernetes — ranked.`

### Description

```
Three viable storage paths for Kubernetes on TrueNAS — Longhorn, democratic-csi, and NFS. They optimize for different things. This video shows how each one works, the failure modes I've hit with each, and which one I run for which workloads.

Chapters:
00:00 — Why storage is the question I get most
00:45 — The three options
02:00 — Option 1: NFS (and why I don't recommend it)
04:00 — Option 2: democratic-csi (the TrueNAS-native path)
07:00 — Option 3: Longhorn (the in-cluster path)
10:00 — What I actually run for which workloads
12:00 — Quick storage gotchas

Links:
— Storage guide (the written reference): https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/docs/storage.md
— Provider repo: https://github.com/bearbinary/omni-infra-provider-truenas
— Install guide: https://dev.to/cliftonz/<hero-post-slug>
— Sizing post (companion in M3): https://dev.to/cliftonz/<sizing-post-slug>

#Kubernetes #TrueNAS #Longhorn #Storage #Homelab #SelfHosted
```

### Script

**[0:00–0:30 — COLD OPEN, face-cam]**

> You set up a Kubernetes cluster on your TrueNAS box. You deploy your first stateful app. You hit a wall: where does the data go? Three real options. Each one has a different failure mode. This video ranks them, names the tradeoffs, and tells you which one I'd actually pick for which workload.

**[0:30–0:45 — Title card + intro tag]**

> [Title card: "Storage for Kubernetes on TrueNAS"]
> I'm Zac. I maintain the open-source Omni provider that makes this whole stack work. I've run all three of these in anger. None of them are bad. They're just for different things.

**[0:45–2:00 — Three options overview, face-cam with on-screen logos]**

> [SCREEN: three logos side by side — NFS, democratic-csi, Longhorn]
> Three paths, in order from "easiest to set up" to "best for production-ish":
> One — NFS. TrueNAS exports an NFS share, your pods mount it. Simplest possible setup.
> Two — democratic-csi. A CSI driver that talks to TrueNAS over its API or SSH and creates ZFS datasets or zvols per persistent volume. Native ZFS snapshots, native ZFS quotas.
> Three — Longhorn. Block storage that runs *inside* your cluster, replicating across worker data disks. No dependency on TrueNAS for storage at runtime.
> All three work. They optimize for different things. Let's go through each.

**[2:00–4:00 — Option 1: NFS, screencast]**

> [TrueNAS UI: Shares > NFS]
> NFS is the "I just want it to work" path. Create a share on TrueNAS, define a PersistentVolume in your cluster that mounts it, done.
> [Cluster terminal: PV YAML with NFS server]
> Five minutes of work. Persistent volumes are files in the NFS share. Easy to back up. Easy to inspect — `ls` the share from any machine.
> [Face-cam, leaning in]
> Here's why I don't recommend it for most use cases.
> [Overlay: "NFS problems"]
> One — file permissions. Containers run as random UIDs, NFS shares expect specific UIDs. You end up `chmod 777`-ing your data or you spend a weekend on `subPath` and `fsGroup` magic.
> Two — locking and concurrency. NFSv4 locking is fine for one-pod-one-volume. For databases or shared-write workloads, you'll see corruption or stalls.
> Three — no snapshot integration. You can snapshot the ZFS dataset under the share manually, but Kubernetes doesn't know about it. No volume snapshot resources, no instant rollback.
> Four — operational sprawl. Every share is a separate config item in TrueNAS. Ten apps, ten shares. Doesn't scale.
> NFS is fine for one read-mostly workload — like a Plex media library mount. It's not fine as the default storage class for your cluster.

**[4:00–7:00 — Option 2: democratic-csi, screencast]**

> [SCREEN: democratic-csi GitHub README]
> democratic-csi is the TrueNAS-native CSI driver. It creates a ZFS dataset or zvol per PVC. NFS-mode or iSCSI-mode depending on workload.
> [Cluster terminal: democratic-csi Helm install]
> Setup is more involved — Helm chart, configuration pointing at TrueNAS, SSH key or API token for the driver to talk to TrueNAS.
> [Overlay: "democratic-csi wins"]
> What you get: every PVC is a ZFS dataset. Native ZFS snapshots that Kubernetes can see via the volume snapshot API. Native ZFS quotas. Native ZFS encryption if you've enabled it. The full power of ZFS, exposed as Kubernetes primitives.
> [Face-cam]
> I love democratic-csi for one specific reason: if you treat ZFS as your source of truth, every backup story works through tools you already know. zfs send, zfs receive, zfs snapshot. You don't learn a new tool.
> [Overlay: "democratic-csi tradeoffs"]
> Tradeoffs. Setup is the most involved of the three. Performance for I/O-heavy workloads (databases) is bottlenecked by your network — every read and write goes from the worker over the LAN to TrueNAS. iSCSI-mode helps, but the LAN is still the path. Less of an issue on the TrueNAS-only setup where storage traffic loops back, but still real.
> And the driver itself has a learning curve. The first time it doesn't behave, you'll be reading the driver's Go source. I've been there.

**[7:00–10:00 — Option 3: Longhorn, screencast]**

> [SCREEN: Longhorn UI showing volumes and replicas]
> Longhorn is the in-cluster path. It uses local block storage on each worker node — that's why the provider attaches a data disk to each worker via storage_disk_size.
> [Cluster terminal: longhorn-system pods]
> Storage replicates inside the cluster. By default, three replicas per volume — written across three workers. No dependency on TrueNAS at runtime for storage I/O.
> [Overlay: "Longhorn wins"]
> What you get: block storage performance. Local-disk speed for reads when the replica is co-located with the pod. CNCF-grade project, active development, good UI. Snapshots, backups to S3 or NFS, disaster recovery, volume cloning — all in the Longhorn UI.
> [Face-cam]
> I run Longhorn as my default StorageClass. Most workloads don't need ZFS-level features per-PVC. They need fast, reliable block storage that recovers gracefully when a worker dies. Longhorn does that.
> [Overlay: "Longhorn tradeoffs"]
> Tradeoffs. You need at least 3 workers for the default 3-replica setup. Single-node or 2-node clusters can't use Longhorn well. You're using cluster RAM and CPU for storage management — Longhorn isn't free. And the data disks on workers need to be sized — when they fill up, Longhorn rebalances or you upgrade. ZFS snapshots underneath are at the *worker disk* level, not the PVC level — you can't snapshot a Longhorn volume from TrueNAS.
> One non-obvious gotcha: Longhorn doesn't recover automatically from a worker disk wipe. If you reprovision a worker through Omni without telling Longhorn, that node's replicas are lost. With 3 replicas you're fine, but you've degraded — go heal them before doing it again.

**[10:00–12:00 — What I run, face-cam with on-screen workload table]**

> [Overlay: "What Zac actually runs"]
> Here's what I run for which workload.
> [Overlay row by row]
> Default StorageClass — Longhorn. 90% of my apps use it.
> Databases — Longhorn. Block storage performance matters here. iSCSI via democratic-csi works too but Longhorn is simpler operationally.
> Plex / Jellyfin media library — NFS off TrueNAS. It's read-mostly, the library lives on TrueNAS anyway, no point copying it into the cluster.
> Backup target for Velero — NFS off TrueNAS. Same reasoning.
> Anything I want a TrueNAS-side snapshot of (very rare for me) — democratic-csi. I almost never reach for this in practice. Backups via Velero + Restic to S3 give me what I need without the per-PVC ZFS coupling.

**[12:00–12:45 — Quick gotchas, face-cam with overlays]**

> Three quick gotchas before I let you go.
> [Overlay: "1. Don't mount NFS into a database"] Don't put a database on NFS. PostgreSQL on NFS is a recipe for an outage.
> [Overlay: "2. Size your worker data disks"] Size your worker data disks for Longhorn from the start. Growing them later is doable but annoying. 100 gigs per worker is my comfortable floor.
> [Overlay: "3. Test your backup before you need it"] Whatever storage you pick, test your backup restore *before* you need it. Velero, zfs send, Longhorn DR — all of them have edge cases that show up when you try to restore for the first time. Find them on a Saturday, not at 2am.

**[12:45–13:15 — CTA, face-cam]**

> Full storage guide on the repo is linked. Sizing post from earlier this month is linked.
> Next video on this channel: upgrading Talos on TrueNAS without breaking ZFS. Subscribe so it shows up.
> Tell me what storage you run and why in the comments. I learn from these.

**[13:10–13:30 — End screen: M4 upgrade video + repo card]**

### Production notes
- The "what I run" overlay (10:00–12:00) is the table viewers will screenshot. **Pause longer there.** Don't rush past it.
- Avoid trash-talking NFS. It has a legitimate niche (read-mostly media). The "don't put a database on NFS" point is enough.
- democratic-csi setup is the most complex segment. Don't try to fully demo it — show the README + the high-level Helm command and move on. Demoing a full install would blow the time budget.
- Longhorn UI looks great on camera. Use it generously in the Longhorn segment.

---

## Cross-promotion plumbing

- **Sizing post** (`03-month-3/01-sizing-post.md`): add V5 link to "Companion video" placeholder once V5 is live.
- **Hero post** (`01-month-1/01-hero-post.md`): in the Storage section, add "I made a video about this" with V5 link.
- **V2 description**: add "If you want to go deeper on storage, V5 here: <link>".
- **Reddit / Talos Slack**: when storage questions come up on V1/V2 cross-posts, link V5.
- **LinkedIn drumbeat M3**: one post = "what storage I actually run" carousel, image-only, drives to V5.

---

## Open placeholders

- Live URLs for sizing post + hero post (replace `<sizing-post-slug>` and `<hero-post-slug>`).
- LinkedIn URL + YouTube channel URL.
- Longhorn version pinned in any screenshots (date the video).
