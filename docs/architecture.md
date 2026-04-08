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
        Provisioner[provisioner/<br/>3 provision steps<br/>+ deprovision]
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

## Transport Auto-Detection

```mermaid
flowchart TD
    Start([Provider starts]) --> Check{Unix socket exists?<br/>/var/run/middleware/middlewared.sock}
    Check -->|Yes| Socket[Unix Socket Transport<br/>Zero-auth · lowest latency]
    Check -->|No| WSCheck{TRUENAS_HOST +<br/>TRUENAS_API_KEY set?}
    WSCheck -->|Yes| WS[WebSocket Transport<br/>API key auth · TLS]
    WSCheck -->|No| Fail([Startup failure:<br/>no transport available])
```

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
