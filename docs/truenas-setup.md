# TrueNAS Setup Guide

Step-by-step instructions for configuring TrueNAS SCALE to work with the Omni infrastructure provider. This covers everything that needs to be set up on the TrueNAS side — the provider handles the rest automatically.

For the overall deployment walkthrough (including Omni account, provider installation, and cluster creation), see the [Getting Started](getting-started.md) guide.

---

## Prerequisites

- TrueNAS SCALE 25.04+ (Fangtooth) — check in **Dashboard** or **System > General**
- A ZFS pool with available space
- Admin access to the TrueNAS web UI

---

## 1. Network Bridge

VMs need a network interface to communicate. A bridge lets VMs share your NAS's physical network connection.

1. Go to **Network > Interfaces**
2. Click **Add**
3. Set **Type** to **Bridge**
4. **Bridge Members**: select your primary network interface (the one your NAS uses for its IP — look for names like `enp5s0`, `eno1`, `eth0`)
5. **Name**: leave the default (usually `br0`)
6. **DHCP**: enable
7. Click **Save**

!!! warning
    Creating a bridge on your primary interface briefly interrupts the NAS's network connection. This is normal — the NAS's IP moves to the bridge. After reconnecting, both the NAS and all VMs share this bridge.

8. **Apply the network changes** when prompted
9. After reconnecting, verify the bridge appears under **Network > Interfaces** with an IP address

**Record the bridge name** (e.g., `br0`) — you'll use it as `DEFAULT_NETWORK_INTERFACE` in the provider config and `network_interface` in MachineClass configs.

### Storage Network Bridge (Optional)

If you want a dedicated storage network (for NFS or iSCSI traffic between your cluster and TrueNAS), create a second bridge on a separate physical NIC or VLAN:

1. Go to **Network > Interfaces > Add**
2. Set **Type** to **Bridge**
3. **Bridge Members**: select the dedicated storage NIC (e.g., `enp6s0`)
4. **Name**: e.g., `br-storage`
5. Assign a static IP on your storage subnet (e.g., `10.10.10.1/24`)
6. Click **Save** and apply

Use this bridge in your MachineClass `additional_nics` config:

```yaml
additional_nics:
  - network_interface: br-storage
    mtu: 9000  # optional: jumbo frames for better throughput
```

### Jumbo Frames (MTU 9000)

For storage bridges, jumbo frames significantly improve NFS/iSCSI throughput. **All devices on the path must use the same MTU** — the bridge, the physical switch ports, and the VM NICs.

1. Go to **Network > Interfaces**
2. Click your storage bridge (e.g., `br-storage`)
3. Set **MTU** to `9000`
4. Click **Save** and apply
5. Configure the same MTU on your physical switch ports

The provider handles the VM side automatically when you set `mtu: 9000` in `additional_nics`.

---

## 2. NFS Share (for Persistent Storage)

If your Kubernetes apps need persistent storage, the simplest option is NFS. TrueNAS serves the share, and your cluster mounts it.

### Create the Dataset

1. Go to **Datasets**
2. Select your pool (e.g., `tank`)
3. Click **Add Dataset**
4. **Name**: `k8s-nfs`
5. **Share Type**: leave as **Generic** (NFS is configured separately)
6. Click **Save**

### Enable the NFS Service

1. Go to **System > Services**
2. Find **NFS** in the list
3. Toggle it **on**
4. Check **Start Automatically** so it survives reboots
5. Click the pencil icon to edit NFS settings:
    - **Number of Servers**: leave default (or increase to 16 for better concurrency)
    - **Enable NFSv4**: recommended
6. Click **Save**

### Create the NFS Share

1. Go to **Shares > NFS**
2. Click **Add**
3. **Path**: `/mnt/tank/k8s-nfs` (or wherever your dataset is)
4. **Maproot User**: `root`
5. **Maproot Group**: `wheel`
6. **Authorized Networks**: add your cluster subnet (e.g., `192.168.1.0/24`) — this restricts who can mount the share
7. Click **Save**
8. If prompted to enable the NFS service, confirm

### Verify

From any machine on your network:

```bash
showmount -e <truenas-ip>
# Should show: /mnt/tank/k8s-nfs <your-subnet>
```

