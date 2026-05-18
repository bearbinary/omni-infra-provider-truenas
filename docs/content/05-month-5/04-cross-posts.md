# M5 Cross-posts — Reddit + Lemmy

Milestone framing per `_shared/reddit-lemmy-post-patterns.md`.

---

## Piece 1: User case study

Live URL: `https://dev.to/cliftonz/<case-study-slug>`

**Critical**: the user named in this case study (per `01-case-study-template.md`) gets to share first. Send draft link 24h before public. Their network is the first wave. The drafts below assume the case study is live and the user has consented to attribution.

### Reddit

#### r/selfhosted

**Title**:
Someone else is running my open-source K8s-on-TrueNAS provider in their homelab — the way they hardened it is something I'd never thought to do

**Body**:

Just published a case study with one of the users running `omni-infra-provider-truenas`. They've been on it for [N] months running [their cluster shape].

The thing that stuck with me: they treat their TrueNAS pool layout like a production storage system. [Specific example from interview — fill in real detail before publishing.] I run a similar workload on the same hardware class and I'd never thought to do that.

This is the underrated value of running an open-source infra tool in public: users teach you what your tool actually is, vs. what you thought you were building.

Three patterns from the conversation I'm pulling back into the docs:

1. [Pattern 1 from real interview]
2. [Pattern 2 from real interview]
3. [Pattern 3 from real interview]

If you maintain something — even something niche — go find one user who's been running it for 6+ months and ask them what they've learned. The answers compound differently than feedback through issues or analytics.

Full case study with their actual hardware, workloads, and advice: <link>
Repo: https://github.com/bearbinary/omni-infra-provider-truenas

#### r/homelab

**Title**:
Case study: [User]'s [N]-node Kubernetes cluster on a single TrueNAS box — hardware, workloads, lessons

**Body**:

Featuring [user] from the homelab community. Their setup is interesting enough to write up because it's pushing the stack in ways I hadn't planned for.

**Hardware**: [their NAS specs — replace with real]
**Cluster shape**: [their CP/worker layout — replace with real]
**Workloads**: [their actual workloads — replace with real]
**ZFS layout**: [their pool design — replace with real]

What I learned from the interview — their priorities aren't mine, but their reasoning is good:

- [Reasoning point 1]
- [Reasoning point 2]
- [Reasoning point 3]

The full case study includes their advice for someone starting today and the things they'd do differently if they were rebuilding.

Genuine thanks to [user] for being open about their setup — self-hosters are typically private, and this kind of transparency is rare.

Case study: <link>
Provider repo: https://github.com/bearbinary/omni-infra-provider-truenas

### Lemmy

#### `!selfhosted@lemmy.world`

**Title**: Case study: another homelab user's TrueNAS Kubernetes setup — hardware, workloads, what they've learned

**URL**: `https://dev.to/cliftonz/<case-study-slug>?utm_source=lemmy&utm_medium=selfhosted&utm_campaign=case-study-2026-09`

**Body**:
> Published a case study with [user], one of the people running the open-source Omni provider I maintain. They've been on it for [N] months running [their cluster shape].
>
> The interesting parts of their setup that aren't mine:
> - [Real pattern from interview]
> - [Real pattern from interview]
>
> Posting because the patterns generalize past their specific hardware, and because hearing how other people operate a stack you maintain is one of the most useful kinds of feedback you can get.

Cross-post to `!homelab@lemmy.ml`.

---

## Piece 2: Six-month retrospective

Live URL: `https://dev.to/cliftonz/<retro-slug>`

**This piece needs real M6 analytics numbers filled in before publishing.** All `[N]`/`[X]` placeholders below correspond to those metrics. Don't post until they're real.

### Reddit

#### r/selfhosted

**Title**:
6 months of intentionally marketing a niche open-source infra tool — the actual numbers, the honest lessons, what I'd do differently

**Body**:

The retro. Real numbers, no vanity-curation.

**The project**: open-source Omni provider that runs Kubernetes on TrueNAS. Solo-maintained. MIT.

**What 6 months looked like**: [N] long-form written posts, 8 YouTube videos, ~24 LinkedIn posts, [N] cross-posts to Reddit/Lemmy/forum, 1 user case study, [N] podcast pitches, [N] awesome-list submissions.

**Key numbers**: GitHub stars [X→Y], YouTube subs 0→[N], LinkedIn followers [X→Y]. Top organic search query "[query]" — [N] impressions per week.

