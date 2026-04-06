# Omni Infrastructure Provider Research

Research findings from analyzing all existing Omni infrastructure providers for implementing a TrueNAS provider.

## Provider Overview

| Provider | Type | Maintained By | Provisioning Model | Boot Method |
|----------|------|--------------|-------------------|-------------|
| bare-metal | Physical servers | Sidero Labs | COSI controllers (custom) | iPXE/PXE + IPMI/Redfish |
| kubevirt | VMs on K8s | Sidero Labs | Provision steps (SDK) | NoCloud qcow2 image |
| libvirt | VMs on libvirt/QEMU | Sidero Labs | Provision steps (SDK) | NoCloud qcow2 image |
| vsphere | VMs on vSphere | Sidero Labs | Provision steps (SDK) | Template clone + guestinfo |
| proxmox | VMs on Proxmox | Sidero Labs | Provision steps (SDK) | NoCloud ISO + CloudInit |
| oxide | Instances on Oxide | Oxide Computer | Provision steps (SDK) | NoCloud raw image + UserData |

---

## Common Architecture Pattern (VM Providers)

All VM-based providers (everything except bare-metal) follow an identical architecture pattern using the Omni SDK's `infra` package. The bare-metal provider is a special case that uses COSI controllers directly.

### Entry Point Pattern

Every provider's `main.go` follows this exact flow:

```go
// 1. Parse CLI flags (cobra) or env vars
// 2. Create platform-specific client (e.g., proxmox.NewClient, govmomi.NewClient)
// 3. Create Provisioner with that client
provisioner := provider.NewProvisioner(platformClient)

// 4. Create infra.Provider with ProviderConfig
ip, err := infra.NewProvider(meta.ProviderID, provisioner, infra.ProviderConfig{
    Name:        cfg.providerName,
    Description: cfg.providerDescription,
    Icon:        base64.RawStdEncoding.EncodeToString(icon),
    Schema:      schema,  // JSON schema for machine class config
})

// 5. Run provider (connects to Omni, starts watching for MachineRequests)
return ip.Run(cmd.Context(), logger, infra.WithOmniEndpoint(cfg.omniAPIEndpoint),
    infra.WithClientOptions(clientOptions...),
    infra.WithEncodeRequestIDsIntoTokens(),
)
```

### CLI Flags (Common Across All Providers)

Every provider accepts these flags:
- `--omni-api-endpoint` / `OMNI_ENDPOINT` env var
- `--omni-service-account-key` / `OMNI_SERVICE_ACCOUNT_KEY` env var
- `--id` (provider ID, defaults to provider name like "proxmox", "kubevirt", etc.)
- `--provider-name` (display name in Omni UI)
- `--provider-description`
- `--insecure-skip-verify` (Omni TLS)

Plus platform-specific flags:
- **Proxmox**: `--config-file` (YAML with Proxmox API credentials)
- **KubeVirt**: `--kubeconfig-file`, `--namespace`, `--data-volume-mode`
- **libvirt**: `--config-file`, `--image-cache-path`
- **vSphere**: `--config-file`
- **Oxide**: `--oxide-host`, `--oxide-token`, `--provisioner-concurrency`

---

## The Provisioner Interface

The core contract that every VM provider implements. Defined in `github.com/siderolabs/omni/client/pkg/infra/provision`:

```go
type Provisioner[T resource.Resource] interface {
    ProvisionSteps() []Step[T]
    Deprovision(ctx context.Context, logger *zap.Logger, state T, machineRequest *infra.MachineRequest) error
}
```

### ProvisionSteps

Returns an ordered list of named steps. Each step is a function that:
- Receives `context.Context`, `*zap.Logger`, and `provision.Context[*resources.Machine]`
- Can store intermediate state in `pctx.State.TypedSpec().Value` (persisted protobuf)
- Can return `provision.NewRetryInterval(duration)` to retry later (async polling)
- Can return `provision.NewRetryError(err, duration)` for retryable errors
- Returns `nil` when the step is complete

The SDK handles re-running steps, persistence, and coordination.

### provision.Context Key Methods

