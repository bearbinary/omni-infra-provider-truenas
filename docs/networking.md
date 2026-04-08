# Networking Guide

Network configuration for TrueNAS-hosted Kubernetes clusters — bridge setup, DHCP reservations, load balancer IPs, VIP, and router-specific guides.

---

## Architecture Overview

```
┌─────────────┐     ┌─────────────────────────────────┐
│   Router     │     │         TrueNAS SCALE            │
│  (UniFi /    │     │                                   │
│  pfSense /   │◄────┤  br100 (VLAN 100)                │
│  OPNsense)   │     │    ├─ omni_cp_1    (DHCP .50)    │
│              │     │    ├─ omni_cp_2    (DHCP .51)    │
│  DHCP Server │     │    ├─ omni_cp_3    (DHCP .52)    │
│  Gateway .1  │     │    ├─ omni_worker_1 (DHCP .60)   │
│              │     │    └─ omni_worker_2 (DHCP .61)   │
└─────────────┘     └─────────────────────────────────┘

VIP:     192.168.100.254  (Talos, floats between CP nodes)
MetalLB: 192.168.100.201-250  (advertised via L2 ARP)
DHCP:    192.168.100.50-200   (managed by router)
```

All VMs share a single Layer 2 broadcast domain via the TrueNAS bridge. The router provides DHCP and acts as the default gateway. MetalLB and VIP operate at Layer 2 using gratuitous ARP — no special router configuration needed.

---

## TrueNAS Bridge Setup

### Option A: Bridge (Recommended)

A bridge groups one or more physical NICs into a virtual switch. VMs connect to this bridge.

1. **TrueNAS UI > Network > Interfaces > Add**
2. Type: **Bridge**
3. Bridge Members: select your physical NIC (e.g., `enp5s0`)
4. If using VLANs: the physical NIC should be a trunk port carrying your VLAN tags

Set `DEFAULT_NETWORK_INTERFACE=br0` (or `br100` for a VLAN-tagged bridge) on the provider.

### Option B: VLAN Interface

If your physical NIC is a trunk port and you want VMs on a specific VLAN without a bridge:

1. **TrueNAS UI > Network > Interfaces > Add**
2. Type: **VLAN**
3. Parent Interface: your physical NIC
4. VLAN Tag: `100`

Set `DEFAULT_NETWORK_INTERFACE=vlan100` on the provider.

### Option C: Physical NIC

Pass a physical NIC directly. Only one VM can use it (or use macvtap). Not recommended for multi-VM clusters.

---

## IP Address Planning

Plan your subnet **before** deploying. Every range must be non-overlapping.

### Recommended `/24` Layout

| Range | Count | Purpose | Managed By |
|---|---|---|---|
| `.1` | 1 | Gateway | Router |
| `.2-.49` | 48 | Infrastructure (NAS, switches, APs) | DHCP reservations |
| `.50-.200` | 151 | DHCP pool (VMs, devices) | Router DHCP server |
| `.201-.250` | 50 | MetalLB / LoadBalancer Services | MetalLB L2 |
| `.251-.253` | 3 | Reserved (future use) | — |
| `.254` | 1 | Kubernetes API VIP | Talos VIP |
| `.255` | 1 | Broadcast | — |

> **Critical: Your router's DHCP range must STOP at `.200` (or wherever you choose) to leave room above it for MetalLB and VIP.** If the DHCP range extends to `.254`, there's nowhere to put load balancer IPs without conflicts. Configure your DHCP server to end its range **before** the MetalLB block starts.
>
> The MetalLB range (`.201-.250`) is how your Kubernetes Services get externally-accessible IPs on your LAN. When you create a `LoadBalancer` Service, MetalLB assigns an IP from this pool and announces it via ARP. Any device on the same VLAN can reach it — this is how you expose Ingress, dashboards, and applications to your home network.

### Smaller `/25` or `/26` Networks

Adjust proportionally. The key constraint is: DHCP range + MetalLB range + VIPs must all fit within the subnet with no overlap.

---

## DHCP Reservations (Stable VM IPs)

