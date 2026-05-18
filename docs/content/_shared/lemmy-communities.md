# Lemmy Communities + Posting Strategy

Lemmy is the federated Reddit alternative. After the 2023 Reddit API changes, a meaningful slice of r/selfhosted, r/homelab, and r/linux migrated to Lemmy. The audience is smaller than Reddit but more technical, more FOSS-aligned, and more welcoming to project drops.

**Strategic role**: Lemmy is a *complementary* channel, not a replacement for Reddit. The overlap with Reddit is real but not total — people who left Reddit on principle are on Lemmy, and they're exactly your ICP for an MIT-licensed self-hosted infra tool.

---

## How Lemmy works (if you haven't used it)

- Lemmy is **federated** — like email, there are many "instances" (servers) and communities live on specific ones. `c/selfhosted@lemmy.world` is a different community than `c/selfhosted@lemmy.ml`, but they federate (visible from many instances).
- You sign up on **one instance** and can subscribe to / post to communities on any other federated instance from there.
- **Cross-posting** is a first-class feature — use Lemmy's cross-post button, not manual duplicates.
- **No paid promotion**, no algorithm tuning. Discovery is by community subscription, "Active" sort, or trending.
- Markdown is supported.

---

## Pick your home instance

Where you sign up matters mildly — it's your default frontend and federates with the rest. Recommendation for your case:

| Instance | Why for you | Caveat |
|---|---|---|
| **lemmy.world** | Largest general instance, hosts most of the big communities you'd post into | Occasional federation lag |
| **programming.dev** | Dev-focused, hosts the `c/golang` and tech-leaning communities | Smaller, but homier for dev content |
| **sh.itjust.works** | Large, general-purpose, well-administered | Less curated |

**Recommendation**: sign up on **lemmy.world** for the main "Zac Clifton" identity. That's where the audience lives.

---

## Communities to post into (ranked by ICP fit)

### Tier 1 — direct ICP, post first

| Community | Address | Why fit | Size (approx.) |
|---|---|---|---|
| **Selfhosted** | `!selfhosted@lemmy.world` | Exact ICP. Largest self-hosted community on Lemmy. | 70k+ |
| **Homelab** | `!homelab@lemmy.ml` | Active homelab crowd, lots of build-shares | 25k+ |
| **Linux** | `!linux@lemmy.ml` | Broader but high overlap with self-hosters | 100k+ |

### Tier 2 — adjacent, useful for specific pieces

| Community | Address | Use for |
|---|---|---|
| **Kubernetes** | `!kubernetes@lemmy.world` | Comparison + technical posts (M2 Talos vs k3s, M4 upgrade, M6 hub) |
| **Open Source** | `!opensource@lemmy.ml` | Origin story + 6-month retro (M5/M6) |
| **TrueNAS** | search per-instance — small communities exist | All TrueNAS-specific drops |
| **Programming** | `!programming@programming.dev` | SAST retro (M3), singleton-lease pattern (M4) |
| **Golang** | `!golang@programming.dev` | SAST retro (M3), Go-heavy code posts |
| **DevOps** | `!devops@lemmy.world` | Sizing post (M3), upgrade post (M4) |

### Tier 3 — long-shot reach, only if directly relevant

| Community | Address | When to use |
|---|---|---|
| **SysAdmin** | `!sysadmin@lemmy.world` | Only for broadly-applicable infra posts |
| **InfoSec** | `!infosec@infosec.pub` | SAST retro (M3) — security-focused crowd |
| **DataHoarder** | `!datahoarder@lemmy.ml` | Storage-related drops where ZFS or NAS-as-storage is the angle |
| **Privacy** | `!privacy@lemmy.ml` | Tangential — only the self-hosting-as-privacy angle |

---

## How to find more communities + verify names

Lemmy community names and activity drift faster than subreddits. Before posting:

