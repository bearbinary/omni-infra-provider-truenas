# Storage Guide

This guide helps you choose and configure persistent storage for Kubernetes clusters running on TrueNAS via the Omni infrastructure provider.

**Longhorn with a dedicated data disk is the default storage approach.** We chose Longhorn over NFS because NFS has significant networking complexity (firewall rules, port 2049 reachability, subnet auto-detection), many critical applications cannot run on NFS at all (see incompatibility list below), and NFS volumes cannot be snapshotted or backed up with standard Kubernetes tools like Velero CSI snapshots. Longhorn gives you block storage, built-in snapshots, S3 backup, and zero TrueNAS-side dependencies. NFS auto-storage remains available as an opt-in alternative for simple read-heavy workloads.

---

## Choosing a Storage System

### Software That Does Not Support NFS

Many popular Kubernetes workloads explicitly require block storage or are known to corrupt data on NFS. If you plan to run any of these, **you must use Longhorn** (or another block storage provider):

| Software | Why NFS Fails |
|---|---|
| **PostgreSQL** / CloudNativePG | NFS file-locking semantics cause WAL corruption, timeouts, and data loss under concurrent writes. [CloudNativePG docs explicitly recommend local/block storage.](https://cloudnative-pg.io/documentation/1.20/storage/) |
| **Elasticsearch** / OpenSearch | Lucene relies on filesystem behavior NFS does not provide. [Elastic explicitly states NFS is not supported](https://discuss.elastic.co/t/why-nfs-is-to-be-avoided-for-data-directories/215240) -- data corruption and index failures will occur. |
| **Redis Enterprise** | [Redis docs state NFS is not supported](https://redis.io/docs/latest/operate/kubernetes/recommendations/persistent-volumes/) -- requires block storage with EXT4/XFS. NFS locking is incompatible with Redis persistence. |
| **MongoDB** | Data directories fail to persist correctly on NFS. Missing subdirectories and silent data loss reported in Kubernetes NFS deployments. |
| **OpenBao / Vault** | Raft consensus storage requires consistent fsync semantics. NFS cannot guarantee the write ordering that Raft needs for safe leader election and log replication. Use block storage with integrated Raft, or Consul as the backend. |
| **etcd** | Requires low-latency, fdatasync-safe storage. [NFS latency and locking cause leader election failures and cluster instability.](https://github.com/etcd-io/etcd/issues/19394) |
| **Loki** (log aggregation) | [Grafana docs warn against NFS for Loki](https://grafana.com/docs/loki/latest/operations/storage/filesystem/) -- shared filesystem causes "a bad experience." Production Loki should use S3 or block storage. |
| **Prometheus** | TSDB requires consistent block writes. NFS adds latency that causes scrape timeouts and compaction failures under load. |
| **MySQL** / MariaDB | InnoDB requires `O_DIRECT` and `fsync` guarantees that NFS does not reliably provide, leading to silent corruption on crash recovery. |
| **CockroachDB** / TiDB | Distributed SQL databases with Raft consensus -- same fsync requirements as etcd. NFS breaks replication consistency. |

**General rule:** If the software uses a write-ahead log (WAL), Raft consensus, or Lucene indexing, it will not work reliably on NFS.

### When NFS Does Not Work (Infrastructure)

Beyond software compatibility, NFS auto-storage requires the provider to have TrueNAS API access **and** the cluster nodes to reach TrueNAS on port 2049. It will not work in these scenarios:

- **Provider deployed to a remote Kubernetes cluster** -- The provider has WebSocket API access, but the cluster VMs may not have network access to TrueNAS port 2049 (NFS). The provider can create the share, but pods can't mount it.
- **Provider deployed via Helm to a different site** -- Multi-site or edge deployments where the cluster is not on the same LAN as TrueNAS.
- **Firewall blocks NFS** -- If a firewall sits between the cluster network and TrueNAS and does not allow NFS traffic (TCP 2049, plus portmapper on 111).
- **TrueNAS NFS service disabled or unavailable** -- Some TrueNAS configurations intentionally disable NFS (e.g., iSCSI-only setups, or when the NFS service conflicts with other workloads).
- **Air-gapped or restricted networks** -- Environments where cluster nodes cannot make outbound connections to the NAS.
- **Shared TrueNAS with NFS conflicts** -- When other NFS consumers on the same TrueNAS box have specific export requirements that conflict with the provider's auto-created shares.

In all of these cases, **use Longhorn** -- it runs entirely inside the cluster and has zero NAS-side dependencies.

### Decision Matrix

| | **Longhorn (Recommended)** | **NFS (Auto Storage)** |
|---|---|---|
| **How it works** | Storage software runs inside the cluster, replicates data across VM disks | TrueNAS serves an NFS share; a provisioner creates subdirectories for each PV |
| **TrueNAS dependency** | None -- self-contained | Requires API access + NFS port reachable from cluster |
| **Extra VM disks needed** | Yes (one per worker node) | No |
| **Storage type** | Block (better for databases) | File (NFS overhead on random I/O) |
| **Data lives on** | Virtual disks inside VMs (replicated by Longhorn) | TrueNAS ZFS pool (snapshots, scrub, replication) |
| **Setup complexity** | Medium -- Helm install + Talos config patch | Low -- one toggle in MachineClass |
| **Access modes** | ReadWriteOnce (single node) | ReadWriteMany (multiple pods) |
| **Survives TrueNAS outage** | Yes -- data is on local VM disks | No -- NFS mount goes offline |
| **Snapshots & backup** | Kubernetes VolumeSnapshots, Velero CSI integration, backup to S3 | No CSI snapshot support — Velero can only do file-level restic/kopia backup, not crash-consistent snapshots |
| **Project health** | Active CNCF incubating project | nfs-subdir-external-provisioner unmaintained since 2022 |

**Choose Longhorn if:** You want reliable, self-contained storage that works regardless of your network topology or TrueNAS configuration, with proper snapshot and backup support. This is the right choice for most users.

**Choose NFS if:** You only need shared read-heavy storage (media files, static assets), your cluster nodes can reach TrueNAS over NFS, and you don't need Kubernetes-native snapshots or database workloads.

Both approaches can coexist in the same cluster -- install Longhorn alongside NFS and use StorageClass selectors per workload.

---

## Option 1: Longhorn (Recommended)

[Longhorn](https://longhorn.io/) is a CNCF incubating project that provides Kubernetes-native distributed block storage. It runs entirely inside your cluster with no TrueNAS API dependency.

### Requirements

- Extra virtual disks attached to worker VMs (use `additional_disks` in MachineClass)
- Talos machine config patches for Longhorn compatibility

### Setup

**1. Add a storage disk to your MachineClass:**

```yaml
providerdata: |
  cpus: 4
  memory: 8192
  disk_size: 40
  pool: default
  network_interface: br100
  storage_disk_size: 100  # GiB, dedicated to Longhorn
```

> `storage_disk_size` is a shorthand that adds a data disk to each VM. You can also use the full `additional_disks: [{size: 100}]` syntax if you need per-disk pool or encryption options.

**2. Apply Talos machine config patches for Longhorn:**

Longhorn needs specific Talos configuration. Apply this as a config patch in Omni:

```yaml
machine:
  kubelet:
    extraMounts:
      - destination: /var/lib/longhorn
        type: bind
        source: /var/lib/longhorn
        options:
          - bind
          - rshared
          - rw
  sysctls:
    vm.overcommit_memory: "1"
```

See the [Longhorn Talos Linux support guide](https://longhorn.io/docs/1.9.0/advanced-resources/os-distro-specific/talos-linux-support/) for the latest configuration requirements.

**3. Install Longhorn via Helm:**

```bash
helm repo add longhorn https://charts.longhorn.io
helm repo update
helm install longhorn longhorn/longhorn \
  --namespace longhorn-system \
  --create-namespace \
  --set defaultSettings.defaultDataPath=/var/lib/longhorn
```

**4. Set as default StorageClass:**

```bash
kubectl patch storageclass longhorn -p '{"metadata":{"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
```

Longhorn's Helm install creates two StorageClasses: `longhorn` (dynamic provisioning -- use this for all PVCs) and `longhorn-static` (for pre-existing volumes you created manually). Kubernetes StorageClasses have no description field, so the naming convention is the only way to communicate intent.

### Why Longhorn

- **No TrueNAS dependency** -- cluster storage is self-contained and works in any deployment topology
- **Block storage** -- significantly better performance for databases (PostgreSQL, MySQL, etcd) and random I/O
- **Built-in replication** across nodes, snapshots, and backup to S3
- **Active CNCF project** with broad community support and regular releases
- **Web UI** for monitoring volumes, replicas, and node health

### Trade-offs

- Requires extra VM disks (adds ZFS write amplification since Longhorn replicates on top of TrueNAS-managed zvols)
- Storage capacity limited by total disk space across worker nodes
- Doesn't leverage TrueNAS ZFS features (snapshots, replication, scrubbing) for cluster data
- ReadWriteOnce only -- no shared volumes across pods on different nodes

---

## Advanced: democratic-csi

For users who want per-PV ZFS dataset isolation with dynamic provisioning, [democratic-csi](https://github.com/democratic-csi/democratic-csi) is purpose-built for TrueNAS. Each PV gets its own ZFS dataset (NFS) or zvol (iSCSI).

This is more complex to set up than Longhorn but gives you:
- Per-PV ZFS dataset/zvol isolation
- ZFS snapshots exposed as Kubernetes VolumeSnapshots
- Both NFS and iSCSI protocols

| Mode | Auth | Notes |
|---|---|---|
| SSH-based (`freenas-nfs`, `freenas-iscsi`) | SSH to TrueNAS | Stable, battle-tested. Requires SSH access with root/sudo. |
| API-based (`freenas-api-nfs`, `freenas-api-iscsi`) | REST API | Experimental. 1 GB minimum volume size. REST v2.0 compatibility with TrueNAS 25.04+ should be verified. |

**iSCSI mode** requires the `iscsi-tools` Talos extension:

```yaml
machine:
  install:
    extensions:
      - image: ghcr.io/siderolabs/iscsi-tools:latest
```

See the [democratic-csi documentation](https://github.com/democratic-csi/democratic-csi) for setup instructions.

---

## Talos Extension Requirements

| Storage Option | Extensions Needed |
|---|---|
| Longhorn | None (uses standard block devices) |
| democratic-csi (iSCSI) | `iscsi-tools` |

---

## Further Reading

- [Longhorn Talos Support](https://longhorn.io/docs/1.9.0/advanced-resources/os-distro-specific/talos-linux-support/) -- Longhorn on Talos Linux
- [Siderolabs CSI Storage Guide](https://docs.siderolabs.com/kubernetes-guides/csi/storage) -- Talos-specific CSI documentation
- [democratic-csi](https://github.com/democratic-csi/democratic-csi) -- TrueNAS-native CSI driver
