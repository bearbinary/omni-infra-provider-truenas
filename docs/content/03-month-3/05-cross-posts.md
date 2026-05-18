# M3 Cross-posts — Reddit + Lemmy

Milestone framing per `_shared/reddit-lemmy-post-patterns.md`. Each post leads with lesson/experience, delivers value before link, repo at the end.

---

## Piece 1: Sizing Talos control planes

Live URL: `https://dev.to/cliftonz/<sizing-post-slug>`

### Reddit

#### r/kubernetes

**Title**:
Most homelab Kubernetes control planes are undersized — here's the trigger I use after sizing dozens of them

**Body**:

Default install gets you a 2 vCPU / 2 GB control plane. That's the right call for a raw cluster.

The moment you install any of these, the math changes:
- Argo CD with many ApplicationSets
- Crossplane
- Rancher / Fleet
- Prometheus Operator at full mesh scrape
- cert-manager + external-dns + 40 CRDs from various operators

**What goes wrong**: apiserver swaps under load. Cluster doesn't break — it just gets intermittently slow. LIST operations go from 200ms to 2 seconds. Nothing logs an error, because nothing failed.

**The trigger I use**:

```bash
kubectl top node -l node-role.kubernetes.io/control-plane=
```

CPU or memory consistently above 70% under normal load → bump it. Now, not later.

For most homelab clusters with one of those operators installed, that means 4 vCPU / 4 GB minimum. For HA control planes running production-ish workloads, 4–6 vCPU and 8 GB+ per replica.

Full post with the four observable triggers, sizing table by cluster scale, and the in-place resize procedure for single-CP setups: <link>

Repo (TrueNAS Omni provider): https://github.com/bearbinary/omni-infra-provider-truenas

#### r/devops

**Title**:
Four observable triggers I use to know when a Kubernetes control plane needs resizing

**Body**:

Pattern I've used to size dozens of clusters, including the homelab one I run on a TrueNAS host. The triggers, ranked by signal strength:

1. **`kubectl top` shows CP pressure**. CPU or memory consistently >70% under normal load. The most direct signal.
2. **apiserver p99 latency >1s on GET/LIST** for common resources. Bump CPU first. If that doesn't fix it, look at etcd disk.
3. **etcd logs "apply request took too long" or "slow fdatasync"**. Root cause is almost always the disk, not CP CPU. Bumping CPU/RAM won't help. The fix is faster storage (NVMe SLOG on ZFS pools, for example) or applying the etcd timeout patch.
4. **apiserver OOMKilled in dmesg**. RAM is undersized. Bump immediately — OOMKills cascade because every controller reconnects on apiserver restart.

The non-obvious one is #3. People bump CP CPU/RAM trying to fix etcd latency issues. Doesn't work. The bug is below the layer you're bumping.

Full post with the sizing table and the in-place resize procedure: <link>

#### r/selfhosted

**Title**:
Sizing tip: most homelab K8s control planes need 4 GB the moment you install Argo or Prometheus, not 2 GB

**Body**:

