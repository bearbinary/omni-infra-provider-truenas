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

## Disaster Recovery Workflow

When a VM fails or an upgrade breaks the cluster:

1. **Let Omni handle the VMs.** Remove the failed machine from the cluster in Omni. Omni deprovisions the broken VM and provisions a fresh replacement automatically.
2. **Wait for the cluster to stabilize.** Once the new node joins and the cluster is healthy (`kubectl get nodes`), proceed to restore.
3. **Restore workloads from Velero.** Run `velero restore create --from-backup <latest>` to bring back your applications, configs, and PV data.

## What Each Layer Protects

| Component             | Protected By | Notes                                                   |
|-----------------------|--------------|---------------------------------------------------------|
| Node lifecycle (VMs)  | Omni         | Automatic reprovision — no backup needed                |
| Talos machine config  | Omni         | Declarative — Omni stores and applies it                |
| etcd / control plane  | Omni         | Built-in etcd backup — see Omni docs                    |
| Kubernetes resources  | Velero       | Deployments, Services, ConfigMaps, Secrets, CRDs, etc.  |
| PersistentVolume data | Velero       | Via file-system backup (node-agent) or CSI snapshots    |

## Recommendations

- **Schedule daily backups** with a 7-day TTL as a baseline
- **Test restores regularly** — a backup you've never restored is a backup you can't trust
- **Use a remote S3 target** — backups on the same NAS as your VMs won't survive a hardware failure
- **Back up before cluster upgrades** — run `velero backup create pre-upgrade` before changing Talos or Kubernetes versions in Omni
- **Exclude ephemeral volumes** if needed — use the `backup.velero.io/backup-volumes-excludes` pod annotation for volumes that don't need backup (caches, temp dirs)
