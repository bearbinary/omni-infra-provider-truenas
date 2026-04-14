---
title: "FAQ — Frequently Asked Questions"
description: "Common questions about running Kubernetes on TrueNAS SCALE with omni-infra-provider-truenas, Sidero Omni, and Talos Linux."
---

# Frequently Asked Questions

## General

### How do I run Kubernetes on TrueNAS?

Install omni-infra-provider-truenas, connect it to a Sidero Omni instance, and define MachineClasses with CPU, memory, and disk specs. The provider automatically creates Talos Linux VMs on your TrueNAS SCALE system, boots them, and enrolls them in your Omni-managed cluster.

### Does TrueNAS support Kubernetes natively?

TrueNAS SCALE 25.04+ (Fangtooth) removed built-in Kubernetes — Apps now run on Docker. This provider restores Kubernetes capability by provisioning Talos Linux VMs managed through Sidero Omni.

### Can I use TrueNAS as a Proxmox alternative for Kubernetes?

Yes. With this provider and Sidero Omni, TrueNAS SCALE becomes a fully automated Kubernetes infrastructure platform. You get ZFS-backed storage, automatic VM provisioning, and cluster management without Proxmox.

### What is Sidero Omni?

Sidero Omni is a SaaS (or self-hosted) Kubernetes management platform by Sidero Labs. It manages Talos Linux clusters across bare-metal, VMs, and cloud. This provider is an Omni infrastructure provider that creates VMs on TrueNAS when Omni requests machines.

### What is Talos Linux?

Talos Linux is an immutable, minimal operating system purpose-built for Kubernetes. It has no SSH, no shell, and no package manager — managed entirely via API. VMs created by this provider run Talos and join Omni clusters automatically via SideroLink (WireGuard).

## Setup

### What are the minimum hardware requirements?

A single-node Kubernetes cluster needs approximately 4 CPU cores, 16 GB RAM, and 50 GB free disk space (including TrueNAS overhead). Control plane VMs need 2 vCPUs and 2 GB RAM minimum; workers scale based on workload.

### Does Omni cost money?

Omni has a free tier for personal/homelab use. Check [omni.siderolabs.com](https://omni.siderolabs.com/) for current pricing. You can also self-host Omni.

### Can I use this without internet?

No. VMs need outbound HTTPS for Talos ISO download (first time, then cached) and SideroLink (WireGuard on port 443) to Omni. No inbound ports required.

## Operations

### How does transport auto-detection work?

The provider connects to TrueNAS via WebSocket (`wss://<host>/websocket`) using a JSON-RPC 2.0 API key. When running the container on the TrueNAS host itself, set `TRUENAS_HOST=localhost` and `TRUENAS_INSECURE_SKIP_VERIFY=true`. The Unix socket transport was removed in v0.14.0 because TrueNAS 25.10 requires authentication on every call, eliminating the zero-auth advantage.

### What ZFS features does this provider use?

VM disks are created as ZFS zvols on your specified pool. ISOs are cached on the pool filesystem with SHA-256 deduplication. You can target different pools per MachineClass (e.g., NVMe for control plane, HDD for workers).

### How much disk space do the VMs use?

Talos ISO: ~100 MB (cached once). Control plane: ~10 GB each. Worker: 40-100 GB each. All ZFS-compressed — actual usage is often less.

### Will this affect my NAS performance?

Yes — VMs share your NAS hardware (CPU, RAM, disk). Start small and monitor. You can remove VMs anytime if things slow down.

### What if my NAS reboots?

VMs restart with TrueNAS. The provider auto-recovers and reconnects to Omni. Kubernetes restarts your workloads automatically.

### Can I SSH into the Kubernetes nodes?

No. Talos Linux has no SSH by design. Manage nodes through `kubectl`, `talosctl`, and the Omni UI.

### Can I run multiple provider instances for HA?

Not with the same `PROVIDER_ID`. The provider enforces a single-writer lease
on startup: the second instance fails fast rather than racing on VM creation,
zvol creation, and ISO upload. If you want redundancy, the standard pattern
is to rely on Kubernetes / systemd to restart the single pod if it crashes —
the lease TTL (default 45s) ensures a replacement instance can take over
even if the previous one was killed ungracefully.

If you genuinely need multiple active instances (e.g., one per TrueNAS host),
give each one a **distinct** `PROVIDER_ID` and register them separately in
Omni. Omni handles scheduling across provider IDs natively.

See [Architecture › Singleton Enforcement](architecture.md#singleton-enforcement).

### How do I set up networking for VMs?

Create a network bridge (e.g., `br0`) in TrueNAS for VM traffic. VMs attach to bridges, VLANs, or physical NICs. For production, configure DHCP reservations, MetalLB for LoadBalancer services, and optionally a VIP for the Kubernetes API. See the [Networking guide](networking.md) for router-specific instructions (UniFi, pfSense, OPNsense, Mikrotik).
