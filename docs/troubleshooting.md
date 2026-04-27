# Troubleshooting

Common issues and their solutions when running the Omni TrueNAS provider.

## Startup Failures

### "TrueNAS API unreachable"

The provider cannot connect to TrueNAS on startup.

- Verify `TRUENAS_HOST` is reachable from the provider container: `curl -k https://<host>/websocket`
- Confirm `TRUENAS_API_KEY` is valid — generate one for a dedicated non-root user with scoped roles; see [TrueNAS Setup > API Key](truenas-setup.md#5-api-key) for the setup and minimum role list
- If using a self-signed cert, ensure `TRUENAS_INSECURE_SKIP_VERIFY=true`
- When running the container on the TrueNAS host itself, set `TRUENAS_HOST=localhost` and `TRUENAS_INSECURE_SKIP_VERIFY=true`
- Check that TrueNAS middleware is running: `midclt call core.ping` on the TrueNAS host

### "pool not found on TrueNAS"

The configured `DEFAULT_POOL` or MachineClass `pool` doesn't exist.

- **Common mistake**: Using a dataset path (e.g., `tank/my-vms` or `default/previewk8`) instead of the pool name (e.g., `tank` or `default`). The `pool` field must be a **top-level ZFS pool**, not a dataset.
- If you want VMs under an existing dataset, use `dataset_prefix`. For example, if your layout is `default/previewk8`, set `pool: "default"` and `dataset_prefix: "previewk8"`.
- List available pools: `zpool list` or `midclt call pool.query | jq '.[].name'` (on TrueNAS)
- Update `DEFAULT_POOL` or the MachineClass `pool` field to match an existing pool name
- Pool names are case-sensitive

### "network interface target not found"

The configured `DEFAULT_NETWORK_INTERFACE` interface doesn't exist.

- List available choices: `midclt call vm.device.nic_attach_choices` (on TrueNAS)
- Common values: `br0`, `br100`, `vlan100`, `enp5s0`
- Bridge interfaces must be created manually in TrueNAS UI under **Network > Interfaces** before use

### "OMNI_ENDPOINT is required"

The `OMNI_ENDPOINT` environment variable is not set.

- Set it to your Omni instance URL (e.g., `https://omni.example.com`)
- If using `.env`, make sure the file is in the working directory or mounted into the container

### "singleton lease acquire failed" / "another provider instance holds the singleton lease"

Two processes are trying to run as the same `PROVIDER_ID`. The provider refuses
to start when it detects a fresh heartbeat from another instance because
running two provisioners in parallel causes races on VM creation, zvol
creation, and ISO upload.

**Expected fields in the error:**

- `instance %q` — the random UUID of the process that currently holds the lease
- `heartbeat N ago at ...` — how long ago the other instance last refreshed
- `provider %q` — your `PROVIDER_ID`

**Diagnosis:**

1. Find the other instance. Match the `singleton_instance_id` log field across
   your running processes (`kubectl logs`, `journalctl -u`, `docker logs`,
   etc.). If no other process has that instance-id, it is almost certainly a
   stale pod that was `kill -9`'d before it could release.
2. If the other instance is legitimate, stop it cleanly (`SIGTERM`) — the
   outgoing process clears the lease annotation so the successor can acquire
   immediately without waiting for `PROVIDER_SINGLETON_STALE_AFTER` to elapse.
3. If the other instance was killed ungracefully and the heartbeat is frozen,
   the successor will take over automatically once `PROVIDER_SINGLETON_STALE_AFTER`
   (default 45s) passes.

**Kubernetes rolling deploys:** use `strategy.type=Recreate` or
`strategy.rollingUpdate.{maxSurge: 0, maxUnavailable: 1}` so the old pod is
fully terminated before the new one starts. With the default `maxSurge=25%`
strategy the new pod can start while the old pod is still in its
`terminationGracePeriodSeconds`, and the new pod will crashloop on the
preflight check.

**Debugging / advanced sharding:** to bypass the check entirely, set
`PROVIDER_SINGLETON_ENABLED=false`. Only do this when you are certain that no
two instances are servicing the same provider ID — the provider will log a
warning on startup as a reminder.

## Provisioning Issues

### Omni shows "Provisioning" forever with no error

Omni's UI shows the machine stuck in "Provisioning" state but no error message. This happens because the provider retries failed steps every 60 seconds, and each retry clears the error briefly.

**How to see the actual error:**

1. **Check provider logs** — the error is always logged:
   ```bash
   # If running locally
   docker logs omni-infra-provider-truenas 2>&1 | grep "provision failed"
   
   # If running via the binary
   grep "provision failed" /path/to/provider/output
   ```

2. **Check MachineRequestStatus via omnictl** — catches the error between retries:
   ```bash
   omnictl get machinerequeststatus -o yaml | grep -A2 "error:"
   ```

3. **Common causes:**
   - **Pool doesn't exist**: `pool "previewk8" not found` — you specified a dataset name instead of a pool name. Use the top-level pool (e.g., `default`, `tank`), not a dataset path.
   - **network interface invalid**: the bridge or VLAN doesn't exist on TrueNAS.
   - **Pool full**: no space for the zvol.
   - **TrueNAS unreachable**: WebSocket connection dropped.

The provider will keep retrying until the issue is fixed. Once you correct the MachineClass config (e.g., fix the pool name), the next retry will succeed automatically.

### VMs are created but don't join Omni

The VM boots but never appears in Omni.

1. **Check VM console** in TrueNAS UI — is Talos booting? Look for kernel output.
2. **Network connectivity** — the VM needs outbound internet access to reach Omni via SideroLink (WireGuard on port 443). Verify:
   - The network interface target has internet access
   - No firewall blocking outbound WireGuard traffic
   - DNS resolution works from the VM's network
3. **Wrong boot method** — if the VM shows a BIOS/UEFI shell instead of booting, try switching `boot_method` between `UEFI` and `BIOS`
4. **ISO not attached** — check the VM devices in TrueNAS UI. There should be a CDROM device with the Talos ISO.

### "schematic generation failed"

The provider failed to generate a Talos image schematic.

- Verify internet access from the provider container (it needs to reach `factory.talos.dev`)
- Check if a custom extension name is misspelled in the MachineClass `extensions` field
- Set `LOG_LEVEL=debug` for detailed error output

### ISO download hangs or fails

- The provider downloads ISOs from `factory.talos.dev` — ensure outbound HTTPS access
- Large ISOs (~100 MB) may take time on slow connections
- Check available disk space on the TrueNAS pool (ISOs are stored at `<pool>/talos-iso/`)

### VM creation succeeds but VM won't start

- **zvol allocation** — ensure the pool has enough free space for the `disk_size` specified
- **CPU mode** — the provider uses `HOST-PASSTHROUGH` CPU mode. Verify the host CPU supports virtualization (VT-x/AMD-V)
- **Host out of memory** — see the dedicated section below.

### VM creation succeeds but VM won't start: host out of memory

**Symptom.** The Omni UI sits on `Running Step: "uploadISO" (2/4)` for an
extended period (the step counter doesn't advance because step 3 is
silently looping). Provider logs show the real cause repeating every
~60 seconds:

```
reconcile failed	{"error": "failed to start existing VM: vm.start (id=NNN) failed:
  truenas api error (code 12): [ENOMEM] Cannot guarantee memory for guest <name>"}
```

**Cause.** TrueNAS / KVM cannot lock the full `memory` value at
`vm.start` because the host doesn't have enough free RAM right now.
Common scenarios:

- Other VMs are already running and have committed the host RAM (the
  most common case — pre-flight on the new MachineRequest passed when
  it was the only VM, but by the time it reached step 3 another VM
  came up).
- ZFS ARC has grown to fill the host and isn't releasing fast enough
  to satisfy the new guest's allocation.
- The `memory` value is genuinely larger than the host's total RAM (a
  configuration mistake the provider's pre-flight should reject before
  this point — if you see ENOMEM with the pre-flight log line missing,
  file a bug).