VMs use DHCP by default. For stable IPs, create **DHCP reservations** on your router — the router assigns a fixed IP based on the VM's MAC address. The VM itself still uses DHCP; it doesn't need static network config.

This is preferred over static IP configuration because:
- The router is the single source of truth for IP assignments
- No Talos machine config patches needed
- Works with any DHCP server
- Easy to change IPs without reprovisioning VMs

### Finding the MAC Address

The provider logs each VM's MAC address during provisioning:

```
VM NIC MAC address — use this for DHCP reservation in your router
  mac=00:a0:98:18:c4:af  vm_name=omni_cluster_workers_abc123  network_interface=br100
```

You can also find it in TrueNAS UI: **Virtualization > click VM > Devices > NIC**.

### UniFi Controller

1. **Network Application > Client Devices** — find the VM by MAC or current IP
2. Click the client > **Settings** (gear icon)
3. Enable **Fixed IP Address**
4. Enter the desired IP (must be within your DHCP range, e.g., `.50-.200`)
5. The reservation takes effect on the VM's next DHCP renewal (reboot or wait for lease expiry)

> **UniFi Note**: Fixed IP assignments are DHCP reservations, not true static IPs. The VM still uses DHCP — UniFi just always gives it the same address.

### pfSense

1. **Services > DHCP Server > select the VLAN interface**
2. Scroll to **DHCP Static Mappings**
3. Click **Add**
4. Enter MAC address and desired IP
5. Save and Apply Changes

### OPNsense

1. **Services > ISC DHCPv4 > select the interface**
2. Scroll to **DHCP Static Mappings**
3. Click **Add** (+)
4. Enter MAC address, IP, and hostname
5. Save and Apply

### dnsmasq / Pi-hole

Add to `/etc/dnsmasq.d/dhcp-reservations.conf`:
```
dhcp-host=00:a0:98:18:c4:af,192.168.100.51,omni-cp-1
dhcp-host=00:a0:98:22:b3:de,192.168.100.52,omni-cp-2
dhcp-host=00:a0:98:33:f1:ab,192.168.100.60,omni-worker-1
```

Restart dnsmasq: `sudo systemctl restart dnsmasq`

### ISC DHCP Server

Add to `/etc/dhcp/dhcpd.conf` inside the subnet block:
```
host omni-cp-1 {
  hardware ethernet 00:a0:98:18:c4:af;
  fixed-address 192.168.100.51;
}
```

Restart: `sudo systemctl restart isc-dhcp-server`

---

## MetalLB (LoadBalancer Services)

MetalLB provides `LoadBalancer` type Services in bare-metal Kubernetes clusters. In Layer 2 mode, it responds to ARP requests for Service IPs — no BGP or special router config needed.

### Why IPs Must Be Outside DHCP Range

MetalLB uses **gratuitous ARP** to announce Service IPs. If a MetalLB IP overlaps with the DHCP range:

1. Router hands `.205` to a phone via DHCP
2. MetalLB also claims `.205` for an Ingress Service
3. Both devices respond to ARP for `.205`
4. Traffic randomly goes to the phone or the Service
5. Intermittent failures, impossible to debug without packet captures

**Solution**: Reserve a block outside DHCP for MetalLB. In the recommended layout: `.201-.250`.

### Installation

```bash
helm repo add metallb https://metallb.github.io/metallb
helm install metallb metallb/metallb --namespace metallb-system --create-namespace
```

### Configuration

```yaml
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: default
  namespace: metallb-system
spec:
  addresses:
    - 192.168.100.201-192.168.100.250
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: default
  namespace: metallb-system
spec:
  ipAddressPools:
    - default
```

> **Important**: The `L2Advertisement` resource is required. Without it, MetalLB allocates IPs but doesn't announce them — Services get an External IP but it's unreachable.

### Verification

```bash
# Check MetalLB assigned an IP
kubectl get svc -A | grep LoadBalancer

# From another device on the same VLAN, verify ARP
arping -I eth0 192.168.100.201

# Should see replies from the node running the MetalLB speaker
```

---

## Control Plane VIP (Kubernetes API HA)

