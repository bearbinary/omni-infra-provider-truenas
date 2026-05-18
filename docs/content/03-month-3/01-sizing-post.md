---
title: "Sizing Talos Control Planes on TrueNAS: when 2 GB stops being enough"
published: false
description: "Observable triggers, sizing tables, and how to actually resize a Talos control plane safely — written from a TrueNAS host's perspective."
tags: kubernetes, talos, truenas, homelab
cover_image: ""
series: "Self-hosted Kubernetes on TrueNAS"
---

**TL;DR — The default 2 vCPU / 2 GiB control plane is fine for a raw cluster. The moment you install Crossplane, Argo CD with many ApplicationSets, Rancher/Fleet, or Prometheus Operator at full scrape, the apiserver swaps under load and the cluster looks intermittently broken with no obvious cause. This post names the four triggers, gives a sizing table by cluster scale, and shows how to resize safely on a TrueNAS host — including the HDD-pool + etcd timing trap that bites real users.**

I'm Zac Clifton. I maintain [`omni-infra-provider-truenas`](https://github.com/bearbinary/openni-infra-provider-truenas) and I've sized more control planes than I'd like to admit. This is the post I wish existed when I started.

For install steps, see [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>). This post assumes you have a working cluster and want it to stay working when you put real load on it.

---

## What makes a control plane bigger

Four drivers. Knowing which one is hurting you tells you *what* to bump — CPU, RAM, disk, or all three.

| Driver | What it hits | Bump |
|---|---|---|
| **Cluster size** (node count) | etcd heartbeat volume, apiserver watch fan-out | CPU + RAM |
| **Pod / object count** | etcd DB size, apiserver list/watch memory | RAM + disk |
| **API churn** (CI/CD, GitOps, operators reconciling) | apiserver CPU, etcd write IOPS | CPU |
| **CRD / operator load** (Argo CD, Crossplane, cert-manager, Rancher/Fleet) | etcd objects, controller-manager CPU | CPU + RAM |

A 3-node cluster running a static app looks nothing like a 3-node cluster running Argo CD + Prometheus + cert-manager + 40 CRDs. Same node count. The second one needs a control plane that's 2–3× bigger.

---

## The four triggers — scale up when

Don't guess. Watch for one of these concrete signals.

### Trigger 1: `kubectl top` shows control-plane pressure

```bash
kubectl top node -l node-role.kubernetes.io/control-plane=
```

- **CPU consistently > 70%** under normal load → bump `cpus`.
- **Memory consistently > 70%** or creeping upward over days → bump `memory`.

A spike during an operator reconcile storm is fine. A sustained floor is not.

### Trigger 2: apiserver p99 latency is high

From your in-cluster Prometheus or the Omni dashboard:

```promql
histogram_quantile(0.99,
  sum by (le, resource, verb) (
    rate(apiserver_request_duration_seconds_bucket{verb!="WATCH"}[5m])
  )
)
```

- **p99 > 1s on `GET` / `LIST`** for common resources (pods, configmaps) → apiserver is CPU-starved or etcd is slow. Bump CPU first. If that doesn't help, look at etcd disk.

### Trigger 3: etcd is warning about slow writes

```bash
omnictl --cluster <name> talosctl logs etcd | grep -iE "took too long|slow"
```

Signals to act on:

- **`apply request took too long`** (> 100 ms) → etcd disk fsync is slow. Root cause is almost always the ZFS pool, not CPU. Bumping control-plane CPU/RAM will *not* fix this. See the HDD-pool section below.
- **`slow fdatasync`** → same story. The zvol needs a faster vdev layout.
- **Leader changes / elections during normal operation** → control-plane CPU contention. Bump `cpus`.

### Trigger 4: apiserver is OOMKilling

```bash
omnictl --cluster <name> talosctl dmesg | grep -i oom
```

If you see kube-apiserver in the kill list, you're undersized on RAM. Bump immediately — OOMKills cascade because the apiserver is the single most load-bearing pod in the cluster, and every restart triggers a watch reconnect storm from every controller.

