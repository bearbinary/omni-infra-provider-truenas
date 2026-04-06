# Omni TrueNAS Provider

Automatically provisions and manages Talos Linux virtual machines on TrueNAS SCALE through the [Sidero Omni](https://omni.siderolabs.com/) platform.

> **Requires TrueNAS SCALE 25.04+ (Fangtooth).** Uses the JSON-RPC 2.0 API exclusively. The legacy REST v2.0 API is not supported.

## What It Does

This infrastructure provider connects your TrueNAS SCALE server to Omni, enabling automatic VM lifecycle management:

- **Provisions** Talos Linux VMs on demand when Omni requests new machines
- **Downloads ISOs automatically** from [Image Factory](https://factory.talos.dev/) — no manual ISO management needed
- **Deprovisions** VMs and cleans up storage when machines are removed
- **Health checks** validate TrueNAS API, pool, and bridge availability

## Zero-Config Auth

When installed as a TrueNAS app, the middleware Unix socket is mounted automatically. **No TrueNAS API key is required.** The provider authenticates the same way TrueNAS's own tools do — as a trusted local process.

## Prerequisites

1. A running [Sidero Omni](https://omni.siderolabs.com/) instance
2. An Omni service account key with `InfraProvider` role
3. A ZFS pool with available space for VM disks
4. A network bridge configured for VM traffic (Network > Interfaces)

## Configuration

Only two fields are required:

- **Omni Endpoint** — Your Omni instance URL (e.g., `https://omni.example.com`)
- **Omni Service Account Key** — From `omnictl serviceaccount create --role=InfraProvider`

Everything else has sensible defaults (pool: `default`, bridge: `br0`, boot: `UEFI`).
