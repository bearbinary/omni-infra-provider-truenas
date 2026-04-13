# Architecture

Detailed architecture of the Omni TrueNAS infrastructure provider.

## System Context

```mermaid
flowchart TB
    User([User / Platform Admin])
    Omni[Sidero Omni]
    Provider[omni-infra-provider-truenas]
    TrueNAS[TrueNAS SCALE]
    Factory[Talos Image Factory]
    VM[Talos Linux VM]

    User -->|scales cluster / creates MachineSet| Omni
    Omni -->|MachineRequest via gRPC| Provider
    Provider -->|JSON-RPC 2.0| TrueNAS
    Provider -->|HTTPS| Factory
    TrueNAS -->|creates & starts| VM
    VM -->|SideroLink · WireGuard| Omni
```

## Component Overview

```mermaid
flowchart LR
    subgraph Entry["cmd/"]
        Main[main.go<br/>env config · transport detection · health checks]
    end

    subgraph Core["internal/"]
        Client[client/<br/>JSON-RPC 2.0 transport<br/>VM · Device · Storage ops]
        Provisioner[provisioner/<br/>4 provision steps<br/>+ deprovision]
        Cleanup[cleanup/<br/>stale ISO · orphan VM<br/>background cleanup]
        Resources[resources/<br/>COSI typed resources]
        Telemetry[telemetry/<br/>OTel · metrics]
    end

    subgraph External
        Omni[Sidero Omni]
        TrueNAS[TrueNAS SCALE]
    end

    Main --> Client
    Main --> Provisioner
    Main --> Cleanup
    Provisioner --> Client
    Cleanup --> Client
    Main -.->|registers provider| Omni
    Client -->|JSON-RPC 2.0| TrueNAS
```

## Provision Lifecycle

The full sequence from MachineRequest to a running, enrolled VM:

```mermaid
sequenceDiagram
    participant Omni as Sidero Omni
    participant P as Provider
    participant TN as TrueNAS SCALE
    participant IF as Image Factory

    Omni->>P: MachineRequest (cpus, memory, disk_size, ...)

    Note over P: Step 1: createSchematic
    P->>IF: POST schematic (extensions list)
    IF-->>P: schematic ID

    Note over P: Step 2: uploadISO
    P->>P: Check ISO cache (SHA-256)
    alt ISO not cached
        P->>IF: Download Talos nocloud ISO
        IF-->>P: ISO bytes
        P->>TN: Upload ISO to pool/talos-iso/
    end

    Note over P: Step 3: createVM
    P->>TN: Create zvol (disk_size GiB)
    P->>TN: Create VM (cpus, memory, boot_method)
    P->>TN: Attach CDROM (ISO)
    P->>TN: Attach DISK (zvol)
    P->>TN: Attach NIC (bridge/VLAN)
    P->>TN: Start VM
    P->>TN: Poll status until RUNNING

    TN-->>P: VM status: RUNNING
    P-->>Omni: MachineRequest fulfilled

    Note over TN: VM boots Talos Linux
    TN->>Omni: SideroLink (outbound WireGuard)
```

## Deprovision Lifecycle

```mermaid
sequenceDiagram
    participant Omni as Sidero Omni
    participant P as Provider
    participant TN as TrueNAS SCALE

    Omni->>P: Machine removal

    P->>TN: Stop VM
    P->>TN: Delete VM (removes all device attachments)
    P->>TN: Delete zvol

    P-->>Omni: Machine deprovisioned
```

## Transport

```mermaid
flowchart TD
    Start([Provider starts]) --> Check{TRUENAS_HOST +<br/>TRUENAS_API_KEY set?}
    Check -->|Yes| WS[WebSocket JSON-RPC 2.0<br/>API key auth · TLS]
    Check -->|No| Fail([Startup failure:<br/>TRUENAS_HOST / TRUENAS_API_KEY required])
```