---

## Sizing table by cluster scale

These are the numbers I actually use. Adjust if your workload is unusual.

| Cluster | Workload | CP vCPU | CP RAM | CP disk |
|---|---|---|---|---|
| Homelab (≤ 5 nodes) | Static apps, no operators | 2 | 2 GB | 20 GB |
| Homelab (≤ 5 nodes) | Argo CD + cert-manager + Prometheus | **4** | **4 GB** | 40 GB |
| Homelab (≤ 5 nodes) | Add Crossplane or Rancher/Fleet | **4** | **6 GB** | 40 GB |
| Small team (5–10 nodes) | Argo CD + observability mesh | 4 | 8 GB | 60 GB |
| Small team (10–20 nodes) | Operators + heavy CRD load | 6 | 12 GB | 80 GB |
| Production-ish (20+ nodes) | HA control plane (3× replicas) | 8 | 16 GB | 100 GB |

**Most homelab undersizing happens at the second row.** People stick with 2 GB after installing Argo and Prometheus, then debug "the cluster is randomly slow" for weeks. The fix is 4 GB.

---

## Why the root disk floor is 20 GiB

The provider enforces a 20 GiB minimum on the control-plane root disk. People ask why.

During cluster bootstrap, Talos pulls every control-plane image: kube-apiserver, etcd, kube-controller-manager, kube-scheduler, kube-proxy, the CNI, and CoreDNS. That's roughly 5–7 GiB of images. The kubelet writes them to the root filesystem before any of them start.

If you set the root disk to 10 GiB, you hit DiskPressure mid-install. The kubelet starts evicting the very images it's about to need. etcd never comes up. The cluster boots into a half-broken state and you debug it for an hour before realizing it's the disk.

20 GiB is the validated floor. Production-ish control planes typically want 40–60 GiB — that's the provider default for a reason.

---

## How to resize without breaking the cluster

There are two distinct paths depending on your topology.

### Path A: HA control plane (3+ replicas) — rolling resize

This is the safe path. Works because Omni orchestrates the rollout and the cluster stays available throughout.

1. Update the MachineClass with the new sizing:

```bash
cat <<'EOF' | omnictl apply -f -
metadata:
  namespace: default
  type: MachineClasses.omni.sidero.dev
  id: truenas-cp
spec:
  autoprovision:
    providerid: truenas
    grpcendpoint: ""
    icon: ""
    configpatch: |
      cpus: 4
      memory: 4096
      disk_size: 40
EOF
```

2. In Omni, scale the control-plane MachineSet down by 1 — Omni drains and removes the oldest CP node.
3. Scale it back up by 1 — Omni creates a new CP with the new MachineClass spec.
4. Wait until the new CP is healthy and etcd has rebalanced.
5. Repeat for the remaining old CPs.

Total time: ~15 minutes for a 3-node HA control plane. The cluster never goes offline.

### Path B: Single control plane — in-place on TrueNAS

This is the path most homelabs are on. There's no rolling option because there's only one CP. The cluster *will* go offline during the resize. Plan accordingly.

1. **Take an etcd snapshot** through Omni first. Always.
2. Stop the VM from the TrueNAS UI.
3. In TrueNAS **Virtualization > [VM] > Edit**, change the CPU and memory.
4. **Do not touch the disk size from the TrueNAS UI for the root disk.** Talos doesn't grow the root partition on boot. Instead, deprovision and re-provision the CP through Omni (which forces a fresh disk at the new size). Yes, this requires re-bootstrapping. Yes, it's annoying. The alternative is a manual zvol resize + Talos partition surgery that I don't recommend.
5. Start the VM. Wait for it to rejoin Omni.

For CPU/RAM only: VM downtime is ~2 minutes. For disk changes: ~10 minutes plus etcd restore time.

---

## HDD-backed pools — tune the cluster, not just the hardware

