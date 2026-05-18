# LinkedIn Weekly Drumbeat — M1 + M2 Templates

LinkedIn rule of thumb for personal-brand growth: 1 post/week, native long-form preferred, hook in first 2 lines (the truncated preview), no external link in the body (kills reach), drop the link in first comment. 3–5 hashtags max.

**Voice**: technical, direct, no hype. Engineering-leader audience. You're not selling — you're sharing what you've learned. They'll click through if you've earned it.

**Cadence**: post Tuesday or Wednesday between 9–11am ET (highest engagement window for tech audience). Don't post weekends.

**Engagement**: reply to every comment for the first 4 hours after posting. LinkedIn's algorithm uses early-engagement signal heavily.

---

## Month 1 — Weeks 1–4

### Week 1 (Mon after hero ships) — Channel launch announcement

**Hook**:
> I'm launching a YouTube channel about a corner of infrastructure nobody else is covering.

**Body**:

> When TrueNAS dropped built-in Kubernetes in 25.04, a lot of homelab clusters quietly died.
>
> Mine was one of them — so I built the way back. An open-source Omni provider that turns a TrueNAS SCALE box into a fleet of Talos Linux VMs, managed by Sidero Omni. One machine. Real multi-node Kubernetes. No second hypervisor.
>
> The project has been live for several months. What I haven't been doing is talking about it — and that ends today.
>
> I'm spinning up a YouTube channel covering the parts of self-hosted Kubernetes nobody else writes about: Talos Linux, Sidero Omni, TrueNAS SCALE, ZFS-backed storage, sizing for real workloads, and the failure modes that bite you in week three.
>
> First video drops this week — the canonical install walkthrough. Monthly cadence after that.
>
> If you've got a NAS sitting there and you've wondered whether you could run a real cluster on it: yes, and I'm going to show you exactly how.

**First comment**:
> Channel link: <YT URL>
> The hero guide if you'd rather read than watch: <hero post URL>

**Hashtags**: `#Kubernetes #TrueNAS #Homelab #Talos #SelfHosted`

---

### Week 2 — Why I built this (origin story callback)

**Hook**:
> The most-honest engineering work I've done in years was the project nobody asked me to build.

**Body**:

> Background: I'm an infrastructure engineer. I run a TrueNAS SCALE box at home. When TrueNAS 25.04 shipped without Kubernetes, I had two options: migrate to Proxmox, or build something that didn't exist yet.
>
> I picked option two. Eight months later, the result is open-source, MIT licensed, listed on the TrueNAS apps community catalog, and has cassette-based integration tests so the CI doesn't need a NAS plugged in.
>
> Three lessons from shipping it:
>
> One — niche tools have higher emotional ROI than they look like on paper. I built this for me. It turns out a lot of other homelabbers had the same problem. Issues come in, people thank me on Reddit. That's a kind of compounding nobody puts on a roadmap doc.
>
> Two — immutable infrastructure (Talos Linux specifically) changes how you think. You stop maintaining nodes. You replace them. Configuration drift stops being a phrase that exists in your life. I now find myself reaching for this mental model in non-K8s contexts at work.
>
> Three — being the only person doing something is a marketing strategy whether you intended it or not. Sidero's official provider list does not include a TrueNAS one. The gap was the story.
>
> If you're sitting on a side project and wondering whether to ship it publicly: ship it. The audience for niche, opinionated tools is bigger than you think.

**First comment**: link to dev.to origin story.

**Hashtags**: `#OpenSource #Infrastructure #SideProject #Homelab #SelfHosted`

---

### Week 3 — V2 install walkthrough video drop

**Hook**:
> I made a video showing how to go from a TrueNAS box to a real Kubernetes cluster in one evening.

**Body**:

> The full canonical walkthrough is up on YouTube — TrueNAS SCALE 25.04+, Sidero Omni, Talos Linux, and the open-source provider that wires them together.
>
> What's in it:
>
> → How to think about hardware sizing before you start (and the two traps that catch most people — HDD pools + etcd, undersized control planes)
> → Step-by-step from cold NAS to running cluster, on my actual rack
> → Why a dedicated API key user matters and why "use the root key" is the wrong shortcut
> → The MachineClass and Omni cluster create flow
> → Storage opinions, briefly — the long version is a future video
> → Three gotchas that bite real users
>
> 12 minutes. No filler. The companion written guide is linked in the description if you want to follow along step-by-step.
>
> If you've been "going to set this up someday," today is a good day.

**First comment**: YT URL + dev.to hero post URL.

**Hashtags**: `#Kubernetes #TrueNAS #Homelab #Talos #SelfHosted`