TrueNAS 25.10 (Goldeye) requires authentication on every JSON-RPC call — including local Unix socket connections. The Unix socket transport was removed in v0.14.0 because there is no longer a zero-auth path. When running as a TrueNAS app, set `TRUENAS_HOST=localhost` and `TRUENAS_INSECURE_SKIP_VERIFY=true`.

## Startup Health Checks

Before accepting work from Omni, the provider validates its environment:

```mermaid
flowchart TD
    Start([Provider starts]) --> Ping[Ping TrueNAS API]
    Ping -->|fail| Die([Exit with error])
    Ping -->|ok| Pool[Verify DEFAULT_POOL exists]
    Pool -->|not found| Die
    Pool -->|ok| NIC{DEFAULT_NETWORK_INTERFACE set?}
    NIC -->|yes| ValidateNIC[Validate NIC target exists]
    ValidateNIC -->|not found| Die
    ValidateNIC -->|ok| Ready([Ready — accept MachineRequests])
    NIC -->|no| Warn[Log warning: MachineClass must specify network_interface]
    Warn --> Ready
```

## Singleton Enforcement

The provider is stateless and the Omni SDK does not elect a leader across
instances with the same `PROVIDER_ID` — every process that registers sees
every `MachineRequest` and would race on VM/zvol/ISO operations. To prevent
this, the provider claims a lease on the `infra.ProviderStatus` resource via
two metadata annotations:

- `bearbinary.com/singleton-instance-id` — UUID generated per process start
- `bearbinary.com/singleton-heartbeat` — RFC3339 timestamp refreshed on a
  configurable interval (default 15s)

These annotations survive the SDK's own `ProviderStatus` update because the
SDK only rewrites the `.Value` field of the spec, leaving metadata alone.

```mermaid
sequenceDiagram
    participant A as Instance A<br/>(existing)
    participant S as Omni State<br/>(ProviderStatus)
    participant B as Instance B<br/>(new)

    A->>S: Acquire: Create + annotations
    loop every RefreshInterval
        A->>S: CAS-update heartbeat
    end

    B->>S: Acquire: Get ProviderStatus
    S-->>B: Fresh heartbeat owned by A
    B--xB: Exit non-zero<br/>(LeaseHeldError)

    Note over A: SIGTERM received
    A->>S: Release: clear annotations
    A-->>A: Process exits

    B->>S: Retry on restart: Acquire
    S-->>B: Unclaimed
    B->>S: CAS-update with B's id
    Note over B: Takes over as leader
```

If the current leader is killed ungracefully (`kill -9`), its heartbeat goes
stale after `PROVIDER_SINGLETON_STALE_AFTER` (default 45s). The next instance
that tries to acquire sees the stale heartbeat and takes over.

The feature is on by default and can be disabled via
`PROVIDER_SINGLETON_ENABLED=false` for debugging or advanced sharding setups.

## Background Cleanup

The cleanup goroutine runs periodically to remove stale resources:

- **Stale ISOs** — ISOs in `<pool>/talos-iso/` that are no longer referenced by any active VM
- **Orphan VMs** — VMs with the provider's naming prefix that have no corresponding MachineRequest in Omni
- **Orphan zvols** — zvols associated with deleted VMs

## Data Flow

| Data | Source | Destination | Method |
|---|---|---|---|
| MachineRequest | Omni | Provider | gRPC (Omni SDK) |
| Image schematic | Provider | Image Factory | HTTPS POST |
| Talos ISO | Image Factory | TrueNAS pool | HTTPS GET + JSON-RPC upload |
| VM CRUD | Provider | TrueNAS | JSON-RPC 2.0 (socket or WebSocket) |
| SideroLink enrollment | Talos VM | Omni | Outbound WireGuard (port 443) |
| Health status | Provider | Omni | gRPC (Omni SDK) |
| Telemetry | Provider | OTel collector | gRPC (OTLP) |
