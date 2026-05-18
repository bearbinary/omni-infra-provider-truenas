# Podcast Pitches (M4 distribution)

Three shows worth pitching this quarter. Each has a different audience and a different ideal angle. Send one pitch per week. Don't bundle.

**Rule**: a podcast pitch with no story is spam. Lead with the story the host would want to tell, not "I built a thing, would you have me on?"

---

## Show 1: Self-Hosted (selfhosted.show — Chris Fisher + Alex Kretzschmar)

### Why this show

- Audience is exactly the ICP: TrueNAS users, homelab Kubernetes operators, FOSS-aligned self-hosters.
- They've covered TrueNAS, Talos, and Sidero topics in past episodes.
- Listener-suggested topics are a regular feature — getting mentioned in their suggestion-comments is itself worth doing.

### The hook

> When TrueNAS dropped built-in Kubernetes in 25.04, a lot of self-hosters got stranded. I built the way back — an open-source provider that runs Talos VMs on TrueNAS as a managed cluster. It's the kind of niche tool nobody would build at a company, but it solves a real homelab problem.

### Cold email template

**Subject**: TrueNAS killed Kubernetes — I shipped the replacement (story idea)

```
Hi Chris and Alex,

Long-time listener. Pitching a story for a future episode.

When TrueNAS dropped built-in Kubernetes in SCALE 25.04, a lot of self-hosted clusters quietly died. Mine was one of them. So I built an open-source Omni infrastructure provider for TrueNAS — it turns a TrueNAS box into a fleet of Talos Linux VMs that Sidero Omni manages as a real multi-node Kubernetes cluster.

Project is MIT, on the TrueNAS apps community catalog, currently v0.16.1 with regular releases. About 5,000 lines of Go. Solo-maintained.

Things I'd love to talk about if it's a fit:
- Why the TrueNAS apps shift to Docker left a gap and why nobody else filled it
- How immutable infrastructure (Talos) changes the homelab mental model — replace, don't fix
- The honest tradeoffs vs Proxmox-based and k3s-in-VM setups
- The unglamorous parts: reverse-engineering TrueNAS's JSON-RPC, building leader election when the SDK doesn't ship one, surviving a TrueNAS upstream-auth change mid-development

I write about this stuff at [dev.to/cliftonz] and on YouTube at [channel link]. Hero install guide: [dev.to/cliftonz/<hero-post-slug>].

Happy to record at your convenience. Pre-recorded a few times before — easy guest, no production overhead on your end.

Thanks for the show.

Zac Clifton
```

### Follow-up (if no reply in 2 weeks)

```
Hi Chris and Alex,

Quick follow-up on the TrueNAS + Kubernetes pitch from two weeks ago — no worries if it's not a fit, just wanted to confirm it landed.

Since I wrote you, the project hit [X] GitHub stars and a couple of users have published their own cluster writeups on the TrueNAS forum. Happy to share those as story material whether or not I come on the show.

— Zac
```

### Don'ts

- Don't pitch this show on hosting infrastructure they don't talk about (e.g., Crossplane internals). Stay in the homelab/self-hosted lane.
- Don't pitch in the same week as a controversial Reddit thread about TrueNAS — they'll already be flooded.

---

## Show 2: The Changelog (changelog.com — Adam Stacoviak + Jerod Santo)

### Why this show

- Broad OSS / developer-tools audience.
- They love stories about niche maintainers shipping under their own name.
- They have a "Founders & CEOs of niche projects" angle that fits.

### The hook

> I shipped an open-source infrastructure tool that filled a gap nobody else was filling — and I learned that being the only person doing something is a marketing strategy whether you meant it that way or not.

### Cold email template

**Subject**: Story idea — solo-maintained infra OSS, niche audience, the marketing strategy that wasn't

```
Hi Adam and Jerod,

Long-time Changelog listener. Pitching a story angle.

I'm Zac Clifton — solo maintainer of an open-source Omni infrastructure provider for TrueNAS. The project filled a gap (TrueNAS dropped built-in Kubernetes in 25.04, nobody else built the bridge) and has been quietly compounding for several months now.

The angle that I think your audience would find interesting: being the only person doing something turns out to be a marketing strategy, even if you didn't mean it that way. The project gets traction not because I'm a great marketer (I'm not) but because there's no competing answer when somebody Googles "Kubernetes on TrueNAS."

Other things I could talk about that fit Changelog's range:
- Building distributed primitives (leader election) when your SDK doesn't ship them
- Issues-only contribution model — why I run it, how it's worked
- Cassette-based integration tests so CI doesn't need hardware (~42 cassettes for the TrueNAS API)
- The economics of solo-maintained infra OSS vs corporate-backed projects

Project: github.com/bearbinary/omni-infra-provider-truenas — MIT, ~5,000 LoC Go, currently v0.16.1.
Writing: dev.to/cliftonz
YouTube: [channel link]

Happy to record any time. Familiar with your format.

Thanks for the show.

Zac Clifton
```

### Pitch the right segment

Changelog has multiple shows. Pitch:

