# Omni Infrastructure Provider for TrueNAS SCALE

Automatically provisions and manages Talos Linux virtual machines on TrueNAS SCALE through the [Sidero Omni](https://omni.siderolabs.com/) platform.

> **Requires TrueNAS SCALE 25.04+ (Fangtooth).** This provider uses the JSON-RPC 2.0 API exclusively. The legacy REST v2.0 API is **not supported**.

## How It Works

The provider connects to both Omni and TrueNAS, watching for machine requests and translating them into VM lifecycle operations:

1. **Omni creates a MachineRequest** — a user scales a cluster or creates a MachineSet
2. **Provider generates a Talos schematic** — defines the OS image with extensions (e.g., `qemu-guest-agent`)
3. **Provider downloads the Talos ISO** — from [Image Factory](https://factory.talos.dev/), cached on TrueNAS to avoid re-downloading
4. **Provider creates a VM** — zvol for disk, CDROM with ISO, NIC on your bridge, starts the VM
5. **VM boots Talos, joins Omni** — via SideroLink (outbound WireGuard tunnel)

When machines are removed, the provider stops the VM, deletes it, and cleans up the zvol.

## Transport: JSON-RPC 2.0 Only

This provider communicates with TrueNAS via **JSON-RPC 2.0** — the same protocol used by TrueNAS's own CLI (`midclt`) and web UI.

Two transports are supported, auto-detected in priority order:

| Transport | When Used | Auth |
|---|---|---|
| **Unix socket** | Running as a TrueNAS app (socket mounted) | **None required** — trusted local process |
| **WebSocket** | Running remotely (k8s, Docker on another host) | API key required |

The legacy REST v2.0 API (`/api/v2.0/`) is **not supported**. TrueNAS deprecated it in 25.04 and will remove it in 26.x.

## Quick Start

### Option 1: TrueNAS App (Recommended)

Deploy directly on your TrueNAS server. The middleware Unix socket is mounted automatically — **no API key needed**.

Paste into TrueNAS Apps > Discover > Install via YAML:

```yaml
services:
  omni-infra-provider-truenas:
    image: ghcr.io/bearbinary/omni-infra-provider-truenas:latest
    restart: unless-stopped
    volumes:
      - /var/run/middleware:/var/run/middleware:ro
    network_mode: host
    environment:
      OMNI_ENDPOINT: "https://omni.example.com"
      OMNI_SERVICE_ACCOUNT_KEY: "<your-key>"
      DEFAULT_POOL: "default"
      DEFAULT_NIC_ATTACH: "br0"  # or vlan100, enp5s0, etc.
```

### Option 2: Kubernetes

```bash
# Edit the configmap and secret with your values
kubectl apply -k deploy/kubernetes/
```

### Option 3: Docker Compose (Remote)

For running on a separate host, the WebSocket transport is used:

```bash
cp .env.example .env
# Fill in OMNI_ENDPOINT, OMNI_SERVICE_ACCOUNT_KEY, TRUENAS_HOST, TRUENAS_API_KEY
docker compose -f deploy/docker-compose.yaml up -d
```

## Prerequisites

1. **TrueNAS SCALE 25.04+** — the JSON-RPC 2.0 API is required
2. **Omni instance** with an infrastructure provider service account
3. **ZFS pool** with available space (default: `default`)
4. **Network interface** for VM traffic — a bridge (e.g., `br0`), VLAN interface (e.g., `vlan100`), or physical NIC

### Creating the Omni Service Account

```bash
omnictl serviceaccount create --role=InfraProvider infra-provider:truenas
```

## Configuration

All configuration is via environment variables. A `.env` file is loaded automatically if present.

| Variable | Required | Default | Description |
|---|---|---|---|
| `OMNI_ENDPOINT` | Yes | — | Omni instance URL |
| `OMNI_SERVICE_ACCOUNT_KEY` | Yes | — | Omni infra provider key |
| `TRUENAS_HOST` | No* | — | TrueNAS hostname (for WebSocket transport) |
| `TRUENAS_API_KEY` | No* | — | TrueNAS API key (for WebSocket transport) |
| `DEFAULT_POOL` | No | `default` | ZFS pool for VM zvols (ISOs cached at `<pool>/talos-iso/` automatically) |
| `DEFAULT_NIC_ATTACH` | No | — | Network interface for VM NICs (bridge, VLAN, or physical) |
| `DEFAULT_BOOT_METHOD` | No | `UEFI` | VM boot method (`UEFI` or `BIOS`) |
| `CONCURRENCY` | No | `4` | Max parallel provision/deprovision workers |
| `LOG_LEVEL` | No | `info` | Log level (`debug`, `info`, `warn`, `error`) |

*Not required when running on TrueNAS with the Unix socket mounted.

### ISO Handling

Talos ISOs are **downloaded automatically** from [Image Factory](https://factory.talos.dev/) based on the schematic generated for each machine request. ISOs are cached on the TrueNAS filesystem (at `<pool>/talos-iso/`) and deduplicated by SHA-256 hash — the same image is never downloaded twice.

## Usage

Once the provider is running and connected to Omni, create a MachineClass and MachineSet to trigger VM provisioning.

### Via CLI (`omnictl`)

**1. Register the infra provider** (one-time setup):

```bash
omnictl infraprovider create truenas
```

**2. Create a service account** (one-time setup):

```bash
omnictl serviceaccount create --role=InfraProvider infra-provider:truenas
```

Save the output — it contains the `OMNI_SERVICE_ACCOUNT_KEY` needed by the provider.

**3. Create a MachineClass:**

```bash
cat <<'EOF' | omnictl apply -f -
metadata:
  namespace: default
  type: MachineClasses.omni.sidero.dev
  id: truenas-small
spec:
  autoprovision:
    providerid: truenas
    grpcendpoint: ""
    icon: ""
    configpatch: |
      cpus: 2
      memory: 4096
      disk_size: 40
EOF
```

For custom values per MachineClass (overrides provider defaults):

```bash
cat <<'EOF' | omnictl apply -f -
metadata:
  namespace: default
  type: MachineClasses.omni.sidero.dev
  id: truenas-large
spec:
  autoprovision:
    providerid: truenas
    grpcendpoint: ""
    icon: ""
    configpatch: |
      cpus: 8
      memory: 16384
      disk_size: 200
      pool: "fast-nvme"
      nic_attach: "vlan100"
EOF
```

**4. Use the MachineClass in a cluster** — assign it when creating a cluster or MachineSet in Omni.

### Via Omni Web UI

**1. Navigate to** Clusters > Create Cluster (or edit an existing cluster).

**2. In the machine selection step**, choose "Auto Provision" and select the `truenas` provider.

**3. Configure the machine**, filling in:

| Field | Default | Description |
|---|---|---|
| **CPUs** | `2` | Number of virtual CPUs |
| **Memory (MiB)** | `4096` | RAM in MiB |
| **Disk Size (GiB)** | `40` | Root disk size |
| **ZFS Pool** | *(provider default)* | Pool for zvols and ISOs |
| **NIC Attach** | *(provider default)* | Bridge, VLAN, or physical interface |
| **Boot Method** | `UEFI` | `UEFI` or `BIOS` |
| **Architecture** | `amd64` | `amd64` or `arm64` |

Fields left blank use the provider's defaults (`DEFAULT_POOL`, `DEFAULT_NIC_ATTACH`, etc.).

**4. Set the desired replica count** and create the cluster. The provider will automatically provision VMs on TrueNAS.

### Recommended MachineClasses

| Class | CPUs | Memory | Disk | Use Case |
|---|---|---|---|---|
| `truenas-control-plane` | 2 | 2048 MiB | 10 GiB | Control plane nodes (etcd + API server) |
| `truenas-worker` | 4 | 8192 MiB | 100 GiB | Worker nodes (application workloads + container images) |

Example — control plane (runs etcd + API server, no container images):

```yaml
cpus: 2
memory: 2048
disk_size: 10
```

Example — worker (runs workloads, stores container images):

```yaml
cpus: 4
memory: 8192
disk_size: 100
```

> **Note:** Talos requires a minimum of 2 GiB RAM for control plane nodes. Control plane disks only store the OS (~1 GB) and etcd data — 10 GiB is sufficient. Workers need more disk for container images.

### MachineClass Config Reference

These fields go in the MachineClass `configpatch` (CLI) or the provider config form (UI):

```yaml
cpus: 2              # Required. Virtual CPUs (minimum: 1)
memory: 2048         # Required. Memory in MiB (minimum: 1024, recommend 2048+ for control planes)
disk_size: 20        # Required. Root disk in GiB (minimum: 10)
pool: "default"      # Optional. ZFS pool (defaults to DEFAULT_POOL)
nic_attach: "br100"  # Optional. NIC target (defaults to DEFAULT_NIC_ATTACH)
boot_method: "UEFI"  # Optional. UEFI or BIOS (defaults to UEFI)
architecture: "amd64" # Optional. amd64 or arm64 (defaults to amd64)
extensions:           # Optional. Additional Talos extensions beyond defaults
  - "siderolabs/iscsi-tools"
```

### Talos System Extensions

Every VM automatically includes these extensions — you do **not** need to add them:

- `siderolabs/qemu-guest-agent` — hypervisor-to-guest communication
- `siderolabs/nfs-utils` — NFS client support for storage
- `siderolabs/util-linux-tools` — required for mount/block device operations

To add more extensions (e.g., `iscsi-tools`), use the `extensions` field in the MachineClass config. These are merged with the defaults when generating the image schematic.

## Development

```bash
make build            # Build binary
make test             # Unit tests
make test-v           # Verbose tests
make test-integration # Integration tests (requires TrueNAS instance)
make lint             # Linters
make image            # Docker image
make generate         # Regenerate protobuf
```

See [docs/testing.md](docs/testing.md) for integration and E2E testing setup.

## License

MIT — see [LICENSE](LICENSE).
