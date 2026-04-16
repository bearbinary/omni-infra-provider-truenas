# AGENT.md

This file helps AI assistants guide users through setting up and operating the Omni TrueNAS infrastructure provider. If a user asks for help deploying, configuring, or troubleshooting this provider, use the information below.

## What This Is

A service that connects Sidero Omni (Kubernetes management platform) to TrueNAS SCALE (storage/virtualization). When a user creates or scales a Kubernetes cluster in Omni, this provider automatically creates Talos Linux VMs on TrueNAS SCALE. When machines are removed, it cleans them up.

> **If the user is new to Kubernetes**, direct them to `docs/getting-started.md` first. It's a complete 6-step tutorial (NAS to running cluster) with Windows/macOS/Linux instructions, a glossary, inline troubleshooting, and an FAQ. The guide below is for users who already understand the concepts and just need setup reference.

## Before You Start — Prerequisites Checklist

Walk the user through each of these before attempting deployment:

### 1. TrueNAS SCALE 25.04+ (Fangtooth)

- **This is a hard requirement.** The provider uses the JSON-RPC 2.0 API, which is only available in 25.04+.
- The legacy REST v2.0 API (`/api/v2.0/`) is NOT supported.
- To check: TrueNAS UI > Dashboard shows the version, or run `midclt call system.version` via SSH.

### 2. Sidero Omni Instance

- The user needs an active Omni instance (self-hosted or SaaS at omni.siderolabs.com).
- They need `omnictl` installed locally: https://omni.siderolabs.com/docs/how-to-guides/install-and-configure/install-omnictl/

### 3. Omni Service Account

Create an infra provider service account:

```bash
omnictl serviceaccount create --role=InfraProvider infra-provider:truenas
```

**Important:** The output contains the `OMNI_SERVICE_ACCOUNT_KEY`. It is shown only once — save it immediately. If lost, delete and recreate the service account.

### 4. ZFS Pool

- At least one ZFS pool must exist on TrueNAS with enough free space for VM disks.
- To check available pools: TrueNAS UI > Storage, or `midclt call pool.query` via SSH.
- The pool name is case-sensitive. Common names: `default`, `tank`, `data`.
- ISOs are cached automatically at `<pool>/talos-iso/` — no manual setup needed.

### 5. Network Interface for VMs

VMs need a network interface to attach to. This can be:
- **Bridge** (recommended): e.g., `br0`, `br100` — create in TrueNAS UI > Network > Interfaces > Add > Bridge
- **VLAN**: e.g., `vlan100` — create in TrueNAS UI > Network > Interfaces > Add > VLAN
- **Physical NIC**: e.g., `enp5s0` — use an existing interface directly

The interface must have:
- Connectivity to the internet (VMs need outbound access to reach Omni via SideroLink/WireGuard on port 443)
- DHCP available on the network (Talos uses DHCP by default)

To list available choices: `midclt call vm.device.nic_attach_choices` via SSH.

### 6. TrueNAS API Key

Required in all deployments (TrueNAS 25.10+ removed implicit local auth).

**Do not use the `root` user's API key.** Create a dedicated non-root user with scoped roles:

1. **Credentials > Local Users > Add**: create `omni-provider` user, disable password.
2. **Credentials > Privileges > Add**: new privilege bound to the user's group, with roles:
   `READONLY_ADMIN`, `VM_READ`, `VM_WRITE`, `VM_DEVICE_READ`, `VM_DEVICE_WRITE`,
   `DATASET_READ`, `DATASET_WRITE`, `DATASET_DELETE`, `POOL_READ`, `DISK_READ`,
   `NETWORK_INTERFACE_READ`, `FILESYSTEM_ATTRS_READ`, `FILESYSTEM_DATA_WRITE`.
3. **Credentials > API Keys > Add**: name `omni-infra-provider`, username `omni-provider`. Copy key once.