---

### Week 4 — Reflection: a niche tool's marketing problem

**Hook**:
> Niche infra tools have a marketing problem nobody talks about: the audience is real, but they're not on the channels marketers default to.

**Body**:

> When you build something niche — a TrueNAS Kubernetes provider, an obscure CLI, a specialized library — the standard marketing playbook breaks.
>
> The ICP isn't on TikTok. They're not buying Facebook ads. They don't read Forbes. They live in:
>
> → Specific subreddits (r/selfhosted, r/homelab, r/kubernetes, r/truenas for me)
> → Project-specific Discord and Slack communities
> → A handful of YouTube creators they trust completely
> → Niche newsletters with 5,000 subscribers and 80% open rates
>
> First month of intentionally marketing my project, the playbook that worked:
>
> 1. Hero canonical doc on a platform with organic discovery (dev.to has decent SEO)
> 2. Companion YouTube walkthrough (creates the face-recognition that subreddit threads then trust)
> 3. Forum announcements in the *small* communities first — r/truenas before r/selfhosted, Talos Slack before Hacker News
> 4. Wait. Niche audiences talk to each other.
>
> The non-obvious part: tuning my hook in the small communities first means it's sharp by the time it hits the bigger ones. Free QA.
>
> If you're shipping niche, this is your playbook. Don't try to be on every channel. Be deeply on the three channels your ICP actually uses.

**First comment**: optional, only if asking for engagement ("What channels work for *your* niche?").

**Hashtags**: `#Marketing #DeveloperRelations #OpenSource #SaaS #BuildInPublic`

---

## Month 2 — Weeks 5–8

### Week 5 — Comparison takeaway: Talos vs k3s

**Hook**:
> I ran k3s and Talos in parallel for 6 months specifically to figure out which one is right for homelab Kubernetes.

**Body**:

> Here's the honest take.
>
> Talos wins on:
> → Day-2 operations. Omni handles upgrades through a UI. No SSH-ing into nodes at 11pm.
> → Immutability. You replace nodes, you don't fix them. Configuration drift stops being real.
> → Multi-node. Scale a MachineSet replica count and watch new nodes appear.
>
> k3s wins on:
> → Debuggability. You can SSH in. You can cat config files. You can strace processes. Every Linux tool you've learned still works.
> → Familiarity. If you've spent your career on Ubuntu, you don't have to learn a new mental model.
> → Single-node simplicity. For one node, the overhead of Talos + Omni isn't worth it.
>
> The case nobody names: the first week of Talos feels alien. No SSH. No shell. talosctl is good but it's a learning curve. You'll get over it. You'll be glad you did. But week one is real.
>
> Most homelabbers should run Talos plus Omni. A meaningful minority should stay on k3s and that's fine. The mistake is picking k3s because it feels familiar without acknowledging that you're trading a week of learning for a year of day-2 ops.
>
> Full comparison in the comments.

**First comment**: link to `02-month-2/01-talos-vs-k3s.md` (published dev.to URL).

**Hashtags**: `#Kubernetes #Talos #k3s #Homelab #SelfHosted`

---

### Week 6 — War story: host-OOM bug

**Hook**:
> I shipped a feature, and three people's TrueNAS hosts immediately OOMed.

**Body**:

> Last month's v0.16.1 release closed a bug that taught me something about validating at the schema boundary.
>
> The provider lets you specify two memory values on a VM: `memory` (the hard limit) and `min_memory` (optional soft floor). The combination is meaningful — `min_memory` must be ≤ `memory`.
>
> The schema didn't enforce that. The runtime accepted the bad pair. TrueNAS accepted the VM creation. The VM tried to start with min_memory > memory, and TrueNAS dutifully tried to lock more memory than the VM was allowed to use. The kernel killed the QEMU process, then started thrashing the host.
>
> Three reports came in within a week. None of them looked the same on the surface — they all manifested as "my TrueNAS rebooted overnight."
>
> The fix had two parts.
>
> One: schema-level validation. If min_memory > memory, reject the spec at apply time. The user sees a clear error message that names both values and the rule. They never get to the runtime.
>
> Two: runtime defense-in-depth. Even if a bad spec somehow lands, the provisioner validates again before calling TrueNAS. A second clear error. Two layers, same rule.
>
> The lesson: schema validation isn't just hygiene, it's user safety. The cost of letting a bad config through wasn't a confusing error — it was someone's NAS rebooting at 2am.
>
> When you design configuration surfaces for infra tools, ask: "if a user gets this wrong, what's the worst that happens?" If the answer is "their host crashes," validate aggressively. Fail loud at the input boundary.