This is a **TrueNAS / KVM-level guard, not a provider check**. KVM
genuinely cannot allocate guest pages it can't lock — bypassing the
guard would OOM the host kernel.

**Diagnose.**

```bash
# On the TrueNAS host:
midclt call vm.query | jq '.[] | select(.status.state == "RUNNING") |
  {id, name, memory, min_memory}'

# Sum of running-guest memory and host total:
midclt call system.info | jq '{physmem_mib: (.physmem / 1024 / 1024)}'
```

If `sum(running .memory)` is close to `physmem_mib`, the host is full.

**Fix — pick one:**

1. **Stop another VM** to free its locked memory. Once vm.start has
   the headroom it needs, the provider's next reconcile (within ~60 s)
   will start the stuck VM and provisioning continues.

2. **Set `min_memory` on the MachineClass** to enable memory
   ballooning. The VM starts with `min_memory` reserved (smaller than
   `memory`) and balloons up only when host RAM is available:

   ```yaml
   memory: 4096       # ceiling — what the guest can grow to
   min_memory: 2048   # floor — what's reserved at vm.start
   ```

   ⚠️  The Talos kernel does not auto-load `virtio-balloon`, so until
   balloon is explicitly enabled in-guest, the VM will run at
   `min_memory` and `memory` becomes a ceiling that's never reached.
   In practice: size `min_memory` to what Talos actually needs (1–2 GiB
   for workers, 2–4 GiB for control planes). The pre-flight check
   compares against `min_memory` when set, so this also unblocks the
   provider-side validation on tight hosts.

