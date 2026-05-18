# YouTube Scripts: M1 Channel Launch (V1 + V2)

Two videos. V1 is the pinned channel intro. V2 is the searchable companion to the dev.to hero post. Record both in one session if possible — same lighting, same audio, same shirt. Ship V1 first; V2 within 7 days.

**Recording setup (one-time)**:
- OBS, USB mic (Samson Q2U-tier minimum), one-light face-cam.
- 1080p60 face-cam, 1080p screencast (don't go 4K — render time isn't worth it monthly).
- Loudness target: -14 LUFS (YouTube normalizes there anyway).
- Save raw .mkv + Audacity-cleaned audio track.

**Channel conventions (lock now)**:
- Cold open before any intro card. No 8-second logo splash.
- Lower-third name plate on first appearance: "Zac Clifton — Infra Engineer."
- End screen: 20s, repo link card + "next video" card.
- Pinned comment template: "Repo: <link> | Hero post: <link> | Issues > stars."

---

## V1 — "Who I am, what I'm building, why TrueNAS"

**Working title**: `I'm building tools for the self-hosted Kubernetes crowd. Here's what this channel is.`
**Length target**: 3:30–4:00.
**Format**: face-cam only. No screencast.
**Pin status**: pin to channel.
**Thumbnail text**: "BUILDING FOR THE SELF-HOSTED" + face.

### Title options (pick one)

1. `I'm building tools for self-hosted Kubernetes. Here's what this channel is.`
2. `Why I'm making a YouTube channel about Talos, Omni, and TrueNAS.`
3. `TrueNAS killed Kubernetes. I brought it back — and I'm making videos about it.`

### Description (publish-ready)

```
I'm Zac Clifton — infrastructure engineer, maintainer of omni-infra-provider-truenas, daily-driver of a Talos + Omni cluster on a TrueNAS SCALE box.

This channel covers the parts of self-hosted Kubernetes that nobody else writes about: Talos Linux, Sidero Omni, TrueNAS SCALE, ZFS-backed storage, sizing for real workloads, and the failure modes that bite you in week three.

If you've got a NAS and you want a real cluster on it, you're in the right place.

— Subscribe for monthly deep-dives.
— Repo: https://github.com/bearbinary/omni-infra-provider-truenas
— Hero guide: https://dev.to/cliftonz/<hero-post-slug>

Issues, questions, broken setups: file an issue on the repo. Contribution model is issues-only — but I respond.

#Kubernetes #TrueNAS #Homelab #Talos #SelfHosted
```

### Script

**[0:00–0:15 — COLD OPEN, face-cam, direct address]**

> When TrueNAS shipped version 25.04, it killed built-in Kubernetes. A lot of homelabs went dark. Mine was one of them.
> So I built the way back. And I'm going to show you how.

**[0:15–0:20 — Title card flash. "Zac Clifton — Infra Engineer."]**

**[0:20–0:45 — Identity, face-cam]**

> I'm Zac Clifton. I'm an infrastructure engineer. I've spent a long time around Kubernetes, around storage systems, around the messy real-world parts of running this stuff at home and at work.
> The tool I build and maintain is called `omni-infra-provider-truenas`. It's MIT-licensed, and it turns a TrueNAS SCALE box into a fleet of Talos Linux VMs that Sidero Omni can manage as a real Kubernetes cluster.

**[0:45–1:30 — The gap, face-cam, leaning in]**

> Here's why this channel exists.
> If you Google "Kubernetes on TrueNAS," you get one of two answers. The first is "use the built-in apps catalog" — which doesn't exist anymore in the form most people remember. The second is "buy a Proxmox box." Both of those answers are fine for some people, and neither one is the answer for most.
> The actual third path — TrueNAS hosting Talos VMs directly, managed by Omni, ZFS underneath, one machine — is barely documented anywhere. So I'm documenting it. On the repo, on dev.to, and here.

**[1:30–2:30 — The product in 60 seconds, face-cam with optional cutaway to repo README]**

> The provider is small. It listens for Omni's machine requests and translates them into TrueNAS API calls — creates a zvol for the disk, creates a VM, attaches a network bridge, boots Talos, and Talos calls home to Omni over a WireGuard tunnel.
> Two to five minutes per node from "I want a cluster" to "the cluster exists."
> Underneath it's Go, JSON-RPC 2.0 over WebSocket, the standard Omni SDK plus a singleton-lease pattern I had to write myself because the SDK doesn't ship one. CI runs against 42 cassette-recorded TrueNAS responses, so I can test without a NAS plugged in.
> If that's the kind of thing you want to hear more about, you're in the right place.

**[2:30–3:30 — Channel roadmap, face-cam, energetic]**