Quick PSA from sizing my homelab cluster (and a bunch of other people's via the issues tracker on the provider I maintain).

**Default install: 2 vCPU / 2 GB control plane.** Fine for a raw cluster running a few stateless apps.

**Add any of these and 2 GB is undersized**:
- Argo CD (especially with many ApplicationSets)
- Prometheus Operator at full scrape
- Crossplane
- Rancher/Fleet
- cert-manager + external-dns + the usual operator stack

**Symptom**: cluster gets intermittently slow. List operations take 2 seconds instead of 200ms. Nothing errors. Nothing logs a problem. You debug for hours assuming something's broken — it's just the apiserver swapping under memory pressure.

**The math**: a control plane running Argo + cert-manager + Prometheus is doing the work of a small enterprise cluster's CP. It needs 4 GB minimum. HA setups (3 replicas) want 6–8 GB each.

Easier to size up *before* you install those operators than after. Same MachineClass change either way, less debugging.

Full sizing guide: <link>
Repo: https://github.com/bearbinary/omni-infra-provider-truenas

### Lemmy

#### `!kubernetes@lemmy.world`

**Title**: Four observable triggers I use to know when a Kubernetes control plane is undersized

**URL**: `https://dev.to/cliftonz/<sizing-post-slug>?utm_source=lemmy&utm_medium=kubernetes&utm_campaign=sizing-2026-07`

**Body**:
> Same content as the r/devops post above — four concrete signals (kubectl top, apiserver p99, etcd slow-fsync warnings, apiserver OOMKill in dmesg), with the non-obvious finding being that #3 is below the CP CPU/RAM layer and people waste time bumping the wrong dimension.
>
> Full sizing table + resize procedure in the canonical above.

#### `!devops@lemmy.world`

Same body, lighter framing on the homelab specifics.

---

## Piece 2: SAST retrospective

Live URL: `https://dev.to/cliftonz/<sast-retro-slug>`

### Reddit

#### r/golang

**Title**:
A SAST sweep on my 5,000-line Go infra project found 6 things. The finding I'll think about for years was where the bug lived, not what it was.

**Body**:

Maintain an open-source Omni infrastructure provider in Go. Ran SAST tooling against it for the first time. Six findings, none CVE-class, all instructive.

**The one that taught me the most**:

A debug log line in the WebSocket transport printed the raw outbound JSON-RPC message — which, on auth methods, included the API key. Only in `LOG_LEVEL=debug`. "Almost certainly never enabled in production."

The reframe: "almost certainly never" isn't never. First time someone files a support issue and pastes their debug log into a GitHub issue, the key leaks publicly.

**The fix**: redaction at the marshaling layer, not at the log call site. Log call sites are too easy to forget. Marshaling-layer redaction means *every* debug log path is automatically safe.

**The principle that generalizes**: "but only in debug mode" is not a security argument. Sensitive data is sensitive in every log level.

The other five findings followed similar shapes — surfaces I'd talked myself out of caring about that real users would predictably touch.

Full retro with all six findings, what was real, what was theoretical, what SAST didn't catch (logic bugs, threat-model gaps, supply chain): <link>

Source: https://github.com/bearbinary/omni-infra-provider-truenas

#### r/programming

**Title**:
Six security findings on a small Go project — and the lesson is "don't talk yourself out of caring about things real users will predictably do"

**Body**:

Static analysis on a ~5kLOC open-source infra tool I maintain. None of the findings were dramatic. All of them were "I knew about this surface and convinced myself it was fine."

Four examples:

1. **Sensitive data in debug logs**: "but debug isn't enabled in production." First time a user pastes a debug log into a GitHub issue, your secret leaks.
2. **`http.DefaultClient` with no timeout**: "but the upstream is reliable." Long-running processes leak goroutines when a single connection hangs.
3. **Path-traversal-shaped surface in a cache**: "but the input comes from a trusted source." Defense in depth means assuming each trust layer can fail.
4. **Insecure-by-default TLS flag scoped too broadly**: "but I only use it for localhost." Future code adding a different HTTPS dependency inherits the skip-verify by accident.

The pattern in all of them: the bug isn't the technical detail. The bug is the "but" that follows. Every assumption about "users won't do that" is wrong over a long enough timeline.

Full retro with all six findings and what SAST didn't catch: <link>

#### r/kubernetes

**Title**:
What I learned about Kubernetes provider security from a SAST sweep on my Omni infrastructure provider

**Body**:

Cross-posting because some of the findings are specific to the provider-API surface that anyone writing Omni / Cluster API / similar infrastructure code will recognize.

Patterns worth knowing about if you're writing an infra provider:

- **API key in marshaled request payload that gets logged at debug level.** Redact at marshal-time, not at log-call-time.
- **Unbounded retry on upstream 5xx.** Bound the retries. Otherwise the provider can wedge silently when its upstream API misbehaves.
- **Error messages that concatenate request URLs.** Strip auth from URL query strings + basic-auth in the URL before logging.
- **Path construction from upstream identifiers.** Even if the upstream is trusted (Omni, in my case), validate identifiers with strict allowlists before constructing filesystem paths.

None of these are dramatic. All of them are "I knew this was a surface; I convinced myself it was fine."

Full write-up: <link>
Source: https://github.com/bearbinary/omni-infra-provider-truenas

### Lemmy

#### `!golang@programming.dev`

**Title**: SAST findings on a 5kLOC Go infra tool — the patterns I'll catch faster next time

**URL**: `https://dev.to/cliftonz/<sast-retro-slug>?utm_source=lemmy&utm_medium=golang&utm_campaign=sast-retro-2026-07`

**Body**:
> Six findings, six lessons. Posting because the patterns generalize past my specific project.
>
> Recurring shape: every finding was a place I'd talked myself out of caring because of "but users won't do X." They will.
>
> Concrete examples: `http.DefaultClient` with no timeout, sensitive data in debug-only log paths, insecure-by-default TLS flag scoped too broadly, error messages that concatenate auth-containing URLs.
>
> Linting + SAST + threat modeling are three different tools that catch three different bug classes. Don't substitute one for another.

#### `!infosec@infosec.pub`

**Title**: Maintainer's perspective: 6 SAST findings on an open-source Go infra tool, where each one came from, and what I'd do differently

**URL**: `https://dev.to/cliftonz/<sast-retro-slug>?utm_source=lemmy&utm_medium=infosec&utm_campaign=sast-retro-2026-07`

**Body**:
> Security-focused audience version. Lean on the "what SAST didn't catch" section — authorization semantics, logic bugs that are also security bugs, supply chain. SAST is a floor, not a ceiling.
>
> Also covers the workflow I'm changing going forward — running SAST on every PR rather than as a sweep, adding lint rules for the patterns SAST found.

---

## Piece 3: Storage deep-dive (written companion to V5)

Live URL: `https://dev.to/cliftonz/<storage-deep-dive-slug>`

### Reddit

#### r/selfhosted

**Title**:
I ran NFS, democratic-csi, and Longhorn in production for 6 months each — here's what I picked for which workload

**Body**:

Three storage paths for Kubernetes on TrueNAS. Ran all three in homelab production for at least 6 months each. Honest take:

**Default StorageClass: Longhorn.** Block storage, independent failure domain (cluster keeps responding when TrueNAS reboots), good UI. ~90% of my apps use it.

**Databases (PostgreSQL, MySQL): Longhorn.** Block storage performance matters. iSCSI via democratic-csi is an alternative but Longhorn is simpler operationally.

**Plex / Jellyfin media library: NFS off TrueNAS.** Read-mostly, library already lives on TrueNAS, no point copying it into the cluster.

**Velero backup target: NFS off TrueNAS.** Write-once, read-on-restore. Perfect NFS workload.

**democratic-csi**: rare for me. Reach for it only if you have a *specific* per-PVC-ZFS-snapshot requirement. Velero + Restic to S3 gives me what I need without per-PVC ZFS coupling.

**Anti-recommendation**: don't put a database on NFS. PostgreSQL on NFS will eventually corrupt or stall. MySQL too. SQLite too. Anything that fsyncs aggressively.

What changed my mind over time: I started assuming democratic-csi was obviously correct ("use the NAS as the storage"). Six months in, the independent failure domain Longhorn gives me matters more than the ZFS coupling.

Full deep-dive with what each option gets right + where each falls apart: <link>

Repo: https://github.com/bearbinary/omni-infra-provider-truenas

#### r/homelab

**Title**:
Storage for homelab K8s on TrueNAS — what I run for which workload after 6+ months with each option

**Body**:

Same hardware, three storage paths, side by side over the last 18 months. Real workloads.

**What I run now**:
- Longhorn: default StorageClass, databases, ~90% of apps
- NFS off TrueNAS: Plex/Jellyfin libraries, Velero backup target (read-mostly workloads)
- democratic-csi: rare; only for the specific case where I really want a ZFS dataset per PVC

**Trap I fell into early**: assumed democratic-csi was the obvious answer ("use the NAS as the storage"). It works. It also couples PVC availability to TrueNAS uptime. When TrueNAS reboots for maintenance, every democratic-csi-backed app goes down. Longhorn-backed apps stay up (degraded but responding).

For homelab maintenance windows where I want some workloads to keep responding while I patch the NAS — that independent failure domain matters way more than I expected.

**Things I'd do from scratch today**:
- 3 workers minimum
- 100 GB data disk per worker for Longhorn (`storage_disk_size` in the provider config)
- Longhorn as default StorageClass
- NFS only for read-mostly, never for databases
- Skip democratic-csi unless you have a specific per-PVC-ZFS-snapshot need

Full deep-dive: <link>
Repo (provider): https://github.com/bearbinary/omni-infra-provider-truenas

#### r/kubernetes

**Title**:
Storage decision matrix for a single-host (NAS-based) homelab Kubernetes cluster, after running 3 options for 6 months each

**Body**:

Specific to single-host setups (TrueNAS-only or similar) — when storage compute and cluster compute are co-located on one machine.

**Three viable paths**:

1. **NFS** — TrueNAS serves the share, pods mount it. Five-minute setup. Works only for read-mostly workloads. Permission/locking issues for anything else.
2. **democratic-csi** — Every PVC is a ZFS dataset or zvol. Native snapshots, native quotas, but every read/write crosses to TrueNAS over the bridge.
3. **Longhorn** — In-cluster block storage replicating across worker data disks. Independent of TrueNAS for runtime I/O.

**Key tradeoff axis: failure-domain independence**:
- NFS and democratic-csi couple cluster storage availability to TrueNAS uptime.
- Longhorn doesn't. Cluster keeps responding (degraded) when TrueNAS reboots.

**For most workloads, that independence matters more than ZFS-native PVCs.** I migrated my default StorageClass from democratic-csi to Longhorn around month 9. Never reversed.

Backup strategy that works across all three: Velero + Restic to S3. Application-level backups, independent of storage layer.

Full write-up: <link>

### Lemmy

#### `!selfhosted@lemmy.world`

**Title**: Storage for homelab Kubernetes on TrueNAS — what I run for which workload after 18 months

**URL**: `https://dev.to/cliftonz/<storage-deep-dive-slug>?utm_source=lemmy&utm_medium=selfhosted&utm_campaign=storage-2026-07`

**Body**:
> Same as the r/selfhosted post — three options ranked by workload fit. Longhorn for default, NFS for read-mostly, democratic-csi for the specific case where you really need per-PVC ZFS snapshots (rare).
>
> The "don't put a database on NFS" line is the most-quoted-back-to-me part of the original V5 video. Including it again here because people keep doing it and being surprised when their data corrupts.

#### `!kubernetes@lemmy.world`

Same body shape as the r/kubernetes post.

---

## Cadence

Same pattern as M2 — stagger by 2-3 days per drop, smaller community first, leave 24h between Reddit and Lemmy same-niche.

M3 spans ~6 weeks if all three pieces get a full cycle.
