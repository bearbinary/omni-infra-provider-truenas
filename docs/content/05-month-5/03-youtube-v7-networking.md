# YouTube Script: V7 — Networking for TrueNAS-hosted Kubernetes (M5)

The networking deep-dive. After storage, networking is the second-most-asked question — bridges, DHCP reservations, MetalLB, jumbo frames, multi-NIC for storage. Lots of pieces, all of them worth showing on real hardware.

**Channel conventions**: same as V1–V6 (cold open, lower-third on first appearance, 20s end screen, pinned comment).

**Recording window**: any time after M4 publishes. Best paired with the M5 case study release week — both pieces serve "operating the cluster" intent.

---

## V7 — "Networking for TrueNAS-hosted Kubernetes — bridges, MetalLB, DHCP, the lot"

**Working title**: `Networking for Kubernetes on TrueNAS: bridges, DHCP reservations, MetalLB, jumbo frames`
**Length target**: 12:00–15:00.
**Format**: face-cam open/close, full screencast through TrueNAS UI + cluster + router UI, on-screen network diagrams as overlays. Real network on screen.
**Thumbnail text**: "K8s NETWORKING on TRUENAS" + face + small bridge/router icons.

### Title options

1. `Networking for Kubernetes on TrueNAS: bridges, DHCP, MetalLB, jumbo frames` ← SEO favorite
2. `How I wired up Kubernetes networking on my TrueNAS homelab — every piece`
3. `MetalLB, VIP, DHCP reservations — the full TrueNAS Kubernetes networking setup`

### Description

```
The full networking setup for a Kubernetes cluster running on TrueNAS — TrueNAS bridges, DHCP reservation planning, MetalLB IP allocation, Talos VIP, jumbo frames for storage. Real hardware, real router, real cluster.

Chapters:
00:00 — Why networking is the second question I get most
00:45 — The network shape: one bridge, one VLAN, three IP ranges
02:30 — TrueNAS bridge setup (and the gotcha that drops your NAS off the network)
04:30 — IP planning: gateway, infrastructure, DHCP pool, MetalLB, VIP
06:30 — DHCP reservations using deterministic MACs
08:30 — MetalLB install and IP pool
10:30 — Talos VIP — why it matters and how to wire it up
12:00 — Multi-NIC for storage (jumbo frames, optional)
13:30 — Router-specific gotchas (UniFi / pfSense / OPNsense)

Links:
— Networking guide (the written reference): https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/docs/networking.md
— Multi-homing guide: https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/docs/multihoming.md
— Provider repo: https://github.com/bearbinary/omni-infra-provider-truenas
— Hero install guide: https://dev.to/cliftonz/<hero-post-slug>

#Kubernetes #TrueNAS #MetalLB #Talos #Homelab #SelfHosted
```

### Script

**[0:00–0:30 — COLD OPEN, face-cam]**

> Networking is the second question I get most after storage. How do you wire up MetalLB? Where do DHCP reservations go? What's a Talos VIP and do you need one? This video covers every piece, on my real homelab network, with the gotchas I hit when I was setting it up the first time.

**[0:30–0:45 — Title card]**

> [Title card: "Networking for TrueNAS-hosted Kubernetes"]
> I'm Zac. I maintain the provider that runs this whole stack. This video assumes you have a cluster running — if you don't yet, the install walkthrough is V2, linked below.

**[0:45–2:30 — Network shape, on-screen diagram]**

> [SCREEN: Mermaid network diagram from the docs]
> The shape of a sensible homelab Kubernetes network on TrueNAS.
> One TrueNAS bridge. All VMs sit behind it. The bridge shares the NAS's primary NIC — so cluster nodes and TrueNAS file shares share the same physical wire, but that's fine because they all live on the same Layer 2 broadcast domain anyway.
> [Highlight three IP ranges on diagram]
> Three IP ranges inside one /24 subnet:
> The DHCP pool — your router hands these out automatically. Cluster nodes get IPs from here.
> The MetalLB range — IPs reserved for Kubernetes LoadBalancer services. MetalLB announces them via ARP.
> A single VIP — the Kubernetes API endpoint, floats between control plane nodes via Talos's built-in VIP support.
> Don't let these ranges overlap. That's the entire planning problem in one sentence.