> What's coming on this channel:
> One — the full canonical walkthrough. Cold NAS to running cluster. That video drops within the week.
> Two — Talos versus k3s on TrueNAS. The honest comparison. No "this one is better" cop-out. Tradeoffs named.
> Three — storage. Longhorn, democratic-csi, NFS. What I actually run and why.
> Four — sizing. The control-plane RAM math when you add Crossplane or Argo. The HDD-pool etcd trap. The stuff that bites you.
> One video a month. Practical, opinionated, no filler.

**[3:30–4:00 — CTA, face-cam, direct]**

> If you've got a TrueNAS box sitting in a closet and you want a real Kubernetes cluster on it, subscribe. Repo's in the description. The canonical guide is there too.
> Issues over stars. Tell me what breaks.
> I'll see you in the next one.

**[3:55–4:15 — End screen: 20s with subscribe button + V2 thumbnail card.]**

### Production notes
- Energy level: calm-confident. Not a hypebeast intro. You're an engineer, not a YouTuber.
- Eye contact through the lens, not the preview screen.
- One b-roll cutaway only if natural — repo README scroll during 1:30–2:30. Don't force it.
- If you flub a line, restart the sentence — don't try to splice mid-sentence.

---

## V2 — "Self-hosted Kubernetes on a TrueNAS box, start to finish"

**Working title**: `Self-hosted Kubernetes on TrueNAS SCALE — start to finish (Talos + Omni)`
**Length target**: 12:00–15:00.
**Format**: face-cam intro/outro, full screencast in the middle, occasional face-cam reaction inserts.
**Thumbnail text**: "REAL K8s ON A NAS" + face + small TrueNAS + Omni logos.

### Title options (pick one — first one is SEO-strongest)

1. `Self-hosted Kubernetes on TrueNAS SCALE — start to finish (Talos + Omni)`
2. `Run a real Kubernetes cluster on your TrueNAS in one evening`
3. `TrueNAS + Talos + Omni: the full setup walkthrough`

### Description (publish-ready)

```
The complete walkthrough: TrueNAS SCALE 25.04+ box to a real, multi-node Kubernetes cluster managed by Sidero Omni — using Talos Linux VMs and the omni-infra-provider-truenas project.

Chapters:
00:00 — What we're building
01:00 — The stack: TrueNAS + Omni + Talos
02:00 — Hardware sizing reality check
03:00 — Step 1: Omni account + omnictl
04:00 — Step 2: TrueNAS prep (pool, bridge, API key)
06:00 — Step 3: install the provider
07:30 — Step 4: define MachineClasses
09:00 — Step 5: create the cluster in Omni
10:30 — Step 6: kubeconfig + deploy nginx
12:00 — Three gotchas that bite real users
13:30 — Storage: which option I pick and why
14:30 — Where to go next

Links:
— Repo: https://github.com/bearbinary/omni-infra-provider-truenas
— Hero guide (the canonical written version): https://dev.to/cliftonz/<hero-post-slug>
— Origin story: https://dev.to/cliftonz/truenas-killed-kubernetes-so-i-brought-it-back-4n7h

Issues > stars. File an issue on the repo if something breaks.

#Kubernetes #TrueNAS #Talos #Homelab #SelfHosted #SideroOmni
```

### Script

**[0:00–0:30 — COLD OPEN, face-cam]**

> If you've got a TrueNAS SCALE box and you want a real, multi-node Kubernetes cluster on it — no Proxmox, no second machine, no abandoning ZFS — this is the video. We're going from cold NAS to a working cluster running an actual workload. I'll name every gotcha I've hit doing this for real. Let's go.

**[0:30–1:00 — Title card + intro tag]**

> [Title card: "Self-hosted Kubernetes on TrueNAS SCALE"]
> I'm Zac. I maintain the open-source provider that makes this whole thing work. Quick channel intro is pinned — go watch that after if you want the why. This video is the how.

**[1:00–2:00 — The stack, face-cam with on-screen text labels]**

> Three pieces.
> [SCREEN: TrueNAS logo] TrueNAS SCALE 25.04 or newer. Hosts the VMs. Provides ZFS storage.
> [SCREEN: Omni logo] Sidero Omni. The Kubernetes management platform. Free tier covers homelab. Self-host if you want.
> [SCREEN: Talos logo] Talos Linux. Runs inside each VM. Immutable, no SSH, no shell. You manage it through APIs.
> And the glue is the provider I maintain. It listens to Omni and creates Talos VMs on TrueNAS automatically.

**[2:00–3:00 — Sizing, face-cam with on-screen table]**

