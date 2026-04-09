# Troubleshooting

Common issues and their solutions when running the Omni TrueNAS provider.

## Startup Failures

### "TrueNAS API unreachable"

The provider cannot connect to TrueNAS on startup.

**Unix socket transport:**
- Verify the socket is mounted: `ls -la /var/run/middleware/middlewared.sock`
- If running as a TrueNAS app, ensure the volume mount is present in your compose config:
  ```yaml
  volumes:
    - /var/run/middleware:/var/run/middleware:ro
  ```
- Check that TrueNAS middleware is running: `midclt call core.ping` on the TrueNAS host

**WebSocket transport:**
- Verify `TRUENAS_HOST` is reachable from the provider container: `curl -k https://<host>/websocket`
- Confirm `TRUENAS_API_KEY` is valid — generate a new one in TrueNAS UI under **Credentials > API Keys**
- If using a self-signed cert, ensure `TRUENAS_INSECURE_SKIP_VERIFY=true`

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

- **Insufficient resources** — TrueNAS needs enough free memory for the VM. Check `memory` in MachineClass config vs. available RAM.
- **zvol allocation** — ensure the pool has enough free space for the `disk_size` specified
- **CPU mode** — the provider uses `HOST-PASSTHROUGH` CPU mode. Verify the host CPU supports virtualization (VT-x/AMD-V)

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
| Using TrueNAS SCALE < 25.04 | Upgrade to 25.04+ (Fangtooth) — the JSON-RPC API is required |
| Setting `TRUENAS_HOST` when running on TrueNAS | Remove it — the Unix socket is auto-detected and preferred |
| Missing `network_mode: host` in Docker | Add `network_mode: host` — required for the middleware socket |
| Pool name mismatch | Pool names are case-sensitive — check with `pool.query` |
| No bridge interface created | Create one in TrueNAS UI under Network > Interfaces first |
