# Backup & Disaster Recovery

This guide covers backup strategies for Kubernetes clusters running on TrueNAS via the Omni infrastructure provider. Backup splits into two concerns: **control plane** (managed by Omni) and **workload data** (PVCs, backed up by Velero).

## Control Plane Backup

Omni handles control plane backup automatically. Cluster state, machine configs, and etcd data are managed by Omni and can be restored through the Omni UI.

See the [Omni backup and disaster recovery documentation](https://omni.siderolabs.com/docs/how-to-guides/how-to-back-up-and-recover-a-cluster/) for:

- Enabling automatic etcd backups
- Restoring a cluster from an etcd snapshot
- Recovering from control plane node failures

Talos nodes are immutable and declarative. If a VM fails, the correct recovery path is to remove it from the cluster in Omni and let Omni reprovision a fresh replacement automatically.

> **Key point:** You do not need to back up VMs or zvols. Omni recreates nodes from scratch using the machine config it stores. VM-level ZFS snapshots are unnecessary for cluster recovery.

## Workload Backup with Velero

**Omni cannot protect your PersistentVolumeClaim data.** Application databases, uploaded files, message queues — anything stored in PVCs lives on your storage backend, not in Omni. If you lose that data, Omni can rebuild the cluster but your application state is gone.

[Velero](https://velero.io/) runs inside the cluster and backs up Kubernetes resources and PVC data to a remote S3 bucket, giving you complete workload recovery independent of the infrastructure.

### Prerequisites

- A running Kubernetes cluster managed by Omni
- A remote S3-compatible backup target (AWS S3, Backblaze B2, Wasabi, MinIO, etc.)
- `kubectl` access to the cluster (via `omnictl kubeconfig`)
- Persistent storage configured (see [Storage Guide](storage.md))

### 1. Install the Velero CLI

```bash
# macOS
brew install velero

# Linux
curl -fsSL https://github.com/vmware-tanzu/velero/releases/latest/download/velero-linux-amd64.tar.gz | \
  tar xz && sudo mv velero-*/velero /usr/local/bin/
```

### 2. Create S3 credentials

Create a credentials file for your S3 bucket:

```bash
cat > /tmp/velero-credentials <<EOF
[default]
aws_access_key_id=<your-access-key>
aws_secret_access_key=<your-secret-key>
EOF
```

### 3. Install Velero into the cluster

```bash
velero install \
  --provider aws \
  --plugins velero/velero-plugin-for-aws:v1.11.0 \
  --bucket <your-bucket-name> \
  --secret-file /tmp/velero-credentials \
  --backup-location-config \
    region=<your-region>,s3ForcePathStyle=true,s3Url=<your-s3-endpoint> \
  --use-node-agent \
  --default-volumes-to-fs-backup
```

Key flags:

- `--use-node-agent` enables file-system-level PV backups (replaces the deprecated restic integration)
- `--default-volumes-to-fs-backup` backs up all PVs by default without requiring per-pod annotations
- `--plugins` must match your storage provider (AWS shown; see [Velero supported providers](https://velero.io/docs/main/supported-providers/) for GCP, Azure, etc.)

**Example with AWS S3:**

```bash
velero install \
  --provider aws \
  --plugins velero/velero-plugin-for-aws:v1.11.0 \
  --bucket my-cluster-backups \
  --secret-file /tmp/velero-credentials \
  --backup-location-config region=us-east-1 \
  --use-node-agent \
  --default-volumes-to-fs-backup
```

**Example with Backblaze B2:**

```bash
velero install \
  --provider aws \
  --plugins velero/velero-plugin-for-aws:v1.11.0 \
  --bucket my-cluster-backups \
  --secret-file /tmp/velero-credentials \
  --backup-location-config \
    region=us-west-004,s3ForcePathStyle=true,s3Url=https://s3.us-west-004.backblazeb2.com \
  --use-node-agent \
  --default-volumes-to-fs-backup
```

### 4. Verify the installation

```bash
velero get backup-locations
kubectl get pods -n velero
```

## Backup Operations

### On-demand backup

```bash
# Back up everything in the cluster
velero backup create full-backup

# Back up a specific namespace
velero backup create app-backup --include-namespaces my-app

# Back up with a TTL (auto-delete after 30 days)
velero backup create weekly-backup --ttl 720h
```

### Scheduled backups

```bash
# Daily backups at 2 AM, retained for 7 days
velero schedule create daily --schedule="0 2 * * *" --ttl 168h

# Weekly full backup
velero schedule create weekly --schedule="0 3 * * 0" --ttl 720h
```

### Check backup status

```bash
velero backup get
velero backup describe <backup-name> --details
velero backup logs <backup-name>
```

## Restore Operations

### Full cluster restore

After Omni reprovisions fresh VMs and the cluster is healthy:

```bash
velero restore create --from-backup full-backup
```

### Namespace restore

```bash
velero restore create --from-backup full-backup --include-namespaces my-app
```

### Restore to a different namespace

```bash
velero restore create --from-backup full-backup \
  --include-namespaces my-app \
  --namespace-mappings my-app:my-app-restored
```

### Check restore status

```bash
velero restore get
velero restore describe <restore-name> --details
```

## Disaster Recovery Scenarios

> **Your data, your responsibility.** The provider manages VM lifecycle and cluster infrastructure. Everything below -- Velero configuration, backup schedules, restore procedures, and data integrity -- is entirely your responsibility. We document these scenarios as a reference, not a guarantee. **Test your restores regularly.**

### Scenario 1: Single Worker Node Failure

A worker VM crashes, gets stuck in ERROR, or is removed from the cluster.

1. **Omni handles it automatically.** The provider deprovisions the broken VM and Omni provisions a fresh replacement.
2. **Longhorn re-replicates.** If you're using Longhorn with replica count >= 2, data is already replicated on other nodes. The new node joins and Longhorn rebuilds replicas automatically. No restore needed.
3. **NFS is unaffected.** NFS data lives on TrueNAS, not the worker. Pods reschedule to healthy nodes and remount.

**Action required:** None -- wait for the cluster to stabilize.

### Scenario 2: Control Plane Failure

The control plane VM crashes or is corrupted.

1. **Omni restores etcd.** Omni manages etcd backups and can restore the control plane from a snapshot. See the [Omni backup and disaster recovery docs](https://omni.siderolabs.com/docs/how-to-guides/how-to-back-up-and-recover-a-cluster/).
2. **Worker data is intact.** PVs on Longhorn or NFS are not affected by control plane failures.
3. **Verify workloads.** Once the control plane is back, check that all Deployments, StatefulSets, and pods are running: `kubectl get pods -A`.

**Action required:** Follow the Omni etcd restore procedure if needed.

### Scenario 3: Total Cluster Loss

All VMs are destroyed (e.g., ZFS pool failure, accidental deletion, TrueNAS hardware failure).

1. **Rebuild the cluster in Omni.** Create a new cluster with the same MachineClasses. Omni provisions fresh VMs.
2. **Reinstall storage.** Run `scripts/install-longhorn.sh <cluster>`.
3. **Reinstall Velero.** Repeat the Velero install command pointing to the same S3 bucket.
4. **List available backups:**
   ```bash
   velero backup get
   ```
5. **Restore everything:**
   ```bash
   velero restore create --from-backup <latest-backup>
   ```
6. **Verify the restore:**
   ```bash
   velero restore describe <restore-name> --details
   kubectl get pods -A
   kubectl get pvc -A  # All PVCs should be Bound
   ```

**Action required:** Full rebuild + Velero restore. This is why off-site S3 backups are critical.

### Scenario 4: Application Data Corruption

A bad deploy, migration, or bug corrupts your database.

1. **Do not delete the PVC.** The corrupted data is still recoverable.
2. **Scale down the affected workload:**
   ```bash
   kubectl scale deployment my-app --replicas=0
   ```
3. **Restore from the last good backup:**
   ```bash
   # Restore only the affected namespace
   velero restore create --from-backup <pre-corruption-backup> \
     --include-namespaces my-app \
     --existing-resource-policy update
   ```
4. **Scale back up and verify:**
   ```bash
   kubectl scale deployment my-app --replicas=1
   ```

**Action required:** Identify the last good backup and restore selectively.

### Scenario 5: TrueNAS NFS Outage (NFS Storage Only)

TrueNAS goes offline or the NFS service stops. Pods using NFS volumes hang.

1. **Fix TrueNAS.** Restart the NFS service or restore the NAS.
2. **Pods recover automatically.** Once NFS is reachable again, hung pods resume. You may need to restart pods that timed out:
   ```bash
   kubectl delete pods --field-selector=status.phase=Failed -A
   ```
3. **If NFS data is lost**, fall back to Velero restore (Scenario 3).

**This is why Longhorn is recommended** -- it has no TrueNAS dependency and survives NAS outages.

### Recovery Time Expectations

| Scenario | Downtime | Data Loss |
|---|---|---|
| Single worker failure | Minutes (auto-replace) | None (Longhorn replicas) |
| Control plane failure | Minutes (Omni etcd restore) | None |
| Total cluster loss | 30-60 min (rebuild + restore) | Since last Velero backup |
| Data corruption | Minutes (selective restore) | Since last good backup |
| TrueNAS NFS outage | Until NFS recovers | None (data on NAS) |

## What Each Layer Protects

| Component             | Protected By | Notes                                                   |
|-----------------------|--------------|---------------------------------------------------------|
| Node lifecycle (VMs)  | Omni         | Automatic reprovision — no backup needed                |
| Talos machine config  | Omni         | Declarative — Omni stores and applies it                |
| etcd / control plane  | Omni         | Built-in etcd backup — see Omni docs                    |
| Kubernetes resources  | Velero       | Deployments, Services, ConfigMaps, Secrets, CRDs, etc.  |
| PersistentVolume data | Velero       | Via file-system backup (node-agent) or CSI snapshots    |

## CSI Snapshots with Velero

The default Velero setup (above) uses **file-system backup** via the node-agent -- it copies files from mounted PVs. This works with any storage backend but is slow for large volumes because it copies every file.

If your CSI driver supports the Kubernetes `VolumeSnapshot` API, Velero can take **CSI snapshots** instead -- instant, crash-consistent, point-in-time captures at the block level. This is significantly faster and more reliable for large databases.

### Which Storage Drivers Support CSI Snapshots?

| Driver | VolumeSnapshot Support | Notes |
|---|---|---|
| **Longhorn** | Yes | Native CSI snapshot support. Recommended. |
| **democratic-csi** | Yes | ZFS-native snapshots exposed as VolumeSnapshots |
| **NFS (nfs-subdir-external-provisioner)** | No | NFS has no snapshot capability -- file-system backup only |

### Prerequisites

- **Velero v1.14+** -- CSI plugin is built-in (no separate plugin install needed)
- **VolumeSnapshot CRDs** installed in the cluster
- **CSI snapshot controller** running in the cluster
- A CSI driver that supports VolumeSnapshots (Longhorn or democratic-csi)

### Setup with Longhorn

**1. Verify VolumeSnapshot CRDs exist:**

```bash
kubectl get crd | grep volumesnapshot
# Should show:
#   volumesnapshotclasses.snapshot.storage.k8s.io
#   volumesnapshotcontents.snapshot.storage.k8s.io
#   volumesnapshots.snapshot.storage.k8s.io
```

If missing, install them:

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/client/config/crd/snapshot.storage.k8s.io_volumesnapshotclasses.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/client/config/crd/snapshot.storage.k8s.io_volumesnapshotcontents.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/client/config/crd/snapshot.storage.k8s.io_volumesnapshots.yaml
```

**2. Create a VolumeSnapshotClass for Longhorn:**

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshotClass
metadata:
  name: longhorn-snapshot
  labels:
    velero.io/csi-volumesnapshot-class: "true"
  annotations:
    snapshot.storage.kubernetes.io/is-default-class: "true"
driver: driver.longhorn.io
deletionPolicy: Delete
parameters:
  type: snap
```

The `velero.io/csi-volumesnapshot-class: "true"` label tells Velero to use this class automatically for Longhorn volumes during backup.

**3. Install Velero with CSI snapshots enabled:**

```bash
velero install \
  --provider aws \
  --plugins velero/velero-plugin-for-aws:v1.11.0 \
  --bucket <your-bucket-name> \
  --secret-file /tmp/velero-credentials \
  --backup-location-config \
    region=<your-region>,s3ForcePathStyle=true,s3Url=<your-s3-endpoint> \
  --features=EnableCSI \
  --use-node-agent \
  --default-volumes-to-fs-backup
```

The key addition is `--features=EnableCSI`. The `--default-volumes-to-fs-backup` flag acts as a fallback for any volumes that don't support CSI snapshots (e.g., if you also have NFS volumes).

**4. Verify CSI snapshot integration:**

```bash
# Create a test backup
velero backup create csi-test --include-namespaces default --wait

# Check the backup used CSI snapshots
velero backup describe csi-test --details | grep -A5 "CSI Snapshots"
```

### Setup with democratic-csi

democratic-csi exposes ZFS snapshots as Kubernetes VolumeSnapshots. Create a VolumeSnapshotClass for your driver:

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshotClass
metadata:
  name: zfs-snapshot
  labels:
    velero.io/csi-volumesnapshot-class: "true"
driver: org.democratic-csi.nfs  # or org.democratic-csi.iscsi
deletionPolicy: Delete
```

The rest of the Velero setup is identical to the Longhorn steps above.

### CSI Snapshot Data Movement (Recommended)

By default, CSI snapshots stay on the same storage backend as the original volume. If TrueNAS fails, you lose both the volume and its snapshots. **CSI Snapshot Data Movement** copies snapshot data to your remote S3 bucket, giving you true off-site protection.

This is enabled automatically when you use `--features=EnableCSI` with `--use-node-agent`. Velero creates a temporary PVC from the snapshot, mounts it read-only, and uploads the data to S3 via the node-agent.

See the [Velero CSI Snapshot Data Movement docs](https://velero.io/docs/main/csi-snapshot-data-movement/) for advanced configuration.

### File-System Backup vs CSI Snapshots

| | File-System Backup | CSI Snapshots |
|---|---|---|
| **Speed** | Slow (copies every file) | Fast (block-level snapshot) |
| **Consistency** | Application must be quiesced | Crash-consistent automatically |
| **Works with NFS** | Yes | No |
| **Works with Longhorn** | Yes | Yes (recommended) |
| **Off-site copy** | Direct to S3 | Via data movement to S3 |

For most users running Longhorn, enable both: CSI snapshots for Longhorn volumes (fast, consistent) and file-system backup as the fallback.

## Recommendations

- **Schedule daily backups** with a 7-day TTL as a baseline
- **Test restores regularly** — a backup you've never restored is a backup you can't trust
- **Use a remote S3 target** — backups on the same NAS as your VMs won't survive a hardware failure
- **Back up before cluster upgrades** — run `velero backup create pre-upgrade` before changing Talos or Kubernetes versions in Omni
- **Exclude ephemeral volumes** if needed — use the `backup.velero.io/backup-volumes-excludes` pod annotation for volumes that don't need backup (caches, temp dirs)
