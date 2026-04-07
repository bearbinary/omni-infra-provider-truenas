# CNI Selection & Setup Guide

Talos Linux ships with **Flannel** as the default CNI. It works out of the box with zero configuration. If you need network policy enforcement, observability, or eBPF performance, you can swap to Cilium or Calico via Omni config patches.

**Important:** CNI must be chosen before cluster bootstrap. Changing CNI on a running cluster requires a full teardown and rebuild.

For the official Siderolabs CNI documentation, see the [Kubernetes Guides - CNI](https://docs.siderolabs.com/kubernetes-guides/cni/).

---

## Quick Comparison

| CNI | Complexity | Network Policy | Observability | Best For |
|---|---|---|---|---|
| Flannel | None (default) | No | No | Getting started, simple clusters |
| Cilium | Moderate | Yes (L3-L7) | Hubble UI | Advanced networking, eBPF, production |
| Calico | Moderate | Yes (L3-L4) | Whisker UI | Network policy, BGP routing |

---

## Flannel (Default)

No configuration needed. Flannel is included in Talos and works immediately after cluster bootstrap.

**When to use:** You don't need network policies, you want the simplest setup, or you're just getting started.

---

## Cilium

[Cilium](https://cilium.io/) uses eBPF for high-performance networking with built-in network policy, load balancing, and the Hubble observability UI. See the [Siderolabs Cilium guide](https://docs.siderolabs.com/kubernetes-guides/cni/deploying-cilium).

### Step 1: Apply Omni Config Patch

Apply this as a **cluster-level config patch** in Omni before creating the cluster. This disables the default Flannel CNI.

**With kube-proxy (simpler):**

```yaml
cluster:
  network:
    cni:
      name: none
```

**Without kube-proxy (recommended for Cilium — better performance, fewer components):**

```yaml
cluster:
  network:
    cni:
      name: none
  proxy:
    disabled: true
```

### Step 2: Install Cilium via Helm

After the cluster is created and you have `kubeconfig` access:

**With kube-proxy:**

```bash
helm repo add cilium https://helm.cilium.io/
helm repo update

helm install cilium cilium/cilium \
    --version 1.18.0 \
    --namespace kube-system \
    --set ipam.mode=kubernetes \
    --set kubeProxyReplacement=false \
    --set securityContext.capabilities.ciliumAgent="{CHOWN,KILL,NET_ADMIN,NET_RAW,IPC_LOCK,SYS_ADMIN,SYS_RESOURCE,DAC_OVERRIDE,FOWNER,SETGID,SETUID}" \
    --set securityContext.capabilities.cleanCiliumState="{NET_ADMIN,SYS_ADMIN,SYS_RESOURCE}" \
    --set cgroup.autoMount.enabled=false \
    --set cgroup.hostRoot=/sys/fs/cgroup
```

**Without kube-proxy:**

```bash
helm install cilium cilium/cilium \
    --version 1.18.0 \
    --namespace kube-system \
    --set ipam.mode=kubernetes \
    --set kubeProxyReplacement=true \
    --set securityContext.capabilities.ciliumAgent="{CHOWN,KILL,NET_ADMIN,NET_RAW,IPC_LOCK,SYS_ADMIN,SYS_RESOURCE,DAC_OVERRIDE,FOWNER,SETGID,SETUID}" \
    --set securityContext.capabilities.cleanCiliumState="{NET_ADMIN,SYS_ADMIN,SYS_RESOURCE}" \
    --set cgroup.autoMount.enabled=false \
    --set cgroup.hostRoot=/sys/fs/cgroup \
    --set k8sServiceHost=localhost \
    --set k8sServicePort=7445
```

**Optional — enable Gateway API:**

Add these flags to either command above:

```bash
    --set gatewayAPI.enabled=true \
    --set gatewayAPI.enableAlpn=true \
    --set gatewayAPI.enableAppProtocol=true
```

### Talos-Specific Notes

- `ipam.mode=kubernetes` — required for Talos networking
- `SYS_MODULE` is intentionally excluded from capabilities — Talos does not allow kernel module loading from workloads
- `cgroup.autoMount.enabled=false` — Talos already provides cgroupv2 and bpffs mounts
- `k8sServiceHost=localhost` / `k8sServicePort=7445` — uses KubePrism when kube-proxy is disabled

---

## Calico

[Calico](https://www.tigera.io/project-calico/) provides network policy enforcement using either NFTables or eBPF. See the [Siderolabs Calico guide](https://docs.siderolabs.com/kubernetes-guides/cni/deploy-calico).

### Step 1: Apply Omni Config Patch

Same as Cilium — apply as a **cluster-level config patch** in Omni before creating the cluster:

```yaml
cluster:
  network:
    cni:
      name: none
```

### Step 2: Install the Tigera Operator

```bash
kubectl create -f https://raw.githubusercontent.com/projectcalico/calico/v3.31.4/manifests/tigera-operator.yaml
```

### Step 3: Configure Calico

Choose one of the following modes:

**NFTables mode (recommended for most setups):**

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: operator.tigera.io/v1
kind: Installation
metadata:
  name: default
spec:
  calicoNetwork:
    bgp: Disabled
    linuxDataplane: Nftables
    ipPools:
    - name: default-ipv4-ippool
      blockSize: 26
      cidr: 10.244.0.0/16
      encapsulation: VXLAN
      natOutgoing: Enabled
      nodeSelector: all()
  kubeletVolumePluginPath: None
---
apiVersion: operator.tigera.io/v1
kind: APIServer
metadata:
  name: default
EOF
```

**eBPF mode (higher performance, replaces kube-proxy):**

First, set the cgroup path for Talos:

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: crd.projectcalico.org/v1
kind: FelixConfiguration
metadata:
  name: default
spec:
  cgroupV2Path: "/sys/fs/cgroup"
EOF
```

Then apply the Installation:

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: operator.tigera.io/v1
kind: Installation
metadata:
  name: default
spec:
  calicoNetwork:
    bgp: Disabled
    linuxDataplane: BPF
    bpfNetworkBootstrap: Enabled
    kubeProxyManagement: Enabled
    ipPools:
    - name: default-ipv4-ippool
      blockSize: 26
      cidr: 10.244.0.0/16
      encapsulation: VXLAN
      natOutgoing: Enabled
      nodeSelector: all()
  kubeletVolumePluginPath: None
---
apiVersion: operator.tigera.io/v1
kind: APIServer
metadata:
  name: default
EOF
```

### Talos-Specific Notes

- `kubeletVolumePluginPath: None` — required for Talos (no writable host paths)
- `cgroupV2Path: "/sys/fs/cgroup"` — Talos mounts cgroups here instead of `/var`
- For eBPF mode, also disable kube-proxy via the Omni config patch (`cluster.proxy.disabled: true`)

---

## Further Reading

- [Siderolabs CNI Overview](https://docs.siderolabs.com/kubernetes-guides/cni/)
- [Cilium Documentation](https://docs.cilium.io/)
- [Calico Documentation](https://docs.tigera.io/calico/latest/about/)
- [Talos Network Configuration](https://docs.siderolabs.com/talos/v1.12/networking/)