**First comment**: optional — link to the v0.16.1 changelog or commit.

**Hashtags**: `#Infrastructure #SoftwareEngineering #API #Kubernetes #SelfHosted`

---

### Week 7 — V3 video drop (Talos vs k3s)

**Hook**:
> The Talos versus k3s decision, on video — including where I think k3s genuinely wins.

**Body**:

> If you've been weighing your Kubernetes path on TrueNAS and the written comparison from a couple weeks ago left you wanting the screen-by-screen version, V3 is up.
>
> 10 minutes. Cold open through to "what I actually run." On-screen scoring overlays per category so you can pause and screenshot the table if that's how you make decisions.
>
> One section I want to flag: 6:30–7:30 is where k3s genuinely wins on debuggability. I don't dunk on k3s. I run it. I just don't run it anymore. The video says why.

**First comment**: YT URL + written companion URL.

**Hashtags**: `#Kubernetes #Talos #k3s #Homelab #SelfHosted`

---

### Week 8 — TrueNAS vs Proxmox decision matrix

**Hook**:
> Should you run Kubernetes on TrueNAS or Proxmox? It depends on five things — none of them being "which one is better."

**Body**:

> The TrueNAS vs Proxmox decision for homelab Kubernetes is the one I get DM'd about most. Here's the honest matrix.
>
> Pick Proxmox + TrueNAS (two boxes) if:
> → You need GPU passthrough or PCI device shenanigans
> → You want live migration
> → You want independent failure domains (your file shares survive if the cluster host dies)
> → You have abundant hardware and rack space
>
> Pick TrueNAS-only (one box) if:
> → You want ZFS as the single source of truth for files, VM disks, and PVCs
> → Hardware sprawl is a real cost — power, space, rent, partner-tolerance
> → You don't need PCI passthrough
> → You'd rather have one thing to manage and fix
>
> What people get wrong: they treat this as a "which is better" decision. It's not. Both setups are running in real homelabs producing real cluster years. The question is which optimization matches your constraints.
>
> What changed for me personally: I ran the Proxmox + TrueNAS split for a year. I wasn't using Proxmox's hypervisor flexibility — I was just paying for it in hardware and power. So I built the TrueNAS-only path. No regrets several months in. The case where I'd reverse: GPU-heavy workloads. For everything else, one box wins.
>
> Full written comparison in the comments. Video version drops this week.

**First comment**: written post URL + V4 video URL when it's live.

**Hashtags**: `#Kubernetes #TrueNAS #Proxmox #Homelab #SelfHosted`

---

## Month 3 — Weeks 9–12

### Week 9 — Sizing rule of thumb

**Hook**:
> Most homelab Kubernetes control planes are undersized. Here's the trigger I use to know.

**Body**:

> Default install gets you a 2 vCPU / 2 GB control plane. That's the right call for a raw cluster running a handful of stateless apps.
>
> The moment you install any of these, the math changes:
>
> → Argo CD with many ApplicationSets
> → Crossplane
> → Rancher / Fleet
> → Prometheus Operator at full mesh scrape
> → cert-manager + external-dns + 40 CRDs from various operators
>
> What goes wrong: kube-apiserver swaps under load. The cluster doesn't break — it just gets intermittently slow. List operations on common resources go from 200ms to 2 seconds. Reconcile loops take 10× longer. Nothing logs an error, because nothing failed.
>
> The trigger I use:
>
> `kubectl top node -l node-role.kubernetes.io/control-plane=`
>
> If CPU or memory is consistently above 70% under normal load — bump it. Now, not later.
>
> For most homelab clusters with one of those operators installed, that means 4 vCPU / 4 GB minimum. For HA control planes running production-ish workloads, plan for 4–6 vCPU and 8 GB+ per replica.
>
> The math gets worse, not better, over time. Operators install more CRDs. CRDs eat etcd memory. apiserver eats apiserver memory. You're not wrong to start small. You're wrong to assume small lasts forever.

**First comment**: link to M3 sizing post.

**Hashtags**: `#Kubernetes #Homelab #DevOps #SelfHosted #Infrastructure`

---

### Week 10 — SAST retrospective

**Hook**:
> I ran a SAST sweep on a 5,000-line Go project. The finding that surprised me most wasn't the bug — it was where the bug lived.

**Body**:

