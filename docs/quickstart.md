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

=== "TrueNAS App (Recommended)"

    Deploy directly on your TrueNAS server. The middleware Unix socket is mounted automatically — **no API key needed**.

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
          DEFAULT_NETWORK_INTERFACE: "br0"
    ```

=== "Kubernetes"

    ```bash
    kubectl apply -k deploy/kubernetes/
    ```

    See `deploy/kubernetes/` for the full manifests.

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
| `TRUENAS_HOST` | — | TrueNAS hostname (WebSocket transport only) |
| `TRUENAS_API_KEY` | — | TrueNAS API key (WebSocket transport only) |
| `TRUENAS_INSECURE_SKIP_VERIFY` | `false` | Skip TLS verification for self-signed certs |
| `TRUENAS_SOCKET_PATH` | `/var/run/middleware/middlewared.sock` | Override Unix socket path |

!!! note
    Not required when running on TrueNAS with the Unix socket mounted.

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
| `cpus` | int | Yes | `2` | Virtual CPUs (min: 1) |
| `memory` | int | Yes | `4096` | Memory in MiB (min: 1024) |
| `disk_size` | int | Yes | `40` | Root disk in GiB (min: 10) |
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
| `encrypted` | bool | No | `false` | Per-disk encryption toggle |

#### `additional_nics` items

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `network_interface` | string | Yes | — | Bridge, VLAN, or physical interface |
| `type` | string | No | `VIRTIO` | `VIRTIO` or `E1000` |
| `mtu` | int | No | host default | MTU size (set to `9000` for jumbo frames) |
| `deterministic_mac` | bool | No | `false` | Derive a stable MAC from machine request ID |