**What worked (and would do again)**:
1. The hero install post compounded — responsible for [X]% of repo referrals
2. Comparison content beat tutorial content [N]× for conversion
3. Smallest community first (r/truenas before r/selfhosted) — tighter ICP fit, better feedback
4. YouTube as face-recognition compounding, not top-of-funnel
5. Telling people "issues > stars" actually changed user behavior

**What didn't (and would skip)**:
1. X / Twitter — lowest-ROI channel by a wide margin
2. Pitched the wrong podcast first (biggest, not best-fit)
3. Wrote LinkedIn for the wrong audience early
4. Should have started analytics review weekly from day one
5. Case-study outreach should have started in M3, not M5

**The meta-lesson**: niche infra OSS doesn't need a marketing team. It needs five things — canonical answer to the user's query, a face people recognize, honest tradeoffs, a clear story about success, and consistency over time. Most of what worked wasn't clever. It was consistent.

Full retro with the full numbers and the from-scratch playbook: <link>

#### r/golang

**Title**:
Marketing a niche Go open-source project for 6 months: the actual numbers and what compounded

**Body**:

Solo-maintained Go OSS, ~5kLOC, infrastructure tool. 6 months of intentional marketing. Real metrics.

**What compounded**:
- Canonical install guide that ranked for the niche keyword. Single highest-leverage piece of marketing work.
- Code-heavy retrospectives (SAST findings, leader-election pattern). Outperformed tutorials by a wide margin in the Go community specifically.
- Issue-tracker-as-funnel. "Issues over stars" framing pulled in higher-quality contributors than star count would predict.

**What didn't compound**:
- Generic dev-tools marketing channels (Twitter especially). The audience for niche Go infra has migrated elsewhere.
- LinkedIn for "enterprise" framing. The actual LinkedIn audience for solo OSS work is other solo developers, not corporate buyers.

**The lesson for solo Go maintainers**: write the canonical answer for the question your users type into Google, post it on a platform with organic discovery (dev.to has decent SEO), and let it compound. Everything else is multiplier on that base.

Full retro: <link>

#### r/opensource

**Title**:
The marketing playbook that worked for a niche open-source infra project, 6 months in

**Body**:

Open-source maintainer running a solo project for several months. Started intentional marketing 6 months ago. Sharing what worked because most OSS marketing advice is for products with broader audiences than mine.

**Five things that worked for niche OSS**:
1. **The canonical install guide** — the single load-bearing piece of marketing. Ranks for the keyword. Cross-links into every subsequent piece.
2. **Comparison content** — readers comparing things have already decided to do something. Higher conversion than tutorials.
3. **Smallest community first** — post in the tightest-ICP-fit subreddit / Lemmy community before the bigger ones. Free hook-tuning.
4. **YouTube as brand investment, not discovery** — subscriber numbers compound slowly but face-recognition pays off everywhere else.
5. **Tell your audience what success looks like to you** — "issues over stars" changed user behavior. People filed substantive bugs instead of drive-by stars.

**What didn't work**:
- X / Twitter (the audience left)
- Generic dev-tools podcasts (small return for huge prep cost)
- LinkedIn for "enterprise buyer" framing (wrong audience)

**Meta-lesson**: consistency beat cleverness. 6 months of "good enough" beats one viral post almost every time.

Full retro: <link>

### Lemmy

#### `!opensource@lemmy.ml`

**Title**: 6 months of intentionally marketing a solo-maintained OSS infra tool — what worked, what didn't, the actual numbers

**URL**: `https://dev.to/cliftonz/<retro-slug>?utm_source=lemmy&utm_medium=opensource&utm_campaign=retro-2026-10`

**Body**:
> Solo-maintained open-source infra tool. 6 months of intentional marketing. Retrospective with real numbers.
>
> Five things that worked, five that didn't. The meta-lesson: consistency beat cleverness. The work that compounded wasn't any single splashy post — it was patient, sustained writing that compounded over months.
>
> Posting here because the FOSS-aligned audience on Lemmy is exactly who I want this lesson to reach. If you maintain something niche and you've been telling yourself "I should market this someday" — the canonical install guide is the load-bearing first step. Everything else builds on it.

#### `!selfhosted@lemmy.world`

Same body, intro emphasizing "the project is a TrueNAS K8s provider, you might have seen it linked here before."

---

## Cadence

Case study and retro space ~3 weeks apart. The retro is HN-bait (and intentional) — submit to HN the same morning the post drops, separate from Reddit. Don't bundle.
