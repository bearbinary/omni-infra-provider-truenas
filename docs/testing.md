# Testing Guide

> **This provider requires TrueNAS SCALE 25.04+ (JSON-RPC 2.0 API). The legacy REST v2.0 API is NOT supported.**

This document covers how to test the Omni TrueNAS infrastructure provider at every level — from unit tests on your laptop to full end-to-end provisioning with Omni.

---

## Unit Tests

Run anytime, no external dependencies:

```bash
make test        # quick
make test-v      # verbose
```

Unit tests cover the TrueNAS API client, provisioner, cleanup, telemetry, and
the singleton lease package: VM CRUD, device attachment, storage operations,
error handling, SHA-256 dedup logic, rate limiting, reconnect, chaos testing,
singleton lease acquire/refresh/steal/release, and full E2E flows. The
singleton tests run against an in-memory cosi-runtime state with an
error-injecting wrapper (`flakyState`) for exercising the refresh loop's
transient-error recovery and abandonment thresholds.

---

## Phase 1: Integration Tests (API-Only, No Nested Virt)

Exercises the full client CRUD lifecycle against a real TrueNAS instance. VMs are created with `autostart: false` — they never boot, so **no nested virtualization is needed**.

### What's tested

| Test | Operations |
|---|---|
| `TestIntegration_Ping` | API reachability + auth validation |
| `TestIntegration_PoolExists` | Pool existence check (positive + negative) |
| `TestIntegration_NICAttachValid` | network interface target check (positive + negative) |
| `TestIntegration_DatasetLifecycle` | Create dataset, EnsureDataset idempotency, delete, delete again |
| `TestIntegration_ZvolLifecycle` | Create 1 GiB zvol, delete |
| `TestIntegration_FileExistence` | Check /mnt exists, check nonexistent path |
| `TestIntegration_FileUpload` | Upload a text file, verify existence |
| `TestIntegration_VMLifecycle` | Create VM, Get, FindByName, List, Delete, delete again |
| `TestIntegration_DeviceAttachment` | Create VM + zvol, attach NIC + DISK devices |
| `TestIntegration_VMNamingConvention` | Create VMs with/without `omni-` prefix, verify filtering |

### Prerequisites

1. **TrueNAS SCALE 25.04+** installed (VM or bare metal)
2. A **ZFS pool** (default: `tank`)
3. A **network interface** for VM NICs — bridge (e.g., `br0`), VLAN (e.g., `vlan666`), or physical interface (e.g., `enp5s0`)
4. A **TrueNAS API key** — create under System > API Keys

### Running

```bash
export TRUENAS_TEST_HOST="192.168.1.100"
export TRUENAS_TEST_API_KEY="1-your-api-key-here"

# Optional overrides (defaults shown):
export TRUENAS_TEST_POOL="tank"
export TRUENAS_TEST_NIC_ATTACH="br0"

make test-integration
```

Or directly:

```bash
go test -tags=integration ./internal/client/... -v -count=1 -timeout=120s
```

### Cleanup

All tests clean up after themselves via `t.Cleanup()`. If a test is interrupted, leftover resources have names prefixed with `omni-inttest-` and can be safely deleted:

- VMs: Virtualization > any VM named `omni-inttest-*`
- Datasets: Storage > Datasets > any under `tank/omni-integration-test-*`

### Quick TrueNAS VM Setup for Phase 1

If you don't have a TrueNAS instance available:

1. Download TrueNAS SCALE 25.04+ from `download.truenas.com/`
2. Create a VM on any hypervisor (Proxmox, VMware, VirtualBox, libvirt):
   - **CPU**: 2 cores (no nested virt flags needed)
   - **RAM**: 8 GB
   - **Boot disk**: 32 GB
   - **Data disks**: 2x 20 GB (for ZFS mirror)
   - **Network**: Bridged
3. Install TrueNAS from the ISO
4. Create a ZFS pool named `tank` from the two data disks
5. Create a bridge (e.g., `br0`) under Network > Interfaces (Type: Bridge, member: your NIC)
6. Create an API key under System > API Keys
7. Run the integration tests

**No nested virtualization required.** All VM operations are on stopped VMs.

---

## Phase 2: End-to-End Testing with Nested Virtualization (Future)

This phase validates the complete flow: Omni creates a MachineRequest, the provider provisions a Talos VM on TrueNAS, the VM boots, joins the Omni cluster via SideroLink.

### Requirements

Phase 2 requires nested virtualization — the TrueNAS VM must be able to run KVM guests inside it.

#### Hardware

| Resource | Minimum | Recommended |
|---|---|---|
| CPU | 4 cores, x86_64, VT-x/AMD-V | 8+ cores |
| RAM | 16 GB | 32 GB |
| Boot disk | 32 GB SSD | 64 GB |
| Data disks | 2x 20 GB | 2x 100 GB |
| Network | 1 bridged NIC | 1 bridged NIC + VLAN trunk |

