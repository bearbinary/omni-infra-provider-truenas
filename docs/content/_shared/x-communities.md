# X (Twitter) Communities + Hashtag Strategy

X has two distinct distribution surfaces for niche tech content: **Communities** (topical subforums you join and post into) and **Hashtags** (organic discovery). Use both, but Communities have higher signal-to-noise once you're in.

**Reality check**: X is the lowest-ROI channel for niche infra content for most people, but the cost of cross-posting from your thread is also near-zero. Treat it as opportunistic reach, not a primary channel.

---

## X Communities to join + post into

Communities are like subreddits — topical, moderated, members-only. To post you must join. Most have rules against pure self-promotion, so deliver value in-thread *then* link.

### Tier 1 — direct ICP fit (post first)

| Community | Why fit | Posting approach |
|---|---|---|
| **Self-Hosted** | Exact ICP overlap with r/selfhosted | Lead with the problem (TrueNAS killed K8s), tease the solution, link |
| **Homelab** | Broader hardware-and-software homelab crowd | Lead with "one box, real cluster" hook |
| **Kubernetes** | Technical depth, larger audience | Lead with the SDK/architecture angle, not install guide |
| **Build in Public** | Indie/OSS maintainers | Lead with origin story or recent shipped feature |

### Tier 2 — adjacent, useful for related drops

| Community | Why fit | Posting approach |
|---|---|---|
| **DevOps** | Crossover audience | Post day-2 ops / observability content here (M3 sizing, M4 upgrades) |
| **Go / Golang** | Code-heavy audience | Post SAST retro (M3) and singleton-lease pattern (M4) here |
| **Open Source** | Maintainer audience | Post 6-month retro (M5) and "what an Omni provider is" (M6) here |
| **Indie Hackers** | Solo-builder audience | Post the marketing/audience-building reflections (M1 W4, M5) here |

### Tier 3 — long-shot reach

| Community | Why fit | Posting approach |
|---|---|---|
| **DevTools** | Tool-builder audience | Only when a specific feature ships |
| **Cloud Native** | CNCF-leaning crowd | Crosspost Longhorn / Talos / Omni stories here |
| **Sysadmin** | Broader audience, lower fit | Skip unless a piece is broadly applicable |

### Verifying community names + finding more

X Communities names change and new ones spin up. Before posting:

1. Search X for "Communities" + your topic (`Kubernetes`, `homelab`, `TrueNAS`, etc.)
2. Check the community's pinned rules — some forbid links entirely, some require value-first
3. Lurk for a few days — see what gets engagement vs ignored
4. Join multiple, post into 2–3 that fit best per piece

**Do not** join 20 communities and cross-post the same thread to all of them. That's spam behavior and X's algorithm will surface less of your future content.

---

## Hashtag Strategy

Hashtags on X are weaker than they used to be — discovery is mostly via algorithm and the For You feed. But specific hashtags still cluster topical engagement for niche audiences.

### Use 2–4 hashtags per post. Pick from these by relevance:

**Tier 1 — always include if relevant**:
- `#Kubernetes` (huge volume, broad)
- `#TrueNAS` (small but exact ICP)
- `#Homelab` (active community, lots of engagement)
- `#SelfHosted` (active, slightly broader than homelab)

**Tier 2 — add when topic-specific**:
- `#Talos` (small, very targeted)
- `#TalosLinux` (smaller alt spelling)
- `#SideroOmni` (almost nobody uses, but ranks you when search is sparse)
- `#Golang` or `#Go` (use `#golang` — `#Go` is too ambiguous with the board game)
- `#OpenSource` (broad, decent volume)
- `#DevOps` (very broad)
- `#CloudNative` (CNCF-leaning audience)

**Tier 3 — community-cadence hashtags**:
- `#BuildInPublic` (Mondays especially)
- `#100DaysOfHomelab` (long-running community tag)
- `#ShipIt` / `#WeekendProject` (timing-dependent)

### Hashtags by piece type

