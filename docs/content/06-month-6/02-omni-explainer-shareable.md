---
title: "What an Omni provider actually is (and why I built one for a NAS)"
published: false
description: "If you've heard of Kubernetes but not Sidero Omni: here's what Omni is, why it has 'infrastructure providers,' and the unreasonable freedom they give you."
tags: kubernetes, omni, infrastructure, opensource
cover_image: ""
series: "Build-in-public: omni-infra-provider-truenas"
---

**TL;DR — Sidero Omni is a Kubernetes management platform. Its killer feature isn't the UI or the upgrade tooling, it's a small extension point called "infrastructure providers" that lets anyone teach Omni how to create cluster nodes on a new kind of hardware. I used that extension point to build one for TrueNAS SCALE, and the experience changed how I think about extensible systems generally. This post explains what infrastructure providers are, why they're interesting, and what you can build with them.**

I'm Zac Clifton. I maintain [`omni-infra-provider-truenas`](https://github.com/bearbinary/omni-infra-provider-truenas) — an open-source Omni infrastructure provider that runs Talos Linux VMs on TrueNAS. If you found this post by Googling "omni provider" or "what is sidero omni" or you just want to understand why people build extensions for managed Kubernetes platforms — this is for you. No prior knowledge of Omni assumed.

---

## What problem does Omni solve?

Kubernetes is famously hard to operate. Not Kubernetes the API — Kubernetes the cluster. Standing up a cluster requires bootstrapping etcd, signing certificates, distributing config, joining nodes, rolling upgrades, handling failures, taking backups, managing access. None of it is impossible. All of it is tedious.

The market response has been roughly: pay a cloud provider (EKS, GKE, AKS) to do the operating part for you, and you just consume the API. That works great if you're in their cloud. It works poorly if you're on-premises, in a homelab, or hybrid.

**Omni's pitch**: a managed Kubernetes control plane that's environment-agnostic. You bring nodes from anywhere — cloud VMs, bare-metal hardware, hypervisor VMs, Raspberry Pis — and Omni handles the operating part. The result is a managed Kubernetes experience without the cloud lock-in.

Two technical choices make this work:

1. **Talos Linux as the node OS**. Talos is purpose-built for Kubernetes. No SSH, no shell, no package manager. Every node is reproducible, every config change is a Kubernetes API call. This makes the "consume node from anywhere" model tractable.
2. **SideroLink for connectivity**. An outbound WireGuard tunnel from each Talos node back to Omni. The node always initiates the connection — no inbound ports needed. This makes "node behind a NAT" work as well as "node in a public VPC."

The combination means Omni can manage a Kubernetes cluster whose nodes are physically scattered across cloud regions, your office rack, and a NAS in your basement, and it all just looks like one cluster to `kubectl`.

---

## Where infrastructure providers come in

There's still a missing step: who *creates* the nodes?

For some setups, you create them by hand: spin up a cloud VM, boot Talos, point it at Omni, done. For setups where you have a lot of hardware churn — autoscaling, ephemeral workloads, dynamic provisioning — manual node creation doesn't cut it. You want Omni to be able to ask "give me 3 more workers" and have something *make* those nodes appear.

That's what an **infrastructure provider** is. It's an extension point. You write a small process that:

1. Talks to Omni and registers itself with a provider ID
2. Listens for `MachineRequest` resources from Omni
3. When a `MachineRequest` arrives, creates the corresponding Talos node on whatever hardware you support — cloud, hypervisor, bare metal, anything
4. When the `MachineRequest` is deleted, tears down the node

Omni doesn't care *how* you create nodes. It cares that nodes appear when requested and disappear when asked. The provider is the contract.

