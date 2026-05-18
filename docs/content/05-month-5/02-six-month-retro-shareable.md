---
title: "6 months shipping a niche infra tool: numbers, lessons, what I'd do differently"
published: false
description: "A solo-maintained open-source Omni provider for TrueNAS, 6 months after I started marketing it intentionally. The metrics, the surprises, and what I'd change."
tags: opensource, infrastructure, marketing, devrel
cover_image: ""
series: "Build-in-public: omni-infra-provider-truenas"
---

**TL;DR — Six months ago I decided to stop hiding `omni-infra-provider-truenas` and start marketing it intentionally. This post is the honest retro: the numbers, what compounded, what fizzled, what I'd do differently next time. Written for solo maintainers of niche infra OSS who wonder whether marketing is worth their time. Short version: yes, but not the way most articles say.**

I'm Zac Clifton. The project is an open-source Omni infrastructure provider for TrueNAS — it lets people run a real multi-node Kubernetes cluster directly on their NAS. MIT licensed, issues-only contribution model, currently v0.16.1. The first version landed publicly several months before this 6-month marketing window started, but until 6 months ago the project was discoverable only by people who already knew Omni and already knew it had no TrueNAS provider.

**[PLACEHOLDER NUMBERS — fill in from the M6 analytics review before publishing. The article doesn't work without them. Be honest with the actual numbers, even if they're smaller than you'd like.]**

---

## The numbers

| Metric | Start (M1) | End (M6) | Delta |
|---|---|---|---|
| GitHub stars | [X] | [Y] | +[Z] |
| Repo forks | [X] | [Y] | +[Z] |
| Open issues | [X] | [Y] | +[Z] |
| YouTube subscribers | 0 | [Y] | +[Y] |
| YouTube total watch hours | 0 | [Y] | +[Y] |
| LinkedIn followers | [X] | [Y] | +[Z] |
| dev.to followers | [X] | [Y] | +[Z] |
| Estimated TrueNAS apps catalog installs | [X] | [Y] | +[Z] |
| Hero post page views (dev.to) | — | [Y] | — |
| Top organic search query (per GSC) | — | "[query]" — [N] impressions, [M] clicks | — |

[Optional: add a chart showing weekly stars + weekly YouTube subs to make the compounding visible. PostHog or GitHub Insights export both.]

---

## What I shipped

In six months:

- **[N] long-form written posts** (hero install guide, 2 comparison posts, sizing, SAST retro, upgrade playbook, singleton-lease deep-dive, hub page)
- **[N] YouTube videos** (channel intro + 7 monthly deep-dives)
- **[N] LinkedIn posts** (~4/month)
- **[N] Reddit / Lemmy / forum drops** across r/selfhosted, r/homelab, r/kubernetes, r/truenas, lemmy.world, TrueNAS forum
- **1 case study** of another user running the project
- **[N] podcast pitches** sent ([N] accepted, [N] aired)
- **[N] awesome-list submissions** ([N] merged)

Plus normal product work — three minor releases, a SAST sweep, the host-OOM safety fix.

---

## What worked (and would do again)

### 1. The hero post compounded

The M1 install guide ([Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>)) is by far the highest-traffic page in the funnel. By M6 it's responsible for roughly [X]% of repo referrals. Google Search Console shows it ranking in the top [N] for "kubernetes on truenas scale" and [N] for "talos on truenas."

The non-obvious part: I had no SEO strategy beyond "write the canonical answer to the question I get asked most." Apparently that's the strategy. Search engines reward authoritative, comprehensive answers to specific queries that aren't well-served elsewhere.

**Repeatable lesson**: if you're a niche-tool maintainer, your single highest-leverage piece of marketing work is the canonical install guide for your tool, ranked for the keyword people actually type.

### 2. Comparison posts converted better than tutorials

The two M2 comparison posts (Talos vs k3s, TrueNAS vs Proxmox) outperformed every M1 tutorial on click-through to the repo. People reading comparisons are deeper in the funnel — they've decided to do *something*, they're picking *which thing*.

The honesty mattered. The Talos vs k3s post explicitly names where k3s wins (debuggability, first-week learning curve). That paragraph is the most-quoted-back-to-me part of the whole 6-month run. Hedge-free recommendations from credible practitioners are rarer than they should be.

**Repeatable lesson**: comparison content beats tutorial content for conversion if you have a specific audience already shopping for a solution.

### 3. The smallest community had the best signal

r/truenas (~[N] subscribers) drove more genuine engagement per post than r/selfhosted (~[N]) and r/homelab (~[N]) combined. Smaller community + tighter ICP fit = higher signal per impression.

I started posting to r/truenas *first* (M1, then again per piece) to tune the hook before the bigger subs saw it. The bigger subs got a more polished version. Free QA.

**Repeatable lesson**: post to your smallest exact-fit community first. Use it to calibrate before fanning out.

### 4. YouTube is slow and worth it

YouTube subs grew slower than every other channel. By M6, the channel has [N] subs and [X] watch hours — not vanity-level, but real. The watch-time numbers compound differently than view counts: a 12-minute video watched 60% to completion is a much stronger signal than a 1-minute clip viewed 1000 times.

The unexpected ROI: YouTube videos build face-recognition. People who watched V2 and then saw my Reddit comment under a TrueNAS post replied with "oh you're the guy from YouTube" — and that recognition lowers the friction on every subsequent touch.

**Repeatable lesson**: YouTube is a brand investment, not a discovery channel. Treat it as compounding face-time, not as a top-of-funnel.

### 5. Issues > stars

I told people, on every CTA, "issues over stars." That actually changed user behavior. By M6 the issue tracker has [N] issues — most of them substantive bug reports, feature requests, or hardware-specific questions. Stars look nicer in screenshots; issues mean people are actually using the thing.

**Repeatable lesson**: tell your audience what success looks like to you. They'll often comply.

---

## What didn't (and would skip or change)

### 1. X (Twitter) was the lowest-ROI channel by a wide margin

[N] tweet threads + [N] single-tweet observations + posts into 4 X Communities = roughly [N] repo referrals over 6 months. That's worse than a single decent Reddit comment.

I'll keep cross-posting threads when a piece ships because the cost is near-zero, but I will not invest in growing the X presence specifically. The audience that consumes niche infra content has been migrating off X for two years and that trend continued during the window.

**What I'd do instead**: redirect the X-specific energy to Lemmy. Lemmy's smaller communities had better engagement-per-subscriber and the audience is more directly aligned.

### 2. The first podcast pitch went to the wrong show

I pitched [Show X — replace before publishing] first because it was the biggest. They never replied. The Self-Hosted Show — smaller audience but tighter ICP fit — would have been the right opening. Cold-pitching big shows when your project doesn't have name recognition is high effort, low yield.

**What I'd do instead**: pitch the niche-fit show first. Use a placement there as the credibility hook for the next pitch up.

### 3. LinkedIn engagement was higher than I expected — but not for the reasons I thought

I assumed LinkedIn would be where engineering leaders found the project and brought it into their teams. That's not what happened. LinkedIn engagement was mostly other solo developers, infra engineers, and OSS maintainers — i.e., my actual peers — not corporate buyers.

This is fine. Peer engagement is its own form of compounding (referrals into communities I'm not in, comments that surface gaps in my thinking). But I went in with the wrong audience model and over-optimized early posts for an audience that wasn't reading.

**What I'd do instead**: write LinkedIn posts for solo developers and OSS peers from the start. Stop trying to sound like a thought leader.

### 4. I should have started analytics earlier

I wired up Plausible in M1, which sounds smart, but I didn't actually *look* at the dashboard with intention until M3. The data I missed in M1–M2 is gone — Plausible has retention limits and I didn't snapshot weekly.

**What I'd do instead**: snapshot the analytics weekly into a spreadsheet from week one. Build the habit of *reading* the data, not just collecting it.

### 5. The case study was harder to source than I expected

I assumed I'd have multiple users willing to be featured by M5. The reality: self-hosters are private, and getting a "yes" on a real-name case study took [N] outreach attempts. I almost dropped the case-study slot from the plan.

**What I'd do instead**: start case-study outreach in M3, not M5. Build the relationship before you need the artifact.

---

## What I'd do differently from scratch

If I were doing this again from M0 with what I know now:

1. **Write the hero post in month 0**, before any marketing cadence starts. It's the load-bearing piece. Everything else cross-links into it. Don't release the project without it.
2. **Start the YouTube channel in month 0** too. Even one channel-intro video. The face-recognition compounds for months before the channel hits "real" subscriber counts.
3. **Pick three channels and ignore the rest.** Mine were: dev.to canonical + YouTube + Reddit. LinkedIn was a fourth that earned its slot. X was a fifth that didn't. Trying to be everywhere is the rookie mistake.
4. **Build a tracking sheet on day one.** A Google Sheet with three tabs. Update it weekly. Don't over-engineer it.
5. **Treat "issues filed" as the primary metric, not stars.** Stars are vanity. Issues are usage. Tell your audience this explicitly.
6. **Do the case-study outreach in month 3.** Build user relationships before you need them.
7. **Pre-write the first 3 months of content before publishing anything.** The cognitive load of "publish + write the next thing + answer comments" was real around M2. Front-loading the writing would have saved the M2 dip.

---

## Honest about the limits

This is a 6-month run on a niche tool with a tight ICP. The numbers above are real but small in absolute terms. If you're working on a tool with a broader audience (a SaaS product, a generic dev tool), the playbook is different.

What scales:

- The "canonical install guide" strategy
- The "smallest community first" cadence
- The "comparison content beats tutorial content for conversion" finding
- The "tell your audience what success looks like" lever
- The "treat YouTube as face-time, not discovery" frame

What doesn't:

- The specific channel mix (subreddits, Lemmy communities, podcasts)
- The cadence (3 pieces + 1 video + 4 LinkedIn per month is what a solo maintainer can sustain; team-marketed projects can do more)
- The "niche" advantage (being the only person doing something is a real moat, but it's not available to everyone)

---

## What's next

Three places I'm betting M7–M12:

1. **Deeper SEO on the hub page** — the M6 complete guide is the canonical destination, and I want it ranked in the top 3 for the primary query by M12.
2. **One more case study** — now that I have a process and a relationship in M5, the next two should be easier.
3. **A focused experiment with paid distribution** — boosting one strong post on Reddit or LinkedIn for ~$200 to see whether the project's CPA pencils out. If it doesn't, I'll know. If it does, I'll have a new lever.

I'm not betting on:

- A separate Discord/Slack community for the project — too early, audience is too small, support load would exceed the value
- Newsletter — high effort, audience overlap with my existing channels is too high to justify
- Conference talks — fun but the prep cost is enormous for the reach; only if invited

---

## The meta-lesson

Niche infra OSS doesn't need a marketing team. It needs:

- One canonical answer to the question your users type into Google
- A face people can recognize
- Honest, well-named tradeoffs against the alternatives
- A clear story about what success looks like ("issues > stars")
- Enough consistency that six months from now there's still something new

Most of what I did wasn't clever. It was just *consistent*. Six months of "good enough" beats one viral post almost every time.

If you maintain something niche and you've been telling yourself "I should market this someday" — today is fine. Start with the canonical post. The rest builds on it.

---

## Try the project

- **Repo**: [github.com/bearbinary/omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas)
- **Hero install guide**: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>)
- **YouTube**: [channel link]

If you're maintaining a niche infra project and have your own retro to share, I want to read it. Find me on [LinkedIn](#) or open an issue.

---

**About the author**: Zac Clifton is an infrastructure engineer building tools for self-hosters and small teams. He maintains `omni-infra-provider-truenas` and writes about the unglamorous parts of shipping infrastructure OSS. Subscribe on [YouTube](#) for monthly deep-dives.

---

## Pre-publish checklist

- [ ] **Fill in every `[X]`, `[Y]`, `[Z]`, `[N]` placeholder with real numbers.** This post does not work with hand-waved metrics.
- [ ] Run the M6 analytics review *before* drafting this post for real (see `_shared/analytics-setup.md`).
- [ ] Optional: replace text "What worked" / "What didn't" framing with the actual top 3 of each from your data — not the ones I guessed.
- [ ] Re-check the "What I'd do differently" section against your honest experience. If anything doesn't match what actually happened, swap it out.
- [ ] Pre-share with one trusted reader (another solo OSS maintainer) for honesty check. Anywhere you flinched is a place worth doubling down.
- [ ] Time the publish for a Tuesday morning ET. Hacker News pickup window matters for this kind of post.