> Static security analysis flagged six findings on an open-source Go infra tool I maintain. None of them were CVE-class. All of them taught me something. The one I'll be thinking about for years:
>
> A debug log line in the WebSocket transport printed the raw outbound JSON-RPC message. On auth methods, that included the API key. Only in `LOG_LEVEL=debug`. Almost certainly never enabled in production.
>
> The reframe: "almost certainly never" isn't never. The first time someone files a support issue and pastes their debug log into a GitHub issue, the key leaks publicly.
>
> The fix was redaction at the marshaling layer, not at the log call site. Log call sites are too easy to forget. Marshaling-layer redaction means *every* debug log path is automatically safe.
>
> The principle that generalizes: "but only in debug mode" is not a security argument. If the data is sensitive, it's sensitive in every log level.
>
> Three other findings followed the same shape — surfaces I'd convinced myself were "fine because nobody does X." Users do X. They will paste logs. They will misconfigure clients. Assume it.
>
> SAST is a floor, not a ceiling. It won't catch logic bugs, won't catch authorization semantics, won't catch threat-model gaps. But it'll catch the patterns you've talked yourself out of caring about. That's worth doing on every infra OSS project that handles secrets or talks to APIs.

**First comment**: link to M3 SAST retro post.

**Hashtags**: `#Golang #Security #OpenSource #DevSecOps #Infrastructure`

---

### Week 11 — V5 storage video drop

**Hook**:
> I ran NFS, democratic-csi, and Longhorn in production for 6 months each. Here's the comparison.

**Body**:

> Storage is the second-most-asked question I get from homelab Kubernetes operators (after "is it real Kubernetes — yes, it is").
>
> Made the comparison video and the written deep-dive — same opinions, on-screen demos for the video.
>
> Where I land after running all three:
>
> → Default StorageClass: Longhorn. Block storage, in-cluster, independent failure domain.
> → Read-mostly workloads (Plex/Jellyfin libraries, Velero backup target): NFS. The right tool when the data already lives on TrueNAS.
> → democratic-csi: only when you specifically need per-PVC ZFS snapshots. Rare in practice.
>
> The non-obvious finding: Longhorn changed my mind. I started assuming democratic-csi was obviously correct ("use the NAS, that's what it's for"). Six months in, the independent failure domain matters more than the ZFS-coupling.
>
> When TrueNAS reboots for maintenance, Longhorn-backed apps stay up (degraded but responding). democratic-csi-backed apps go offline. For homelab maintenance windows that don't take everything down at once, that's huge.
>
> One thing I cannot say loudly enough: don't put a database on NFS. PostgreSQL on NFS will eventually corrupt or stall. MySQL too. SQLite too. Anything that fsyncs aggressively.
>
> Video link in comments. Written companion has the same content with more numbers and the longer "what I'd do from scratch today" section.

**First comment**: V5 YouTube URL + storage deep-dive post URL.

**Hashtags**: `#Kubernetes #Storage #Longhorn #Homelab #SelfHosted`

---

### Week 12 — Creator outreach reflection

**Hook**:
> I cold-emailed one YouTube creator about my open-source project this week. The thing I learned wasn't in the reply — it was in writing the pitch.

**Body**:

> Reaching out to a creator in your niche feels like a marketing move. The actual lesson is in the prep.
>
> Writing a credible cold pitch forces you to answer five questions you probably haven't asked yourself in this order:
>
> 1. What's the specific story angle that would make their audience care?
> 2. What's the demo / artifact / setup you could send them that's worth their time?
> 3. What can they do with your project that nobody else's project lets them do?
> 4. What's the ask, in one sentence, with zero ambiguity?
> 5. What happens if they say no?
>
> The clarity from answering these is independent of whether they reply. You end up with sharper positioning for every other channel.
>
> What I learned about my own project from writing the pitch: I lead with the wrong thing in most of my marketing copy. I lead with "what it is." The creator's audience doesn't care what it is — they care what unusual recipe it enables them to demonstrate that nobody else's tool would let them demonstrate.
>
> Going to rewrite the homepage and the next round of social copy with that frame. Whether or not the creator replies, the email already paid for itself.
>
> If you maintain something and you've been telling yourself "I should reach out to [creator]" — go write the email. Don't send it yet. Just write it. Then read it back and ask whether your positioning survived the exercise.

**First comment**: (no link — engagement post, not a CTA-driven one).

**Hashtags**: `#Marketing #OpenSource #DeveloperRelations #BuildInPublic #Positioning`

---

## Month 4 — Weeks 13–16

### Week 13 — The "don't help" rule

**Hook**:
> The most important rule of a rolling Kubernetes upgrade: when it's running, don't help.

**Body**:

> Upgrade days. Half the cluster has rolled. You're watching the Omni dashboard. You see a node taking longer than the last one. The instinct is to do *something* — drain it manually, force a reschedule, restart a pod.
>
> Don't.
>
> A rolling upgrade is an orchestrated state machine. It knows what node is being drained. It knows what workloads need to migrate. It knows what health checks pass before moving to the next node. Every manual intervention you make is a state-change it didn't expect.
>
> The two failure modes I've seen from "helping":
>
> 1. Manually draining a node that the upgrader was about to drain. The upgrader sees the node already drained, gets confused, moves on. The new Talos image never installs because the node is still mid-cycle.
> 2. Force-deleting a pod that was about to be evicted gracefully. The pod's PVC handlers don't get to finish. Longhorn replicas end up in a weird state. You spend the next hour healing volumes.
>
> The right move during a rolling upgrade is: wait. If you've been waiting more than 10 minutes for a single node, check etcd logs (`talosctl logs etcd`). 90% of "stuck" upgrades are etcd taking forever because the ZFS pool is slow. The node will come up. Just wait.
>
> The principle generalizes. The smartest thing you can do during any automated rollout — software deploys, schema migrations, config rollouts — is sit on your hands. Trust the orchestrator until it explicitly asks for help.
>
> Posting because I needed to read this myself, six months ago.

**First comment**: link to M4 upgrade playbook post.

**Hashtags**: `#Kubernetes #SRE #DevOps #Operations #Homelab`

---

### Week 14 — Singleton-lease pattern

**Hook**:
> I built distributed leader election in 200 lines of Go because the SDK I was using didn't ship one. Sharing the pattern.

**Body**:

> The Omni SDK has no built-in leader election. Run two infrastructure-provider processes with the same provider ID and they both race on every machine request. State corruption follows.
>
> No way to add etcd, no way to add Redis — this is a single-binary OSS project, every dependency would have to live in users' homelabs.
>
> The pattern that worked: use Omni's existing COSI resource store as the lease backend.
>
> 1. Annotate `ProviderStatus` with `instance-id`, `heartbeat`, `epoch`.
> 2. Read-then-write with optimistic concurrency. If another instance updated the resource since I last read, my write fails with a version conflict and I retry.
> 3. Heartbeat tick every 15 seconds. Stale-after threshold 45 seconds. Three missed heartbeats = takeover allowed.
> 4. Epoch counter as a fencing token — bumps on every takeover so the previous holder, if alive, can detect preemption.
>
> Around 200 lines. No external services. The full implementation is in the repo if you want to lift it.
>
> Honest disclosure: I haven't fully threaded the fencing token through every state-mutating call yet. The epoch is observability-only. That's the bug I haven't filed against myself.
>
> The lesson that generalizes: when your framework doesn't ship a primitive you need, build it from the primitives you have. Optimistic concurrency on existing state *is* leader election if you squint right.

**First comment**: link to M4 singleton-lease deep-dive post.

**Hashtags**: `#Golang #DistributedSystems #Kubernetes #OpenSource #Infrastructure`

---

### Week 15 — V6 upgrade video drop

**Hook**:
> I recorded a live cluster upgrade on my actual homelab. No edits to hide the boring parts.

**Body**:

> The upgrade video is up. Real cluster, real workloads, no demo cluster.
>
> What's in it:
>
> → Pre-flight ritual — etcd snapshot, ZFS snapshot, the health checks I always run
> → The Talos rolling upgrade with a real time-lapse of the 25-minute wait
> → The Kubernetes upgrade afterward
> → What to actually do when a node stalls (PDB violations + HDD etcd timing being the two common cases)
> → Post-upgrade verification and snapshot cleanup
>
> The one piece of advice that keeps mattering: 24 hours of normal operation before deleting your pre-upgrade snapshots. If something subtle broke, you want the rollback path available.
>
> If you're managing a homelab Kubernetes cluster and you've been putting off the next version bump because "what if it breaks" — the video shows what "it breaks" actually looks like on this stack, and how to handle it without panicking.
>
> Spoiler: it doesn't break. The upgrade tooling on Talos + Omni is genuinely good. Worst case is a stalled node, and stalled nodes have known causes with known fixes.

**First comment**: V6 YouTube URL + upgrade playbook post URL.

**Hashtags**: `#Kubernetes #Talos #SRE #Homelab #SelfHosted`

---

### Week 16 — Boring parts that matter

**Hook**:
> The most valuable work I did on my open-source project this year was the parts nobody will ever notice.

**Body**:

> Cassette-based integration tests. Schema validation at the boundary. Singleton-lease leader election. Sensitive-data redaction at the marshaling layer. ZFS recordsize tuning recommendations in the docs.
>
> None of those are "features." Nothing in the changelog reads "now with leader election" or "now with cassette tests" in the marketing-y sense. They're the kind of work that earns you no stars and shows up on no roadmap.
>
> They also account for roughly 60% of the time I spent on the project this year.
>
> Why does it pay off? Because the boring work compounds. The cassette tests caught five regressions over the last two months that would have shipped to users without them. The schema validation closed a host-OOM bug that had hit three users. The singleton lease prevented a class of split-brain failures I'll never know about because they didn't happen.
>
> The career incentive in OSS pushes toward visible features. Maintainers who only build features burn out, ship buggier code, and lose the trust that compounds over years. Maintainers who do the boring work get to keep maintaining.
>
> If you're early in your OSS journey: the boring work is the work. Don't apologize for it. Don't underweight it. Schedule it as deliberately as feature work.
>
> If you're hiring engineers: ask candidates about the boring work they're proudest of. The answer tells you everything.

**First comment**: optional — link to GitHub repo with "this is what 60% of the time looks like" framing.

**Hashtags**: `#OpenSource #SoftwareEngineering #InfrastructureEngineering #SRE #BuildInPublic`

---

## Month 5 — Weeks 17–20

### Week 17 — Case study highlight

**Hook**:
> Someone else is running my open-source project in their homelab — and the way they hardened it is something I'd never thought to do.

**Body**:

> Just published a case study with one of the users running `omni-infra-provider-truenas`. They've been on it for [N] months, running [their specific cluster shape].
>
> The thing that stuck with me: they treat their TrueNAS pool layout like a production storage system. [Specific thing they do — replace with real example from interview, e.g., "Dedicated SLOG mirror on optane, separate metadata vdev for the cluster pool"]. I run a similar workload on the same hardware class and I'd never thought to do that.
>
> This is the value of running an open-source infra tool in public that nobody talks about: users teach you what your tool actually is, vs. what you thought you were building.
>
> Three patterns from the conversation that I'm going to bring back into the docs:
>
> 1. [Pattern 1 — fill in from real interview]
> 2. [Pattern 2 — fill in from real interview]
> 3. [Pattern 3 — fill in from real interview]
>
> If you maintain something — even something niche — go find one user who's been running it for more than 6 months and ask them what they've learned. The answers compound differently than feedback through issues or analytics.
>
> Full case study in the comments. Genuine thanks to [user] for being open about their setup.

**First comment**: link to case study post.

**Hashtags**: `#OpenSource #CaseStudy #Homelab #Kubernetes #BuildInPublic`

---

### Week 18 — Analytics lesson

**Hook**:
> Four months into actually reading my marketing analytics, I learned the channel I assumed was working wasn't, and the one I'd written off was.

**Body**:

> Set up Plausible on day one. Wired up GitHub Insights, YouTube Studio, Google Search Console. Felt very organized. Didn't actually *read* the dashboards with intention for three months.
>
> When I finally sat down and made a tracking spreadsheet (one row per piece, weekly snapshots), here's what changed:
>
> → I'd been writing for LinkedIn assuming engineering leaders were the audience. Data showed the actual audience is other solo developers and OSS maintainers. Tone was wrong. Rewrote the playbook.
>
> → X (Twitter) cross-posts were getting near-zero conversion. Genuinely lowest-ROI channel of the five I'd been investing in. Cut back to opportunistic cross-posts, redirected the saved energy to Lemmy.
>
> → The hero install post — the canonical SEO piece — was responsible for roughly [X]% of all repo referrals. Way more than I'd guessed. Doubled down on cross-linking back to it from every subsequent piece.
>
> → Reddit conversion per post was wildly variable by subreddit. r/truenas (small audience) outperformed r/selfhosted (much larger) per impression because the ICP fit was tighter.
>
> The takeaway isn't "use better analytics tools." Plausible was fine. The takeaway is: *read the data weekly*. Make a one-hour calendar block on Mondays. Write the trends down in a tracking sheet. The data was always there. I just wasn't looking.
>
> If you've been collecting analytics on your side project without reviewing them — that's me four months ago. Pick a day. Read them. The findings will be uncomfortable and worth it.

**First comment**: optional — link to analytics setup notes if public.

**Hashtags**: `#Analytics #Marketing #BuildInPublic #DeveloperRelations #OpenSource`

---

### Week 19 — V7 networking video drop

**Hook**:
> Networking on a TrueNAS Kubernetes cluster has three pieces that bite everyone. I made a video that names them.

**Body**:

> V7 is up — networking for TrueNAS-hosted Kubernetes. Real router, real network, on screen.
>
> The three things that trip up almost every setup:
>
> 1. Router DHCP range conflicts with MetalLB. Default DHCP scopes go to .254. MetalLB wants .201–.250. Conflict. Shrink your DHCP range to .200 *before* installing MetalLB.
>
> 2. MTU mismatch on additional NICs. If you set jumbo frames (MTU 9000) on the VM side, the bridge and the switch ports must also be 9000. Mismatch anywhere = mysterious dropped packets that look like cluster instability.
>
> 3. People skip the Talos VIP. The VIP is built-in, free, and gives you the cheapest possible HA on the Kubernetes API endpoint. If you've got 3 control planes and your kubeconfig points at one specific CP IP — you've left HA on the table.
>
> Video covers all three with on-screen demos plus the rest: bridge setup, DHCP reservations using deterministic MACs, MetalLB install, multi-NIC for storage segmentation, and router-specific notes for UniFi, pfSense, and OPNsense.
>
> 14 minutes. Linked in comments.

**First comment**: V7 YouTube URL + networking docs URL.

**Hashtags**: `#Kubernetes #MetalLB #TrueNAS #Networking #Homelab`

---

### Week 20 — Pre-retro teaser

**Hook**:
> Six-month retro on marketing my open-source project drops in two weeks. One number from the data already changed my plan for next year.

**Body**:

> The full retro is the M6 piece. Numbers, channels, what worked, what didn't, what I'd do differently from scratch.
>
> Won't spoil the whole thing, but: the comparison content beat the tutorial content by roughly [N]× on conversion to repo clicks. That wasn't on my bingo card.
>
> Comparison posts (Talos vs k3s, TrueNAS vs Proxmox) draw readers who are *already deciding* — they've committed to doing something, they're picking which thing. That's the deepest part of the funnel I had access to without paid acquisition.
>
> The implication for next year: when I plan content, I'm going to weight comparison posts higher and tutorial posts lower than I did in this run. The hero install guide is still the load-bearing anchor — but most of the new content under it should help the *decision* phase, not the *learn* phase.
>
> Full retro in two weeks. I'll share the numbers — the actual ones, not vanity-curated.
>
> If you maintain anything and you've been telling yourself "I should look at what worked" — the data is already there. Spend an afternoon. The patterns are not what you think they are.

**First comment**: optional — tease the M6 retro post drop date.

**Hashtags**: `#Marketing #BuildInPublic #OpenSource #ContentStrategy #DeveloperRelations`

---

## Month 6 — Weeks 21–24

### Week 21 — Hub page launch

**Hook**:
> Six months of writing about one specific corner of self-hosted Kubernetes, now interlinked in one place. This is the page I wish had existed when I started.

**Body**:

> Just published the complete guide to running Kubernetes on TrueNAS via Talos + Omni. It's the hub that links every piece I've written this year:
>
> → Install guide (the canonical anchor)
> → Comparison posts (Talos vs k3s, TrueNAS vs Proxmox)
> → Sizing, storage, networking, upgrades
> → Build-in-public series (SAST sweep, singleton-lease pattern, host-OOM war story)
> → User case study
> → Failure-modes table — the gotchas, with diagnostics and fixes
>
> If you're new to this stack: start with the install guide. Bookmark the hub. Come back to it when you're operating the cluster.
>
> If you're already running it: the failure-modes table at the bottom of the hub is the highest-utility section for returning visitors. I update it every release. Bookmark that one specifically.
>
> The work that compounds in open source isn't the splashy releases. It's the patient, year-long process of writing down every lesson you learn so the next person doesn't have to learn it the slow way.
>
> Six months ago this hub didn't exist. Today it does, and it'll keep growing.

**First comment**: link to hub page.

**Hashtags**: `#Kubernetes #TrueNAS #Homelab #SelfHosted #Documentation`

---

### Week 22 — Omni explainer

**Hook**:
> If you've heard of Kubernetes but not Sidero Omni: Omni's killer feature isn't the UI. It's a small extension point most people never see.

**Body**:

> Sidero Omni is a Kubernetes management platform. Free tier covers homelab use. The pitch is "managed Kubernetes without cloud lock-in."
>
> The interesting part isn't the management UI. It's a small extension point called **infrastructure providers**.
>
> An infrastructure provider is roughly: a small process that listens for "I need a node" requests from Omni, creates the corresponding Talos VM on whatever hardware you support, and tears it down when asked.
>
> The Sidero team can't have built a provider for every hardware target. They built it for cloud (AWS, GCP, Hetzner), Proxmox, and baremetal. The long tail — TrueNAS, Hyper-V, VMware Workstation, Raspberry Pi clusters, specialized hardware — is open.
>
> I built the TrueNAS one. About 5,000 lines of Go. MIT licensed.
>
> What the contract gives you, that I didn't expect: complete freedom underneath. Omni doesn't care how you create the node, only that the node appears. So my provider does ZFS-backed zvols, deterministic MAC addresses, cassette-based tests, singleton-lease leader election — none of which Omni knows about or cares about. The contract is narrow. Everything else is mine.
>
> If you have hardware nobody else has and an API to control it: you could write an Omni provider for it. The cost is a few thousand lines of Go and a working knowledge of the platform. The reward is filling an ecosystem gap that's yours to own.
>
> Narrow, well-defined extension points are a gift to the people building niche tools on top of platforms. Omni's is one of the better ones I've worked with recently.