3. **Manually unblock the existing stuck VM** (no code change). On the
   TrueNAS host:

   ```bash
   # Replace 700 with the VM ID from the error log.
   midclt call vm.update 700 '{"min_memory": 2048}'
   midclt call vm.start 700
   ```

   The provider doesn't reshape existing VMs after creation, so this
   override sticks until the VM is deprovisioned.

4. **Reduce `memory` in the MachineClass** if the requested amount is
   simply too large for the host. Edit the class, then either delete
   the stuck MachineRequest (provider deprovisions the VM) or wait for
   the next reconcile to attempt the smaller `memory`.

5. **Add physical RAM** if this is a recurring pattern. Pre-flight is
   non-blocking when the running-guest aggregate query fails — so a
   host that's chronically near-full will eventually wedge a
   provisioning attempt.

**Provider behavior on persistent ENOMEM.** From v0.16.2 onward, the
provider retries up to `MaxStartOOMAttempts` (default 5) with the
operator-actionable error message, then returns a **permanent**
failure that surfaces on `MachineRequestStatus` instead of looping
forever. Earlier versions retry indefinitely; symptom is the same
"step 2/4" UI freeze.

### VM boots but kubelet loops on `DiskPressure` / etcd never starts

**Symptom.** `talosctl -n <cp-node> logs kubelet` shows the kubelet
entering image garbage-collection mode on every reconcile loop:

```
image garbage collection failed: ImageFS stats not available
eviction manager: attempting to reclaim ephemeral-storage
DiskPressure condition is true
```

etcd never comes up because its image gets evicted mid-pull:

```
failed to pull image "registry.k8s.io/etcd:<version>":
  write: no space left on device
```

**Cause.** The root disk is too small to hold the Talos system image
plus every control-plane container image the kubelet pulls during
bootstrap (apiserver, etcd, scheduler, controller-manager, CNI,
CoreDNS). The 10% GC headroom trips, the kubelet evicts images it
still needs, the next reconcile re-pulls them, the cycle repeats.