| Piece type | Recommended hashtags |
|---|---|
| Hero post / install guide | `#Kubernetes #TrueNAS #Homelab #SelfHosted` |
| Comparison post | `#Kubernetes #TrueNAS #Talos #SelfHosted` |
| Build-in-public devlog | `#BuildInPublic #OpenSource #golang #Kubernetes` |
| Storage deep-dive | `#Kubernetes #TrueNAS #Longhorn #Homelab` |
| Sizing / production post | `#Kubernetes #DevOps #Homelab #SelfHosted` |
| Networking / multi-host | `#Kubernetes #TrueNAS #Homelab #MetalLB` |
| Security / SAST retro | `#golang #Security #OpenSource #DevSecOps` |
| Sidero / Talos community drops | `#Talos #Kubernetes #SideroOmni #SelfHosted` |

---

## Accounts to tag (sparingly)

Tagging puts your post in the tagged account's notifications. Done well = amplification. Done lazily = blocked.

**Only tag when the post is genuinely relevant to that account.** Never tag for reach alone.

| Account | When to tag | Why |
|---|---|---|
| `@SideroLabs` | Anything about Talos or Omni that's substantive | They actively boost provider authors. Don't tag every post. |
| `@TrueNAS` (and `@iXsystems`) | TrueNAS-specific posts where you're saying useful things about the platform | They retweet community content occasionally |
| `@longhorn_io` | When you're showcasing Longhorn working well | They share community use cases |
| `@kubernetesio` | Almost never — too noisy on their end | Skip |
| Individual maintainers (e.g., `@andrewrynhard`, `@AndrewSav` if active) | If you're discussing their specific work | Direct attribution, not bait |

**Rule**: tag at most 1–2 accounts per post. Tagging 5+ accounts is a signal you're farming reach, and accounts learn to mute you fast.

---

## X-specific posting tactics

### What works for niche infra content

- **Threads outperform single tweets** for substantive content. 6–10 tweet threads land best.
- **Code screenshots > raw code** in tweets. Carbon.now.sh or polacode-style screenshots get more engagement than `\`\`\`` blocks.
- **Diagrams as images**. Render Mermaid to PNG. X compresses them, so use high-contrast colors.
- **Quote-tweets of your own threads** with new context days later get a second wave of engagement.
- **Replying to relevant threads** (search "Kubernetes on TrueNAS", "Talos VM", etc.) with substantive answers builds inbound follows faster than original posting.

### What doesn't work

- Single-tweet "blog post is up: <link>". Zero engagement.
- Posting at 2am ET. Tech audience is mostly US working hours.
- Posting weekends. Reach drops ~60%.
- Engagement-bait questions ("What's your favorite K8s distro?"). The algorithm has gotten wise.
- Cross-posting your LinkedIn copy word-for-word. X audience tone is more direct, less polished.

### Optimal X cadence for this campaign

| Frequency | What |
|---|---|
| 1× per major piece (M1, M2, M3, etc.) | Full thread with the canonical hook |
| 2–3× per month | Single-tweet observations / takes that don't need a full thread |
| 5–10× per month | Replies to other people's threads in your niche |
| 1× per month | Quote-tweet your own best thread with new framing |

---

## X for the M1 hero post — specific plan

From `01-month-1/02-cross-posts-reddit-linkedin-x.md`, the 10-tweet thread is drafted. Distribution into X Communities:

1. **Post the thread** from your main feed first.
2. **Wait 6 hours** — let it accumulate organic engagement.
3. **Re-share into Communities** by posting the first tweet *as a new post* inside each Community, with a link to the thread. (X doesn't let you cross-post threads directly into Communities, so this is the workaround.)
4. Communities to hit, in order: **Self-Hosted → Homelab → Kubernetes → Build in Public**.
5. Stagger by 24 hours between Communities to avoid spam flags.

---

## Open follow-ups

- Audit which Communities actually exist on X today — names rotate. Lurk in 5–7 candidates for a week before joining/posting.
- Once you've posted 2–3 threads, look at which hashtag combinations got engagement. Double down on the ones that worked.
- Consider X Premium (subscriber) for analytics + algorithmic boost — if you're going to invest in this channel, the ~$10/mo is worth it for niche audiences.
