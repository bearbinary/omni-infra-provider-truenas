# Reddit + Lemmy Post Patterns — Milestone Framing Playbook

**Hard rule**: every Reddit and Lemmy post in this campaign is **milestone-framed**, not product-announcement-framed. The project is the resource you mention near the end, never the headline.

Reddit and Lemmy audiences downvote anything that smells like marketing. They upvote stories, lessons, real specs, honest tradeoffs. The same content — reframed — gets dramatically different engagement.

This file is the playbook every M1–M6 cross-post should follow.

---

## The reframe — side by side

| Product-announcement framing (don't) | Milestone framing (do) |
|---|---|
| "I built X that does Y — here's the guide" | "After [N] months doing Y, here's what I learned" |
| "Check out my new X for [audience]" | "What [N] months of [audience problem] taught me" |
| "Shipping X solves [problem]" | "I solved [problem] after [time/setbacks]. Sharing the writeup." |
| "New feature: X" | "I shipped [feature]. The [unexpected lesson] surprised me." |
| "I wrote a guide to X" | "Wrote up [N] months of running X for anyone going down the same path" |

The cognitive shift: **the post is about you and what you learned, not the product**. The product appears once it's been earned by the value delivered above it.

---

## Title patterns that work

Pick the pattern that fits the piece. All of these lead with milestone/experience/lesson.

### Pattern 1: Time-based experience

```
After [N] months of [thing], here's [the lesson / the writeup / what works]
```

Examples:
- "After 6+ months running Kubernetes on a single TrueNAS box, here's the writeup of what actually works"
- "Three months in on shipping an open-source infra tool — what I wish I'd known about marketing it"
- "Six months of building [thing] in public — the actual numbers and the honest lessons"

### Pattern 2: Lesson-first

```
[Specific concrete lesson]. Here's the [writeup / context / story].
```

Examples:
- "Most homelab Kubernetes control planes are undersized. Here's the trigger I use to know."
- "HDD pools and etcd don't get along. After 6 months of debugging it, here's what I do now."
- "The most important rule of a rolling upgrade is: when it's running, don't help. Writeup."

### Pattern 3: Problem-solved

```
[Problem] / [How I solved it after [setbacks]] / [Sharing because I know I'm not the only one]
```

Examples:
- "For anyone who lost K3s when TrueNAS shipped 25.04 — here's the path I rebuilt my homelab Kubernetes with"
- "My TrueNAS rebooted overnight 3 times before I found the host-OOM bug. Here's what was actually wrong."
- "I spent a weekend chasing 'intermittent NodeNotReady' flaps. Turned out to be ZFS."

### Pattern 4: Show your work / build report

```
My homelab [setup] runs on [unusual hardware shape]. [Setup, lessons, what I'd change].
```

Examples:
- "My homelab Kubernetes cluster runs on a single TrueNAS box — setup, lessons, what I'd do differently after 6 months"
- "Running a 5-node Talos cluster on a 12-core NAS — workloads, performance, what surprised me"
- "I run Argo + Prometheus + Longhorn on a NAS. Here's the cluster shape after 6 months of tuning."

### Pattern 5: Decision-made / opinion-formed

```
[Decision]. Here's why, after [running both / trying alternatives].
```

Examples:
- "Why I picked Talos over k3s for my homelab — 6 months of running both"
- "Why I stopped using NFS for my Kubernetes persistent volumes (and what I use now)"
- "I switched from Proxmox to TrueNAS-only for my homelab K8s. Here's the honest tradeoff."

### Pattern 6: Build-in-public technical retrospective

```
[Technical artifact / lesson]. Built it because [SDK gap / surprise]. Here's the pattern.
```

Examples:
- "Built distributed leader election in 200 lines because my SDK didn't ship one. Sharing the pattern."
- "Lessons from shipping an Omni infrastructure provider for a non-cloud target, 6 months in"
- "A SAST sweep on a 5kLOC Go project taught me more than any book did. Here are the 6 findings."

---

## Body structure (works for any pattern)

```
[1] Setup / context — 1-2 sentences. The situation you found yourself in.
    Why this is relevant. NOT a product pitch.

[2] Concrete content — the lesson(s), specs, gotchas, decisions.
    This is 80%+ of the body. Bullet points or numbered list work well.
    Be specific. Numbers. Hardware models. Failure messages. Real specs.

[3] (Optional) "What I'd do differently" — vulnerability earns trust.

[4] Link block — 2 sentences max:
    Writeup link first.
    Project/repo link second, framed as "the [tool / project / source] that makes this work" or "if you want to go down the same path."
    Never lead with the project link.

[5] Closing line — invite discussion, not clicks. "Happy to answer questions."
    "Curious what others have done differently." "Tell me what I got wrong."
```

---

## Per-piece reframe — quick reference for M1–M6

### M1 Hero install guide
- ❌ "I built an Omni provider for TrueNAS — full guide"
- ✅ "After 6+ months running Kubernetes on a TrueNAS box, here's the canonical writeup"

### M2 Talos vs k3s comparison
- ❌ "Wrote a comparison: Talos+Omni vs k3s on TrueNAS"
- ✅ "I ran k3s and Talos in parallel for 6 months on my homelab TrueNAS. Here's the honest comparison."

### M2 TrueNAS vs Proxmox
- ❌ "TrueNAS vs Proxmox for K8s — my take"
- ✅ "I switched my homelab K8s from Proxmox+TrueNAS to TrueNAS-only after [N] months. Here's the tradeoff."

### M2 Host-OOM war story
- ❌ "v0.16.1 fixes the host-OOM bug"
- ✅ "My TrueNAS host OOMed in production three times before I tracked down a memory-config bug. Here's what was actually wrong."

### M3 Sizing post
- ❌ "Guide: sizing Talos control planes"
- ✅ "Most homelab K8s control planes are undersized. Here's the trigger I use to know — after sizing dozens of them."

### M3 SAST retro
- ❌ "I ran SAST on my Go project — 6 findings"
- ✅ "A SAST sweep on a 5kLOC Go infra project taught me more than I expected. 6 lessons that generalize."

### M3 Storage deep-dive
- ❌ "Storage options for Kubernetes on TrueNAS"
- ✅ "I ran NFS, democratic-csi, and Longhorn in production for 6 months each. Here's what I picked and why."

### M4 Upgrade playbook
- ❌ "How to upgrade Talos via Omni"
- ✅ "After [N] cluster upgrades, here's the pre-flight ritual I always run. Saved my weekend more than once."

### M4 Singleton-lease pattern
- ❌ "I implemented leader election for my Omni provider"
- ✅ "Built distributed leader election in ~200 lines because my SDK didn't ship one. The pattern, the upstream bug, the part I'd build differently."

### M5 Case study (user)
- ❌ "Featured user case study"
- ✅ "Someone else is running this stack in their homelab — and the way they hardened it is something I'd never thought to do"

### M5 Six-month retro
- ❌ "6 months of my open-source project"
- ✅ "I marketed a niche open-source infra tool intentionally for 6 months. Here are the actual numbers and the honest lessons."

### M6 Hub page
- ❌ "Complete guide to Kubernetes on TrueNAS"
- ✅ "Bookmarked: every lesson from 6 months of running Kubernetes on a TrueNAS box, in one place"

### M6 Omni explainer
- ❌ "What is an Omni provider?"
- ✅ "I built an Omni infrastructure provider. Here's what an Omni provider actually is, and the unreasonable freedom they give you."

---

## Subreddit-specific tonal shifts

Same milestone framing, different emphasis per audience.

| Sub | Lead with | Body emphasis |
|---|---|---|
| **r/selfhosted** | Time + journey ("6 months in...") | Lessons, gotchas, the honest tradeoffs |
| **r/homelab** | Hardware specs + build report | Real specs, workloads, "what I'd change" |
| **r/kubernetes** | Technical milestone ("shipped X pattern after Y") | SDK gaps, architecture, code-adjacent insights |
| **r/truenas** | Shared problem + solution ("if you also lost K3s in 25.04...") | TrueNAS-specific gotchas, ZFS, API quirks |
| **r/golang** (M3 SAST, M4 lease) | Technical pattern + retrospective | Code snippets, design tradeoffs, "what I'd do differently" |
| **r/devops** (M3 sizing, M4 upgrade) | Operational lesson | Day-2 practices, observable triggers, real numbers |

---

## Lemmy-specific notes

Same patterns as Reddit, plus:

- **Smaller communities, higher engagement-per-subscriber.** A "small" post on Lemmy (~50 upvotes) is genuine engagement, not lurkers.
- **More tolerant of long-form bodies.** Reddit's TL;DR culture is weaker here. A 600-word body for context on a link post is normal.
- **FOSS-aligned audience.** Lean into MIT licensing, issues-only model, solo-maintainer transparency. These read as features here.
- **Lower tolerance for anything that smells corporate.** "We" when you mean "I" — Lemmy notices. Avoid.

---

## What never to do

- **Never lead with the project name in the title.** Even if the project name is genuinely the most interesting thing about the post, lead with the lesson, mention the project once you've earned the click.
- **Never put the repo link at the top of the body.** Repo is at the bottom of the value delivery, after the lessons/specs/gotchas.
- **Never use "introducing" or "announcing"** in a Reddit or Lemmy title. Those words signal "this is marketing." They are.
- **Never cross-post the exact same body** to two subs without adjusting the angle for each sub's audience interest.
- **Never bury the lesson under product copy.** If a reader has to scroll past 2 paragraphs of "what the project does" before getting to a lesson — you've inverted the post.

---

## Test before posting

For each Reddit/Lemmy draft, ask:

1. Does the title lead with a lesson, milestone, or experience? (Not the project name.)
2. Does the first paragraph of the body deliver value, not pitch?
3. Are there at least 3 specific, concrete claims (numbers, model names, failure modes)?
4. Is the project mentioned once in the body, near the end, as a resource?
5. Would I upvote this if I weren't the author?

If any answer is no — rewrite before posting. The cost of a re-write is small. The cost of a downvoted-into-oblivion post is a closed channel for the next piece.