**Fix.** Set `disk_size` to at least 20 (the provider's validation
floor as of v0.15.x); 40 is recommended for production CPs so a
Kubernetes minor upgrade (old + new image coexist during rollover)
doesn't re-trigger the condition. See
[sizing guide § Why the root disk has a 20 GiB minimum](sizing.md#why-the-root-disk-has-a-20-gib-minimum)
for the full image-size breakdown.

VMs stamped out against an older MachineClass with `disk_size` below
20 need to be reprovisioned against an updated class — resizing the
zvol in place doesn't expand Talos's EPHEMERAL partition.

### VM halts on reboot with "Talos is already installed to disk but booted from another media"

Log output (repeats every 30s until the VM is shut down):

```
[talos] task haltIfInstalled (1/1): Talos is already installed to disk but booted
from another media and talos.halt_if_installed kernel parameter is set. Please
reboot from the disk.
```

**Cause.** On provider versions **≤ v0.14.1**, the VM's CDROM was attached with UEFI
boot `order=1000` and the root disk with `order=1001`. bhyve's UEFI boot manager
tries the entry with the lowest `order` first, so on every reboot UEFI re-entered
the Talos ISO instead of the disk. The initial install worked because the disk
was empty on first boot, but once Talos was installed any reboot (manual stop,
TrueNAS restart, host reboot) re-entered the ISO — and the ISO's
`talos.halt_if_installed=1` kernel parameter halts the boot as a safeguard
against overwriting an existing installation.

**Fix for existing VMs** — bump the CDROM `order` above the root disk's
`order` (1000). Recommended value: `1500`.

TrueNAS UI:

1. **Virtualization > Virtual Machines > _your VM_ > Devices**
2. Edit the CDROM device
3. Change **Device Order** from `1000` to `1500`
4. Save, then start the VM

TrueNAS shell (faster if you have many VMs):

```bash
# Find the CDROM device ID for a VM (replace <VM_ID>)
midclt call vm.device.query '[["vm","=",<VM_ID>]]' | jq '.[] | select(.attributes.dtype=="CDROM") | {id, order}'

# Update the order
midclt call vm.device.update <CDROM_DEVICE_ID> '{"order": 1500}'

# Start the VM
midclt call vm.start <VM_ID>
```

**Fix for new VMs** — upgrade the provider to a version that includes the
correct boot order (root disk = 1000, additional disks = 1001+, CDROM = 1500,
NIC = 2001). New VMs provisioned after the upgrade boot correctly without
manual intervention.

Do **not** detach the CDROM. Talos may still be mid-install when you notice
the issue, and detaching a device requires stopping the VM — which interrupts
the install. Reordering is always safe.

## Deprovision Issues

### Orphan VMs or zvols after deletion

The background cleanup process handles this automatically. If you see stale resources:

1. Check provider logs for cleanup errors
2. Manually remove via TrueNAS UI: **Virtualization > VMs** (delete VM) and **Storage > Datasets** (delete zvol)
3. ISOs are cleaned up automatically when no longer referenced by active VMs

## Debugging

### Enable debug logging

```bash
LOG_LEVEL=debug
```

This logs all JSON-RPC calls, provision step progress, and transport-level details.

### Check provider health

The provider reports health to Omni. If it shows as unhealthy in the Omni UI:

1. Check provider logs for health check errors
2. Verify TrueNAS is reachable (the health check pings the API, checks the pool, and validates the NIC)
3. Restart the provider container

### View raw JSON-RPC calls

With `LOG_LEVEL=debug`, every JSON-RPC request and response is logged. This is useful for diagnosing TrueNAS API issues.

## Common Mistakes

| Mistake | Fix |
|---|---|
| Using TrueNAS SCALE < 25.10 | Upgrade to 25.10+ (Goldeye) — v0.13.2+ requires the JSON-RPC 2.0 WebSocket API |
| Omitting `TRUENAS_HOST` / `TRUENAS_API_KEY` when running on TrueNAS | Set `TRUENAS_HOST=localhost`, create an API key, and set `TRUENAS_INSECURE_SKIP_VERIFY=true` |
| Missing `network_mode: host` in Docker | Add `network_mode: host` — required for the provider to reach `localhost:443` |
| Pool name mismatch | Pool names are case-sensitive — check with `pool.query` |
| No bridge interface created | Create one in TrueNAS UI under Network > Interfaces first |