**[2:30–4:30 — TrueNAS bridge setup, screencast]**

> [TrueNAS UI: Network > Interfaces]
> Bridge setup in TrueNAS. Network, Interfaces, Add. Type: Bridge.
> [Click Add Bridge]
> Bridge members — pick your primary NIC. Mine's enp5s0.
> DHCP — on. Name — br0 is fine.
> [Click Save]
> [Face-cam reaction]
> Heads up. The moment you apply this, your NAS will briefly drop off the network. Its IP is moving from the physical NIC to the bridge. This is normal but it does mean you need to reconnect.
> [TrueNAS UI: bridge with IP visible]
> Reconnected. Bridge has an IP. Note the name. We use this for DEFAULT_NETWORK_INTERFACE in the provider config.

**[4:30–6:30 — IP planning, on-screen table]**

> [SCREEN: IP planning table from docs/networking.md]
> IP planning. Here's the layout I recommend for a /24.
> Gateway is .1 — that's your router.
> .2 through .49 — infrastructure. NAS, switches, access points, anything with a static IP.
> .50 through .200 — DHCP pool. This is what your router hands out. Cluster nodes go here.
> .201 through .250 — MetalLB. Reserved for LoadBalancer services.
> .254 — Talos VIP. The Kubernetes API endpoint.
> [Face-cam, direct]
> The mistake everyone makes: their router's DHCP range extends to .254. So when MetalLB tries to grab .201, the router has already handed it to a random device. Conflict. Mysterious.
> [SCREEN: router DHCP settings]
> Go into your router. Set the DHCP end to .200. That's it. Now there's room above for MetalLB and the VIP.

**[6:30–8:30 — DHCP reservations, screencast]**

> [Face-cam]
> DHCP reservations. The provider gives every VM a deterministic MAC address — same machine request always gets the same MAC. That means you can reserve a specific IP for a specific MAC on your router, and the VM gets the same IP across reprovisioning.
> [Provider logs: "attached primary NIC" line]
> Here's where the provider logs the MAC. Grab it.
> [Router DHCP reservations UI]
> Go into your router's DHCP reservations. Add a reservation: MAC, IP. Done.
> [Face-cam]
> When would you actually do this? Two cases. One — workers that need stable IPs for monitoring or for backups that reference IPs. Two — control plane nodes if you're not using VIP, so you can put their IPs in your kubeconfig and they don't shift.
> If you're using VIP — and you probably are — control plane reservations are less critical. The VIP is what kubectl talks to. Individual CP IPs can shift.

**[8:30–10:30 — MetalLB install, screencast]**