- **The Changelog (main show)** for the "solo infra maintainer" angle
- **Go Time** for the singleton-lease deep-dive (see Show 3)
- **Ship It!** for the upgrade/operations playbook
- **JS Party / Practical AI** — not a fit, skip

If you're cold-pitching the main show, also mention Ship It! and Go Time as backup options. Helps Adam route the pitch.

---

## Show 3: Go Time (changelog.com/gotime — rotating panel)

### Why this show

- Pure Go audience. Code-heavy episodes.
- The singleton-lease pattern is a perfect topic for them: real Go code, distributed-systems angle, opinions about API design.
- Episodes regularly feature solo project maintainers.

### The hook

> I built distributed leader election on top of my application's existing API state, in ~200 lines of Go, because the SDK I was using didn't ship one. Three lessons that generalize beyond my project: fencing tokens aren't optional, optimistic concurrency is leader election if you squint right, and shipping a clear failure message matters more than preventing the failure.

### Cold email template

**Subject**: Topic pitch for Go Time — building leader election when your SDK doesn't ship one

```
Hi Go Time team,

Topic pitch: building distributed leader election from scratch in Go when the framework you're using doesn't ship one.

Context: I maintain an open-source Omni infrastructure provider in Go. The Omni SDK has no built-in leader election, but the consequence of running two instances against the same provider ID is racing on VM creation and zvol allocation — exactly the kind of state corruption you'd write a Jepsen test against.

I built a singleton-lease pattern in ~200 lines on top of Omni's COSI resource store (optimistic concurrency for free). Annotations on a status resource as the lease object, heartbeat tick, epoch as a fencing token, fail-fast at startup, three-strikes-out error counter for loss detection.

The post-mortem honesty: my fencing token is still observability-only — I haven't threaded it through every state-mutating call yet. That's the bug I haven't filed against myself, and I think it's an interesting "why this would bite under partition" discussion topic.

Things I could speak to on an episode:
- Why optimistic concurrency on existing state beats adding etcd/Redis for a single-binary OSS project
- The shape of "fencing token" vs "lease holder check" — the distinction that matters under partition
- Routing around an upstream gRPC-gateway bug with substring-matching on error strings (the unfortunate-but-correct workaround)
- Designing for solo-maintenance — fewer dependencies = fewer things that can break in production for users you've never met

Source: github.com/bearbinary/omni-infra-provider-truenas/blob/main/internal/singleton/singleton.go
Writeup: [dev.to/cliftonz/<singleton-lease-post-slug>]

MIT licensed, ~5,000 LoC total. Go 1.26+.

Happy to record.

Zac Clifton
```

### Notes

- The singleton-lease shareable post (M4 piece 02) is the artifact this pitch references. **Publish that post before sending this pitch** so the link exists.
- If they bite, prep code samples + a one-pager outline. Go Time episodes have prep calls — be ready to brainstorm with the host.

---

## Pitch tracking

| Show | Date sent | Reply? | Follow-up date | Outcome |
|---|---|---|---|---|
| Self-Hosted | | | | |
| Changelog (main) | | | | |
| Go Time | | | | |

---

## Cadence

| Week | Pitch |
|---|---|
| M4 W1 | Self-Hosted Show |
| M4 W2 | (wait — give Self-Hosted breathing room before stacking another pitch from same email domain) |
| M4 W3 | Changelog (main show) |
| M4 W4 | Go Time |
| M5 W2 | Follow-up Self-Hosted (if no reply) |
| M5 W3 | Follow-up Changelog (if no reply) |
| M5 W4 | Follow-up Go Time (if no reply) |

**Realistic odds**: 1 of 3 books. Self-Hosted Show is the highest-probability hit because the ICP overlap is so direct. Go Time is a long shot but the topic is unusually well-fitted. Changelog main is competitive — they get pitched constantly.

---

## What to do if booked

1. **Block 2 hours for prep** the day before. Re-listen to the host's last 3 episodes for tone/format.
2. **Bring stories, not facts**. Hosts can read facts off your README. They want narratives — the moment something broke, the moment something compounded.
3. **Have 5 "if asked, here's where I'd go deeper" topics in your back pocket.** Hosts will follow tangents that interest them.
4. **Mention the audience-facing CTA up front and again at the end.** Repo URL is the canonical. Hero post URL is the next-best.
5. **Send a short thank-you email after recording.** Mention one concrete thing from the conversation. Hosts remember guests who do this.

---

## Other shows to consider (Q3+)

If the first three don't pan out or if you've got time later:

- **Ship It! (Changelog)** — operations/SRE focus, good fit for upgrade post material
- **Kubernetes Podcast (kubernetespodcast.com)** — large audience, longer queue, lower-volume signal
- **Software Engineering Daily** — broad, occasional infra deep-dives
- **Coder Radio (JB)** — adjacent audience, focus on developer experience
- **The Tech Tribe / Self-Hosted-adjacent niche podcasts** — research these specifically for new entrants

Don't fan out beyond 3 active pitches at a time. Wait for replies (or non-replies) before extending.