See [TrueNAS Setup > API Key](docs/truenas-setup.md#5-api-key) for the rationale per role.

Alternate: `FULL_ADMIN` on a non-root user works too — broader than necessary but simpler to configure.

## Deployment — Three Options

Ask the user which deployment method they prefer, then guide them through the appropriate section.

### Option A: Docker Compose on TrueNAS (Recommended)

Best for: Running directly on the TrueNAS host via the built-in Apps UI.

**Step 1:** In TrueNAS UI, go to Apps > Discover Apps > Install via YAML (Custom App / Docker Compose).

**Step 2:** Use this compose config:

```yaml
services:
  omni-infra-provider-truenas:
    image: ghcr.io/bearbinary/omni-infra-provider-truenas:latest
    restart: unless-stopped
    network_mode: host
    environment:
      OMNI_ENDPOINT: "https://<omni-url>"
      OMNI_SERVICE_ACCOUNT_KEY: "<key-from-step-3-above>"
      TRUENAS_HOST: "localhost"
      TRUENAS_API_KEY: "<truenas-api-key>"
      TRUENAS_INSECURE_SKIP_VERIFY: "true"
      DEFAULT_POOL: "<pool-name>"
      DEFAULT_NETWORK_INTERFACE: "<interface-name>"
```

**Step 3:** Replace the placeholder values:
- `OMNI_ENDPOINT`: Their Omni URL (e.g., `https://omni.example.com`)
- `OMNI_SERVICE_ACCOUNT_KEY`: The key from the service account creation step
- `TRUENAS_API_KEY`: Create via the dedicated non-root user + scoped privilege described in section 6 above. Do not use the `root` user's key.
- `DEFAULT_POOL`: Their ZFS pool name (e.g., `default`, `tank`)
- `DEFAULT_NETWORK_INTERFACE`: Their network interface (e.g., `br0`, `vlan100`)

**Step 4:** Deploy the app. Check logs for:
```
"starting TrueNAS infra provider"
"startup checks passed"
```

If they see both lines, the provider is running and connected.

### Option B: Kubernetes (Helm)

Best for: Running on an external Kubernetes cluster.

**Step 1:** Clone or download the repo.

**Step 2:** Install with Helm:
```bash
helm install omni-infra-provider deploy/helm/omni-infra-provider-truenas \
  --namespace omni-infra-provider --create-namespace \
  --set omniEndpoint="https://<omni-url>" \
  --set truenasHost="<truenas-hostname-or-ip>" \
  --set secrets.omniServiceAccountKey="<key>" \
  --set secrets.truenasApiKey="<truenas-api-key>" \
  --set defaults.pool="<pool-name>" \
  --set defaults.networkInterface="<interface-name>"
```

**Step 3:** Check logs:
```bash
kubectl logs -n omni-infra-provider -l app.kubernetes.io/name=omni-infra-provider-truenas
```

### Option C: Docker Compose (Remote Host)

Best for: Running on a separate Linux host (not TrueNAS, not Kubernetes).

**Step 1:** Clone or download the repo.

**Step 2:** Copy and edit the env file:
```bash
cp .env.example .env
```

**Step 3:** Fill in `.env`:
```bash
OMNI_ENDPOINT=https://<omni-url>
OMNI_SERVICE_ACCOUNT_KEY=<key>
TRUENAS_HOST=<truenas-hostname-or-ip>
TRUENAS_API_KEY=<truenas-api-key>
DEFAULT_POOL=<pool-name>
DEFAULT_NETWORK_INTERFACE=<interface-name>
```

**Step 4:** Start:
```bash
docker compose -f deploy/docker-compose.yaml up -d
```

**Step 5:** Check logs:
```bash
docker compose -f deploy/docker-compose.yaml logs -f
```

## Verifying the Deployment

After deployment, guide the user through these checks:

### 1. Provider Logs

Look for these two lines in the logs:
```
"startup checks passed" transport=<socket|websocket> pool=<name> network_interface=<name>
"starting TrueNAS infra provider" provider_id=truenas omni_endpoint=https://...
```

If they don't appear, check the troubleshooting section below.

### 2. Omni UI

In the Omni web UI, navigate to the Infrastructure Providers section. The `truenas` provider should appear as healthy.

### 3. Create a Test MachineClass

```bash
cat <<'EOF' | omnictl apply -f -
metadata:
  namespace: default
  type: MachineClasses.omni.sidero.dev
  id: truenas-test
spec:
  autoprovision:
    providerid: truenas
    grpcendpoint: ""
    icon: ""
    configpatch: |
      cpus: 2
      memory: 2048
      disk_size: 10
EOF
```

### 4. Test Provisioning

Create a single-node cluster using the MachineClass in Omni (UI or CLI). Watch:
- Provider logs should show the 4 provision steps running
- TrueNAS UI > Virtualization should show a new VM appearing
- Omni UI should show the machine enrolling after 2-5 minutes

## Creating MachineClasses for Production

### Control Plane Nodes

Minimal resources — runs etcd + Kubernetes API server, no container workloads:

```yaml
cpus: 2
memory: 2048
disk_size: 10
```

### Worker Nodes

More resources — runs application workloads and stores container images. Includes a storage disk for Longhorn persistent storage:

```yaml
cpus: 4
memory: 8192
disk_size: 100
storage_disk_size: 100
```

### Per-Class Overrides

Any MachineClass can override provider defaults:

```yaml
cpus: 8
memory: 16384
disk_size: 200
pool: "fast-nvme"           # Different ZFS pool
network_interface: "vlan100"       # Different network
boot_method: "BIOS"         # Instead of UEFI
architecture: "arm64"       # Instead of amd64
extensions:
  - "siderolabs/iscsi-tools"  # Extra Talos extensions
```

### Default Extensions (Always Included)

These are included in every VM automatically — users do NOT need to add them:
- `siderolabs/qemu-guest-agent`
- `siderolabs/util-linux-tools`
- `siderolabs/iscsi-tools` (required by Longhorn)

If you need NFS client support (for democratic-csi NFS mode), add `siderolabs/nfs-utils` to the MachineClass `extensions` field.

## Enabling Metrics Server (HPA / `kubectl top`)

Talos kubelets serve their metrics endpoint with a self-signed cert, so `metrics-server` fails until kubelet certificate rotation is on and a serving-cert approver is deployed. Best handled at **cluster creation** — every node boots with the right flag and nothing needs to roll later.

In the Omni cluster create form, add this under **Cluster Config Patches**:

```yaml
machine:
  kubelet:
    extraArgs:
      rotate-server-certificates: true
```

Add these to the cluster's **Extra Manifests**:

- `https://raw.githubusercontent.com/alex1989hu/kubelet-serving-cert-approver/main/deploy/standalone-install.yaml`
- `https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml`

For an existing cluster: apply the same config patch in Omni (it rolls kubelets), then `kubectl apply -f` both URLs.

Verify with `kubectl top nodes` and `kubectl top pods -A` — CPU/memory should return within ~60s. If it errors, the kubelet flag hasn't landed on every node yet.

Upstream reference: https://docs.siderolabs.com/kubernetes-guides/monitoring-and-observability/deploy-metrics-server/

## All Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `OMNI_ENDPOINT` | Yes | — | Omni instance URL |
| `OMNI_SERVICE_ACCOUNT_KEY` | Yes | — | Omni infra provider service account key |
| `TRUENAS_HOST` | Remote only | — | TrueNAS hostname or IP (WebSocket transport) |
| `TRUENAS_API_KEY` | Remote only | — | TrueNAS API key (WebSocket transport) |
| `TRUENAS_INSECURE_SKIP_VERIFY` | No | `false` | Skip TLS verification for self-signed certs |
| `TRUENAS_HOST` | **Yes** | — | TrueNAS hostname or IP (use `localhost` when running the container on the TrueNAS host itself) |
| `TRUENAS_API_KEY` | **Yes** | — | TrueNAS API key from a dedicated non-root user with scoped roles — see [TrueNAS Setup > API Key](docs/truenas-setup.md#5-api-key) |
| `TRUENAS_INSECURE_SKIP_VERIFY` | No | `false` | Skip TLS verification (recommended `true` for `localhost`) |
| `PROVIDER_ID` | No | `truenas` | Provider ID registered with Omni |
| `PROVIDER_NAME` | No | `TrueNAS` | Display name in Omni UI |
| `PROVIDER_DESCRIPTION` | No | `TrueNAS SCALE infrastructure provider` | Description in Omni UI |
| `DEFAULT_POOL` | No | `default` | ZFS pool for VM zvols and ISO cache |
| `DEFAULT_NETWORK_INTERFACE` | No | — | Network interface for VM NICs |
| `DEFAULT_BOOT_METHOD` | No | `UEFI` | VM boot method: `UEFI` or `BIOS` |
| `CONCURRENCY` | No | `4` | Max parallel provision/deprovision workers |
| `LOG_LEVEL` | No | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `OMNI_INSECURE_SKIP_VERIFY` | No | `false` | Skip TLS verification for Omni connection |
| `TRUENAS_MAX_CONCURRENT_CALLS` | No | `8` | Max concurrent JSON-RPC calls to TrueNAS |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | — | OpenTelemetry collector endpoint |
| `OTEL_EXPORTER_OTLP_INSECURE` | No | `true` | Use insecure gRPC for OTel |
| `OTEL_SERVICE_NAME` | No | `omni-infra-provider-truenas` | OTel service name |
| `PYROSCOPE_URL` | No | — | Pyroscope endpoint for continuous profiling |
| `PROVIDER_SINGLETON_ENABLED` | No | `true` | Fail fast if another instance holds the singleton lease for this `PROVIDER_ID` |
| `PROVIDER_SINGLETON_REFRESH_INTERVAL` | No | `15s` | Lease heartbeat cadence |
| `PROVIDER_SINGLETON_STALE_AFTER` | No | `45s` | Stale-lease takeover threshold (must be ≥ 2× refresh) |

## Troubleshooting

### Startup Errors

| Error | Cause | Fix |
|---|---|---|
| `TrueNAS API unreachable` | Can't connect to TrueNAS | **Socket:** Check volume mount. **WebSocket:** Check `TRUENAS_HOST` is reachable and `TRUENAS_API_KEY` is valid. |
| `pool "X" not found` | Pool name wrong or doesn't exist | Check pool name with `midclt call pool.query`. Names are case-sensitive. |
| `network interface target "X" not found` | Interface doesn't exist | Check with `midclt call vm.device.nic_attach_choices`. May need to create a bridge first. |
| `OMNI_ENDPOINT is required` | Missing env var | Set the `OMNI_ENDPOINT` environment variable. |
| `singleton lease acquire failed` / `another provider instance holds the singleton lease` | Two processes running with the same `PROVIDER_ID` | Stop the other instance (clean `SIGTERM` releases the lease immediately), or wait ~45s for the stale-heartbeat takeover. For k8s rolling deploys, use `strategy.type=Recreate`. See `docs/troubleshooting.md`. |

### VMs Created But Don't Join Omni

1. Check VM console in TrueNAS UI — is Talos booting?
2. Verify the VM's network has outbound internet (SideroLink needs port 443 outbound)
3. Verify DHCP is available on the VM's network
4. Try switching `boot_method` between `UEFI` and `BIOS`
5. Set `LOG_LEVEL=debug` and check provider logs for step-by-step output

### Provider Shows Unhealthy in Omni

The health check pings TrueNAS, verifies the pool exists, and validates the NIC. Check:
1. TrueNAS is reachable from the provider
2. The configured pool still exists
3. The configured NIC interface still exists
4. Restart the provider container

### Detailed Debugging

Set `LOG_LEVEL=debug` to see all JSON-RPC calls, provision step progress, and transport details.

## Upgrading

The provider is stateless — all VM state lives on TrueNAS and Omni. To upgrade:

1. Check [`docs/upgrading.md`](docs/upgrading.md) for version-specific notes
2. Update the image tag or binary
3. Restart the provider

No breaking changes have been introduced from v0.1.0 through v0.7.0. Rolling back is as simple as reverting the image tag.

## For More Information

- Full README: `README.md`
- Beginner tutorial: `docs/getting-started.md` (NAS to Kubernetes, no prior experience needed — Windows/macOS/Linux, glossary, inline troubleshooting, FAQ)
- Architecture with diagrams: `docs/architecture.md`
- Detailed troubleshooting: `docs/troubleshooting.md`
- Upgrade guide: `docs/upgrading.md`
- Test setup: `docs/testing.md`
- Environment variable reference: `.env.example`