The official Sidero-maintained providers cover the obvious cases: AWS, GCP, Hetzner, Proxmox, baremetal (Sidero's existing metal product). Community providers cover the long tail. There's no provider for TrueNAS SCALE, so I wrote one.

---

## What the contract actually looks like

In Go, the entire shape of an infrastructure provider is roughly:

```go
provider, err := infra.NewProvider(
    "truenas",                 // provider ID
    state,                      // Omni state client
    infra.ProviderConfig{...}, // capabilities, schema
)

provisioner := provision.NewProvisioner(steps...)
deprovisioner := provision.NewDeprovisioner(deprovisionStep)

provider.Run(ctx, provisioner, deprovisioner)
```

The interesting work is in the steps. A "step" is something the provider does in response to a `MachineRequest`. My TrueNAS provider has four:

1. **`createSchematic`** — Tell Sidero's Image Factory what features this node needs (e.g., qemu-guest-agent extension). Get back a schematic ID.
2. **`uploadISO`** — Download the corresponding Talos ISO from the factory, upload it to TrueNAS where it can be attached to a VM.
3. **`createVM`** — Create a ZFS zvol for the disk, define a VM in TrueNAS, attach the ISO and the disk and a NIC, start it.
4. **`healthCheck`** — Verify the VM is still alive periodically, and if it's been deleted out from under us, reset our state so Omni can re-provision.

Plus a deprovision step that does the reverse: stop the VM, delete the VM definition, delete the zvol, garbage-collect the ISO if it's no longer in use.

The whole thing is roughly 5,000 lines of Go. It's not magic. It's just a small server talking to two APIs — Omni's and TrueNAS's — and translating between them.

---

## The unreasonable freedom of an extension point

Here's the part that changed how I think about systems design.

When I started building the provider, I assumed I'd have to constantly fight Omni's expectations. The reality: Omni's contract is so narrow that as long as I produce running Talos nodes when asked, I can do whatever I want underneath.

**Examples of "whatever I want" that the provider does**:

- **ZFS-backed VM disks**, not a generic file. zvols are first-class block devices. Talos doesn't know it's running on ZFS underneath, and it doesn't need to.
- **Optional second data disk per worker** — for Longhorn. Talos auto-formats and mounts it via UserVolumeConfig because the provider emits the right config patch.
- **Singleton lease** — Omni's SDK doesn't provide leader election, so the provider implements its own using Omni's resource store as the lease backend. Omni doesn't know or care.
- **Deterministic MAC addresses** derived from the machine request ID, so DHCP reservations survive node reprovisioning.
- **Cassette-based integration tests** that record real TrueNAS API responses and replay them, so CI doesn't need TrueNAS hardware.

None of those are in the Omni contract. None of them required Omni's permission. The contract says "produce a Talos node." Everything else is mine.

This is the design lesson: **narrow, well-defined extension points are a gift**. They let an ecosystem of niche tools exist that the core team would never have built. Omni doesn't have a TrueNAS provider because Sidero shouldn't have to know about TrueNAS. I built it because I should have to know about TrueNAS, and the provider contract let me without modifying Omni.

---

## What can you build with this?

Beyond my TrueNAS provider, the design space is open. Existing community providers and ideas I've seen:

- **Hyper-V** for Windows-based homelabs
- **VMware Workstation** for laptop-based dev clusters
- **Raspberry Pi cluster** providers for cheap edge-style setups
- **Auto-scaling cloud providers** that hit specific cloud APIs not yet covered
- **Mixed-fleet providers** that present multiple hardware pools as one
- **Specialized hardware** — GPU servers, FPGA boxes, ARM-based edge boxes

If you've got hardware that can run a Talos VM and an API to control it, you can write a provider. The cost is a few thousand lines of Go and a working knowledge of the underlying platform.

The Sidero team has explicitly designed for this — the SDK is in the open, the provider contract is documented, and they're responsive when you have questions. Building an Omni provider as a community member is one of the friction-free experiences in the Kubernetes ecosystem right now.

---

## Why I built mine

The narrow answer: I run TrueNAS, I wanted Kubernetes, and the gap was obvious.

The broader answer: I wanted to know what it felt like to fill an obvious ecosystem gap. The whole project — researching the Omni SDK, reverse-engineering TrueNAS's JSON-RPC API, designing the singleton lease, the cassette tests, the SAST sweep — has been the most consistently engaging engineering work I've done in years. Niche tools, well-scoped, with a clear extension point to extend from, are an underrated career resource.

If you're an infrastructure engineer with an itch and access to hardware nobody else has — there's probably an Omni provider you could build. The provider I wrote for TrueNAS is MIT licensed, ~5,000 lines, and a reasonable starting reference. Read the source, lift the patterns, write your own.

---

## What this isn't

Some honest scope-setting for any reader considering Omni:

- **Omni is not Kubernetes itself.** It's a control plane around Kubernetes. Kubernetes is still Kubernetes — you'll write the same manifests, run the same tools.
- **Omni is not free for all use cases.** Personal/homelab use has a free tier; production use scales by node count. Self-hosting Omni is also an option (it's a Go binary).
- **Omni is not the only managed Kubernetes for on-prem.** Rancher, OpenShift, k0sproject, and others exist. They're shaped differently. Pick the one that fits your operational model.
- **An infrastructure provider is not a CSI driver, a CNI, or a cluster API integration.** Those are different extension points in the broader Kubernetes ecosystem. Don't confuse them.

---

## Try it

If this got you curious about Omni + Talos + a community provider:

- **My provider (TrueNAS)**: [github.com/bearbinary/omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas)
- **The install guide**: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>)
- **Sidero Omni**: [omni.siderolabs.com](https://omni.siderolabs.com/)
- **Talos Linux**: [talos.dev](https://www.talos.dev/)
- **Omni SDK** (if you want to build a provider): [github.com/siderolabs/omni/client](https://github.com/siderolabs/omni/tree/main/client)

If you're thinking about building a provider for hardware that doesn't have one yet — open an issue on my repo or find me on [LinkedIn](#). Happy to share what I learned the hard way.

---

**About the author**: Zac Clifton is an infrastructure engineer building tools for self-hosters and small teams. He maintains `omni-infra-provider-truenas` and writes about the unglamorous parts of shipping infrastructure OSS. Subscribe on [YouTube](#) for monthly deep-dives.

---

## Editor notes (delete before publish)

- This is the **broad-reach explainer** for M6. The target reader has heard of Kubernetes but maybe not Omni. Don't assume.
- Title and TL;DR are doing all the SEO work here. Verify before publishing that "what is an omni provider" / "sidero omni explained" / "omni infrastructure provider" return weak/no canonical answers in Google — that's the gap this piece fills.
- This piece is intentionally framed to **not require interest in TrueNAS**. The reader could be evaluating Omni for cloud-only use and still get value.
- Cross-post to r/kubernetes, lemmy `!kubernetes@lemmy.world`, the Sidero Slack (with mods' approval), and HN. This is the broadest-audience piece in the whole 6-month run.