Talos supports a **shared virtual IP** for the Kubernetes API server. One control plane node holds the VIP at a time; on failure, another takes over via etcd leader election (~1 minute failover).

### How It Works

1. Talos runs a VIP manager on each control plane node
2. The etcd leader sends gratuitous ARP for the VIP address
3. All traffic to the VIP hits the current leader
4. If the leader fails, a new etcd leader is elected and takes over the VIP
5. Clients experience ~1 minute interruption during failover

### Setup

Apply as an **Omni config patch on the control plane machine set** (not per-machine — all CP nodes need the same VIP config):

```yaml
machine:
  network:
    interfaces:
      - interface: eth0
        vip:
          ip: 192.168.100.254
```

### Requirements

- All control plane nodes on the **same Layer 2 network** (same bridge/VLAN)
- VIP **outside** DHCP range and MetalLB pool
- Minimum 3 control plane nodes for HA (1 node works but has no failover)
- VIP is **unavailable during initial bootstrap** until etcd is running
- Use for **Kubernetes API only** — Talos API should be accessed per-node

### Using the VIP

After cluster creation, your kubeconfig endpoint should point to the VIP:

```bash
# Omni typically sets this automatically. To verify:
kubectl config view | grep server
# Should show: https://192.168.100.254:6443
```

See [Talos VIP documentation](https://docs.siderolabs.com/talos/v1.12/networking/advanced/vip/) for advanced options.

---

## UniFi-Specific Guide

### Dedicated Kubernetes VLAN (Recommended)

Do **not** use UniFi Auto-Scale Network for Kubernetes VMs. Create a dedicated VLAN with a fixed subnet.

#### Step-by-Step

1. **UniFi Console > Settings > Networks > Create New Network**
   - Name: `Kubernetes` (or `K8s-Cluster`)
   - Router: your UDM/USG
   - VLAN ID: `100` (or any unused ID)
   - Gateway/Subnet: `192.168.100.1/24`
   - DHCP Range: `192.168.100.50` - `192.168.100.200` (**stop at .200** — leave .201+ for MetalLB/VIP)
   - DHCP DNS: your DNS servers (or leave default)

2. **Configure the switch port for TrueNAS**
   - UniFi Console > Devices > your switch > Ports
   - Find the port connected to TrueNAS
   - Port Profile: either `All` (trunk all VLANs) or create a custom profile that includes VLAN 100
   - If TrueNAS has multiple NICs, you can dedicate one to this VLAN

3. **Create the bridge on TrueNAS**
   - TrueNAS UI > Network > Interfaces > Add
   - Type: Bridge, or VLAN if you want a tagged interface
   - For VLAN: Parent = physical NIC, VLAN Tag = 100

4. **Set the provider config**
   ```
   DEFAULT_NETWORK_INTERFACE=br100
   ```

#### Why Not Auto-Scale Network?

UniFi Auto-Scale dynamically creates VLANs and subnets. Problems for Kubernetes:

| Issue | Impact |
|---|---|
| Unpredictable subnets | Can't pre-configure MetalLB IP pool |
| DHCP range not configurable | Can't carve out static ranges for LB |
| Subnet may change | MetalLB config becomes invalid |
| No per-VLAN DHCP customization | Can't set lease times or DNS per-VLAN |

**Auto-Scale is designed for transient consumer devices** (phones, IoT, guests). Kubernetes clusters need stable, predictable networking.

**Recommendation**: Auto-Scale for everything else, dedicated fixed VLAN for Kubernetes.

---

## pfSense / OPNsense Guide

### VLAN Setup

1. **Interfaces > VLANs > Add**
   - Parent: the interface connected to TrueNAS
   - VLAN Tag: `100`

2. **Interfaces > Assignments** — assign the new VLAN as an interface (e.g., `OPT1`)

3. **Interfaces > OPT1** — enable, set static IP `192.168.100.1/24`

4. **Services > DHCP Server > OPT1**
   - Enable DHCP
   - Range: `192.168.100.50` to `192.168.100.200` (**end before .201** — reserve .201+ for MetalLB)
   - DNS: your preferred DNS servers

5. **Firewall > Rules > OPT1** — add rules to allow traffic (at minimum: allow all from the K8s VLAN, or be more restrictive)

### Inter-VLAN Routing

By default, pfSense/OPNsense routes between VLANs. If you want to access MetalLB Services from your main LAN, no extra config is needed — the firewall handles routing.

If you want **isolation** (K8s VLAN can't reach other VLANs), add firewall rules to block inter-VLAN traffic except specific ports.

---

## Mikrotik / RouterOS Guide

### VLAN and DHCP

```routeros
# Create VLAN on trunk port
/interface vlan add name=vlan100 vlan-id=100 interface=ether1

# Assign IP
/ip address add address=192.168.100.1/24 interface=vlan100

# DHCP pool — STOP at .200, leave .201+ for MetalLB and VIP
/ip pool add name=k8s-dhcp ranges=192.168.100.50-192.168.100.200

# DHCP server
/ip dhcp-server add interface=vlan100 address-pool=k8s-dhcp
/ip dhcp-server network add address=192.168.100.0/24 gateway=192.168.100.1 dns-server=1.1.1.1,8.8.8.8

# DHCP reservation example
/ip dhcp-server lease add mac-address=00:A0:98:18:C4:AF address=192.168.100.51 server=dhcp-k8s
```

---

## Multiple Clusters on One TrueNAS

Use separate VLANs per cluster for network isolation:

| Cluster | VLAN | Subnet | Bridge | MetalLB Range |
|---|---|---|---|---|
| Production | 100 | `192.168.100.0/24` | `br100` | `.201-.250` |
| Staging | 101 | `192.168.101.0/24` | `br101` | `.201-.250` |
| Dev | 102 | `192.168.102.0/24` | `br102` | `.201-.250` |

Each cluster's MachineClass targets a different bridge:

```yaml
# Production
network_interface: "br100"

# Staging
network_interface: "br101"
```

VMs on different VLANs can't communicate at Layer 2 — full isolation without firewall rules.

---

## Troubleshooting

### VM has no IP address

1. **Check bridge exists**: SSH to TrueNAS, run `ip link show br100` — should show `UP`
2. **Check DHCP server**: From TrueNAS, `dhcping -s 192.168.100.1` (or check router DHCP logs)
3. **Test manually**: Create an Alpine VM on the same bridge, run `ip link set eth0 up && udhcpc -i eth0`
4. **Check VLAN tagging**: If using tagged VLANs, verify the switch port is configured as a trunk carrying that VLAN

### VM gets IP but can't reach internet

1. **Check gateway**: `ip route` on the VM should show default via your gateway
2. **Check DNS**: `nslookup google.com` — if this fails, DNS is wrong
3. **Check firewall**: Your router may block traffic from the K8s VLAN. Add a permit rule.
4. **Check NAT**: pfSense/OPNsense need an outbound NAT rule for the K8s VLAN if it's not on the default LAN

### MetalLB IPs not reachable from other VLANs

1. **Layer 2 only**: MetalLB L2 mode only works within the same broadcast domain. From another VLAN, traffic must be routed through the gateway.
2. **Check routing**: Your router must have a route to the MetalLB subnet. If MetalLB IPs are on the same subnet as the nodes, the router already knows the route.
3. **Check firewall**: The router may block traffic to the MetalLB range. Add a permit rule for the destination IPs.
4. **Verify ARP**: From a device on the same VLAN: `arping 192.168.100.201` — should get a reply from a node MAC.

### VIP not working

1. **Check etcd**: `talosctl -n <cp-node-ip> etcd members` — all CP nodes should be listed
2. **Check VIP config**: `talosctl -n <cp-node-ip> get addresses` — should show the VIP
3. **Same L2 required**: VIP uses gratuitous ARP — all CP nodes must be on the same bridge/VLAN
4. **Bootstrap timing**: VIP is unavailable until etcd has quorum. On a fresh cluster, wait for all CP nodes to join.
5. **ARP cache**: If VIP just moved between nodes, clients may have stale ARP. Wait 1-2 minutes or clear ARP: `arp -d 192.168.100.254`