You can now use this share with [democratic-csi](storage.md#democratic-csi-nfs-or-iscsi), [manual NFS PVs](storage.md#manual-nfs-pvs-fallback), or the nfs-subdir-external-provisioner.

---

## 3. iSCSI Service (for Block Storage)

iSCSI provides block-level storage — significantly faster than NFS for databases and random I/O workloads. Used with democratic-csi in iSCSI mode.

### Enable the iSCSI Service

1. Go to **System > Services**
2. Find **iSCSI** in the list
3. Toggle it **on**
4. Check **Start Automatically**

### Configure iSCSI (for democratic-csi)

If using democratic-csi in iSCSI mode, the driver creates targets and zvols automatically. You just need the service running. The driver handles:

- Creating a zvol for each PersistentVolume
- Creating an iSCSI target and extent
- Mapping the zvol to the target

No manual target or extent configuration is needed — democratic-csi does it all via SSH or API.

### Talos Extension

Your Talos nodes need the `iscsi-tools` extension to connect to iSCSI targets. Add it to your MachineClass:

```yaml
extensions:
  - siderolabs/iscsi-tools
```

Or via Omni config patch:

```yaml
machine:
  install:
    extensions:
      - image: ghcr.io/siderolabs/iscsi-tools:latest
```

---

## 4. SSH Access (for democratic-csi SSH Mode)

democratic-csi's SSH-based drivers execute ZFS commands directly on TrueNAS. This is the most battle-tested mode.

### Create a Dedicated User

Don't use `root` — create a dedicated service account:

1. Go to **Credentials > Local Users**
2. Click **Add**
3. **Username**: `csi`
4. **Full Name**: `CSI Storage Driver`
5. **Password**: set a strong password (or use SSH key auth — see below)
6. **Home Directory**: `/nonexistent`
7. **Shell**: `bash`
8. Click **Save**

### Grant Sudo Access

The CSI driver needs to run ZFS commands as root:

1. Go to **System > Advanced > Allowed Sudo Commands**
2. Add a sudoers entry for the `csi` user. On TrueNAS SCALE 25.04+:
    - Go to **Credentials > Local Users** > click `csi` > **Edit**
    - Enable **Permit Sudo**
    - Or add to the sudoers group

Alternatively, use SSH key authentication (more secure):

### SSH Key Authentication (Recommended)

1. Generate an SSH key pair on the machine where democratic-csi will run (or use your existing key):
   ```bash
   ssh-keygen -t ed25519 -f ~/.ssh/csi-truenas -N ""
   ```
2. Copy the **public key** (`~/.ssh/csi-truenas.pub`)
3. In TrueNAS: **Credentials > Local Users** > click `csi` > **Edit**
4. Paste the public key into the **SSH Public Key** field
5. Click **Save**

### Enable SSH Service

1. Go to **System > Services**
2. Find **SSH** in the list
3. Toggle it **on**
4. Check **Start Automatically**
5. Click the pencil icon:
    - **Allow TCP Port Forwarding**: disable (not needed)
    - **Password Login Groups**: leave empty if using key auth only
6. Click **Save**

### Verify

```bash
ssh -i ~/.ssh/csi-truenas csi@<truenas-ip> "sudo zfs list"
# Should show your ZFS pools and datasets
```

---

## 5. API Key (for Remote Provider Deployment)

If running the provider **outside** TrueNAS (Kubernetes deployment, remote Docker), you need an API key for the WebSocket connection.

!!! note
    Skip this if running the provider directly on TrueNAS — the Unix socket is mounted automatically and requires no authentication.

1. Go to **Credentials > API Keys**
2. Click **Add**
3. **Name**: `omni-infra-provider`
4. Click **Save**
5. **Copy the key immediately** — it's only shown once

Use this key as `TRUENAS_API_KEY` in your provider config.

---

## Verification Checklist

Before deploying the provider, verify everything is set up:

| Item | How to Check | Expected |
|------|-------------|----------|
| TrueNAS version | Dashboard | 25.04+ (Fangtooth) |
| ZFS pool | **Storage** | Pool visible with free space |
| Network bridge | **Network > Interfaces** | Bridge has an IP, VMs can reach it |
| NFS service | **System > Services** | Running, auto-start enabled |
| NFS share | **Shares > NFS** | Share visible, path correct |
| iSCSI service (if needed) | **System > Services** | Running, auto-start enabled |
| SSH service (if needed) | **System > Services** | Running, CSI user can connect |
| API key (if remote) | **Credentials > API Keys** | Key created and saved |

Once everything checks out, proceed to [install the provider](getting-started.md#step-3-install-the-provider).

---

## Common Mistakes

| Mistake | Symptom | Fix |
|---------|---------|-----|
| Using pool name `tank/k8s` instead of `tank` | `pool not found` error | The `pool` field must be a top-level ZFS pool. Use `dataset_prefix` for nested paths. |
| Bridge not created | VMs have no network | Create a bridge under **Network > Interfaces** |
| NFS service not running | Pods stuck in `ContainerCreating` | Enable NFS under **System > Services** |
| NFS share not authorized for cluster subnet | `mount: permission denied` | Add your cluster subnet to **Authorized Networks** on the share |
| SSH key not added to CSI user | democratic-csi can't connect | Paste public key into the user's SSH Public Key field |
| MTU mismatch | Dropped packets, poor storage performance | Set the same MTU on the bridge, switch ports, and VM NIC config |
| Using TrueNAS SCALE < 25.04 | Provider fails at startup | Upgrade to 25.04+ (Fangtooth) |
