# Analytics + Tracking Setup

Before any M1 piece ships, wire up enough tracking to make M6's "what worked" review possible. Don't over-build. Five tools, each cheap or free.

---

## 1. Plausible (or PostHog) — site analytics

**What it tracks**: visits, referrers (the part you actually need), basic conversion events.

**Why Plausible over Google Analytics**: privacy-friendly, no cookie banner needed, simpler dashboard, $9/mo for the cheapest plan. PostHog is free up to 1M events/month and gives you product analytics on top.

**Recommendation**: Plausible if you only need website analytics. PostHog if you also want event-level analytics (which click pattern leads to issues opened, etc.).

### Setup (Plausible)

1. Sign up at plausible.io.
2. Add your domain (e.g., `bearbinary.dev` for your docs site, or wherever the canonical landing is).
3. Add the tracking snippet to the docs site `<head>`:
   ```html
   <script defer data-domain="bearbinary.dev" src="https://plausible.io/js/script.js"></script>
   ```
4. Configure goals:
   - Outbound link click to `github.com/bearbinary/omni-infra-provider-truenas`
   - Outbound link click to `omni.siderolabs.com`
5. Set up email reports (weekly summary).

### Setup (PostHog alternative)

1. Sign up at posthog.com.
2. Install snippet on docs site.
3. Define events:
   - `repo_click` (clicks to GitHub repo)
   - `install_click` (clicks to install instructions)
   - `signup_click` (clicks to omni.siderolabs.com)
4. Set up a dashboard tracking weekly totals + referrer breakdown.

### What you'll watch

| Metric | What it tells you |
|---|---|
| Sessions by source (UTM source) | Which channel sent the most traffic |
| Repo clicks by campaign | Which piece converted best to install intent |
| Top landing pages | Which hero/comparison post actually ranked |
| Bounce rate per source | Quality of traffic, not just quantity |

---

## 2. Google Search Console — SEO signal

**What it tracks**: search queries Google shows your content for, impressions, clicks, position. The most important tool for the P1 (searchable) pillar.

### Setup

1. Sign in at search.google.com/search-console.
2. Verify ownership of your docs site (DNS TXT record is cleanest).
3. Submit the docs site sitemap (`/sitemap.xml`).
4. **Wait 48–72 hours** for data to start flowing.
5. (Optional) If dev.to is your canonical: you can't verify dev.to in GSC, but you *can* verify any custom domain you redirect from. Worth doing if you set up `bearbinary.dev/blog/<slug>` → dev.to redirects.

### What you'll watch

| Metric | What it tells you |
|---|---|
| Impressions per query | Which keywords you're ranking for |
| Average position | Are you climbing? |
| CTR | Is the title/meta good enough to earn clicks? |
| Top pages | Which hero/comparison page is doing the SEO work |

**Target for M6 review**: hero post in top 10 for `kubernetes on truenas scale`. Comparison posts in top 20 for `talos vs k3s truenas` and `truenas vs proxmox kubernetes`.

---

## 3. YouTube Studio — channel analytics

**What it tracks**: subs, watch time, traffic sources (suggested videos, search, external), retention curves per video.

### Setup

Already enabled on every YT channel. The setup is just *using it intentionally*.

### What you'll watch weekly

| Metric | What it tells you |
|---|---|
| Subs gained per video | Brand growth |
| Watch time per video | Channel health (YT promotes high-watch-time videos) |
| Avg. % viewed per video | Where viewers drop off — find the dead spots |
| Traffic source breakdown | Which referrer (search / suggested / external) is working |
| Top external traffic source | Dev.to vs LinkedIn vs Reddit vs direct |

**Set up a weekly email digest** from YouTube Studio so you don't have to log in proactively.

### Per-video deep-dive (after each upload)

After every video lands, look at:

- **The retention curve**: where do people drop off? Anything before 30 seconds is a hook problem. A cliff at minute 4 is a pacing problem.
- **CTR on impressions**: under 4% = thumbnail/title isn't selling. Iterate.
- **Subs from this video**: under 10 = audience-fit problem. Iterate the framing.

---

## 4. GitHub Insights — repo signal

**What it tracks**: stars, forks, traffic, clones, referrers.

### Setup

Already enabled. Visit `https://github.com/bearbinary/omni-infra-provider-truenas/graphs/traffic` (must be logged in as repo admin).

### What you'll watch weekly

| Metric | What it tells you |
|---|---|
| Stars (cumulative + delta) | Brand/awareness signal |
| Unique cloners | Real installs / serious eval signal |
| Top referrers | Which platform sent the traffic |
| Top content (pages within repo) | Which README/docs pages people land on |

**Important**: GitHub only keeps 14 days of traffic data. Screenshot weekly or use a tool like `gh-stat` to archive.

---

## 5. Simple tracking spreadsheet

Override the urge to build something fancy. A Google Sheet works.

### Tabs to maintain

**Tab 1: Piece log** (one row per content piece)

| Date published | Piece type | Title | Platform | URL | Pillar | Notes |
|---|---|---|---|---|---|---|
| 2026-05-15 | hero | Kubernetes on TrueNAS SCALE | dev.to | <URL> | P1 | M1 anchor |
| 2026-05-16 | LinkedIn | Channel launch | LinkedIn | <URL> | brand | W1 drumbeat |
| ... | | | | | | |

**Tab 2: Per-piece metrics** (snapshot weekly)

| Piece (campaign) | Week 1 views | Week 2 views | Week 4 views | Week 8 views | Issues opened | Stars delta |
|---|---|---|---|---|---|---|
| hero-2026-05 | | | | | | |
| talos-vs-k3s-2026-06 | | | | | | |
| ... | | | | | | |

**Tab 3: Channel totals** (snapshot monthly)

| Month-end | YT subs | YT total watch hours | GH stars | LinkedIn follows | X follows | Issues opened (cumulative) |
|---|---|---|---|---|---|---|
| 2026-05-31 | | | | | | |
| 2026-06-30 | | | | | | |
| ... | | | | | | |

**That's it.** Three tabs. Update once a week. M3 and M6 reviews look at this sheet first.

---

## Tracking workflow

### Once per piece published

1. Add row to **Piece log** tab.
2. Append UTMs per `utm-conventions.md` to every outbound link.
3. Note the publish time in case you want to look at first-day vs steady-state response.

### Once per week

1. **Monday 9am**: 15-minute check.
   - GitHub traffic page (screenshot).
   - Plausible/PostHog summary email.
   - YT Studio digest email.
   - GSC top queries (one tab, scan, screenshot if anything moved).
   - Update **Per-piece metrics** tab.

### Once per month

1. **End of month**: 30-minute review.
   - Update **Channel totals** tab.
   - Note which piece drove the most repo clicks. Why?
   - Note any unexpected sources (someone tweeted your post, a podcast mentioned you, etc.).
   - Decide next month's tweaks. Don't blow up the plan — just adjust.

### Once at M3 and M6

1. **2-hour review**.
   - Look at the **Per-piece metrics** tab. Rank pieces by repo clicks.
   - Look at the **Channel totals** tab. Rank channels by trajectory.
   - Write a short retrospective (separate doc): what compounded, what surprised me, what's next.
   - Decide whether the next 3 months double down on the same channels or pivot.

---

## Don't over-build

You will be tempted to:

- Set up custom dashboards in PostHog with funnels and cohorts
- Connect GitHub Actions to a Slack channel for stars
- Build a personal site that auto-pulls dev.to stats and YT stats and renders them

**Don't.** Not yet. The marginal value of fancier tracking is approximately zero until you have 6 months of data to analyze. Ship the content. The spreadsheet is fine.

If at M6 you decide tracking is the bottleneck, you can rebuild then. It won't be.