**First comment**: link to M6 Omni explainer post + provider repo.

**Hashtags**: `#Kubernetes #Omni #OpenSource #Infrastructure #BuildInPublic`

---

### Week 23 — V8 retro video drop

**Hook**:
> The full 6-month retrospective on marketing my open-source project is up. Real numbers, real regrets, no fluff.

**Body**:

> V8 dropped. The retro video covers what 6 months of intentional marketing on a niche infra OSS tool actually looked like.
>
> Numbers. Subscriber growth. Search ranking. Repo referrals by channel. Which channels worked, which didn't, what I'd do differently from scratch.
>
> Three things I cover that I think generalize past my project:
>
> 1. The canonical install guide is the single highest-leverage piece of marketing for a niche tool. Write it before anything else.
> 2. Comparison content beats tutorial content for conversion. Readers comparing things have already decided to do something.
> 3. YouTube is slow and worth it — but treat it as face-recognition compounding, not top-of-funnel.
>
> Also the things I would not do again: the X channel, the order I sent podcast pitches in, writing for the wrong LinkedIn audience early on.
>
> 16 minutes. Slide-deck format, different from the rest of the channel — this is the retrospective, treated deliberately as one.
>
> If you maintain anything and you've been wondering whether marketing is worth your time, this is the most useful 15 minutes I can offer.

**First comment**: V8 YouTube URL + written retro post URL.

**Hashtags**: `#OpenSource #Marketing #BuildInPublic #DeveloperRelations #YearInReview`

---

### Week 24 — Forward-look

**Hook**:
> Six months of building in public taught me what works. Here's what I'm betting on for the next six.

**Body**:

> Wrapping the first 6-month run. Three bets for the next chapter:
>
> 1. **Deeper SEO on the hub page**. The complete guide is the canonical destination. Goal: top-3 ranking for the primary query by month 12. That's a one-page, year-long investment.
>
> 2. **Two more user case studies**. The first one was hard to source — self-hosters are private. Now that I have a process and a relationship, the next two should be easier. Social proof compounds.
>
> 3. **One focused experiment with paid distribution**. Boost one strong post on Reddit or LinkedIn for ~$200. Honest test of whether the project's cost-per-repo-click pencils out. If it does, that's a new lever. If it doesn't, I'll know.
>
> Things I'm explicitly *not* betting on:
>
> → A Discord/Slack community for the project. Too early, audience too small, support load would exceed value.
> → A newsletter. Audience overlap with existing channels is too high.
> → Conference talks. Prep cost is enormous for the reach. Only if invited.
>
> The meta-lesson from six months: niche infra OSS doesn't need a marketing team. It needs five things — one canonical answer to the question users type, a face people recognize, honest tradeoffs against alternatives, a clear story about success ("issues > stars"), and consistency over time.
>
> Consistency was 80% of what worked. The rest was choosing the right places to be consistent.
>
> See you in M7. Same monthly cadence. Different angle.

**First comment**: optional — invite engagement ("What are you betting on for the next 6 months?").

**Hashtags**: `#OpenSource #BuildInPublic #YearAhead #Marketing #DeveloperRelations`

---

## Posting checklist (run before each)

- [ ] Hook in first 2 lines? (LinkedIn truncates after ~210 chars on mobile preview)
- [ ] Body under 1300 chars total (the "see more" cutoff)
- [ ] No external links in body (kills reach by ~50%)
- [ ] Link in first comment ready to drop within 60 seconds of publishing
- [ ] 3–5 hashtags max (more = spam signal)
- [ ] Replied to every comment for first 4 hours

## Template patterns to reuse later

- **Hook formula**: "I [did unusual thing] for [time period]. Here's [the honest finding]."
- **Body formula**: 3 numbered lessons → "what I actually do now" → tease the full version in the comments.
- **CTA formula**: ask a question that invites engineering-leader audience to share their own take. Engagement bait works on LinkedIn in a way it doesn't elsewhere.