```go
pctx.GetRequestID()                    // Machine request ID (used as VM name)
pctx.GetTalosVersion()                 // Target Talos version
pctx.UnmarshalProviderData(&data)      // Unmarshal machine class config into provider Data struct
pctx.ConnectionParams.JoinConfig       // The Talos join config (cloud-init userdata)
pctx.GenerateSchematicID(ctx, logger,  // Generate Talos image factory schematic
    provision.WithExtraKernelArgs(...),
    provision.WithExtraExtensions(...),
    provision.WithoutConnectionParams(),
)
pctx.SetMachineUUID(uuid)             // Set the UUID that Omni will use to identify this machine
pctx.SetMachineInfraID(id)            // Set the infra-specific ID
pctx.CreateConfigPatch(ctx, name, patch) // Create Talos machine config patch in Omni
pctx.GetMachineRequestSetID()         // Get machine request set ID (for anti-affinity)
```

### Deprovision

Called when a machine needs to be destroyed. Receives the persisted machine state and the machine request. Must clean up all platform resources (VM, disks, volumes, etc.).

---

## Per-Provider Provisioning Flows

### KubeVirt (Simplest VM provider - good reference)

1. **validateRequest** - Check request ID length
2. **createSchematic** - Generate Talos image schematic (nocloud qcow2)
3. **ensureVolume** - Download Talos image as CDI DataVolume, poll until ready
4. **syncMachine** - Create/update KubeVirt VirtualMachine, configure CPU/memory/disk/network, pass join config via CloudInitNoCloud
5. **Deprovision** - Delete VirtualMachine (which cascades to volumes)

### Proxmox (Most relevant for TrueNAS VMs)

1. **pickNode** - Select Proxmox node (auto-select by memory + anti-affinity, or use configured node)
2. **createSchematic** - Generate Talos image schematic with qemu-guest-agent extension
3. **uploadISO** - Download Talos nocloud ISO from image factory, upload to Proxmox ISO storage
4. **syncVM** - Create VM with: CPU type, cores, sockets, memory, SCSI disk, network bridge, VLAN, additional disks, PCI passthrough, NUMA, hugepages
5. **startVM** - Inject CloudInit config (join config + hostname), start VM
6. **Deprovision** - Stop VM, delete VM

Key Proxmox features:
- Storage selection via CEL expressions (`storage_selector: 'name == "local-lvm"'`)
- Additional disks with per-disk options (SSD, discard, iothread, cache, AIO)
- Additional NICs with VLAN and firewall config
- PCI device passthrough via resource mappings
- GPU support (machine type q35, NUMA, hugepages)

### libvirt

1. **generateUUID** - Find unused UUID in libvirt
2. **createSchematic** - Generate Talos image schematic
3. **provisionPrimaryDisk** - Download Talos qcow2 image (via cache), create/upload storage volume, resize
4. **provisionAdditionalDisks** - Create additional qcow2 volumes (sata/nvme types)
5. **provisionCidata** - Generate NoCloud CIDATA ISO with hostname
6. **createVM** - Build libvirt domain XML, define domain
7. **startVM** - Start the domain
8. **Deprovision** - Destroy domain, undefine, delete volumes (main + additional + cidata)

### vSphere

1. **createVM** - Clone template, configure CPU/memory/disk/network, pass join config via guestinfo.talos.config
2. **powerOnVM** - Power on VM
3. **Deprovision** - Power off, destroy VM

Unique: Uses VM templates (clone-based), guestinfo for config, session keep-alive, CA cert support.

### Oxide (3rd-party - good example of minimal implementation)

1. **generate_schematic_id** - Generate schematic with Oxide-specific extensions (iscsi-tools, util-linux-tools)
2. **generate_image_factory_url** - Build nocloud-amd64.raw.xz URL
3. **generate_image_name** - Hash-based name for deduplication
4. **fetch_image_id** - Check if image already exists in Oxide
5. **create_image** - Download, decompress, bulk-import as Oxide disk, finalize as image
6. **instance_create** - Create instance with boot disk, VPC/subnet networking, pass join config as base64 UserData
7. **config_patch_provider_id** - Create kubelet providerID config patch
8. **Deprovision** - Stop instance, wait, delete instance, delete boot disk

