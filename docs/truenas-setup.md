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

You can now use this share with [democratic-csi](storage.md#advanced-democratic-csi) or manual NFS PV definitions.

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

## 5. API Key

The provider connects to TrueNAS via WebSocket with an API key — required in all deployments (as of v0.14.0 / TrueNAS 25.10, which removed implicit Unix-socket auth).

### Recommended: dedicated non-root user in `builtin_administrators`

Create a user dedicated to the provider and add it to the **`builtin_administrators`** group. This is better than using the `root` user's key:

- The provider's API audit trail is separated from interactive admin activity.
- The key can be revoked by deleting just the provider user, without touching `root`.
- No password on the user → fewer attack surfaces than root, which typically has a console password.

**Why `builtin_administrators` membership (not a scoped privilege):**

The provider uploads Talos ISOs to TrueNAS via the `/_upload` HTTP endpoint (`filesystem.put` with a pipe-based multipart request). This endpoint enforces the `SYS_ADMIN` account attribute on top of the regular role system. `SYS_ADMIN` is granted **only** by membership in `builtin_administrators` — no custom privilege or granular role combination substitutes for it. This was verified empirically on TrueNAS SCALE 25.10.1; see [upstream bug reports](upstream-bugs/) for the specifics.

Users not in `builtin_administrators` can call every JSON-RPC method the provider needs (VM lifecycle, dataset CRUD, filesystem queries) — but ISO upload fails with HTTP 403. The provider does not currently have a fallback, so `builtin_administrators` membership is required.

### Setup

**Create the user:**

1. **Credentials > Local Users > Add**
2. **Username**: `omni-provider` (or similar)
3. **Full Name**: `Omni Infra Provider`
4. **Password Disabled**: ✅ check (API-only, no interactive login)
5. **Shell**: `nologin`
6. **Create New Primary Group**: ✅ check
7. Save

**Add the user to `builtin_administrators`:**

1. **Credentials > Groups > builtin_administrators > Edit**
2. Under **Members**, add the `omni-provider` user.
3. Save.

Or via `midclt` (requires root / another admin):

```bash
# Get current member list + the new user's id
GROUP_ID=$(sudo midclt call group.query '[["name","=","builtin_administrators"]]' | jq '.[0].id')
USER_ID=$(sudo midclt call user.query '[["username","=","omni-provider"]]' | jq '.[0].id')
CURRENT=$(sudo midclt call group.query '[["name","=","builtin_administrators"]]' | jq -c '.[0].users')
NEW=$(echo "$CURRENT" | jq -c ". + [$USER_ID]")
sudo midclt call group.update "$GROUP_ID" "{\"users\": $NEW}"
```

**Create the API key for this user:**

1. **Credentials > API Keys > Add**
2. **Name**: `omni-infra-provider`
3. **Username**: select `omni-provider`
4. Save and **copy the key immediately** — it's shown only once.

Use this key as `TRUENAS_API_KEY` in your provider config.

### Can scoped privileges work instead?

Empirically, no — not in TrueNAS 25.10.1. A custom privilege with these 13 roles covers every JSON-RPC method the provider calls:

```
READONLY_ADMIN, VM_READ, VM_WRITE, VM_DEVICE_READ, VM_DEVICE_WRITE,
DATASET_READ, DATASET_WRITE, DATASET_DELETE,
POOL_READ, DISK_READ, NETWORK_INTERFACE_READ,
FILESYSTEM_ATTRS_READ, FILESYSTEM_DATA_WRITE
```

A user with exactly these roles (and nothing else) can provision and deprovision VMs end-to-end EXCEPT for the Talos ISO upload, which fails at `/_upload` with HTTP 403 because `SYS_ADMIN` is missing.

This is [a TrueNAS upstream bug](upstream-bugs/truenas-upload-role-gap.md) — `FILESYSTEM_DATA_WRITE` should reasonably cover HTTP file-upload, not just the JSON-RPC `filesystem.put` method — but until upstream fixes it, `builtin_administrators` membership is required.

### Do not

- **Do not** use the literal `root` user's API key. Create a dedicated user instead.
- **Do not** attach `FULL_ADMIN` as a role to a custom privilege in TrueNAS 25.10.1. It triggers an [infinite-recursion middleware bug](upstream-bugs/truenas-role-recursion.md) that breaks all auth for users bound to that privilege; recovery requires editing the privilege via `midclt` to remove the offending roles.

### Going further

For the full hardening story (key rotation, network isolation, secret storage, TLS, container-level controls, ZFS encryption, monitoring), see [Security Hardening](hardening.md).

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