1. Visit your home instance's **Communities** browser (e.g., `lemmy.world/communities`).
2. Sort by **Subscribers** or **Posts/Week** to find active ones in your niche.
3. Check the sidebar rules — some communities forbid linking out, some welcome it explicitly.
4. Look at the most recent posts — if the top post is from 6 months ago, the community is dead. Move on.
5. **Search across instances**: Lemmy Explorer (browse.feddit.de or similar tools) lets you search communities across all federated instances at once.

---

## Posting strategy

### Format

Lemmy posts have:
- **Title** (the whole pitch, like Reddit)
- **URL** (optional — if set, post is "link post" style)
- **Body** (optional — markdown supported)

For long-form drops (hero post, comparison posts), **set the URL field to the dev.to canonical and use the body for context**. This way Lemmy renders the dev.to OG image as a thumbnail and the body adds your personal framing.

### Tone

More technical than Reddit on average. Less performative. The audience appreciates:

- Concrete claims with numbers
- Honest tradeoffs (not "it's amazing!" pitches)
- "I built this, here's what broke" framing
- FOSS / MIT licensing called out explicitly
- Avoiding "we" when you mean "I" — solo maintainers are respected here

Avoid:

- Hype words ("revolutionary," "game-changer," "x10")
- Engagement bait
- Reposting screenshots of HN drama
- Anything that smells like corporate marketing

### Cross-posting within Lemmy

After the first post, use Lemmy's **Cross-Post** button to share to 2–4 additional communities. The cross-post links back to the original post's comment thread, so engagement consolidates.

**Do** cross-post: from `!selfhosted` → `!homelab` → `!opensource`.
**Don't** cross-post: into 10 communities at once. Spam-flag.

### Cadence (integrate with existing plan)

Reddit and Lemmy audiences overlap but aren't identical. Sequence:

| Day from publish | Channel |
|---|---|
| Day 0 | dev.to (canonical) |
| Day 1 | LinkedIn |
| Day 2 | X thread |
| Day 4 | Reddit r/truenas |
| Day 5 | **Lemmy `!selfhosted@lemmy.world`** (first Lemmy drop — tune hook on bigger audience here, then iterate) |
| Day 7 | Reddit r/selfhosted |
| Day 8 | **Lemmy cross-post to `!homelab@lemmy.ml`** |
| Day 10 | Reddit r/homelab |
| Day 11 | **Lemmy cross-post to `!kubernetes@lemmy.world`** |
| Day 14 | Reddit r/kubernetes |
| Day 15 | **Lemmy cross-post to `!opensource@lemmy.ml`** (only if relevant — origin/retro pieces) |

Spacing Reddit and Lemmy 24 hours apart lets you absorb feedback from one before posting to the other.

---

## Worked example — M1 hero post on Lemmy (milestone-framed)

**Framing rule**: same as Reddit (see `_shared/reddit-lemmy-post-patterns.md`). Lead with milestone/lesson/experience. Repo is the resource at the end, not the headline. Lemmy audiences are even less tolerant of product-pitch shapes than Reddit — they self-selected away from corporate-feeling content.

### Post into `!selfhosted@lemmy.world`

**Title**:
> After 6+ months running Kubernetes on a single TrueNAS box — here's the canonical writeup of what works

**URL** (drop the dev.to canonical here so Lemmy renders the OG card):
`https://dev.to/cliftonz/<hero-post-slug>?utm_source=lemmy&utm_medium=selfhosted&utm_campaign=hero-2026-05`