> **Apple Silicon note:** TrueNAS SCALE is x86_64-only. On Apple Silicon Macs, UTM would need to emulate x86 (not virtualize), which is unusably slow. Use a remote x86 Linux machine or cloud instance instead.

#### Hypervisor Configuration for Nested Virt

| Platform | How to enable |
|---|---|
| **Proxmox** | CPU type: `host`, enable "Nested" checkbox. Or set `args: -cpu host` in VM config. |
| **VMware Workstation/Fusion** | Add `vhv.enable = "TRUE"` to .vmx, or check "Virtualize Intel VT-x/EPT or AMD-V/RVI" in Processor settings. Enable promiscuous mode on the virtual switch. |
| **libvirt/KVM** | Set `<cpu mode='host-passthrough'/>` in the domain XML. |
| **VirtualBox** | Nested virt is experimental. Enable via `VBoxManage modifyvm <name> --nested-hw-virt on`. Not recommended. |

#### TrueNAS Setup

Same as Phase 1, plus:

- Verify nested virt works: SSH into TrueNAS, run `grep -c vmx /proc/cpuinfo` (Intel) or `grep -c svm /proc/cpuinfo` (AMD). Should return > 0.
- Ensure the bridge has outbound internet access (Talos needs to reach the Omni endpoint and Image Factory).
- If using VLANs: configure the VLAN + bridge per the networking guide in `docs/provider-research.md` Section 13.

#### Omni Setup

1. Have a running Omni instance (cloud or self-hosted)
2. Create an infra provider:
   ```bash
   omnictl infraprovider create truenas
   ```
3. Create a service account key:
   ```bash
   omnictl serviceaccount create --role=InfraProvider infra-provider:truenas
   ```
4. Note the `OMNI_ENDPOINT` and `OMNI_SERVICE_ACCOUNT_KEY` values

#### Running the Provider

```bash
docker run -d --network=host \
  -e OMNI_ENDPOINT="https://omni.example.com" \
  -e OMNI_SERVICE_ACCOUNT_KEY="<key>" \
  -e TRUENAS_HOST="192.168.1.100" \
  -e TRUENAS_API_KEY="<key>" \
  -e DEFAULT_POOL="tank" \
  -e DEFAULT_NETWORK_INTERFACE="br0" \
  ghcr.io/bearbinary/omni-infra-provider-truenas:latest
```

Or via Docker Compose on TrueNAS: paste `deploy/docker-compose.yaml` into Apps > Install via YAML.

#### E2E Test Scenarios

| Test | Steps | Pass Criteria |
|---|---|---|
| Single VM provision | Create MachineClass `truenas-small` (2 CPU, 4GB, 40GB). Create MachineSet with 1 replica. | VM created on TrueNAS, boots Talos ISO, machine appears in Omni within 5 minutes. |
| Scale up | Set MachineSet replicas to 3. | 3 VMs running, all visible in Omni. |
| Scale down | Set replicas to 1. | 2 VMs deprovisioned, zvols deleted, 1 remains. |
| Full teardown | Delete MachineSet. | All VMs and zvols cleaned up. |
| Crash recovery | Kill provider container mid-provision, restart. | Provider resumes from last completed step. |
| Concurrent provisioning | Request 5 machines simultaneously. | All 5 provision without race conditions. |
| Invalid network interface | Set `network_interface: "nonexistent"` in MachineClass. | Step fails with clear error in MachineRequestStatus. |

#### Example MachineClass

```yaml
apiVersion: infrastructure.omni.siderolabs.io/v1alpha1
kind: MachineClass
metadata:
  name: truenas-small
spec:
  type: auto-provision
  provider: truenas
  config:
    cpus: 2
    memory: 4096
    disk_size: 40
    pool: "tank"
    network_interface: "br0"
    boot_method: "UEFI"
```

### Automation with Vagrant/Packer

For reproducible TrueNAS test instances, use [rgl/truenas-scale-vagrant](https://github.com/rgl/truenas-scale-vagrant):

```bash
git clone https://github.com/rgl/truenas-scale-vagrant
cd truenas-scale-vagrant

# Build a TrueNAS SCALE Vagrant box (requires Packer + QEMU)
make build

# Start a disposable TrueNAS instance
vagrant up
```

This automates the TrueNAS installer via simulated keystrokes, producing a ready-to-use Vagrant box with SSH access. You can then configure the pool, bridge, and API key via the TrueNAS API or SSH.

---

## API Version Notes

| TrueNAS Version | API | VM Backend | Provider Support |
|---|---|---|---|
| 24.10.x (Electric Eel) | REST v2.0 only | KVM/libvirt | **Not supported** |
| **25.04.x+ (Fangtooth)** | JSON-RPC 2.0 (+ deprecated REST) | Incus + classic KVM | **Supported** |

This provider uses JSON-RPC 2.0 exclusively. The REST v2.0 API is not supported.
