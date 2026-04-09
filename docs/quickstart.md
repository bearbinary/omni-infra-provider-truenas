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

### MachineClass Config

These fields go in the MachineClass `configpatch`:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `cpus` | int | Yes | `2` | Virtual CPUs (min: 1) |
| `memory` | int | Yes | `4096` | Memory in MiB (min: 1024) |
| `disk_size` | int | Yes | `40` | Root disk in GiB (min: 10) |
| `pool` | string | No | `DEFAULT_POOL` | ZFS pool for zvols and ISOs |
| `network_interface` | string | No | `DEFAULT_NETWORK_INTERFACE` | Bridge, VLAN, or physical interface |
| `boot_method` | string | No | `UEFI` | `UEFI` or `BIOS` |
| `architecture` | string | No | `amd64` | `amd64` or `arm64` |
| `extensions` | list | No | — | Additional Talos extensions |