> [SCREEN: sizing table from hero post]
> Sizing reality. To try this out you need 4 cores and 16 gigs of RAM free, minimum. Comfortable home cluster is 8 cores, 32 gigs. Production-ish HA is 16 cores and 64 gigs.
> [Face-cam, leaning in]
> Two traps to know before you start. One: HDD pools and etcd hate each other. If you're on spinning rust with no SLOG, you'll get random NodeNotReady flaps and you'll think the cluster is broken. It's not. It's etcd timing out on fsync. Either add an NVMe SLOG or patch the heartbeat timeouts.
> Two: the root disk has a 20-gig floor. Don't try to go smaller. Talos pulls every control-plane image at bootstrap and a 10-gig disk fills up mid-install. Trust me.

**[3:00–4:00 — Step 1: Omni + omnictl, SCREENCAST]**

> [SCREENCAST: omni.siderolabs.com signup page]
> Sign up at omni.siderolabs.com. Free tier is fine.
> [SCREENCAST: terminal]
> Install omnictl. On Mac it's a brew tap, on Linux it's a curl.
> [Terminal showing: brew install siderolabs/tap/omnictl]
> Authenticate against your Omni URL.
> [Terminal: omnictl config url https://...]
> Then create the service account the provider will use.
> [Terminal: omnictl serviceaccount create --role=InfraProvider infra-provider:truenas]
> [Highlight the printed key]
> This key prints once. Copy it. Save it. We'll paste it in step 3.

**[4:00–6:00 — Step 2: TrueNAS prep, SCREENCAST through the TrueNAS UI]**

> [TrueNAS UI: Storage tab]
> Note the name of your ZFS pool. Mine is `tank`. Yours might be `data` or `default`. Case-sensitive.
> [TrueNAS UI: Network > Interfaces]
> Now we make a bridge. Add, Type: Bridge, members: your primary NIC, DHCP on. Save.
> [Face-cam reaction insert]
> Heads up — this will briefly drop your NAS off the network. That's normal. Your NAS's IP moves onto the bridge.
> [TrueNAS UI: bridge created, IP visible]
> Verify the bridge has an IP. Note the name — mine is `br0`.
> [TrueNAS UI: Credentials > Local Users > Add]
> Now create a dedicated user for the provider's API key. Username `omni-provider`, password disabled, shell nologin.
> [TrueNAS UI: Credentials > Groups > builtin_administrators]
> Add that user to `builtin_administrators`. This is required — there's a quirk in TrueNAS where the file-upload endpoint needs SYS_ADMIN, and you only get SYS_ADMIN by being in builtin_administrators. Custom roles don't substitute.
> [TrueNAS UI: API Keys > Add]
> Create an API key for that user. Copy it once — same drill as the Omni key.

**[6:00–7:30 — Step 3: install the provider, SCREENCAST]**

> [TrueNAS UI: Apps > Discover > Custom App]
> Apps tab, Custom App. Paste the compose YAML.
> [SCREEN: paste the compose block from hero post]
> Replace the placeholders: your Omni URL, the Omni service account key, the TrueNAS API key, your pool name, your bridge name.
> [Click Install/Deploy]
> [Face-cam reaction]
> Now we wait about 30 seconds for the container to come up.
> [TrueNAS UI: App logs]
> What you're looking for is two log lines: "startup checks passed" and "starting TrueNAS infra provider."
> [Highlight both lines]
> Both there? Green light. If you see pool-not-found or interface-not-found, your names are wrong — case sensitive — go back and check.

**[7:30–9:00 — Step 4: MachineClasses, SCREENCAST]**

> [Terminal]
> Two MachineClasses. One small one for control planes, one bigger one for workers.
> [Terminal: paste truenas-small heredoc, apply]
> Small is 2 CPU, 2 gigs RAM, 20-gig disk.
> [Face-cam reaction]
> Quick note. If you're going to install Crossplane, or Argo with a lot of ApplicationSets, or Prometheus Operator at full scrape — bump the control plane to 4 CPU and 4 gigs of RAM before you create the cluster. The apiserver swaps under load otherwise and the cluster looks intermittently broken. Don't say I didn't warn you.
> [Terminal: paste truenas-worker heredoc, apply]
> Worker is 2 CPU, 4 gigs RAM, 40-gig disk, plus a 100-gig data disk for Longhorn.
> [Highlight `storage_disk_size: 100`]
> That `storage_disk_size` line attaches a separate data disk that Longhorn will pick up. If you skip it, you have nowhere to put persistent volumes.

**[9:00–10:30 — Step 5: create the cluster in Omni, SCREENCAST]**

> [Omni UI: Clusters > Create Cluster]
> In the Omni web UI: Clusters, Create Cluster. Name it `homelab`.
> [Form filling]
> Control plane: Auto Provision, provider `truenas`, MachineClass `truenas-small`, replicas 1.
> Workers: Auto Provision, provider `truenas`, MachineClass `truenas-worker`, replicas 1 or more.
> Click Create.
> [Split screen: TrueNAS Virtualization tab + Omni Machines tab]
> Watch both windows. On TrueNAS you'll see VMs named `omni-` appear. On Omni you'll see machines register as they boot.
> [Time-lapse, ~30s of footage]
> Two to five minutes per node. Get a coffee.
> [Omni UI: cluster status = Running]
> Cluster Running. Let's go.

**[10:30–12:00 — Step 6: kubeconfig + deploy nginx, SCREENCAST]**

> [Terminal]
> Grab the kubeconfig.
> [Terminal: omnictl kubeconfig -c homelab > ~/.kube/config]
> Check nodes.
> [Terminal: kubectl get nodes, both Ready]
> Both Ready. Now let's deploy something to prove it works.
> [Terminal commands]
> kubectl create deployment hello --image=nginx
> kubectl expose deployment hello --port=80 --type=NodePort
> kubectl get service hello
> kubectl get nodes -o wide
> [Browser: nginx welcome page at http://<node-ip>:<nodeport>]
> nginx welcome page. You're running Kubernetes on your NAS.
> [Face-cam reaction, brief smile]
> That's it. That's the whole thing.

**[12:00–13:30 — Three gotchas, face-cam with bullet overlays]**

> Three things that bite real users. Watch for them.
> [Overlay: "1. Don't use root's API key"] One. Don't use the root user's API key. You lose audit separation and you can't revoke without consequences. Dedicated user, like we did in step 2.
> [Overlay: "2. `pool` is top-level only"] Two. In your MachineClass, the `pool` field is the top-level pool name only. Don't write `tank/k8s`. Use `dataset_prefix: k8s` for nesting.
> [Overlay: "3. HDD pool? Patch etcd timeouts"] Three. If your pool is HDDs and you don't have an NVMe SLOG, apply the etcd timeout patch from the sizing doc. Otherwise the cluster will look intermittently broken and you'll waste a weekend debugging it.

**[13:30–14:30 — Storage opinion, face-cam with on-screen ranked list]**

> Quick storage take, because I get this question every time.
> [Overlay: ranked list]
> Longhorn is my default. Runs in-cluster, block storage, CNCF project. Needs the data disk on each worker — which we set up.
> Democratic-csi if you want every PVC backed by a ZFS dataset with snapshots. Battle-tested. More moving parts.
> nfs-subdir-external-provisioner — please don't. Permissions are loose, failure modes are weird, and there's no real ownership story.
> Full reasoning is in the storage doc on the repo.

**[14:30–15:00 — CTA, face-cam, direct]**

> If this worked for you, the next video is the honest comparison between Talos plus Omni and the built-in TrueNAS apps k3s setup. Subscribe so it shows up.
> Repo is in the description. Hero post is in the description. Issues over stars.
> Tell me what breaks.
> I'll see you in the next one.

**[14:55–15:15 — End screen: 20s, V1 channel intro card + repo link card.]**

### Production notes
- The screencast portion is the show. Slow down on cursor movement. Pause for half a second after each click.
- Pre-stage everything. Don't make people watch you type a 200-character Omni URL — paste it.
- Pre-record a clean run, then re-record the voiceover. Splice. Audio is what people stay for.
- Don't apologize for cuts ("sorry, let me try that again"). Just cut them out.
- The 30-second time-lapse at 9:30 is critical pacing. Don't skip it — viewers need to feel "this takes a few minutes" without watching three full minutes of progress bars.
- Chapter markers in the description = YouTube auto-creates them on the timeline. Worth the 30 seconds.

---

## Cross-promotion plumbing

After both videos are up:

- **Dev.to hero post**: replace the placeholder `[Self-hosted Kubernetes on a TrueNAS box, start to finish](#)` link with the live V2 URL.
- **LinkedIn cross-post**: edit to mention "Companion video on YouTube" with the V2 URL in the first comment alongside the dev.to link.
- **X thread T10**: edit to include the V2 link.
- **Reddit comments**: when someone asks "is there a video version," drop the V2 link. Don't include it in the original post body — Reddit demotes posts with multiple outbound links.
- **YouTube description on V2**: ensure the chapter timestamps in the description match what's actually on the timeline. Off-by-a-few-seconds breaks YouTube's chapter feature.

---

## Open placeholders to fill before record

- Real domain for `bearbinary.dev` references (or strip them).
- Real LinkedIn URL + YouTube URL for description templates.
- Final dev.to hero slug (replace `<hero-post-slug>` throughout).
- Decide cold-open shirt / framing / location once — keep consistent for the M1–M6 video set so they look like one channel, not six.
