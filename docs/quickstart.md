---
title: "Quick Start — Configuration and Deployment"
description: "Deploy omni-infra-provider-truenas on TrueNAS SCALE, Kubernetes, or Docker Compose. Full configuration reference for all environment variables."
---

# Quick Start

## Prerequisites

1. **TrueNAS SCALE 25.04+** — the JSON-RPC 2.0 API is required
2. **Omni instance** with an infrastructure provider service account
3. **ZFS pool** with available space
4. **Network interface** for VM traffic — a bridge (e.g., `br0`), VLAN, or physical NIC

## Create the Omni Service Account

```bash
omnictl serviceaccount create --role=InfraProvider infra-provider:truenas
# Save the output — it contains OMNI_SERVICE_ACCOUNT_KEY
```

## Deployment Options

=== "Docker Compose on TrueNAS (Recommended)"

    Run the container directly on your TrueNAS host via **Apps > Discover > Install via YAML**. Create an API key first — see [TrueNAS Setup > API Key](truenas-setup.md#5-api-key) for the recommended dedicated non-root user + scoped roles. Do **not** use the `root` user's key.

    ```yaml
    services:
      omni-infra-provider-truenas:
        image: ghcr.io/bearbinary/omni-infra-provider-truenas:latest
        restart: unless-stopped
        network_mode: host
        environment:
          OMNI_ENDPOINT: "https://omni.example.com"
          OMNI_SERVICE_ACCOUNT_KEY: "<your-key>"
          TRUENAS_HOST: "localhost"
          TRUENAS_API_KEY: "<truenas-api-key>"
          TRUENAS_INSECURE_SKIP_VERIFY: "true"
          DEFAULT_POOL: "default"
          DEFAULT_NETWORK_INTERFACE: "br0"
    ```

=== "Kubernetes (Helm)"

    ```bash
    helm install omni-infra-provider deploy/helm/omni-infra-provider-truenas \
      --namespace omni-infra-provider --create-namespace \
      --set omniEndpoint="https://omni.example.com" \
      --set truenasHost="truenas.local" \
      --set secrets.omniServiceAccountKey="<your-key>" \
      --set secrets.truenasApiKey="<your-api-key>" \
      --set defaults.pool="default"
    ```

    See `deploy/helm/omni-infra-provider-truenas/values.yaml` for all options.

=== "Docker Compose (Remote)"

    ```bash
    cp .env.example .env
    # Fill in OMNI_ENDPOINT, OMNI_SERVICE_ACCOUNT_KEY, TRUENAS_HOST, TRUENAS_API_KEY
    docker compose -f deploy/docker-compose.yaml up -d
    ```

## Configuration Reference

### Required

| Variable | Description |
|---|---|
| `OMNI_ENDPOINT` | Omni instance URL (e.g., `https://omni.example.com`) |
| `OMNI_SERVICE_ACCOUNT_KEY` | Omni infra provider service account key |

### TrueNAS Connection

| Variable | Default | Description |
|---|---|---|
| `TRUENAS_HOST` | — | **Required.** TrueNAS hostname or IP (use `localhost` when running the container on the TrueNAS host itself) |
| `TRUENAS_API_KEY` | — | **Required.** TrueNAS API key from a dedicated non-root user with scoped roles ([setup](truenas-setup.md#5-api-key)). |
| `TRUENAS_INSECURE_SKIP_VERIFY` | `false` | Skip TLS verification for self-signed certs (recommended `true` for `localhost`) |

!!! note
    TrueNAS 25.10 removed implicit Unix socket authentication, so an API key is required in all deployments.

### Provider Defaults

| Variable | Default | Description |
|---|---|---|
| `DEFAULT_POOL` | `default` | ZFS pool for VM zvols and ISO cache |
| `DEFAULT_NETWORK_INTERFACE` | — | Network interface for VM NICs |
| `DEFAULT_BOOT_METHOD` | `UEFI` | VM boot method (`UEFI` or `BIOS`) |
| `CONCURRENCY` | `4` | Max parallel provision/deprovision workers |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

### Operational

| Variable | Default | Description |
|---|---|---|
| `GRACEFUL_SHUTDOWN_TIMEOUT` | `30` | Seconds to wait for ACPI shutdown before force-stop |
| `MAX_ERROR_RECOVERIES` | `5` | Max consecutive ERROR recoveries before auto-replacing a VM (set to `-1` to disable) |
| `HEALTH_LISTEN_ADDR` | `:8081` | Address for `/healthz` and `/readyz` HTTP endpoints |

### MachineClass Config

These fields go in the MachineClass `configpatch`:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `cpus` | int | Yes | `2` | Virtual CPUs (min: 1). For control planes, see [Sizing Guide](sizing.md). |
| `memory` | int | Yes | `4096` | **Maximum / hard memory limit** in MiB (min: 1024). When `min_memory` is unset, this is fully reserved on the TrueNAS host at vm.start — if the host can't lock the full amount, the VM fails with ENOMEM. For control planes, see [Sizing Guide](sizing.md). |
| `min_memory` | int | No | — | Optional **soft memory floor** in MiB. When set, the VM starts with `min_memory` reserved and balloons up to `memory` as host RAM is available — useful for over-committing on tight hosts. Must be ≥ 1024 and ≤ `memory`. Note: Talos doesn't auto-load virtio-balloon, so the VM may sit at `min_memory` until balloon is enabled in-guest; size `min_memory` to what Talos actually needs. See [Troubleshooting § Host out of memory](troubleshooting.md#vm-creation-succeeds-but-vm-wont-start-host-out-of-memory). |
| `disk_size` | int | Yes | `40` | Root disk in GiB (min: 20 — the floor is there because Talos CP nodes pull kube-apiserver + etcd + scheduler + controller-manager + CNI + CoreDNS images during bootstrap and 10 GiB trips DiskPressure mid-install. See [Sizing Guide § Why the root disk has a 20 GiB minimum](sizing.md#why-the-root-disk-has-a-20-gib-minimum)). |
| `pool` | string | Yes | `DEFAULT_POOL` | ZFS pool for zvols and ISOs |
| `network_interface` | string | Yes | `DEFAULT_NETWORK_INTERFACE` | Bridge, VLAN, or physical interface |
| `boot_method` | string | Yes | `UEFI` | `UEFI` or `BIOS` |
| `architecture` | string | Yes | `amd64` | `amd64` or `arm64` |
| `encrypted` | bool | No | `false` | Enable ZFS encryption (AES-256-GCM) on the root disk |
| `dataset_prefix` | string | No | — | Nested dataset path (e.g., `prod/k8s` → `pool/prod/k8s/omni-vms/`) |
| `extensions` | list | No | — | Additional Talos extensions (e.g., `siderolabs/iscsi-tools`) |
| `advertised_subnets` | string | No | — | Pin etcd/kubelet to specific CIDRs for multi-NIC setups |
| `additional_disks` | list | No | — | Extra data disks — see below |
| `additional_nics` | list | No | — | Extra NICs for network segmentation — see below |

#### `additional_disks` items

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `size` | int | Yes | — | Disk size in GiB |
| `pool` | string | No | primary pool | ZFS pool override for this disk |
| `dataset_prefix` | string | No | MachineClass prefix | Per-disk dataset prefix override |
| `encrypted` | bool | No | `false` | Per-disk encryption toggle |
| `name` | string | No | `data-N` (1-indexed) | Talos `UserVolumeConfig` name — the disk is formatted and mounted at `/var/mnt/<name>` inside the guest. Set to `longhorn` to match Longhorn's `defaultDataPath`. `storage_disk_size` auto-sets this to `longhorn`. |
| `filesystem` | string | No | `xfs` | Filesystem for the emitted `UserVolumeConfig` — `xfs` (default) or `ext4`. |

Each additional disk is attached to the VM **and** made usable inside the guest:
the provider emits a Talos `UserVolumeConfig` per disk (selector keyed by exact
zvol byte-size) so Talos formats it and mounts it at `/var/mnt/<name>`. Without
this, the disk shows up as a raw unformatted block device and Kubernetes
storage drivers can't see it.

#### `additional_nics` items

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `network_interface` | string | Yes | — | Bridge, VLAN, or physical interface |
| `type` | string | No | `VIRTIO` | `VIRTIO` or `E1000` |
| `mtu` | int | No | host default | MTU size (set to `9000` for jumbo frames) |

All NICs (primary and additional) get a deterministic MAC derived from the machine request ID, so DHCP reservations survive reprovisioning.