Key Oxide differences from Sidero providers:
- Uses `github.com/ardanlabs/conf/v3` instead of cobra for config
- Uses `base64.StdEncoding` (not `RawStdEncoding`) for icon
- Sets `infra.WithConcurrency()` and `infra.WithHealthCheckFunc()`
- Creates kubelet providerID config patch
- Uses `pctx.SetMachineInfraID()` in addition to `SetMachineUUID()`

### Bare Metal (Special Architecture - Not SDK-based)

The bare-metal provider is fundamentally different:
- Does NOT use `infra.NewProvider()` or the provision steps SDK
- Uses full COSI controller runtime with multiple custom controllers
- Runs its own DHCP proxy, TFTP server, iPXE server, and HTTP API server
- Manages physical machines via IPMI/Redfish for power management
- Has its own agent (talos-metal-agent) that runs on machines
- Manages PXE boot, machine config injection, power operations, wipe operations, reboot cycles
- Registers as a "static" infra provider (sets `LabelIsStaticInfraProvider`)

---

## Resource / State Model

### Protobuf MachineSpec (Provider State)

Each provider defines a protobuf `MachineSpec` that stores provisioning state. This is persisted in Omni and passed back to provision steps and deprovision.

| Provider | Fields |
|----------|--------|
| KubeVirt | uuid, schematic, talos_version, volume_id |
| Proxmox | uuid, schematic, talos_version, volume_id, node, volume_upload_task, vm_create_task, vm_start_task, vmid |
| libvirt | uuid, schematic_id, talos_version, vm_vol_name, additional_disks[], network_interfaces[], cidata_vol_name, pool_name, vm_name |
| vSphere | uuid, schematic_id, talos_version, vm_vol_name, pool_name, vm_name, datacenter |
| Oxide | uuid, instance_id, image_id, image_name, talos_schematic_id, talos_image_url |

Pattern: Store enough state to track async operations and clean up on deprovision.

### COSI Resource Registration

Each provider defines a `Machine` typed resource:

```go
type Machine = typed.Resource[MachineSpec, MachineExtension]
type MachineSpec = protobuf.ResourceSpec[specs.MachineSpec, *specs.MachineSpec]

// ResourceDefinition uses provider-scoped type and namespace
func (MachineExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
    return meta.ResourceDefinitionSpec{
        Type:             infra.ResourceType("Machine", providerID),
        Aliases:          []resource.Type{},
        DefaultNamespace: infra.ResourceNamespace(providerID),
        PrintColumns:     []meta.PrintColumn{},
    }
}
```

### Machine Class Schema (JSON Schema)

Each provider embeds a `schema.json` file that defines what configuration users can set when creating a machine class in Omni. This is a JSON Schema document that Omni uses to render the UI.

Common fields across providers: `cores/vcpus`, `memory`, `disk_size`
Provider-specific: `storage_pool`, `network_bridge`, `vlan`, `datacenter`, `template`, `project`, `vpc`, `subnet`, etc.

---

## Configuration Patterns

### Config File (Proxmox, libvirt, vSphere)

YAML file with platform API credentials:

```yaml
# Proxmox example
proxmox:
  url: "https://proxmox:8006/api2/json"
  username: root
  password: secret
  realm: "pam"
  # OR token-based:
  tokenID: "root@pam!provider"
  tokenSecret: "..."
  insecureSkipVerify: true
```

### Environment Variables (KubeVirt, Oxide)

KubeVirt uses kubeconfig file or in-cluster config.
Oxide uses `--oxide-host` and `--oxide-token` flags.

---

## Build System

All Sidero Labs providers use **kres** (auto-generated Makefile + Dockerfile):
- `make omni-infra-provider-<name>-linux-amd64` - Build binary
- `make image-omni-infra-provider-<name>` - Build Docker image
- `make unit-tests` - Run tests
- `make lint` - Run linters (golangci-lint, gofumpt, govulncheck, markdownlint)
- `make generate` - Regenerate protobuf
- `make fmt` - Format code

Oxide provider uses a simpler custom Makefile.

Docker images published to `ghcr.io/siderolabs/omni-infra-provider-<name>`.

---

## Key Dependencies

