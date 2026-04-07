# CSI Storage Guide

This guide covers persistent storage options for Kubernetes clusters running on TrueNAS via the Omni infrastructure provider. It helps you choose the right CSI driver based on your workload, complexity tolerance, and whether you want to leverage TrueNAS-managed storage or run distributed storage inside the cluster.

For Talos Linux-specific CSI guidance, see the [Siderolabs Storage Documentation](https://docs.siderolabs.com/kubernetes-guides/csi/storage).

---

## Storage Approaches

There are two fundamentally different ways to provide persistent storage to your cluster:

1. **NAS-backed storage** — TrueNAS serves storage (NFS exports or iSCSI targets) to the cluster over the network. Your data lives on TrueNAS ZFS pools with all the benefits that brings (snapshots, replication, RAID-Z, scrubbing). The cluster nodes don't need extra disks.

2. **Node-local distributed storage** — Storage software runs inside the cluster and uses extra virtual disks attached to each VM. Data is replicated across nodes. TrueNAS is only involved as the hypervisor, not as a storage server.

---

## NAS-Backed Storage

These options use TrueNAS as the storage backend. Your data is managed by TrueNAS ZFS and served to the cluster via NFS or iSCSI.

### NFS with nfs-subdir-external-provisioner

The simplest path to persistent storage. TrueNAS shares an NFS export, and the [nfs-subdir-external-provisioner](https://github.com/kubernetes-sigs/nfs-subdir-external-provisioner) dynamically creates subdirectories for each PersistentVolume.

**Pros:**
- No Talos system extensions required (NFS client is built into the Talos kubelet image)
- Minimal configuration — just an NFS share path and server IP
- TrueNAS manages the storage, ZFS snapshots/replication work as normal
- Easiest to set up and debug

**Cons:**
- File-level storage (not block) — slower for database workloads
- NFS locking and contention can be a bottleneck under heavy concurrent writes
- No dynamic dataset creation on TrueNAS (all PVs share one export)

**Talos extensions required:** None

**Setup summary:**
1. Create an NFS share on TrueNAS (e.g., `/mnt/pool/k8s-nfs`)
2. Ensure the NFS service is running and the share is accessible from the cluster network
3. Install via Helm:
   ```bash
   helm repo add nfs-subdir-external-provisioner https://kubernetes-sigs.github.io/nfs-subdir-external-provisioner
   helm install nfs-provisioner nfs-subdir-external-provisioner/nfs-subdir-external-provisioner \
     --set nfs.server=<truenas-ip> \
     --set nfs.path=/mnt/pool/k8s-nfs \
     --set storageClass.defaultClass=true
   ```

### democratic-csi (NFS or iSCSI)

[democratic-csi](https://github.com/democratic-csi/democratic-csi) is purpose-built for TrueNAS. It dynamically creates ZFS datasets (NFS) or zvols (iSCSI) on TrueNAS for each PersistentVolume, giving you per-PV isolation and ZFS-level snapshot support.

**Pros:**
- Dynamic provisioning — each PV gets its own ZFS dataset or zvol
- Supports NFS, iSCSI, and SMB protocols
- ZFS snapshots exposed as Kubernetes VolumeSnapshots
- Purpose-built for FreeNAS/TrueNAS

**Cons:**
- More complex setup than simple NFS
- Two driver modes with different trade-offs (see below)

**Driver modes:**

| Mode | Auth Method | Maturity | Notes |
|---|---|---|---|
| SSH-based (`freenas-nfs`, `freenas-iscsi`) | SSH to TrueNAS | Stable | Requires SSH access with root/sudo. Executes ZFS commands directly. Most battle-tested. |
| API-based (`freenas-api-nfs`, `freenas-api-iscsi`) | REST API | Experimental | Uses TrueNAS REST v2.0 API. No SSH needed. 1 GB minimum volume size. |

> **Compatibility note:** The API-based drivers use the TrueNAS REST v2.0 API. TrueNAS SCALE 25.04+ (Fangtooth) has transitioned to a JSON-RPC 2.0 API internally — the REST API may still work via a compatibility layer, but this should be verified on your specific version. The SSH-based drivers are unaffected since they execute ZFS commands directly.

**Talos extensions required:**
- iSCSI mode: `iscsi-tools` system extension
- NFS mode: None (built into kubelet)

**Setup summary:**
1. Choose your protocol (NFS or iSCSI) and driver mode (SSH or API)
2. For SSH mode: enable SSH on TrueNAS and create a dedicated user with sudo access
3. For iSCSI: enable the iSCSI service on TrueNAS and install the `iscsi-tools` Talos extension
4. Install via Helm following the [democratic-csi documentation](https://github.com/democratic-csi/democratic-csi#installation)

### iSCSI (Manual or via democratic-csi)

iSCSI provides block-level storage, which is significantly faster than NFS for database workloads and anything doing heavy random I/O.

**Talos extensions required:** `iscsi-tools`

To add the `iscsi-tools` extension to your Talos nodes, include it in your machine config or Omni config patch:

```yaml
machine:
  install:
    extensions:
      - image: ghcr.io/siderolabs/iscsi-tools:latest
```

You can use iSCSI manually (create targets on TrueNAS, configure initiators on each node) or let democratic-csi handle it dynamically.

---

## Node-Local Distributed Storage (Not Recommended)

These options run storage software inside the Kubernetes cluster itself. They require extra virtual disks attached to each VM — TrueNAS acts only as the hypervisor, not as a storage server.

> **Why not recommended?** In a TrueNAS VM environment, these drivers treat virtual disks as if they were real physical drives. This adds a redundant replication and management layer on top of storage that TrueNAS is already managing via ZFS — resulting in double write amplification and no benefit from ZFS features like snapshots, replication, or scrubbing. They're documented here for completeness, but NAS-backed storage (above) is a better fit for this environment.

> **Note:** Attaching extra disks to VMs requires the multi-disk VM support feature (see [backlog](backlog.md)). Until that feature lands, you would need to manually add disks to VMs via the TrueNAS UI.

### Longhorn

[Longhorn](https://longhorn.io/) is a lightweight, Kubernetes-native distributed block storage system. It replicates data across nodes and provides snapshots and backups.

**Pros:**
- Simple to install and operate
- Built-in replication, snapshots, and backup to S3
- Good UI for monitoring storage
- Active CNCF project (incubating)

**Cons:**
- Requires extra virtual disks on each VM
- Storage capacity limited by total disk space across nodes
- No NAS integration — doesn't leverage TrueNAS ZFS features
- Not ideal for very large clusters

**Talos extensions required:** None (uses standard Linux block devices)

**Talos-specific setup:** See the [Longhorn Talos Linux support guide](https://longhorn.io/docs/1.9.0/advanced-resources/os-distro-specific/talos-linux-support/).

### Rook/Ceph

[Rook](https://rook.io/) deploys [Ceph](https://ceph.io/) inside Kubernetes, providing block, file, and object storage on a distributed cluster.

**Pros:**
- Enterprise-grade, battle-tested at scale
- Provides block, file (CephFS), and object (S3-compatible) storage
- Self-healing, auto-rebalancing
- Multi-tenant capable

**Cons:**
- Complex to operate and troubleshoot
- Slow on small clusters (3-5 nodes) — Ceph has significant overhead
- Requires extra disks and substantial resources (RAM, CPU)
- Overkill for homelab and small deployments

**Talos extensions required:** None

**When to consider:** Large clusters (10+ nodes) where you need enterprise storage features, multi-tenancy, or S3-compatible object storage.

### Mayastor (OpenEBS)

[Mayastor](https://github.com/openebs/Mayastor) is a Rust-based storage engine using NVMe-oF for ultra-low latency.

**Pros:**
- Highest performance of the distributed options
- Modern architecture (NVMe over Fabrics)

**Cons:**
- Complex setup — requires Huge Pages, Pod Security patches, node labels
- Requires disabling `nvme_tcp` module check (built into Talos kernel)
- Newer project, smaller community than Longhorn or Ceph

**Talos extensions required:** None, but requires kernel module and Huge Pages configuration.

**When to consider:** Performance-critical workloads where latency matters more than simplicity.

---

## Recommendation Matrix

| Use Case | Recommended | Why |
|---|---|---|
| Getting started / homelab | NFS (`nfs-subdir-external-provisioner`) | Zero extensions, minimal config, TrueNAS does the heavy lifting |
| Stateful apps / databases | iSCSI via `democratic-csi` | Block storage performance with dynamic provisioning |
| Replicated / HA storage | Longhorn (not recommended) | Works but inefficient — adds redundant replication on top of ZFS |
| Enterprise / large scale | Rook/Ceph (not recommended) | Works but overkill — double write amplification on virtual disks |

For most users running Kubernetes on TrueNAS, **start with NFS**. It's the fastest path to working persistent storage and leverages the TrueNAS storage you already have. Move to iSCSI or democratic-csi when you need better performance or per-PV isolation. Node-local options like Longhorn and Ceph are possible but not recommended — they treat virtual disks as physical drives, duplicating work that TrueNAS ZFS is already doing.

---

## Talos Extension Requirements

| CSI Driver | Extensions Needed | Notes |
|---|---|---|
| NFS (any) | None | NFS client built into Talos kubelet image |
| iSCSI (any) | `iscsi-tools` | Install via Talos system extension |
| Longhorn | None | Uses standard Linux block devices |
| Rook/Ceph | None | Uses standard Linux block devices |
| Mayastor | None | Requires Huge Pages and kernel config |

---

## Further Reading

- [Siderolabs CSI Storage Guide](https://docs.siderolabs.com/kubernetes-guides/csi/storage) — Talos-specific CSI documentation
- [democratic-csi](https://github.com/democratic-csi/democratic-csi) — TrueNAS-native CSI driver
- [nfs-subdir-external-provisioner](https://github.com/kubernetes-sigs/nfs-subdir-external-provisioner) — Simple NFS dynamic provisioner
- [Longhorn](https://longhorn.io/) — Kubernetes-native distributed block storage
- [Rook/Ceph](https://rook.io/) — Enterprise distributed storage orchestrator
- [Mayastor](https://openebs.io/docs/concepts/mayastor) — High-performance NVMe-oF storage
