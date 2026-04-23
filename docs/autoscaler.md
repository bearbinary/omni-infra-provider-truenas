# Autoscaler — experimental

> **Status**: experimental. Scale-up only. Per-MachineClass opt-in via
> annotations. API (annotation keys, gRPC contract, metric names) may
> change between minor releases until this feature is promoted to stable.
> Production use at your own discretion — the feature is observable, gated
> at multiple layers, and shippable behind a disabled default, but it has
> not yet accumulated the field experience of the core provisioner path.

The provider ships with an `autoscaler` subcommand that implements the
[Kubernetes Cluster Autoscaler external-gRPC cloud-provider
interface](https://github.com/kubernetes/autoscaler/blob/master/cluster-autoscaler/cloudprovider/externalgrpc/README.md).
When deployed alongside the upstream `cluster-autoscaler` sidecar, it
translates pending-pod pressure into `MachineAllocation.MachineCount`
updates on Omni `MachineSet` resources — Omni then tells the infra
provider (in this case, our TrueNAS provider) to create the new VMs.

Scale-down is disabled at multiple layers and out of scope for the
experimental phase.

## What you get

- **Scale-up on pending-pod pressure.** Cluster-autoscaler's standard
  detection, no special scheduling required.
- **TrueNAS-aware capacity gating.** Refuses scale-up when the target
  pool's free bytes or host memory would drop below configured
  thresholds — avoids queuing VM creates the hypervisor can't fulfill.
  Per-MachineClass hard/soft gating.
- **Per-cluster opt-in** via MachineClass annotations. Clusters without
  annotated MachineClasses are untouched.
- **Shared image** with the provisioner — one binary, one release
  stream, one Omni service-account key if you want to colocate.

## What you don't get (yet)

- Scale-down. The sidecar runs with `--scale-down-enabled=false`; the
  gRPC server also returns `Unimplemented` on scale-down RPCs as belt-
  and-suspenders.
- Control-plane autoscaling. MachineSets with the control-plane role
  label are skipped wholesale — CP scaling isn't a cluster-autoscaler
  concern upstream.
- Host-memory capacity check. The interface is in place but the
  TrueNAS `system.mem_info` wrapper lands in a follow-up. Until
  then, set `bearbinary.com/autoscale-min-host-mem-gib: "0"` on
  annotated MachineClasses to disable the memory dimension. Pool-free
  check is fully wired.
- Scale-from-zero. MachineSets with `min: 0` are not yet supported;
  the CAS sidecar's upstream behavior around zero-sized node groups
  relies on `NodeGroupTemplateNodeInfo`, which we don't implement yet.
- Multi-cluster from one Deployment. One autoscaler Deployment per
  Omni cluster, matching the upstream CAS assumption.

## Opt-in: MachineClass annotations

An Omni `MachineClass` opts in by setting
`bearbinary.com/autoscale-min` and `bearbinary.com/autoscale-max`.
A MachineClass without both annotations is not discovered.

| Annotation                                       | Required | Default    | Meaning |
|--------------------------------------------------|----------|------------|---------|
| `bearbinary.com/autoscale-min`                   | yes      | —          | Node group minimum (integer ≥ 0). |
| `bearbinary.com/autoscale-max`                   | yes      | —          | Node group maximum (integer ≥ min). |
| `bearbinary.com/autoscale-pool`                  | no       | *(unset)*  | TrueNAS pool the capacity gate queries. Falls back to the provider's `DEFAULT_POOL` when empty. |
| `bearbinary.com/autoscale-capacity-gate`         | no       | `hard`     | `hard` blocks scale-up on capacity breach; `soft` logs a warn and proceeds. |
| `bearbinary.com/autoscale-min-pool-free-gib`     | no       | `50`       | Hard-gate pool-free threshold in GiB. `0` disables the pool check. |
| `bearbinary.com/autoscale-min-host-mem-gib`      | no       | `8`        | Hard-gate host-free-memory threshold in GiB. **Set to `0` until the `system.mem_info` wrapper lands**, otherwise the gate fails closed. |

### Example: annotate a MachineClass via omnictl

```bash
omnictl get machineclasses talos-home-workers -o yaml > /tmp/mc.yaml

# Edit /tmp/mc.yaml and add under metadata.annotations:
#   bearbinary.com/autoscale-min: "2"
#   bearbinary.com/autoscale-max: "8"
#   bearbinary.com/autoscale-pool: "default"
#   bearbinary.com/autoscale-min-host-mem-gib: "0"

omnictl apply -f /tmp/mc.yaml
```

Any parse failure on an annotation causes that **single MachineSet** to
be skipped with a warn log; other MachineSets keep autoscaling. Run
`kubectl logs deployment/omni-autoscaler -c autoscaler | grep
machineset` to see which classes parsed and which got skipped.

## Deploy

```bash
helm install omni-autoscaler deploy/helm/omni-autoscaler \
  --namespace omni-autoscaler --create-namespace \
  --set cluster.name=talos-home \
  --set omni.endpoint=https://omni.example.com \
  --set-file omni.serviceAccountKeyB64=/path/to/base64.txt \
  --set truenas.host=truenas.lan \
  --set-file truenas.apiKey=/path/to/api-key.txt
```

One Deployment per cluster. The chart renders two containers in one pod:

- `autoscaler` — our provider binary in `autoscaler` subcommand mode,
  gRPC-listening on `:8086`.
- `cluster-autoscaler` — the upstream sidecar dialing `localhost:8086`
  via the `externalgrpc` provider.

The pod `bearbinary.com/experimental=true` label makes it easy to find
every resource the chart produced via `kubectl get all -l
bearbinary.com/experimental=true -A`.

### RBAC

cluster-autoscaler watches nodes/pods/PDBs in the workload cluster to
detect pressure. The chart does not create the ClusterRoleBinding
because many operators already run their own curated CAS role set.
Apply the upstream one against this chart's ServiceAccount:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: omni-autoscaler
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-autoscaler
subjects:
  - kind: ServiceAccount
    name: omni-autoscaler
    namespace: omni-autoscaler
```

## Observe

**Logs** — structured JSON; key entries:

| Grep for                                   | Meaning |
|--------------------------------------------|---------|
| `autoscaler EXPERIMENTAL`                  | Boot banner. Exactly one per process start. |
| `NodeGroupIncreaseSize: scaled up`         | A successful scale-up write. Includes group, delta, old/new size. |
| `capacity gate denied`                     | Hard-gate blocked a request. Includes reason. |
| `capacity gate soft-warn, proceeding`      | Soft-gate logged a near-miss but allowed the write. |
| `skipping MachineSet: classification failed` | An annotation parse or MachineClass lookup failed — that one set is skipped. |
| `autoscaler: TRUENAS_HOST unset`           | The deploy is running without capacity gating. Intentional for dry-runs; not recommended long-term. |

**Metrics** — not yet wired. The server registers no custom metrics in
the experimental phase; observability flows through logs + Omni's own
MachineSet watch history. A follow-up adds
`truenas_autoscaler_scaleup_requests_total{result=…}` +
`truenas_autoscaler_capacity_denials_total{reason=…}`.

## Disable

The feature is opt-in at two layers. Either is sufficient:

1. **Remove the autoscale annotations from the MachineClass.** Takes
   effect on the next refresh (~60s). No pod restart required.
2. **`helm uninstall omni-autoscaler -n omni-autoscaler`.** Removes the
   Deployment; provisioner is untouched.

## Known limitations

- **Cassette-based tests don't cover the write path.** Phase 3d+
  wire-path tests use an in-memory COSI state. Integration tests
  against a real Omni land alongside the broader
  `test-integration` target only once the feature graduates.
- **Node → node-group mapping (`NodeGroupForNode` RPC) always returns
  "not ours".** The full mapping requires joining MachineSetNode +
  ClusterMachine, and scale-down is disabled anyway — CAS only calls
  this during scale-down decisions. Revisited when scale-down ships.
- **No autoscale-from-zero.** `NodeGroupTemplateNodeInfo` is
  `Unimplemented`; CAS needs template info to schedule pods on
  zero-sized groups. If your MachineSet can drop to zero, the
  autoscaler won't bring it back up.
- **Scale decisions are not rate-limited independently of CAS.** The
  sidecar's `--max-node-provision-time` and `--max-graceful-termination-sec`
  flags still apply.

## Upstream reference

- Cluster Autoscaler external-gRPC cloud-provider contract:
  https://github.com/kubernetes/autoscaler/tree/master/cluster-autoscaler/cloudprovider/externalgrpc
- Justin Rothgar's `omni-node-autoscaler` PoC that this subcommand
  vendors code from (private, by request).
- The vendored protos under `internal/autoscaler/proto/externalgrpc/`
  include `PROVENANCE.md` for refresh workflow.