### Required by all providers
- `github.com/siderolabs/omni/client` - The Omni client SDK (infra package, resources, client)
- `github.com/cosi-project/runtime` - COSI runtime for resource types
- `google.golang.org/protobuf` - Protobuf for machine state
- `github.com/planetscale/vtprotobuf` - Fast protobuf marshaling (Sidero providers)
- `go.uber.org/zap` - Logging

### Platform-specific
- **Proxmox**: `github.com/luthermonson/go-proxmox`
- **KubeVirt**: `kubevirt.io/api`, `kubevirt.io/containerized-data-importer-api`, `sigs.k8s.io/controller-runtime`
- **libvirt**: `github.com/digitalocean/go-libvirt`, `libvirt.org/go/libvirtxml`
- **vSphere**: `github.com/vmware/govmomi`
- **Oxide**: `github.com/oxidecomputer/oxide.go/oxide`

### CLI
- **Sidero providers**: `github.com/spf13/cobra`
- **Oxide**: `github.com/ardanlabs/conf/v3`

---

## Testing Patterns

Most providers have minimal testing:
- **Proxmox**: Unit tests for provision logic (`provision_test.go`, `export_test.go`)
- **Bare-metal**: Integration tests for BMC operations, unit tests for controllers
- **Others**: No tests found in the repos

---

## Implications for TrueNAS Provider

### Architecture Decision: VM-based Provider

TrueNAS SCALE supports VMs via its API (based on libvirt/bhyve). The provider should follow the standard VM provider pattern using the Omni SDK's `infra.NewProvider()` + `provision.Step` architecture.

### Recommended Provision Steps

1. **createSchematic** - Generate Talos schematic (nocloud image, likely with qemu-guest-agent extension)
2. **uploadImage** - Download Talos image, upload to TrueNAS storage (zvol or ISO)
3. **createVM** - Create VM via TrueNAS API with CPU, memory, disk, NIC config
4. **startVM** - Inject cloud-init config and start VM
5. **Deprovision** - Stop VM, delete VM, clean up storage

### TrueNAS API Client

TrueNAS SCALE exposes a REST API (WebSocket-based in some versions). Key endpoints:
- `/vm` - CRUD for VMs
- `/pool` - Storage pool management
- `/disk` - Disk management
- `/vm/start`, `/vm/stop`, `/vm/poweroff` - Power management

### Machine Class Schema (schema.json)

```json
{
  "type": "object",
  "properties": {
    "cores": { "type": "integer", "minimum": 1 },
    "memory": { "type": "integer", "description": "In MiB" },
    "disk_size": { "type": "integer", "description": "In GiB" },
    "storage_pool": { "type": "string", "description": "ZFS pool name" },
    "network_bridge": { "type": "string", "description": "Network bridge" },
    "vlan": { "type": "integer", "description": "VLAN tag (optional)" }
  },
  "required": ["cores", "memory", "disk_size", "storage_pool"]
}
```

### Protobuf MachineSpec

```protobuf
message MachineSpec {
  string uuid = 1;
  string schematic = 2;
  string talos_version = 3;
  string vm_id = 4;          // TrueNAS VM ID
  string zvol_name = 5;      // Backing zvol for disk
  string image_volume = 6;   // Cached Talos image location
}
```

### Config File

```yaml
truenas:
  url: "https://truenas.local/api/v2.0"
  api_key: "1-abc123..."
  insecureSkipVerify: true
```

### Project Structure (Recommended)

```
omni-infra-provider-truenas/
  cmd/omni-infra-provider-truenas/
    main.go           # Entry point, CLI flags, TrueNAS client setup
    data/
      schema.json     # Machine class JSON schema
      icon.svg        # Provider icon for Omni UI
  api/specs/
    specs.proto       # MachineSpec protobuf definition
    specs.pb.go       # Generated
    specs_vtproto.pb.go # Generated (optional, for vtprotobuf)
  internal/pkg/
    config/
      config.go       # TrueNAS connection config struct
    provider/
      provision.go    # Provisioner struct + ProvisionSteps + Deprovision
      data.go         # Machine class Data struct (matches schema.json)
      meta/
        meta.go       # ProviderID = "truenas"
      resources/
        machine.go    # Machine COSI resource definition
  go.mod
  Makefile
  Dockerfile
```