> [Terminal: helm repo add metallb]
> MetalLB install. Helm.
> [Terminal: helm install metallb]
> [Terminal: kubectl get pods -n metallb-system]
> Pods up. Now we tell MetalLB what IPs it owns.
> [SCREEN: IPAddressPool YAML]
> Two manifests. An IPAddressPool — the range we reserved earlier, .201 to .250.
> [SCREEN: L2Advertisement YAML]
> An L2Advertisement — tells MetalLB to use Layer 2 ARP announcement. This is the right choice for a flat home network. BGP is for fancier setups you don't need yet.
> [kubectl apply]
> [Terminal: kubectl create deployment + expose with type=LoadBalancer]
> Test it. Deploy nginx, expose as LoadBalancer.
> [Terminal: kubectl get service, EXTERNAL-IP shows .201]
> External IP from the pool. Curl it from another machine.
> [Terminal: curl http://192.168.100.201, nginx welcome page]
> Works.

**[10:30–12:00 — Talos VIP, screencast]**

> [Face-cam]
> Talos VIP. Built-in feature. Talos's kubelet on each CP node fights for a virtual IP — whoever wins answers Kubernetes API traffic. If the winner dies, another CP takes over the VIP within seconds.
> [SCREEN: VIP config in Omni cluster patch]
> Configure it in Omni's cluster patches. One line under machine.network.interfaces[0].vip.ip.
> [SCREEN: cluster patch with VIP IP]
> [Apply, wait for kubelet to pick it up]
> [Terminal: kubectl --server=https://192.168.100.254 get nodes]
> Now your kubeconfig points at the VIP, not at any individual CP IP. CP node dies — your cluster keeps responding because the VIP shifts.
> [Face-cam, direct]
> This is the cheapest HA you can buy. If you've got 3 control planes, you want VIP. Use it.

**[12:00–13:30 — Multi-NIC for storage, screencast with caveats]**

> [Face-cam]
> Optional: a second NIC dedicated to storage traffic. Useful if you're running heavy I/O workloads — databases, video transcoding — and you want to keep that traffic off your main LAN.
> [TrueNAS UI: second bridge]
> Create a second bridge in TrueNAS. Different physical NIC. Different subnet.
> [SCREEN: MachineClass additional_nics config]
> In your worker MachineClass, add an additional_nics entry pointing at the new bridge.
> [Face-cam reaction]
> Heads up. If you're going to use jumbo frames — MTU 9000 — you need every device on the path to agree. The bridge in TrueNAS at 9000. The physical switch ports at 9000. The VM NICs at 9000. Mismatch anywhere = mysterious dropped packets.
> The provider handles the VM side automatically when you set mtu 9000 in additional_nics. Bridge and switch are on you.
> [Face-cam]
> Most homelab clusters don't need this. Add it when you have a measurable problem, not before.

**[13:30–14:30 — Router-specific gotchas, face-cam with bullet overlays]**

> Three router-specific gotchas before I let you go.
> [Overlay: "UniFi"]
> UniFi — set the DHCP range explicitly per network. The default network's DHCP range goes to .254 out of the box, which conflicts with MetalLB. Edit the network. Set end to .200.
> [Overlay: "pfSense / OPNsense"]
> pfSense and OPNsense — clean UIs for DHCP ranges. Same fix, just less hunting for the setting. Both also support DHCP reservations cleanly.
> [Overlay: "Consumer routers"]
> Consumer routers — some of them don't let you set the DHCP end, only the start. If yours doesn't, you can usually shrink the range from the start side instead (e.g., DHCP runs .150 to .254, MetalLB grabs .50 to .149). The shape is flipped but the rule is the same: no overlap.

**[14:30–15:00 — CTA, face-cam]**

> Written networking guide on the repo has every config snippet plus a multi-homing deep-dive that goes further than this video. Both linked in description.
> Next video on this channel — the 6-month retrospective. What worked, what didn't, the actual numbers. Subscribe so it shows up.
> Tell me how your network is wired up in the comments. I love seeing the diversity of homelab setups.

**[14:55–15:15 — End screen: M6 retro video + repo card]**

### Production notes
- **Real router on screen, real network**. Same credibility constraint as V6. Don't fake.
- **Network diagram overlays are the most-screenshotted part**. Pause longer on them.
- The UniFi gotcha at 13:30 is the single most common networking mistake. Make sure it lands clearly.
- If you don't have multi-NIC storage configured, omit the 12:00–13:30 segment and the video drops to ~13:30 total. Don't fake a feature you don't run.

---

## Cross-promotion plumbing

- **Hero post** (`01-month-1/01-hero-post.md`): in the "What's next" section, replace `[Networking guide](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/docs/networking.md)` with V7 video URL too.
- **Upgrade post** (`04-month-4/01-upgrade-post.md`): add V7 link to the "Try it" section.
- **Storage video V5 description**: add "Networking deep-dive: <V7 URL>".
- **Reddit / Lemmy**: when networking questions come up on cross-posts, V7 link.
- **LinkedIn drumbeat M5**: weekly post about the "DHCP range conflict" trap, the VIP as cheapest HA, V7 clip, jumbo-frames-or-don't reflection.

---

## Open placeholders

- Live URLs for hero, upgrade, storage video.
- Confirm your router is on a recent firmware before recording (UniFi UI changes have been frequent).