**Body**:
> Six months ago, TrueNAS 25.04 shipped without built-in Kubernetes and my homelab cluster went dark. I didn't want to add a Proxmox box just to host K8s VMs — so I went a different way: Talos Linux directly on TrueNAS, managed by Sidero Omni.
>
> Six months in, here's what I learned the hard way:
>
> 1. **Immutability changes everything.** Talos has no SSH, no shell, no package manager. By month three you stop "fixing" nodes — you replace them. Configuration drift stops being a phrase you use.
>
> 2. **Sizing matters more than people admit.** A 2 GB control plane is fine for a raw cluster. Add Argo CD with many ApplicationSets, Prometheus Operator at full scrape, or Crossplane — the apiserver swaps and the cluster looks intermittently broken. Bump to 4 GB *before* you install those.
>
> 3. **HDD pools and etcd don't get along.** etcd assumes sub-10 ms fsync. HDDs under load see 50–200 ms. Without an NVMe SLOG you'll see intermittent NodeNotReady flaps and waste a weekend debugging.
>
> 4. **ZFS recordsize matters.** Default 128 KiB hurts etcd's 4–16 KiB writes. Set `recordsize=16K` on the dataset hosting your VM zvols.
>
> 5. **The path is genuinely smoother than I expected once you accept "replace, don't fix."**
>
> Wrote it all up — the install steps, the sizing tables, the storage opinions, the gotchas — so the next person doesn't have to learn it the slow way.
>
> The Omni provider that makes this work is MIT-licensed, listed on the TrueNAS apps community catalog, and maintained under an issues-only contribution model. Linked in the canonical above. Happy to answer questions here.

After this post settles for 24 hours, **cross-post** to `!homelab@lemmy.ml`, then to `!kubernetes@lemmy.world`. Adjust the body to each community's interest:

- **`!homelab`** — lead with the hardware specs + setup story (your rack, your workloads), not the lessons. Homelab audiences want to see real builds.
- **`!kubernetes`** — lead with the Omni-provider-pattern angle (SDK gaps, singleton lease, cassettes). Technical audience, technical hook.
- **`!opensource`** — lead with the issues-only contribution model and what it's been like to maintain solo. OSS-maintainer angle.

In every case: lesson/experience first, project as a resource at the end. Never the other way around.

---

## What works specifically on Lemmy (vs Reddit)

Observations from posting tech content on Lemmy over the past year+:

- **Engagement-per-subscriber is higher than Reddit**. Smaller communities, but the people who are there actually comment. Expect more substantive thread discussions.
- **FOSS / MIT / non-corporate framing lands harder**. The audience self-selected away from Reddit partly *because of* Reddit's corporate moves. Lean into that.
- **Long posts do well**. Reddit's TL;DR culture is weaker here. A 500-word body for context on a link post is normal.
- **The audience is more sympathetic to single-maintainer projects**. Mention "I'm the solo maintainer, issues-only contribution model" — that's a feature here, not a red flag.
- **Hacker News-style content (build-in-public, technical retros) gets the best traction**. The SAST retro (M3) and singleton-lease pattern (M4) will outperform the install guide on Lemmy specifically.

## What doesn't work on Lemmy

- Pure marketing posts ("Check out my product!") — instant downvote.
- Posts with affiliate links of any kind — instant ban in most communities.
- Reposting Reddit/HN content without adding original commentary.
- Engagement bait questions.

---

## Integrating Lemmy into the tracking spreadsheet

Add Lemmy as a source in the tracking spreadsheet (`analytics-setup.md`):

| campaign | source | medium | URL | published_at |
|---|---|---|---|---|
| hero-2026-05 | lemmy | selfhosted | <full URL> | 2026-05-20 |
| hero-2026-05 | lemmy | homelab | <full URL> | 2026-05-23 |
| hero-2026-05 | lemmy | kubernetes | <full URL> | 2026-05-26 |

UTM convention (from `utm-conventions.md`):
- `utm_source=lemmy`
- `utm_medium=<community>` (e.g., `selfhosted`, `homelab`, `kubernetes`, `opensource`)
- `utm_campaign=<piece-slug>-<year>-<month>`

---

## Open follow-ups

- Sign up on lemmy.world. Lurk in `!selfhosted@lemmy.world` for a week before posting — see the cadence and tone.
- Build an upvote history with non-promotional engagement (comment thoughtfully on 5–10 other people's posts) before dropping your first link. Lemmy mods notice account-age + comment history.
- After your first Lemmy hero drop, audit: did Lemmy convert better or worse per visitor than Reddit? Per-channel investment in M4+ depends on this.