This is the trap that catches almost everyone running TrueNAS as a hypervisor.

**etcd assumes sub-10 ms fsync.** That's a hard assumption baked into its heartbeat, election, and node-monitor timeouts. On an NVMe pool, you're well under that. On a spinning-rust pool under any kind of load, you're at 50–200 ms. etcd interprets normal HDD latency as a node failure and triggers leader changes.

The symptom: intermittent `NodeNotReady` flaps, leader changes in `etcd` logs during normal operation, the cluster "looks broken" for 30 seconds and then recovers. No obvious cause. People rebuild the cluster, hit the same issue, blame Talos, blame Omni, blame the network.

**The fix is one of two things:**

**Option 1: add an NVMe SLOG to the pool.** SLOG (separate intent log) batches synchronous writes so etcd's fsyncs land on NVMe instead of HDD. This is the right answer if you have the slot for it. Even a 32 GiB consumer NVMe works — SLOG doesn't need to be big.

**Option 2: patch the Talos cluster's etcd and kubelet timeouts.** Tell the cluster that "slow" is the new normal:

```yaml
machine:
  kubelet:
    extraArgs:
      node-status-update-frequency: "30s"
cluster:
  etcd:
    extraArgs:
      heartbeat-interval: "1000"
      election-timeout: "10000"
controlPlane:
  extraManifests:
    - |
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: node-monitor-tuning
        namespace: kube-system
      data:
        node-monitor-grace-period: "120s"
```

Apply via Omni's cluster config patches. The cluster now tolerates HDD latency without misinterpreting it as failure. You'll have slower failure detection (which is correct for the underlying hardware), but no false-positive flaps.

Pick option 1 if you can. Option 2 is for when you can't.

---

## ZFS + etcd: the rest of the story

Even with SLOG, etcd on ZFS has two more things worth knowing.

**Recordsize**: etcd writes in roughly 4–16 KiB chunks. ZFS's default 128 KiB recordsize means write amplification — every small etcd write reads and rewrites a 128 KiB record. Set `recordsize=16K` on the dataset where etcd lives. (The provider doesn't do this for you yet — set it manually on `<pool>/omni-vms` or wherever your zvols live.)

**Compression**: ZSTD or LZ4 are both fine. ZSTD compresses tighter but costs CPU. For etcd specifically, LZ4 is the safer choice — etcd data is mostly already-compressed protobuf, and the CPU saved matters at the latency tail.

**`sync=standard`**: Don't set `sync=disabled` to "speed up etcd." It works until the host crashes, at which point you've lost in-flight cluster state and your cluster is broken in ways that are very hard to recover from. The whole point of SLOG is to keep `sync=standard` *and* be fast.

---

## What I actually run

My homelab control plane is single-replica, 4 vCPU, 6 GB RAM, 40 GiB disk on a NVMe pool with no SLOG (because the whole pool is NVMe — no need). Cluster is roughly 5 nodes total, runs Argo CD, cert-manager, Longhorn, Prometheus, and ~15 application workloads. Apiserver p99 sits at ~150 ms steady-state. Memory usage hovers around 60%.

If I were running HDDs with an SLOG, I'd be on the same sizing. If I were running HDDs without an SLOG, I'd be running option 2 above and accepting slower failure detection in exchange for a stable cluster.

---

## Try it

- **Provider repo + install**: [github.com/bearbinary/omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas)
- **Canonical install guide**: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>)
- **Companion video**: [Sizing Talos control planes on TrueNAS](#) (M3 YouTube, drops within the week)

Sized your cluster differently and it's working? Drop a comment — I learn from these too.

---

**About the author**: Zac Clifton is an infrastructure engineer building tools for self-hosters and small teams. He maintains `omni-infra-provider-truenas` and writes about pragmatic homelab Kubernetes. Subscribe on [YouTube](#) for monthly deep-dives on Talos, Omni, TrueNAS, and the parts of self-hosted infra nobody else is writing about.
